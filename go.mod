module github.com/networkservicemesh/sdk-k8s

go 1.15

require (
	github.com/fsnotify/fsnotify v1.4.9
	github.com/golang/protobuf v1.4.3
	github.com/google/uuid v1.1.2
	github.com/networkservicemesh/api v0.0.0-20210202152048-ec956057eb3a
	github.com/networkservicemesh/sdk v0.0.0-20210216095211-a36209b21c4b
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
