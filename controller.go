package main

import (
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
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
	"reflect"
	"time"
)

const controllerAgentName = "service-controller"

// Service 定义一个Service结构体
type Service struct {
	Name      string
	Namespace string
	Selector  map[string]string
}

// 定义一个Map用于存储pre service
var preServices = make(map[string]Service)

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

	// 用于处理k8s api服务器上的资源事件，每当有资源发生变化时，注册的事件处理函数就会被调用
	// 将资源变化转换成待处理的任务，相当于生产者。因此只需要在此处入队，而不需要做逻辑操作
	serviceInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			controller.addService(controller, obj)
		},
		UpdateFunc: func(old, new interface{}) {
			controller.enqueueSvc(new)
		},
		DeleteFunc: controller.enqueueSvc,
	})
	return controller
}

func (c *Controller) addService(controller *Controller, obj interface{}) {
	service, err := controller.convertToService(obj)
	if err != nil {
		return
	}
	preServices[service.Name] = service
	controller.enqueueSvc(obj)
	fmt.Printf("Add service: %s/%s\n", service.Namespace, service.Name)
}

func (c *Controller) deleteService(controller Controller, obj interface{}) {
	service, err := controller.convertToService(obj)
	if err != nil {
		return
	}
	// 从 preServices 中删除 Service 对象
	delete(preServices, service.Name)
	controller.enqueueSvc(obj)
	fmt.Printf("Delete service: %s/%s\n", service.Namespace, service.Name)
}

