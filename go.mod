module github.com/networkservicemesh/sdk-k8s

go 1.16

require (
	github.com/fsnotify/fsnotify v1.4.9
	github.com/golang/protobuf v1.4.3
	github.com/google/uuid v1.1.2
	github.com/networkservicemesh/api v0.5.1-0.20210618184350-e3fc29dc170b
	github.com/networkservicemesh/sdk v0.5.1-0.20210630092016-3b8e683e3d9c
	github.com/onsi/ginkgo v1.13.0 // indirect
	github.com/pkg/errors v0.9.1
	github.com/sirupsen/logrus v1.7.0
	github.com/stretchr/testify v1.7.0
	go.uber.org/atomic v1.7.0
	go.uber.org/goleak v1.1.10
	google.golang.org/grpc v1.35.0
	google.golang.org/protobuf v1.25.0
	k8s.io/api v0.20.1
	k8s.io/apimachinery v0.20.1
	k8s.io/client-go v0.20.1
	k8s.io/kubelet v0.20.1
)
