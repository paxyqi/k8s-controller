package k8s_controller

import (
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	v1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"time"
)

const controllerAgentName = "service-controller"

type Controller struct {
	// kubeclientset is a standard kubernetes clientset
	kubeclientset kubernetes.Interface

	serviceLister corev1listers.ServiceLister
	serviceSynced cache.InformerSynced

	// 限制工作队列速率
	workqueue workqueue.RateLimitingInterface
	recorder  record.EventRecorder
}

func NewController(ctx context.Context, kubeclientset kubernetes.Interface, serviceInformer v1.ServiceInformer) *Controller {
	logger := klog.FromContext(ctx)

	//创建k8s的事件广播器，broadcaster可以将事件发到k8s api中供其他应用获取和查看
	eventBroadcaster := record.NewBroadcaster()
	//启动广播器的结构化日志记录
	eventBroadcaster.StartStructuredLogging(0)
	//连接到k8s api
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeclientset.CoreV1().Events("")})
	//创建一个事件记录器并于broadcaster关联
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerAgentName})

	controller := &Controller{
		kubeclientset: kubeclientset,
		serviceLister: serviceInformer.Lister(),
		serviceSynced: serviceInformer.Informer().HasSynced,
		workqueue:     workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Service"),
		recorder:      recorder,
	}

	logger.Info("Setting up event handlers")

	serviceInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		//TODO:增删改的事件
		//AddFunc: ,
		//DeleteFunc: ,
		//UpdateFunc: ,
	})
	return controller
}

// Run 定义controller类型的方法
func (c *Controller) Run(ctx context.Context, workers int) error {
	// 处理控制器发生崩溃/关闭事件
	defer utilruntime.HandleCrash()
	defer c.workqueue.ShutDown()
	logger := klog.FromContext(ctx)

	// Start the informer factories to begin populating the informer caches
	logger.Info("Starting Service controller")

	// Wait for the caches to be synced before starting workers
	logger.Info("Waiting for informer caches to sync")

	if ok := cache.WaitForCacheSync(ctx.Done(), c.serviceSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	logger.Info("Starting workers", "count", workers)

	// Launch two workers to process Service resources
	for i := 0; i < workers; i++ {
		// 每个period（time.Second)都在运行c.runWorker，是一个循环
		go wait.UntilWithContext(ctx, c.runWorker, time.Second)
	}

	logger.Info("Started workers")
	<-ctx.Done()
	logger.Info("Shutting down workers")

	return nil
}

// runWorker 会长时间运行，不断的调用processNextWorkItem来处理队列中的消息
func (c *Controller) runWorker(ctx context.Context) {
	for c.processNextWorkItem(ctx) {
	}
}
