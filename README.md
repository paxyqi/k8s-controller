# k8s-controller
为k8s提供一个custom controller，用于监测svc资源，针对svc资源的增删改对pods做相应的修改。

k8s集群本身在service进行修改时，只会处理其名下的endpoints，而不会处理pods，因此本项目的主要作用就是处理service名下的pods
### 🎯具体实现目标
针对service资源进行增删改时，对pod进行相应的操作
1. add：检查此svc有无endpoint，若没有则创建pod，要求新建pod的label与svc的selector相同；若有则直接打印所有endpoint的信息
2. update：更新selector时，删除所有老的pod并根据新selector创建符合要求的pod
3. delete：删除所有pod
### ❤️背景介绍
在 Kubernetes 中，Service、Pod、Selector 和 Endpoint 是四个关键概念。它们之间的关系如下：
1. Pod：Pod 是 Kubernetes 中最小的基本部署单元。Pod 中包含一个或多个容器，可以运行在同一主机上，并共享网络和存储等资源。
2. Selector：Selector 是用于标识一组 Pod 的标签，以便可以对这些 Pod 进行操作。例如，可以使用 Selector 选择所有带有特定标签的 Pod。
3. Service：Service 是 Kubernetes 中用于暴露应用程序服务的资源对象。Service 可以将多个 Pod 组合成一个逻辑单元，并提供唯一的访问地址，以便可以通过该地址访问该服务，并自动将请求负载均衡到服务的各个 Pod 上。
4. Endpoint：Endpoint 是 Service 的一部分，用于保存与 Service 关联的 Pod 的网络地址。当 Service 接收到请求时，将会将请求转发到与 Endpoint 关联的 Pod 上。

因此，Pod 是 Kubernetes 中最小的部署单元，通过标签可以将多个 Pod 分组。Service 可以将多个 Pod 组合成一个逻辑单元，并提供唯一的访问地址。Endpoint 是 Service 的一部分，用于保存与 Service 关联的 Pod 的网络地址，以便负载均衡请求到与之对应的 Pod 上。Service 使用 Selector 来选择与之对应的 Pod，Endpoint 将选择的 Pod 与 Service 关联起来，以便将请求转发到对应的 Pod 上。

需要注意的是，Service 通过 Pod 的 Label Selector 来选择与之对应的 Pod，并在 Service 创建过程中自动创建 Endpoint，将 Pod 的网络地址保存到 Endpoint 中。当 Pod 发生变化时，Endpoint 会自动更新 Pod 的网络地址，以便 Service 可以将请求转发到最新的 Pod 上。
### 🌟业务难点
#### syncHandler如何工作
完成了controller代码的框架编写工作，同时遇到了新的问题
1. AddEventHandler函数添加add/delete/update任务，作用为*生产者*
2. Run调用runworker，runworker调用processNextItem，processNextItem相当于*消费者*，处理任务
3. processNextItem处理任务的方式是通过调用syncHandler进行的，因此问题出现： 

   a.syncHandler是如何发现到来的任务是add/delete/update呢？
   
   b.发现任务是何种目标的方式可能有很多，那么如何考虑到可能的服务重启导致的事件丢失的问题呢？
#### 解决方案
1. 引入finalizer字段：
   此字段作用是指在删除一个资源对象时，需要在其metadata.finalizer中添加一个字符串，用于标识某些需要在删除该资源之前完成的操作。
   因此可以在service的yaml中添加
   ```
   kubectl patch service my-service -n my-namespace -p '{"metadata":{"finalizers":["delete-pods"]}}'
   ```
   添加了finalizer字段的资源对象会在delete时不直接删除，而是在其配置中增加GetDeletionTimestamp字段这样就可以保证在delete事件到来时通过检测该字段是否为空来判断是否时delete操作
2. 引入map存储：
   将到来的service均存入内存中，这样当service事件到来时判断preServices是否有该svc，若有则是update，否则为新增
   ```
   // Service 定义一个Service结构体
   type Service struct {
   Name      string
   Namespace string
   Selector  map[string]string
   }

   // 定义一个Map用于存储pre service
   var preServices map[string]Service
   ```
3. 考虑到服务运行异常，为避免重启丢失将preServices定期持久化到存储的文件中
