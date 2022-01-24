module github.com/networkservicemesh/cmd-forwarder-ovs

go 1.16

require (
	github.com/antonfisher/nested-logrus-formatter v1.3.1
	github.com/edwarnicke/debug v1.0.0
	github.com/edwarnicke/grpcfd v0.1.1
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/networkservicemesh/api v1.1.2-0.20220119092736-21eda250c390
	github.com/networkservicemesh/sdk v0.5.1-0.20220124073803-419fa7053a1a
	github.com/networkservicemesh/sdk-k8s v0.0.0-20211202072319-42a95584fc60
	github.com/networkservicemesh/sdk-ovs v0.0.0-20220124075806-3a9828a01c67
	github.com/networkservicemesh/sdk-sriov v0.0.0-20220124074808-6eb537000575
	github.com/pkg/errors v0.9.1
	github.com/sirupsen/logrus v1.8.1
	github.com/spiffe/go-spiffe/v2 v2.0.0-beta.2
	github.com/stretchr/testify v1.7.0
	google.golang.org/grpc v1.42.0
)
