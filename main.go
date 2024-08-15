// Copyright (c) 2021-2024 Nordix Foundation.
//
// Copyright (c) 2023-2024 Cisco Foundation.
//
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at:
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build linux
// +build linux

package main

import (
	"context"
	"crypto/tls"
	"net"
	"net/url"
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"
	"time"

	nested "github.com/antonfisher/nested-logrus-formatter"
	"github.com/edwarnicke/debug"
	"github.com/edwarnicke/genericsync"
	"github.com/edwarnicke/grpcfd"
	"github.com/kelseyhightower/envconfig"
	registryapi "github.com/networkservicemesh/api/pkg/api/registry"
	k8sdeviceplugin "github.com/networkservicemesh/sdk-k8s/pkg/tools/deviceplugin"
	k8spodresources "github.com/networkservicemesh/sdk-k8s/pkg/tools/podresources"
	"github.com/networkservicemesh/sdk-ovs/pkg/networkservice/chains/forwarder"
	"github.com/networkservicemesh/sdk-ovs/pkg/networkservice/mechanisms/vxlan"
	ovsutil "github.com/networkservicemesh/sdk-ovs/pkg/tools/utils"
	sriovconfig "github.com/networkservicemesh/sdk-sriov/pkg/sriov/config"
	"github.com/networkservicemesh/sdk-sriov/pkg/sriov/pci"
	"github.com/networkservicemesh/sdk-sriov/pkg/sriov/resource"
	sriovtoken "github.com/networkservicemesh/sdk-sriov/pkg/sriov/token"
	"github.com/networkservicemesh/sdk/pkg/networkservice/chains/endpoint"
	"github.com/networkservicemesh/sdk/pkg/networkservice/common/authorize"
	registryclient "github.com/networkservicemesh/sdk/pkg/registry/chains/client"
	registryauthorize "github.com/networkservicemesh/sdk/pkg/registry/common/authorize"
	"github.com/networkservicemesh/sdk/pkg/registry/common/sendfd"
	"github.com/networkservicemesh/sdk/pkg/tools/grpcutils"
	"github.com/networkservicemesh/sdk/pkg/tools/log"
	"github.com/networkservicemesh/sdk/pkg/tools/log/logruslogger"
	monitorauthorize "github.com/networkservicemesh/sdk/pkg/tools/monitorconnection/authorize"
	"github.com/networkservicemesh/sdk/pkg/tools/opentelemetry"
	"github.com/networkservicemesh/sdk/pkg/tools/pprofutils"
	"github.com/networkservicemesh/sdk/pkg/tools/spiffejwt"
	"github.com/networkservicemesh/sdk/pkg/tools/token"
	"github.com/networkservicemesh/sdk/pkg/tools/tracing"
	"github.com/ovn-org/ovn-kubernetes/go-controller/pkg/util"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/networkservicemesh/cmd-forwarder-ovs/internal/l2resourcecfg"
	"github.com/networkservicemesh/cmd-forwarder-ovs/internal/ovsinit"
)

