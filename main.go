package k8s_controller

import (
	"flag"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"k8s.io/sample-controller/pkg/signals"
	"time"
)

var (
	masterURL  string
	kubeconfig string
)

func main() {
	klog.InitFlags(nil)
	flag.Parse()

	//处理shutdown信号
	ctx := signals.SetupSignalHandler()
	logger := klog.FromContext(ctx)

	//构建一个访问k8s api的配置对象，以便连接到指定的k8s集群，例如获取deployment，svc，pod等资源对象
	cfg, err := clientcmd.BuildConfigFromFlags(masterURL, kubeconfig)
	if err != nil {
		logger.Error(err, "Error building kubeconfig")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}

	//创建k8s客户端对象
	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		logger.Error(err, "Error building kubernetes clientset")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}

	//Informer是可以监听k8s api服务器上的资源对象，并在资源对象发生变化时触发回调函数
	//kubeInformerFactory创建了一个共享的Informer的工厂，用于创建informer对象
	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, time.Second*30)

	//创建controller
	controller := NewController(ctx, kubeClient, kubeInformerFactory.Core().V1().Services())
}
