// client_install_test.go 覆盖客户端安装命令的输入和配置更新行为。
// Covers input handling and configuration updates for client installation.
// 版本: v0.0.16
// 日期: 2026-09-09
// 作者: Codex
package common

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunClientInstall_CodexRejectsExtraArguments verifies the command usage validation.
// 版本: v0.0.16
// 日期: 2026-09-09
func TestRunClientInstall_CodexRejectsExtraArguments(t *testing.T) {
	err := RunClientInstall([]string{"codex", "extra"}, strings.NewReader(""), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "usage") {
		t.Fatalf("expected usage error, got %v", err)
	}
}

// TestRunClientInstall_InteractiveCodexWritesProvider verifies the menu keeps all prompt input.
// 版本: v0.0.16
// 日期: 2026-09-09
func TestRunClientInstall_InteractiveCodexWritesProvider(t *testing.T) {
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	var output bytes.Buffer
	err := RunClientInstall(nil, strings.NewReader("1\nhttps://api.example.com/v1\ngpt-test\n"), &output)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(codexHome, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "https://api.example.com/v1") || !strings.Contains(output.String(), "gpt-test") {
		t.Fatalf("unexpected installer output or config: %s", output.String())
	}
}

// TestReplaceInstallBlock_ReplacesManagedBlock verifies reinstallation updates its owned block only.
// 版本: v0.0.16
// 日期: 2026-09-09
func TestReplaceInstallBlock_ReplacesManagedBlock(t *testing.T) {
	original := []byte("model = \"gpt-5\"\n" + installBlockStart + "\nold\n" + installBlockEnd + "\n[projects.\"/repo\"]\ntrust_level = \"trusted\"\n")
	updated, changed, err := replaceInstallBlock(original, []byte(installBlockStart+"\nnew\n"+installBlockEnd+"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !changed || strings.Contains(string(updated), "old") || !strings.Contains(string(updated), "new") || !strings.Contains(string(updated), "[projects.") {
		t.Fatalf("unexpected updated config: %s", updated)
	}
}

// TestReplaceInstallBlock_RejectsExistingProvider verifies an unmanaged provider is never overwritten.
// 版本: v0.0.16
// 日期: 2026-09-09
func TestReplaceInstallBlock_RejectsExistingProvider(t *testing.T) {
	_, _, err := replaceInstallBlock([]byte("[model_providers.one-api-pro]\nname = \"Custom\"\n"), []byte("new"))
	if err == nil || !strings.Contains(err.Error(), "already defines") {
		t.Fatalf("expected unmanaged provider error, got %v", err)
	}
}
