module github.com/networkservicemesh/cmd-forwarder-ovs

go 1.16

require (
	github.com/antonfisher/nested-logrus-formatter v1.3.1
	github.com/edwarnicke/debug v1.0.0
	github.com/edwarnicke/grpcfd v0.1.1
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/networkservicemesh/api v1.0.1-0.20211110183123-3038992da61a
	github.com/networkservicemesh/sdk v0.5.1-0.20211124120744-6431f82bd853
	github.com/networkservicemesh/sdk-k8s v0.0.0-20211102193828-c29ab6e0f743
	github.com/networkservicemesh/sdk-ovs v0.0.0-20211124122047-70506cb8269a
	github.com/networkservicemesh/sdk-sriov v0.0.0-20211124121425-00a9501498fa
	github.com/pkg/errors v0.9.1
	github.com/sirupsen/logrus v1.8.1
	github.com/spiffe/go-spiffe/v2 v2.0.0-beta.2
	google.golang.org/grpc v1.38.0
)
