package common

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Daemon-related flags
var (
	DaemonMode    = flag.Bool("daemon", false, "run in daemon (background) mode")
	DataDir       = flag.String("data-dir", defaultDaemonDataDir(), "daemon state directory")
	PidFile       = flag.String("pid-file", "", "specify the PID file path (default: <data-dir>/one-api-pro.pid)")
	DaemonLogFile = flag.String("log-file", "", "daemon log file (default: <data-dir>/one-api-pro.log)")
	DaemonStop    = flag.Bool("stop", false, "stop the running daemon")
	DaemonStatus  = flag.Bool("status", false, "check daemon status")
)

func defaultDaemonDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ".one-api-pro"
	}
	return filepath.Join(home, ".one-api-pro")
}

// DaemonManager handles daemon mode operations
type DaemonManager struct {
	pidFile string
	logDir  string
}

// NewDaemonManager creates a new daemon manager
func NewDaemonManager(pidFile, dataDir string) *DaemonManager {
	return &DaemonManager{
		pidFile: pidFile,
		logDir:  dataDir,
	}
}

// GetPidFilePath returns the PID file path
func (d *DaemonManager) GetPidFilePath() string {
	if d.pidFile != "" {
		return d.pidFile
	}
	return filepath.Join(d.logDir, "one-api-pro.pid")
}

// WritePidFile writes the current process PID to file
func (d *DaemonManager) WritePidFile() error {
	pidPath := d.GetPidFilePath()
	dir := filepath.Dir(pidPath)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create PID directory: %w", err)
		}
	}
	return os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0644)
}

// RemovePidFile removes the PID file
func (d *DaemonManager) RemovePidFile() {
	os.Remove(d.GetPidFilePath())
}

// ReadPidFile reads the PID from file
func (d *DaemonManager) ReadPidFile() (int, error) {
	data, err := os.ReadFile(d.GetPidFilePath())
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("invalid PID in file: %w", err)
	}
	return pid, nil
}

// EnsureCanStart rejects a duplicate daemon and removes a stale PID file.
// 版本: v0.0.16
// 日期: 2026-09-11
func (d *DaemonManager) EnsureCanStart() error {
	pid, err := d.ReadPidFile()
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read PID file: %w", err)
	}
	if IsPidRunning(pid) {
		return fmt.Errorf("daemon is already running with PID %d", pid)
	}
	d.RemovePidFile()
	return nil
}

// IsPidRunning checks if a process with the given PID is running
func IsPidRunning(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, FindProcess always succeeds; send signal 0 to check if alive
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

// StopDaemon stops the running daemon
func (d *DaemonManager) StopDaemon() error {
	pid, err := d.ReadPidFile()
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("Daemon is not running (no PID file at %s)\n", d.GetPidFilePath())
			return nil
		}
		return fmt.Errorf("failed to read PID file: %w", err)
	}

	if !IsPidRunning(pid) {
		fmt.Printf("Process %d is not running, cleaning up PID file\n", pid)
		d.RemovePidFile()
		return nil
	}

	fmt.Printf("Sending SIGTERM to process %d...\n", pid)
	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("failed to find process %d: %w", pid, err)
	}

	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to send SIGTERM to process %d: %w", pid, err)
	}

	// Wait for process to exit (max 10 seconds)
	for i := 0; i < 100; i++ {
		if !IsPidRunning(pid) {
			fmt.Printf("Process %d stopped successfully\n", pid)
			d.RemovePidFile()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("process %d did not exit within 10 seconds, you may need to kill it manually", pid)
}

// StatusDaemon checks and reports daemon status
func (d *DaemonManager) StatusDaemon() error {
	pid, err := d.ReadPidFile()
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("Daemon is not running (no PID file at %s)\n", d.GetPidFilePath())
			return nil
		}
		return fmt.Errorf("failed to read PID file: %w", err)
	}

	if IsPidRunning(pid) {
		fmt.Printf("Daemon is running with PID %d\n", pid)
		return nil
	}

	fmt.Printf("Daemon is not running (stale PID file for %d)\n", pid)
	d.RemovePidFile()
	return nil
}

// Daemonize forks the current process to background
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

// GetDaemonLogFilePath resolves the daemon output path independently of the
// caller's working directory.
// 版本: v0.0.16
// 日期: 2026-09-11
func GetDaemonLogFilePath() string {
	if strings.TrimSpace(*DaemonLogFile) != "" {
		return *DaemonLogFile
	}
	return filepath.Join(*DataDir, "one-api-pro.log")
}

func buildDaemonArgs(exe string, arguments []string) []string {
	args := make([]string, 1, len(arguments)+1)
	args[0] = exe
	args = append(args, arguments...)
	return args
}

// SetupDaemonSignals sets up signal handlers for the daemon
func SetupDaemonSignals(dm *DaemonManager, shutdown func()) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)

	go func() {
		for {
			sig := <-sigChan
			switch sig {
			case syscall.SIGTERM, syscall.SIGINT:
				fmt.Printf("\nReceived signal %v, shutting down...\n", sig)
				dm.RemovePidFile()
				shutdown()
				os.Exit(0)
			case syscall.SIGHUP:
				fmt.Printf("Received SIGHUP, reloading configuration...\n")
				// SIGHUP can be used for config reload in future
			}
		}
	}()
}
