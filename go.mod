module github.com/networkservicemesh/sdk-k8s

go 1.16

require (
	github.com/fsnotify/fsnotify v1.4.9
	github.com/golang/protobuf v1.5.2
	github.com/google/uuid v1.1.2
	github.com/networkservicemesh/api v1.3.0-rc.1.0.20220405210054-fbcde048efa5
	github.com/networkservicemesh/sdk v0.5.1-0.20220427224149-241ef7526a53
	github.com/pkg/errors v0.9.1
	github.com/sirupsen/logrus v1.8.1
	github.com/stretchr/testify v1.7.0
	go.uber.org/atomic v1.7.0
	go.uber.org/goleak v1.1.12
	golang.org/x/tools v0.1.7 // indirect
	google.golang.org/genproto v0.0.0-20211129164237-f09f9a12af12 // indirect
	google.golang.org/grpc v1.42.0
	google.golang.org/protobuf v1.27.1
	k8s.io/api v0.22.1
	k8s.io/apimachinery v0.22.1
	k8s.io/client-go v0.22.1
	k8s.io/klog/v2 v2.40.1 // indirect
	k8s.io/kubelet v0.22.1
)
