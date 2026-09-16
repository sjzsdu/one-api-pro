// daemon_test.go 验证后台进程参数构造
// Test daemon child-process argument construction.
// 版本: v0.0.16
// 日期: 2026-09-11
// 作者: Codex
package common

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
)

func TestBuildDaemonArgs_IncludesArgv0AndPreservesFlags(t *testing.T) {
	got := buildDaemonArgs("/usr/local/bin/one-api-pro", []string{"--daemon", "--port", "3001", "--log-dir", "./logs"})
	want := []string{"/usr/local/bin/one-api-pro", "--daemon", "--port", "3001", "--log-dir", "./logs"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("daemon argv mismatch: got %#v, want %#v", got, want)
	}
}

func TestParseServeCommand_AcceptsLifecycleCommands(t *testing.T) {
	tests := []struct {
		arguments []string
		action    string
		remaining []string
	}{
		{arguments: []string{"serve"}, action: "start"},
		{arguments: []string{"serve", "start", "--daemon"}, action: "start", remaining: []string{"--daemon"}},
		{arguments: []string{"serve", "stop", "--pid-file", "/tmp/app.pid"}, action: "stop", remaining: []string{"--pid-file", "/tmp/app.pid"}},
		{arguments: []string{"serve", "restart", "--daemon"}, action: "restart", remaining: []string{"--daemon"}},
		{arguments: []string{"serve", "status"}, action: "status"},
		{arguments: []string{"--daemon"}, action: "start", remaining: []string{"--daemon"}},
	}
	for _, test := range tests {
		remaining, action, err := parseServeCommand(test.arguments)
		if err != nil {
			t.Fatalf("parseServeCommand(%v): %v", test.arguments, err)
		}
		if action != test.action || !reflect.DeepEqual(remaining, test.remaining) {
			t.Fatalf("parseServeCommand(%v) = (%v, %q), want (%v, %q)", test.arguments, remaining, action, test.remaining, test.action)
		}
	}
}

func TestParseServeCommand_RejectsUnknownAction(t *testing.T) {
	if _, _, err := parseServeCommand([]string{"serve", "launch"}); err == nil {
		t.Fatal("expected unknown serve action to fail")
	}
}

func TestDaemonManager_DefaultPidFileUsesDataDir(t *testing.T) {
	dataDir := t.TempDir()
	manager := NewDaemonManager("", dataDir)
	want := filepath.Join(dataDir, "one-api-pro.pid")
	if got := manager.GetPidFilePath(); got != want {
		t.Fatalf("GetPidFilePath() = %q, want %q", got, want)
	}
}

func TestDaemonManager_StopAndStatusAreIdempotentWithoutPidFile(t *testing.T) {
	manager := NewDaemonManager("", t.TempDir())
	if err := manager.StatusDaemon(); err != nil {
		t.Fatalf("StatusDaemon() without PID file returned error: %v", err)
	}
	if err := manager.StopDaemon(); err != nil {
		t.Fatalf("StopDaemon() without PID file returned error: %v", err)
	}
}

func TestDaemonManager_EnsureCanStartRemovesStalePidFile(t *testing.T) {
	manager := NewDaemonManager("", t.TempDir())
	if err := os.WriteFile(manager.GetPidFilePath(), []byte("99999999"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := manager.EnsureCanStart(); err != nil {
		t.Fatalf("EnsureCanStart() returned error for stale PID: %v", err)
	}
	if _, err := os.Stat(manager.GetPidFilePath()); !os.IsNotExist(err) {
		t.Fatalf("stale PID file still exists: %v", err)
	}
}

func TestDaemonManager_EnsureCanStartRejectsRunningPid(t *testing.T) {
	manager := NewDaemonManager("", t.TempDir())
	if err := os.WriteFile(manager.GetPidFilePath(), []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
		t.Fatal(err)
	}
	if err := manager.EnsureCanStart(); err == nil {
		t.Fatal("expected EnsureCanStart() to reject running PID")
	}
}
