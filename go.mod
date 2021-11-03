module github.com/networkservicemesh/cmd-forwarder-ovs

go 1.16

require (
	github.com/antonfisher/nested-logrus-formatter v1.3.1
	github.com/edwarnicke/debug v1.0.0
	github.com/edwarnicke/grpcfd v0.1.1
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/networkservicemesh/api v1.0.1-0.20211103071234-5e9299dd5ac3
	github.com/networkservicemesh/sdk v0.5.1-0.20211103084119-5bcf896b3c2a
	github.com/networkservicemesh/sdk-k8s v0.0.0-20210929180939-adf19e0dded1
	github.com/networkservicemesh/sdk-ovs v0.0.0-20211103085505-7106d90b0972
	github.com/networkservicemesh/sdk-sriov v0.0.0-20211103084801-548762184d2a
	github.com/pkg/errors v0.9.1
	github.com/sirupsen/logrus v1.8.1
	github.com/spiffe/go-spiffe/v2 v2.0.0-beta.2
	google.golang.org/grpc v1.38.0
)
