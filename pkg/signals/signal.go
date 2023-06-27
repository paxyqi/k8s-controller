package signals

import (
	"context"
	"os"
	"os/signal"
)

// 创建了一个没有缓冲区的channel，用于进行协程间的同步
// 该channel的类型为struct{}，内存分配为0，可以理解为只有一个信号semaphore
var onlyOneSignalHandler = make(chan struct{})

// SetupSignalHandler registered for SIGTERM and SIGINT. A context is returned
// which is cancelled on one of these signals. If a second signal is caught,
// the program is terminated with exit code 1.
func SetupSignalHandler() context.Context {
	//调用close用于确保只有一个信号处理器在运行，如果此函数被多次调用会panic-->因此可以避免此Setup函数被多次调用，确保只有一个信号处理器在运行
	close(onlyOneSignalHandler) // panics when called twice
	//然后创建一个os.Signal类型的channel c用于接收指定信号；是真正处理例如shutdown等信号的channel
	c := make(chan os.Signal, 2)
	//使用context包创建一个带有cancel函数的context.Context对象（此cancel函数被调用后该ctx对象及其子ctx对象均会被取消，其派生的协程会收到一个已取消的消息），用于协调多个goroutine的操作
	ctx, cancel := context.WithCancel(context.Background())
	//将指定信号加入的channel中，以便在接收到指定信号时触发后续的操作
	signal.Notify(c, shutdownSignals...)
	//创建goroutine
	go func() {
		//接收信号
		<-c
		//收到第一个时，调用cancel函数，关闭context.Context；调用cancel函数后，可以通过监听ctx.Done()来获取取消的消息
		cancel()
		//接收到第二个信号
		<-c
		//收到第二个信号则 直接退出；设置两个信号缓冲的目的是如果在第一个信号关闭程序出现问题或需要一些时间进行关闭，可以有余裕再次接收信号并退出程序
		os.Exit(1) // second signal. Exit directly.
	}()
	//在接收到指定的信号时，关闭程序，释放资源，避免程序异常退出带来的异常
	return ctx
}