// 将obj转化为service对象
func (c *Controller) convertToService(obj interface{}) (Service, error) {
	// 将obj转化为runtime.Object对象
	ro, ok := obj.(runtime.Object)
	if !ok {
		return Service{}, fmt.Errorf("failed to convert to runtime.Object")
	}

	// 将runtime.object转化为service对象
	service, ok := ro.(*corev1.Service)
	if !ok {
		return Service{}, fmt.Errorf("failed to convert to service")
	}

	// 构造存储用对象
	return Service{
		Name:      service.Name,
		Namespace: service.Namespace,
		Selector:  service.Spec.Selector,
	}, nil
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

// processNextWorkItem will read a single work item off the workqueue and
// attempt to process it, by calling the syncHandler.
// 将监听到的资源变化转换成待处理的任务，并将任务添加到工作队列中，然后通过工作线程来异步处理这些任务
// 相当于消费者
func (c *Controller) processNextWorkItem(ctx context.Context) bool {
	// 获取待处理的资源对象
	obj, shutdown := c.workqueue.Get()
	logger := klog.FromContext(ctx)

	if shutdown {
		return false
	}

	//创建匿名函数用于处理：1.将资源对象转换为形如namespace/name的key 2.从队列中获取对象并对象进行处理 3.若处理失败则再次压入队列中
	err := func(obj interface{}) error {
		defer c.workqueue.Done(obj)
		var key string
		var ok bool

		if key, ok = obj.(string); !ok {
			//检查发现key不符合类型为string的要求，则forget，即将对象资源再次放回队列中
			c.workqueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}

		if err := c.syncHandler(ctx, key); err != nil {
			// 任务处理失败时，重新添加回队列
			c.workqueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}

		// 任务处理成功，直接forget，表示不需要再次执行
		c.workqueue.Forget(obj)
		logger.Info("Successfully synced", "resourceName", key)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}

	return true
}

// TODO 此函数应该用于对关心的资源对象进行操作
// 使用 Client 对象从 Kubernetes API 服务器上获取资源对象的最新状态，并根据资源对象的状态来判断应该对资源对象进行哪种操作（增加、修改或删除）
func (c *Controller) syncHandler(ctx context.Context, key string) error {
	logger := klog.LoggerWithValues(klog.FromContext(ctx), "resourceName", key)

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	// 通过namespace/name获取svc资源对象
	svc, err := c.serviceLister.Services(namespace).Get(name)
	if err != nil {
		// 若资源不存在则报错
		if errors.IsNotFound(err) {
			utilruntime.HandleError(fmt.Errorf("foo '%s' in work queue no longer exists", key))
			return nil
		}
		return err
	}

	// 通过deletionTimestamp字段判断是否是del操作， 若是Del，则删除service名下的所有pods
	if svc.GetObjectMeta().GetDeletionTimestamp() != nil {
		// 获取svc的所有selector标签
		selector := &metav1.LabelSelector{MatchLabels: svc.Spec.Selector}
		selectorLabel, err := metav1.LabelSelectorAsSelector(selector)
		if err != nil {
			return err
		}
		// 根据selector标签获取相关的pod对象
		podList, err := c.kubeclientset.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{LabelSelector: selectorLabel.String()})
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("get Service: '#{name}' pods failed"))
		}
		// 删除所有相关的pod对象
		for _, pod := range podList.Items {
			err = c.kubeclientset.CoreV1().Pods(namespace).Delete(context.Background(), pod.Name, metav1.DeleteOptions{})
			if err != nil {
				utilruntime.HandleError(fmt.Errorf("delete Service: '#{name}' pods failed"))
			}
			//TODO
		}
		// 进行完Del即可返回
		return nil
	}
	// 通过map数据结构将先前的Service存储在内存中，当有新的任务到来时，可以对比当前的Service状态和之前的Service的状态，判断是否需要创建新的Service资源/更新已有资源
	if preService, ok := preServices[svc.Name]; ok {
		// preService中已经存在该service， 执行更新操作
		// 获取原selector并删除对应pods
		preSelector := preService.Selector
		if !reflect.DeepEqual(preSelector, svc.Spec.Selector) {
			err := c.kubeclientset.CoreV1().Pods(namespace).DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
				LabelSelector: labels.Set(preSelector).String(),
			})
			if err != nil {
				utilruntime.HandleError(fmt.Errorf("delete Service: '#{name}' pods for update failed"))
			}
			return err
		}

		// 更新preService中的selector信息
		delete(preServices, svc.Name)
		preServices[svc.Name] = Service{
			Name:      svc.Name,
			Namespace: svc.Namespace,
			Selector:  svc.Spec.Selector,
		}

		// 根据新的selector创建pods

		svcLabel := svc.Labels
		podName := name + "-pod"
		_, err := c.kubeclientset.CoreV1().Pods(namespace).Create(ctx, &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:   podName,
				Labels: svcLabel,
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "my-container",
						Image: "nginx",
						Ports: []corev1.ContainerPort{
							{
								ContainerPort: 8080,
								Protocol:      corev1.ProtocolTCP,
							},
						},
					},
				},
			},
		}, metav1.CreateOptions{})
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("create pods for Srvice: '#{name}' failed"))
			return err
		}
		// wait for the Pod and Service to start up
		time.Sleep(5 * time.Second)
		// 检查是否create成功
		pod, err := c.kubeclientset.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("create pods for Srvice: '#{name}' failed"))
			return err
		}
		logger.Info("create pod: %s for service: %s successes.\n ", pod.Name, svc.Name)

	} else {
		// preService中不存在此service，则新建pods/打印endpoint信息
		// 通过client检查Service是否存在对应的endpoint
		// 如果该Service有Endpoints则打印所有EndPoints信息
		// 当一个Service没有endpoints时，它仍然具有一个Cluster IP地址，但是该地址将为空字符串。在这种情况下，service.Spec.ClusterIP的长度将等于0。通常，没有endpoints的Service通常用于在集群内部使用，会使用DNS发现机制与内部pod进行路由，而不需要通过Kubernetes Service进行路由
		if len(svc.Spec.ClusterIP) > 0 {
			endpoints, err := c.kubeclientset.CoreV1().Endpoints(namespace).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				utilruntime.HandleError(fmt.Errorf("get Service's endpoints '#{name}' failed"))
				return err
			}
			fmt.Printf("Endpoints for %s:\n", name)
			for _, subset := range endpoints.Subsets {
				for _, address := range subset.Addresses {
					fmt.Printf("\tIP: %s\n", address.IP)
				}
				for _, port := range subset.Ports {
					fmt.Printf("\tPort: %d\n", port.Port)
				}
			}
		} else { // 如果该Service没有Endpoints则创建Pod， 要求pod的label与svc的selector相同
			svcLabel := svc.Labels
			podName := name + "-pod"
			_, err := c.kubeclientset.CoreV1().Pods(namespace).Create(ctx, &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:   podName,
					Labels: svcLabel,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "my-container",
							Image: "nginx",
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 8080,
									Protocol:      corev1.ProtocolTCP,
								},
							},
						},
					},
				},
			}, metav1.CreateOptions{})
			if err != nil {
				utilruntime.HandleError(fmt.Errorf("create pods for Srvice: '#{name}' failed"))
				return err
			}
			// wait for the Pod and Service to start up
			time.Sleep(5 * time.Second)
			// 检查是否create成功
			pod, err := c.kubeclientset.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				utilruntime.HandleError(fmt.Errorf("create pods for Srvice: '#{name}' failed"))
				return err
			}
			logger.Info("create pod: %s for service: %s successes.\n ", pod.Name, svc.Name)
		}
	}

	return nil
}

// 检测到k8s api事件入队操作
func (c *Controller) enqueueSvc(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.workqueue.Add(key)
}
