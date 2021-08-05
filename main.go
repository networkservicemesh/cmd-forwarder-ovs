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
	"syscall"
	"time"

	nested "github.com/antonfisher/nested-logrus-formatter"
	"github.com/edwarnicke/debug"
	"github.com/edwarnicke/grpcfd"
	"github.com/kelseyhightower/envconfig"
	registryapi "github.com/networkservicemesh/api/pkg/api/registry"
	"github.com/networkservicemesh/sdk-k8s/pkg/tools/deviceplugin"
	k8sdeviceplugin "github.com/networkservicemesh/sdk-k8s/pkg/tools/deviceplugin"
	k8spodresources "github.com/networkservicemesh/sdk-k8s/pkg/tools/podresources"

	"github.com/networkservicemesh/sdk-ovs/pkg/networkservice/chains/xconnectns"
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
	"github.com/networkservicemesh/sdk/pkg/tools/resetting"
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
	Name                string        `default:"forwarder" desc:"Name of Endpoint"`
	NSName              string        `default:"ovsconnectns" desc:"Name of Network Service to Register with Registry"`
	BridgeName          string        `default:"br-nsm" desc:"Name of the OvS bridge"`
	TunnelIP            net.IP        `desc:"IP to use for tunnels" split_words:"true"`
	ConnectTo           url.URL       `default:"unix:///connect.to.socket" desc:"url to connect to" split_words:"true"`
	MaxTokenLifetime    time.Duration `default:"24h" desc:"maximum lifetime of tokens" split_words:"true"`
	ResourcePollTimeout time.Duration `default:"30s" desc:"device plugin polling timeout" split_words:"true"`
	DevicePluginPath    string        `default:"/var/lib/kubelet/device-plugins/" desc:"path to the device plugin directory" split_words:"true"`
	PodResourcesPath    string        `default:"/var/lib/kubelet/pod-resources/" desc:"path to the pod resources directory" split_words:"true"`
	SRIOVConfigFile     string        `default:"pci.config" desc:"PCI resources config path" split_words:"true"`
	PCIDevicesPath      string        `default:"/sys/bus/pci/devices" desc:"path to the PCI devices directory" split_words:"true"`
	PCIDriversPath      string        `default:"/sys/bus/pci/drivers" desc:"path to the PCI drivers directory" split_words:"true"`
	CgroupPath          string        `default:"/host/sys/fs/cgroup/devices" desc:"path to the host cgroup directory" split_words:"true"`
	VFIOPath            string        `default:"/host/dev/vfio" desc:"path to the host VFIO directory" split_words:"true"`
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
	jaegerCloser := jaeger.InitJaeger(ctx, "cmd-forwarder-sriov")
	defer func() { _ = jaegerCloser.Close() }()

	// ********************************************************************************
	// Debug self if necessary
	// ********************************************************************************
	if err := debug.Self(); err != nil {
		log.FromContext(ctx).Infof("%s", err)
	}

	starttime := time.Now()

	// enumerating phases
	log.FromContext(ctx).Infof("there are 9 phases which will be executed followed by a success message:")
	log.FromContext(ctx).Infof("the phases include:")
	log.FromContext(ctx).Infof("1: get config from environment")
	log.FromContext(ctx).Infof("2: retrieve spiffe svid")
	log.FromContext(ctx).Infof("3: create ovsconnect network service endpoint")
	log.FromContext(ctx).Infof("4: create grpc server and register ovsxconnect")
	log.FromContext(ctx).Infof("5: register ovsconnectns with the registry")
	log.FromContext(ctx).Infof("6: final success message with start time duration")

	log.FromContext(ctx).Infof("executing phase 1: get config from environment (time since start: %s)", time.Since(starttime))
	now := time.Now()
	config := &Config{}
	if err := envconfig.Usage("nsm", config); err != nil {
		logrus.Fatal(err)
	}
	if err := envconfig.Process("nsm", config); err != nil {
		logrus.Fatalf("error processing config from env: %+v", err)
	}

	log.FromContext(ctx).Infof("Config: %#v", config)
	log.FromContext(ctx).WithField("duration", time.Since(now)).Infof("completed phase 1: get config from environment")

	log.FromContext(ctx).Infof("executing phase 2: retrieving svid, check spire agent logs if this is the last line you see (time since start: %s)", time.Since(starttime))
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

	log.FromContext(ctx).Infof("executing phase 3: create ovsxconnect network service endpoint (time since start: %s)", time.Since(starttime))
	var endpoint endpoint.Endpoint
	if isSriovConfig(config.SRIOVConfigFile) {
		endpoint, err = createSriovInterposeEndpoint(ctx, config, source)
		if err != nil {
			logrus.Fatalf("error configuring sriov endpoint: %+v", err)
		}
	} else {
		endpoint, err = xconnectns.NewKernelServer(
			ctx,
			config.Name,
			authorize.NewServer(),
			spiffejwt.TokenGeneratorFunc(source, config.MaxTokenLifetime),
			&config.ConnectTo,
			config.BridgeName,
			config.TunnelIP,
			grpc.WithTransportCredentials(
				resetting.NewCredentials(
					grpcfd.TransportCredentials(credentials.NewTLS(tlsconfig.MTLSClientConfig(source, source, tlsconfig.AuthorizeAny()))),
					source.Updated())),
			grpc.WithDefaultCallOptions(
				grpc.WaitForReady(true),
				grpc.PerRPCCredentials(token.NewPerRPCCredentials(spiffejwt.TokenGeneratorFunc(source, config.MaxTokenLifetime))),
			),
			grpcfd.WithChainStreamInterceptor(),
			grpcfd.WithChainUnaryInterceptor(),
		)
		if err != nil {
			logrus.Fatalf("error configuring kernel endpoint: %+v", err)
		}
	}
	log.FromContext(ctx).WithField("duration", time.Since(now)).Info("completed phase 3: create ovsxconnect network service endpoint")

	log.FromContext(ctx).Infof("executing phase 4: create grpc server and register ovsxconnect (time since start: %s)", time.Since(starttime))
	tmpDir, err := ioutil.TempDir("", "ovs-forwarder")
	if err != nil {
		log.FromContext(ctx).Fatalf("error creating tmpDir: %+v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()
	listenOn := &url.URL{Scheme: "unix", Path: path.Join(tmpDir, "listen_on.io.sock")}

	server := grpc.NewServer(append(
		opentracing.WithTracing(),
		grpc.Creds(
			grpcfd.TransportCredentials(
				credentials.NewTLS(tlsconfig.MTLSServerConfig(source, source, tlsconfig.AuthorizeAny())),
			),
		))...)
	endpoint.Register(server)
	srvErrCh := grpcutils.ListenAndServe(ctx, listenOn, server)
	exitOnErrCh(ctx, cancel, srvErrCh)
	log.FromContext(ctx).WithField("duration", time.Since(now)).Info("completed phase 4: create grpc server and register ovsxconnect")

	// ********************************************************************************
	log.FromContext(ctx).Infof("executing phase 5: register %s with the registry (time since start: %s)", config.NSName, time.Since(starttime))
	// ********************************************************************************
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

	nseRegistryClient := registryclient.NewNetworkServiceEndpointRegistryInterposeClient(ctx, &config.ConnectTo, registryclient.WithDialOptions(clientOptions...))
	_, err = nseRegistryClient.Register(ctx, &registryapi.NetworkServiceEndpoint{
		Name:                config.Name,
		NetworkServiceNames: []string{config.NSName},
		Url:                 grpcutils.URLToTarget(listenOn),
	})
	if err != nil {
		log.FromContext(ctx).Fatalf("failed to connect to registry: %+v", err)
	}
	log.FromContext(ctx).WithField("duration", time.Since(now)).Infof("completed phase 5: register %s with the registry", config.NSName)

	log.FromContext(ctx).Infof("Startup completed in %v", time.Since(starttime))

	// TODO - cleaner shutdown across these two channels
	<-ctx.Done()
	<-srvErrCh

}

func createSriovInterposeEndpoint(ctx context.Context, config *Config, source *workloadapi.X509Source) (endpoint.Endpoint, error) {
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
	if err = deviceplugin.StartServers(
		ctx,
		tokenPool,
		config.ResourcePollTimeout,
		k8sdeviceplugin.NewClient(config.DevicePluginPath),
		k8spodresources.NewClient(config.PodResourcesPath),
	); err != nil {
		return nil, err
	}

	return xconnectns.NewSriovServer(
		ctx,
		config.Name,
		authorize.NewServer(),
		spiffejwt.TokenGeneratorFunc(source, config.MaxTokenLifetime),
		&config.ConnectTo,
		config.BridgeName,
		config.TunnelIP,
		pciPool,
		resourcePool,
		sriovConfig,
		grpc.WithTransportCredentials(
			resetting.NewCredentials(
				grpcfd.TransportCredentials(credentials.NewTLS(tlsconfig.MTLSClientConfig(source, source, tlsconfig.AuthorizeAny()))),
				source.Updated())),
		grpc.WithDefaultCallOptions(
			grpc.WaitForReady(true),
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
