module github.com/networkservicemesh/sdk-k8s

go 1.15

require (
	github.com/fsnotify/fsnotify v1.4.9
	github.com/golang/protobuf v1.4.3
	github.com/google/uuid v1.1.2
	github.com/networkservicemesh/api v0.0.0-20210112152104-45029fb10e27
	github.com/networkservicemesh/sdk v0.0.0-20210127171236-70aa662da077
	github.com/pkg/errors v0.9.1
	github.com/sirupsen/logrus v1.7.0
	github.com/stretchr/testify v1.6.1
	go.uber.org/goleak v1.1.10
	google.golang.org/grpc v1.33.2
	google.golang.org/protobuf v1.25.0
	k8s.io/apimachinery v0.20.1
	k8s.io/client-go v0.20.1
	k8s.io/kubelet v0.20.1
)
