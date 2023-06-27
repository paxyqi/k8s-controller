# k8s-controller
为k8s提供一个custom controller，用于监测svc资源，针对svc资源的增删改对pods做相应的修改
### 🎯具体实现目标
针对service资源进行增删改时，对pod进行相应的操作
1. add svc：检查此svc有无endpoint，若没有则创建pod，要求新建pod的label与svc的selector相同；若有则直接打印所有endpoint的信息
2. update：更新selector，删除所有老的pod并根据新selector创建符合要求的pod
3. delete：删除所有pod
### ❤️背景介绍
在 Kubernetes 中，Service、Pod、Selector 和 Endpoint 是四个关键概念。它们之间的关系如下：
1. Pod：Pod 是 Kubernetes 中最小的基本部署单元。Pod 中包含一个或多个容器，可以运行在同一主机上，并共享网络和存储等资源。
2. Selector：Selector 是用于标识一组 Pod 的标签，以便可以对这些 Pod 进行操作。例如，可以使用 Selector 选择所有带有特定标签的 Pod。
3. Service：Service 是 Kubernetes 中用于暴露应用程序服务的资源对象。Service 可以将多个 Pod 组合成一个逻辑单元，并提供唯一的访问地址，以便可以通过该地址访问该服务，并自动将请求负载均衡到服务的各个 Pod 上。
4. Endpoint：Endpoint 是 Service 的一部分，用于保存与 Service 关联的 Pod 的网络地址。当 Service 接收到请求时，将会将请求转发到与 Endpoint 关联的 Pod 上。

因此，Pod 是 Kubernetes 中最小的部署单元，通过标签可以将多个 Pod 分组。Service 可以将多个 Pod 组合成一个逻辑单元，并提供唯一的访问地址。Endpoint 是 Service 的一部分，用于保存与 Service 关联的 Pod 的网络地址，以便负载均衡请求到与之对应的 Pod 上。Service 使用 Selector 来选择与之对应的 Pod，Endpoint 将选择的 Pod 与 Service 关联起来，以便将请求转发到对应的 Pod 上。

需要注意的是，Service 通过 Pod 的 Label Selector 来选择与之对应的 Pod，并在 Service 创建过程中自动创建 Endpoint，将 Pod 的网络地址保存到 Endpoint 中。当 Pod 发生变化时，Endpoint 会自动更新 Pod 的网络地址，以便 Service 可以将请求转发到最新的 Pod 上。