// Config - configuration for cmd-forwarder-ovs
type Config struct {
	Name                   string            `default:"forwarder" desc:"Name of Endpoint"`
	Labels                 map[string]string `default:"p2p:true" desc:"Labels related to this forwarder-vpp instance"`
	NSName                 string            `default:"forwarder" desc:"Name of Network Service to Register with Registry"`
	BridgeName             string            `default:"br-nsm" desc:"Name of the OvS bridge" split_words:"true"`
	TunnelIP               string            `desc:"IP or CIDR to use for tunnels" split_words:"true"`
	VxlanPort              uint16            `default:"4789" desc:"VXLAN port to use" split_words:"true"`
	ConnectTo              url.URL           `default:"unix:///connect.to.socket" desc:"url to connect to" split_words:"true"`
	DialTimeout            time.Duration     `default:"50ms" desc:"Timeout for the dial the next endpoint" split_words:"true"`
	MaxTokenLifetime       time.Duration     `default:"24h" desc:"maximum lifetime of tokens" split_words:"true"`
	RegistryClientPolicies []string          `default:"etc/nsm/opa/common/.*.rego,etc/nsm/opa/registry/.*.rego,etc/nsm/opa/client/.*.rego" desc:"paths to files and directories that contain registry client policies" split_words:"true"`
	ResourcePollTimeout    time.Duration     `default:"30s" desc:"device plugin polling timeout" split_words:"true"`
	DevicePluginPath       string            `default:"/var/lib/kubelet/device-plugins/" desc:"path to the device plugin directory" split_words:"true"`
	PodResourcesPath       string            `default:"/var/lib/kubelet/pod-resources/" desc:"path to the pod resources directory" split_words:"true"`
	SRIOVConfigFile        string            `default:"pci.config" desc:"PCI resources config path" split_words:"true"`
	L2ResourceSelectorFile string            `default:"" desc:"config file for resource to label matching" split_words:"true"`
	PCIDevicesPath         string            `default:"/sys/bus/pci/devices" desc:"path to the PCI devices directory" split_words:"true"`
	PCIDriversPath         string            `default:"/sys/bus/pci/drivers" desc:"path to the PCI drivers directory" split_words:"true"`
	CgroupPath             string            `default:"/host/sys/fs/cgroup/devices" desc:"path to the host cgroup directory" split_words:"true"`
	VFIOPath               string            `default:"/host/dev/vfio" desc:"path to the host VFIO directory" split_words:"true"`
	LogLevel               string            `default:"INFO" desc:"Log level" split_words:"true"`
	OpenTelemetryEndpoint  string            `default:"otel-collector.observability.svc.cluster.local:4317" desc:"OpenTelemetry Collector Endpoint" split_words:"true"`
	MetricsExportInterval  time.Duration     `default:"10s" desc:"interval between mertics exports" split_words:"true"`
	PprofEnabled           bool              `default:"false" desc:"is pprof enabled" split_words:"true"`
	PprofListenOn          string            `default:"localhost:6060" desc:"pprof URL to ListenAndServe" split_words:"true"`
}

// supervisor starting ovsdb-server and ovs-vswitchd,
// each with 5 seconds starting timeout and 3 retries
const startOvsTimeout = 30

