module github.com/networkservicemesh/cmd-forwarder-ovs

go 1.16

require (
	github.com/antonfisher/nested-logrus-formatter v1.3.1
	github.com/edwarnicke/debug v1.0.0
	github.com/edwarnicke/grpcfd v0.1.0
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/networkservicemesh/api v1.0.1-0.20210907194827-9a36433d7d6e
	github.com/networkservicemesh/sdk v0.5.1-0.20210920202612-c7b15bacaae5
	github.com/networkservicemesh/sdk-k8s v0.0.0-20210810055749-0005caa246db
	github.com/networkservicemesh/sdk-ovs v0.0.0-20210920203918-9e23a9f36d69
	github.com/networkservicemesh/sdk-sriov v0.0.0-20210920203242-f70665ff69a0
	github.com/pkg/errors v0.9.1
	github.com/sirupsen/logrus v1.8.1
	github.com/spiffe/go-spiffe/v2 v2.0.0-beta.2
	google.golang.org/grpc v1.38.0
)
