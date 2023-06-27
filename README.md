# k8s-controller
为k8s提供一个custom controller，用于监测svc资源，针对svc资源的增删改对pods做相应的修改
### 🎯具体实现目标
针对service资源进行增删改时，对pod进行相应的操作
1. add svc：检查此svc有无endpoint，若没有则创建pod，要求新建pod的label与svc的selector相同；若有则直接打印所有endpoint的信息
2. update：更新selector，删除所有老的pod并根据新selector创建符合要求的pod
3. delete：删除所有pod