func main() {
	// ********************************************************************************
	// setup context to catch signals
	// ********************************************************************************
	ctx, cancel := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		// More Linux signals here
		syscall.SIGHUP,
		syscall.SIGTERM,
		syscall.SIGQUIT,
	)
	defer cancel()

	setupLogger(ctx)

	starttime := time.Now()

	logPhases(ctx)

	// ********************************************************************************
	log.FromContext(ctx).Infof("executing phase 1: get config from environment (time since start: %s)", time.Since(starttime))
	// ********************************************************************************
	now := time.Now()
	config := &Config{}
	if err := envconfig.Usage("nsm", config); err != nil {
		logrus.Fatal(err)
	}
	if err := envconfig.Process("nsm", config); err != nil {
		logrus.Fatalf("error processing config from env: %+v", err)
	}

	setLogLevel(config.LogLevel)

	log.FromContext(ctx).Infof("Config: %#v", config)
	log.FromContext(ctx).WithField("duration", time.Since(now)).Infof("completed phase 1: get config from environment")

	// ********************************************************************************
	// Configure Open Telemetry
	// ********************************************************************************
	if opentelemetry.IsEnabled() {
		collectorAddress := config.OpenTelemetryEndpoint
		spanExporter := opentelemetry.InitSpanExporter(ctx, collectorAddress)
		metricExporter := opentelemetry.InitOPTLMetricExporter(ctx, collectorAddress, config.MetricsExportInterval)
		o := opentelemetry.Init(ctx, spanExporter, metricExporter, "forwarder-ovs")
		defer func() {
			if err := o.Close(); err != nil {
				log.FromContext(ctx).Fatal(err)
			}
		}()
	}

	// ********************************************************************************
	// Configure pprof
	// ********************************************************************************
	if config.PprofEnabled {
		go pprofutils.ListenAndServe(ctx, config.PprofListenOn)
	}

	// ********************************************************************************
	log.FromContext(ctx).Infof("executing phase 2: ensure ovs is running (time since start: %s)", time.Since(starttime))
	// ********************************************************************************
	now = time.Now()
	if !ovsinit.IsOvsRunning() {
		// start ovs by supervisord
		ovsErrCh := ovsinit.StartSupervisord(ctx)
		exitOnErrCh(ctx, cancel, ovsErrCh)
		if err := ovsinit.WaitForOvs(ctx, startOvsTimeout); err != nil {
			log.FromContext(ctx).Fatal(err)
		}
		log.FromContext(ctx).Info("local ovs is being used")
	} else {
		log.FromContext(ctx).Info("host ovs is being used")
	}
	log.FromContext(ctx).WithField("duration", time.Since(now)).Info("completed phase 2: ensure ovs is running")

	// ********************************************************************************
	log.FromContext(ctx).Infof("executing phase 3: retrieving svid, check spire agent logs if this is the last line you see (time since start: %s)", time.Since(starttime))
	// ********************************************************************************
	now = time.Now()
	source, err := workloadapi.NewX509Source(ctx)
	if err != nil {
		logrus.Fatalf("error getting x509 source: %+v", err)
	}
	svid, err := source.GetX509SVID()
	if err != nil {
		logrus.Fatalf("error getting x509 svid: %+v", err)
	}
	logrus.Infof("SVID: %q", svid.ID)
	log.FromContext(ctx).WithField("duration", time.Since(now)).Info("completed phase 3: retrieving svid")

	tlsClientConfig := tlsconfig.MTLSClientConfig(source, source, tlsconfig.AuthorizeAny())
	tlsClientConfig.MinVersion = tls.VersionTLS12
	tlsServerConfig := tlsconfig.MTLSServerConfig(source, source, tlsconfig.AuthorizeAny())
	tlsServerConfig.MinVersion = tls.VersionTLS12

	// ********************************************************************************
	log.FromContext(ctx).Infof("executing phase 4: create ovsxconnect network service endpoint (time since start: %s)", time.Since(starttime))
	// ********************************************************************************
	xConnectEndpoint, err := createInterposeEndpoint(ctx, config, tlsClientConfig, source)
	if err != nil {
		logrus.Fatalf("error configuring forwarder endpoint: %+v", err)
	}
	defer func() {
		stdout, stderr, cmdErr := util.RunOVSVsctl("del-br", config.BridgeName)
		if err != nil {
			log.FromContext(ctx).Fatalf("Failed to remove bridge %s, stdout: %q, stderr: %q, error: %v", config.BridgeName, stdout, stderr, cmdErr)
		}
		log.FromContext(ctx).Debugf("Bridge %s removed", config.BridgeName)
	}()
	log.FromContext(ctx).WithField("duration", time.Since(now)).Info("completed phase 4: create ovsxconnect network service endpoint")

	// ********************************************************************************
	log.FromContext(ctx).Infof("executing phase 5: create grpc server and register ovsxconnect (time since start: %s)", time.Since(starttime))
	// ********************************************************************************
	tmpDir, err := os.MkdirTemp("", "cmd-forwarder-ovs")
	if err != nil {
		log.FromContext(ctx).Fatalf("error creating tmpDir: %+v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()
	listenOn := &url.URL{Scheme: "unix", Path: path.Join(tmpDir, "listen_on.io.sock")}

	server := registerGRPCServer(tlsServerConfig, xConnectEndpoint)
	srvErrCh := grpcutils.ListenAndServe(ctx, listenOn, server)
	exitOnErrCh(ctx, cancel, srvErrCh)
	log.FromContext(ctx).WithField("duration", time.Since(now)).Info("completed phase 5: create grpc server and register ovsxconnect")

	// ********************************************************************************
	log.FromContext(ctx).Infof("executing phase 6: register %s with the registry (time since start: %s)", config.NSName, time.Since(starttime))
	// ********************************************************************************
	err = registerEndpoint(ctx, config, source, tlsClientConfig, listenOn)
	if err != nil {
		log.FromContext(ctx).Fatalf("failed to connect to registry: %+v", err)
	}
	log.FromContext(ctx).WithField("duration", time.Since(now)).Infof("completed phase 6: register %s with the registry", config.NSName)

	log.FromContext(ctx).Infof("Startup completed in %v", time.Since(starttime))

	<-ctx.Done()
	<-srvErrCh
}

func setupLogger(ctx context.Context) {
	// ********************************************************************************
	// setup logging
	// ********************************************************************************
	log.EnableTracing(true)
	logrus.SetFormatter(&nested.Formatter{})
	ctx = log.WithLog(ctx, logruslogger.New(ctx, map[string]interface{}{"cmd": os.Args[0]}))
	ctx = log.WithLog(ctx, logruslogger.New(ctx))

	// ********************************************************************************
	// Debug self if necessary
	// ********************************************************************************
	if err := debug.Self(); err != nil {
		log.FromContext(ctx).Infof("%s", err)
	}
}

func logPhases(ctx context.Context) {
	// enumerating phases
	log.FromContext(ctx).Infof("there are 5 phases which will be executed followed by a success message:")
	log.FromContext(ctx).Infof("the phases include:")
	log.FromContext(ctx).Infof("1: get config from environment")
	log.FromContext(ctx).Infof("2: ensure ovs is running")
	log.FromContext(ctx).Infof("3: retrieve spiffe svid")
	log.FromContext(ctx).Infof("4: create ovs forwarder network service endpoint")
	log.FromContext(ctx).Infof("5: create grpc server and register ovsxconnect")
	log.FromContext(ctx).Infof("6: register ovs forwarder network service with the registry")
	log.FromContext(ctx).Infof("a final success message with start time duration")
}

func getL2ConnectionPointMap(ctx context.Context, cfg *Config) map[string]*ovsutil.L2ConnectionPoint {
	if cfg.L2ResourceSelectorFile == "" {
		return nil
	}
	resource2LabSel, err := l2resourcecfg.ReadConfig(ctx, cfg.L2ResourceSelectorFile)
	if err != nil {
		log.FromContext(ctx).Fatalf("failed to get device selector configuration file: %+v", err)
	}
	if len(resource2LabSel.Interfaces) == 0 && len(resource2LabSel.Bridges) == 0 {
		log.FromContext(ctx).Warn("skipping matching labels to device names: empty interface and bridge list")
		return nil
	}
	l2C := make(map[string]*ovsutil.L2ConnectionPoint)
	for _, device := range resource2LabSel.Interfaces {
		egressPoint := &ovsutil.L2ConnectionPoint{}
		egressPoint.Bridge = device.Bridge
		for i := range device.Matches {
			for j := range device.Matches[i].LabelSelector {
				egressPoint.Interface = device.Name
				l2C[device.Matches[i].LabelSelector[j].Via] = egressPoint
			}
		}
	}
	for _, bridge := range resource2LabSel.Bridges {
		egressPoint := &ovsutil.L2ConnectionPoint{}
		egressPoint.Bridge = bridge.Name
		for i := range bridge.Matches {
			for j := range bridge.Matches[i].LabelSelector {
				l2C[bridge.Matches[i].LabelSelector[j].Via] = egressPoint
			}
		}
	}
	return l2C
}

func parseTunnelIPCIDR(tunnelIPStr string) (net.IP, error) {
	var egressTunnelIP net.IP
	var err error
	if strings.Contains(tunnelIPStr, "/") {
		egressTunnelIP, _, err = net.ParseCIDR(tunnelIPStr)
	} else {
		egressTunnelIP = net.ParseIP(tunnelIPStr)
		if egressTunnelIP == nil {
			err = errors.New("tunnel IP must be set to a valid IP")
		}
	}
	return egressTunnelIP, err
}

func createInterposeEndpoint(ctx context.Context, config *Config, tlsClientConfig *tls.Config, source x509svid.Source) (xConnectEndpoint endpoint.Endpoint, err error) {
	egressTunnelIP, err := parseTunnelIPCIDR(config.TunnelIP)
	if err != nil {
		return
	}
	l2cMap := getL2ConnectionPointMap(ctx, config)
	if isSriovConfig(config.SRIOVConfigFile) {
		xConnectEndpoint, err = createSriovInterposeEndpoint(ctx, config, tlsClientConfig, source, egressTunnelIP, l2cMap)
	} else {
		xConnectEndpoint, err = createKernelInterposeEndpoint(ctx, config, tlsClientConfig, source, egressTunnelIP, l2cMap)
	}
	return
}

func createKernelInterposeEndpoint(ctx context.Context, config *Config, tlsConfig *tls.Config, source x509svid.Source,
	egressTunnelIP net.IP, l2cMap map[string]*ovsutil.L2ConnectionPoint,
) (endpoint.Endpoint, error) {
	var spiffeidmap genericsync.Map[spiffeid.ID, *genericsync.Map[string, struct{}]]

	dialOptions := append(
		tracing.WithTracingDial(),
		grpc.WithBlock(),
		grpc.WithTransportCredentials(
			grpcfd.TransportCredentials(credentials.NewTLS(tlsConfig))),
		grpc.WithDefaultCallOptions(
			grpc.WaitForReady(true),
			grpc.PerRPCCredentials(token.NewPerRPCCredentials(spiffejwt.TokenGeneratorFunc(source, config.MaxTokenLifetime))),
		),
		grpcfd.WithChainStreamInterceptor(),
		grpcfd.WithChainUnaryInterceptor())
	return forwarder.NewKernelServer(
		ctx,
		spiffejwt.TokenGeneratorFunc(source, config.MaxTokenLifetime),
		egressTunnelIP,
		l2cMap,
		forwarder.WithName(config.Name),
		forwarder.WithBridgeName(config.BridgeName),
		forwarder.WithAuthorizeServer(authorize.NewServer(authorize.WithSpiffeIDConnectionMap(&spiffeidmap))),
		forwarder.WithAuthorizeMonitorConnectionServer(monitorauthorize.NewMonitorConnectionServer(monitorauthorize.WithSpiffeIDConnectionMap(&spiffeidmap))),
		forwarder.WithClientURL(&config.ConnectTo),
		forwarder.WithDialTimeout(config.DialTimeout),
		forwarder.WithVxlanOptions(vxlan.WithPort(config.VxlanPort)),
		forwarder.WithDialOptions(dialOptions...),
	)
}

func createSriovInterposeEndpoint(ctx context.Context, config *Config, tlsConfig *tls.Config, source x509svid.Source,
	egressTunnelIP net.IP, l2cMap map[string]*ovsutil.L2ConnectionPoint,
) (endpoint.Endpoint, error) {
	sriovConfig, err := sriovconfig.ReadConfig(ctx, config.SRIOVConfigFile)
	if err != nil {
		return nil, err
	}

	if err = pci.UpdateConfig(config.PCIDevicesPath, config.PCIDriversPath, sriovConfig); err != nil {
		return nil, err
	}

	tokenPool := sriovtoken.NewPool(sriovConfig)
	// create pci pool with skip checking bound driver on VF because it is no more valid for VLAN trunking
	// when handling multiple ns clients over a single VF on the endpoint side.
	pciPool, err := pci.NewPCIPool(config.PCIDevicesPath, config.PCIDriversPath, config.VFIOPath, sriovConfig, true)
	if err != nil {
		return nil, err
	}

	resourcePool := resource.NewPool(tokenPool, sriovConfig)

	// Start device plugin server
	if err = k8sdeviceplugin.StartServers(
		ctx,
		tokenPool,
		config.ResourcePollTimeout,
		k8sdeviceplugin.NewClient(config.DevicePluginPath),
		k8spodresources.NewClient(config.PodResourcesPath),
	); err != nil {
		return nil, err
	}

	var spiffeidmap genericsync.Map[spiffeid.ID, *genericsync.Map[string, struct{}]]

	dialOptions := append(
		tracing.WithTracingDial(),
		grpc.WithBlock(),
		grpc.WithTransportCredentials(
			grpcfd.TransportCredentials(credentials.NewTLS(tlsConfig))),
		grpc.WithDefaultCallOptions(
			grpc.WaitForReady(true),
			grpc.PerRPCCredentials(token.NewPerRPCCredentials(spiffejwt.TokenGeneratorFunc(source, config.MaxTokenLifetime))),
		),
		grpcfd.WithChainStreamInterceptor(),
		grpcfd.WithChainUnaryInterceptor())

	return forwarder.NewSriovServer(
		ctx,
		spiffejwt.TokenGeneratorFunc(source, config.MaxTokenLifetime),
		egressTunnelIP,
		pciPool,
		resourcePool,
		sriovConfig,
		l2cMap,
		forwarder.WithName(config.Name),
		forwarder.WithBridgeName(config.BridgeName),
		forwarder.WithAuthorizeServer(authorize.NewServer(authorize.WithSpiffeIDConnectionMap(&spiffeidmap))),
		forwarder.WithAuthorizeMonitorConnectionServer(monitorauthorize.NewMonitorConnectionServer(monitorauthorize.WithSpiffeIDConnectionMap(&spiffeidmap))),
		forwarder.WithClientURL(&config.ConnectTo),
		forwarder.WithDialTimeout(config.DialTimeout),
		forwarder.WithVxlanOptions(vxlan.WithPort(config.VxlanPort)),
		forwarder.WithDialOptions(dialOptions...),
	)
}

func exitOnErrCh(ctx context.Context, cancel context.CancelFunc, errCh <-chan error) {
	// If we already have an error, log it and exit
	select {
	case err := <-errCh:
		log.FromContext(ctx).Fatal(err)
	default:
	}
	// Otherwise wait for an error in the background to log and cancel
	go func(ctx context.Context, errCh <-chan error) {
		err := <-errCh
		log.FromContext(ctx).Error(err)
		cancel()
	}(ctx, errCh)
}

func isSriovConfig(confFile string) bool {
	sriovConfig, err := os.Stat(confFile)
	if os.IsNotExist(err) {
		return false
	}
	return !sriovConfig.IsDir()
}

func registerGRPCServer(tlsServerConfig *tls.Config, xConnectEndpoint endpoint.Endpoint) *grpc.Server {
	server := grpc.NewServer(append(
		tracing.WithTracing(),
		grpc.Creds(
			grpcfd.TransportCredentials(credentials.NewTLS(tlsServerConfig)),
		),
	)...)
	xConnectEndpoint.Register(server)
	return server
}

func registerEndpoint(ctx context.Context, cfg *Config, source *workloadapi.X509Source, tlsClientConfig *tls.Config, listenOn *url.URL) error {
	clientOptions := append(
		tracing.WithTracingDial(),
		grpc.WithBlock(),
		grpc.WithDefaultCallOptions(
			grpc.WaitForReady(true),
			grpc.PerRPCCredentials(token.NewPerRPCCredentials(spiffejwt.TokenGeneratorFunc(source, cfg.MaxTokenLifetime)))),
		grpc.WithTransportCredentials(
			grpcfd.TransportCredentials(credentials.NewTLS(tlsClientConfig))),
		grpcfd.WithChainStreamInterceptor(),
		grpcfd.WithChainUnaryInterceptor(),
	)

	registryClient := registryclient.NewNetworkServiceEndpointRegistryClient(ctx, registryclient.WithClientURL(&cfg.ConnectTo),
		registryclient.WithDialOptions(clientOptions...),
		registryclient.WithNSEAdditionalFunctionality(
			sendfd.NewNetworkServiceEndpointRegistryClient(),
		),
		registryclient.WithAuthorizeNSERegistryClient(registryauthorize.NewNetworkServiceEndpointRegistryClient(
			registryauthorize.WithPolicies(cfg.RegistryClientPolicies...))),
	)
	_, err := registryClient.Register(ctx, &registryapi.NetworkServiceEndpoint{
		Name: cfg.Name,
		NetworkServiceLabels: map[string]*registryapi.NetworkServiceLabels{
			cfg.NSName: {
				Labels: cfg.Labels,
			},
		},
		NetworkServiceNames: []string{cfg.NSName},
		Url:                 grpcutils.URLToTarget(listenOn),
	})
	if err != nil {
		log.FromContext(ctx).Fatalf("failed to connect to registry: %+v", err)
	}

	return err
}

func setLogLevel(level string) {
	l, err := logrus.ParseLevel(level)
	if err != nil {
		logrus.Fatalf("invalid log level %s", level)
	}
	logrus.SetLevel(l)
}
