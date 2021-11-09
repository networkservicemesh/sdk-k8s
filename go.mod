module github.com/networkservicemesh/sdk-k8s

go 1.16

require (
	github.com/fsnotify/fsnotify v1.4.9
	github.com/golang/protobuf v1.5.2
	github.com/google/uuid v1.1.2
	github.com/networkservicemesh/api v1.0.1-0.20211108174538-15c88bce33f3
	github.com/networkservicemesh/sdk v0.5.1-0.20211109212653-03505466fd16
	github.com/pkg/errors v0.9.1
	github.com/sirupsen/logrus v1.7.0
	github.com/stretchr/testify v1.7.0
	go.uber.org/atomic v1.7.0
	go.uber.org/goleak v1.1.10
	google.golang.org/grpc v1.38.0
	google.golang.org/protobuf v1.26.0
	k8s.io/api v0.22.1
	k8s.io/apimachinery v0.22.1
	k8s.io/client-go v0.22.1
	k8s.io/kubelet v0.22.1
)
