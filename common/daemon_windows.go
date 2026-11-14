//go:build windows

// daemon_windows.go Windows 平台的后台进程占位实现
// Windows stub for daemon background mode, which has no fork primitive.
// 版本: v0.0.28
// 日期: 2026-11-14
// 作者: opencode

package common

import "fmt"

// Daemonize reports that background mode is unavailable on Windows so the
// Windows release binary can be built without syscall.ForkExec.
func Daemonize(logFile string) error {
	return fmt.Errorf("daemon (background) mode is not supported on Windows; run the process in the foreground or use a service wrapper")
}
