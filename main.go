// Copyright (c) 2021 Nordix Foundation.
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

//+build linux

// package main contains ovs forwarder implmentation
package main

import (
	"context"
	"io/ioutil"
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
	"github.com/edwarnicke/grpcfd"
	"github.com/kelseyhightower/envconfig"
	registryapi "github.com/networkservicemesh/api/pkg/api/registry"
	k8sdeviceplugin "github.com/networkservicemesh/sdk-k8s/pkg/tools/deviceplugin"
	k8spodresources "github.com/networkservicemesh/sdk-k8s/pkg/tools/podresources"
	"github.com/pkg/errors"

	"github.com/networkservicemesh/cmd-forwarder-ovs/internal/l2resourcecfg"

	"github.com/networkservicemesh/sdk-ovs/pkg/networkservice/chains/forwarder"
	ovsutil "github.com/networkservicemesh/sdk-ovs/pkg/tools/utils"
	sriovconfig "github.com/networkservicemesh/sdk-sriov/pkg/sriov/config"
	"github.com/networkservicemesh/sdk-sriov/pkg/sriov/pci"
	"github.com/networkservicemesh/sdk-sriov/pkg/sriov/resource"
	sriovtoken "github.com/networkservicemesh/sdk-sriov/pkg/sriov/token"
	"github.com/networkservicemesh/sdk/pkg/networkservice/chains/endpoint"
	"github.com/networkservicemesh/sdk/pkg/networkservice/common/authorize"
	registryclient "github.com/networkservicemesh/sdk/pkg/registry/chains/client"
	"github.com/networkservicemesh/sdk/pkg/tools/grpcutils"
	"github.com/networkservicemesh/sdk/pkg/tools/jaeger"
	"github.com/networkservicemesh/sdk/pkg/tools/log"
	"github.com/networkservicemesh/sdk/pkg/tools/log/logruslogger"
	"github.com/networkservicemesh/sdk/pkg/tools/opentracing"
	"github.com/networkservicemesh/sdk/pkg/tools/spiffejwt"
	"github.com/networkservicemesh/sdk/pkg/tools/token"
	"github.com/sirupsen/logrus"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// Config - configuration for cmd-forwarder-ovs
type Config struct {
	Name                   string        `default:"forwarder" desc:"Name of Endpoint"`
	NSName                 string        `default:"forwarder" desc:"Name of Network Service to Register with Registry"`
	BridgeName             string        `default:"br-nsm" desc:"Name of the OvS bridge"`
	TunnelIP               string        `desc:"IP or CIDR to use for tunnels" split_words:"true"`
	ConnectTo              url.URL       `default:"unix:///connect.to.socket" desc:"url to connect to" split_words:"true"`
	DialTimeout            time.Duration `default:"50ms" desc:"Timeout for the dial the next endpoint" split_words:"true"`
	MaxTokenLifetime       time.Duration `default:"24h" desc:"maximum lifetime of tokens" split_words:"true"`
	ResourcePollTimeout    time.Duration `default:"30s" desc:"device plugin polling timeout" split_words:"true"`
	DevicePluginPath       string        `default:"/var/lib/kubelet/device-plugins/" desc:"path to the device plugin directory" split_words:"true"`
	PodResourcesPath       string        `default:"/var/lib/kubelet/pod-resources/" desc:"path to the pod resources directory" split_words:"true"`
	SRIOVConfigFile        string        `default:"pci.config" desc:"PCI resources config path" split_words:"true"`
	L2ResourceSelectorFile string        `default:"" desc:"config file for resource to label matching" split_words:"true"`
	PCIDevicesPath         string        `default:"/sys/bus/pci/devices" desc:"path to the PCI devices directory" split_words:"true"`
	PCIDriversPath         string        `default:"/sys/bus/pci/drivers" desc:"path to the PCI drivers directory" split_words:"true"`
	CgroupPath             string        `default:"/host/sys/fs/cgroup/devices" desc:"path to the host cgroup directory" split_words:"true"`
	VFIOPath               string        `default:"/host/dev/vfio" desc:"path to the host VFIO directory" split_words:"true"`
	LogLevel               string        `default:"INFO" desc:"Log level" split_words:"true"`
}

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
	log.FromContext(ctx).Infof("executing phase 2: retrieving svid, check spire agent logs if this is the last line you see (time since start: %s)", time.Since(starttime))
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
	log.FromContext(ctx).WithField("duration", time.Since(now)).Info("completed phase 2: retrieving svid")

	// ********************************************************************************
	log.FromContext(ctx).Infof("executing phase 3: create ovsxconnect network service endpoint (time since start: %s)", time.Since(starttime))
	// ********************************************************************************
	xConnectEndpoint, err := createInterposeEndpoint(ctx, config, source)
	if err != nil {
		logrus.Fatalf("error configuring forwarder endpoint: %+v", err)
	}
	log.FromContext(ctx).WithField("duration", time.Since(now)).Info("completed phase 3: create ovsxconnect network service endpoint")

	// ********************************************************************************
	log.FromContext(ctx).Infof("executing phase 4: create grpc server and register ovsxconnect (time since start: %s)", time.Since(starttime))
	// ********************************************************************************
	tmpDir, err := ioutil.TempDir("", "cmd-forwarder-ovs")
	if err != nil {
		log.FromContext(ctx).Fatalf("error creating tmpDir: %+v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()
	listenOn := &url.URL{Scheme: "unix", Path: path.Join(tmpDir, "listen_on.io.sock")}

	server := registerGRPCServer(source, xConnectEndpoint)
	srvErrCh := grpcutils.ListenAndServe(ctx, listenOn, server)
	exitOnErrCh(ctx, cancel, srvErrCh)
	log.FromContext(ctx).WithField("duration", time.Since(now)).Info("completed phase 4: create grpc server and register ovsxconnect")

	// ********************************************************************************
	log.FromContext(ctx).Infof("executing phase 5: register %s with the registry (time since start: %s)", config.NSName, time.Since(starttime))
	// ********************************************************************************
	err = registerEndpoint(ctx, config, source, listenOn)
	if err != nil {
		log.FromContext(ctx).Fatalf("failed to connect to registry: %+v", err)
	}
	log.FromContext(ctx).WithField("duration", time.Since(now)).Infof("completed phase 5: register %s with the registry", config.NSName)

	log.FromContext(ctx).Infof("Startup completed in %v", time.Since(starttime))

	<-ctx.Done()
	<-srvErrCh
}

func setupLogger(ctx context.Context) {
	// ********************************************************************************
	// setup logging
	// ********************************************************************************
	logrus.SetFormatter(&nested.Formatter{})
	ctx = log.WithFields(ctx, map[string]interface{}{"cmd": os.Args[0]})
	ctx = log.WithLog(ctx, logruslogger.New(ctx))

	// ********************************************************************************
	// Configure open tracing
	// ********************************************************************************
	log.EnableTracing(true)
	jaegerCloser := jaeger.InitJaeger(ctx, "cmd-forwarder-ovs")
	defer func() { _ = jaegerCloser.Close() }()

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
	log.FromContext(ctx).Infof("2: retrieve spiffe svid")
	log.FromContext(ctx).Infof("3: create ovs forwarder network service endpoint")
	log.FromContext(ctx).Infof("4: create grpc server and register ovsxconnect")
	log.FromContext(ctx).Infof("5: register ovs forwarder network service with the registry")
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
	if len(resource2LabSel.Interfaces) == 0 {
		log.FromContext(ctx).Warn("skipping matching labels to device names: empty interface list")
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

func createInterposeEndpoint(ctx context.Context, config *Config, source *workloadapi.X509Source) (xConnectEndpoint endpoint.Endpoint, err error) {
	egressTunnelIP, err := parseTunnelIPCIDR(config.TunnelIP)
	if err != nil {
		return
	}
	l2cMap := getL2ConnectionPointMap(ctx, config)
	if isSriovConfig(config.SRIOVConfigFile) {
		xConnectEndpoint, err = createSriovInterposeEndpoint(ctx, config, source, egressTunnelIP, l2cMap)
	} else {
		xConnectEndpoint, err = createKernelInterposeEndpoint(ctx, config, source, egressTunnelIP, l2cMap)
	}
	return
}

func createKernelInterposeEndpoint(ctx context.Context, config *Config, source *workloadapi.X509Source,
	egressTunnelIP net.IP, l2cMap map[string]*ovsutil.L2ConnectionPoint) (endpoint.Endpoint, error) {
	return forwarder.NewKernelServer(
		ctx,
		config.Name,
		authorize.NewServer(),
		spiffejwt.TokenGeneratorFunc(source, config.MaxTokenLifetime),
		&config.ConnectTo,
		config.BridgeName,
		egressTunnelIP,
		config.DialTimeout,
		l2cMap,
		grpc.WithBlock(),
		grpc.WithTransportCredentials(
			grpcfd.TransportCredentials(credentials.NewTLS(tlsconfig.MTLSClientConfig(source, source, tlsconfig.AuthorizeAny())))),
		grpc.WithDefaultCallOptions(
			grpc.PerRPCCredentials(token.NewPerRPCCredentials(spiffejwt.TokenGeneratorFunc(source, config.MaxTokenLifetime))),
		),
		grpcfd.WithChainStreamInterceptor(),
		grpcfd.WithChainUnaryInterceptor(),
	)
}

func createSriovInterposeEndpoint(ctx context.Context, config *Config, source *workloadapi.X509Source,
	egressTunnelIP net.IP, l2cMap map[string]*ovsutil.L2ConnectionPoint) (endpoint.Endpoint, error) {
	sriovConfig, err := sriovconfig.ReadConfig(ctx, config.SRIOVConfigFile)
	if err != nil {
		return nil, err
	}

	if err = pci.UpdateConfig(config.PCIDevicesPath, config.PCIDriversPath, sriovConfig); err != nil {
		return nil, err
	}

	tokenPool := sriovtoken.NewPool(sriovConfig)

	pciPool, err := pci.NewPool(config.PCIDevicesPath, config.PCIDriversPath, config.VFIOPath, sriovConfig)
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

	return forwarder.NewSriovServer(
		ctx,
		config.Name,
		authorize.NewServer(),
		spiffejwt.TokenGeneratorFunc(source, config.MaxTokenLifetime),
		&config.ConnectTo,
		config.BridgeName,
		egressTunnelIP,
		pciPool,
		resourcePool,
		sriovConfig,
		config.DialTimeout,
		l2cMap,
		grpc.WithBlock(),
		grpc.WithTransportCredentials(
			grpcfd.TransportCredentials(credentials.NewTLS(tlsconfig.MTLSClientConfig(source, source, tlsconfig.AuthorizeAny())))),
		grpc.WithDefaultCallOptions(
			grpc.PerRPCCredentials(token.NewPerRPCCredentials(spiffejwt.TokenGeneratorFunc(source, config.MaxTokenLifetime))),
		),
		grpcfd.WithChainStreamInterceptor(),
		grpcfd.WithChainUnaryInterceptor(),
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

func registerGRPCServer(source *workloadapi.X509Source, xConnectEndpoint endpoint.Endpoint) *grpc.Server {
	server := grpc.NewServer(append(
		opentracing.WithTracing(),
		grpc.Creds(
			grpcfd.TransportCredentials(
				credentials.NewTLS(tlsconfig.MTLSServerConfig(source, source, tlsconfig.AuthorizeAny())),
			),
		))...)
	xConnectEndpoint.Register(server)
	return server
}

func registerEndpoint(ctx context.Context, config *Config, source *workloadapi.X509Source, listenOn *url.URL) error {
	clientOptions := append(
		opentracing.WithTracingDial(),
		grpc.WithBlock(),
		grpc.WithDefaultCallOptions(grpc.WaitForReady(true)),
		grpc.WithTransportCredentials(
			grpcfd.TransportCredentials(
				credentials.NewTLS(
					tlsconfig.MTLSClientConfig(source, source, tlsconfig.AuthorizeAny()),
				),
			),
		),
	)

	nseRegistryClient := registryclient.NewNetworkServiceEndpointRegistryClient(ctx, &config.ConnectTo, registryclient.WithDialOptions(clientOptions...))
	_, err := nseRegistryClient.Register(ctx, &registryapi.NetworkServiceEndpoint{
		Name:                config.Name,
		NetworkServiceNames: []string{config.NSName},
		Url:                 grpcutils.URLToTarget(listenOn),
	})

	return err
}

func setLogLevel(level string) {
	l, err := logrus.ParseLevel(level)
	if err != nil {
		logrus.Fatalf("invalid log level %s", level)
	}
	logrus.SetLevel(l)
}
