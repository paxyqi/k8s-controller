package signals

import (
	"os"
	"syscall"
)

// 检测到这两个变量：os.Interrupt, syscall.SIGTERM 均会被认为是程序关闭信号
var shutdownSignals = []os.Signal{os.Interrupt, syscall.SIGTERM}
