//go:build !windows

// daemon_unix.go Unix 平台的后台进程 fork 实现
// Unix implementation of daemon forking for background mode.
// 版本: v0.0.28
// 日期: 2026-11-14
// 作者: opencode

package common

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Daemonize forks the current process to background on Unix-like systems.
func Daemonize(logFile string) error {
	// Check if already a daemon
	if os.Getenv("ONE_API_PRO_DAEMON") == "1" {
		return nil
	}

	// Get current executable path
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Build argv with argv[0] included; ForkExec does not add it for us.
	args := buildDaemonArgs(exe, os.Args[1:])
	stdin, err := os.OpenFile(os.DevNull, os.O_RDONLY, 0)
	if err != nil {
		return fmt.Errorf("failed to open daemon stdin: %w", err)
	}
	defer stdin.Close()
	if err := os.MkdirAll(filepath.Dir(logFile), 0755); err != nil {
		return fmt.Errorf("failed to create daemon log directory: %w", err)
	}
	output, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open daemon log file: %w", err)
	}
	defer output.Close()

	// Fork using ForkExec
	pid, err := syscall.ForkExec(exe, args, &syscall.ProcAttr{
		Files: []uintptr{stdin.Fd(), output.Fd(), output.Fd()},
		Env:   append(os.Environ(), "ONE_API_PRO_DAEMON=1"),
		Sys: &syscall.SysProcAttr{
			Setsid: true,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to fork process: %w", err)
	}

	// Parent exits
	fmt.Printf("Daemon started with PID %d\n", pid)
	os.Exit(0)

	return nil // unreachable
}
