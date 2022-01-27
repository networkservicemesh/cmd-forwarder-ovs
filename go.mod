module github.com/networkservicemesh/cmd-forwarder-ovs

go 1.16

require (
	github.com/antonfisher/nested-logrus-formatter v1.3.1
	github.com/edwarnicke/debug v1.0.0
	github.com/edwarnicke/grpcfd v0.1.1
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/networkservicemesh/api v1.1.2-0.20220119092736-21eda250c390
	github.com/networkservicemesh/sdk v0.5.1-0.20220127193211-6d5254af4bb4
	github.com/networkservicemesh/sdk-k8s v0.0.0-20211202072319-42a95584fc60
	github.com/networkservicemesh/sdk-ovs v0.0.0-20220127195805-6cd2e1072504
	github.com/networkservicemesh/sdk-sriov v0.0.0-20220127194319-723f27bc2105
	github.com/pkg/errors v0.9.1
	github.com/sirupsen/logrus v1.8.1
	github.com/spiffe/go-spiffe/v2 v2.0.0-beta.2
	github.com/stretchr/testify v1.7.0
	google.golang.org/grpc v1.42.0
)
