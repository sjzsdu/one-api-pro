// client_install.go 配置 AI 编程客户端使用 One API Pro。
// Configures AI coding clients to use One API Pro.
// 版本: v0.0.17
// 日期: 2026-09-09
// 作者: Codex
package common

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	codexProviderName = "one-api-pro"
	defaultCodexURL   = "http://127.0.0.1:3000/v1"
	defaultCodexModel = "gpt-5.3-codex"
	installBlockStart = "# BEGIN one-api-pro codex"
	installBlockEnd   = "# END one-api-pro codex"
)

// Claude Code configuration constants
const (
	defaultClaudeURL   = "http://127.0.0.1:3000"
	defaultClaudeModel = "claude-sonnet-4-20250514"
	claudeSettingsFile = "settings.json"
)

// Gemini CLI configuration constants
const (
	defaultGeminiURL   = "http://127.0.0.1:3000"
	defaultGeminiModel = "gemini-2.0-flash"
	geminiSettingsFile = "settings.json"
)

// RunClientInstall installs the requested client integration or presents a client selector.
// 版本: v0.0.17
// 日期: 2026-09-09
func RunClientInstall(args []string, input io.Reader, output io.Writer) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: one-api-pro install [codex|claude|gemini]")
	}
	reader := bufio.NewReader(input)
	target := ""
	if len(args) == 1 {
		target = strings.ToLower(args[0])
	}
	if target == "" {
		fmt.Fprintln(output, "Select a client to configure:")
		fmt.Fprintln(output, "  1) Codex")
		fmt.Fprintln(output, "  2) Claude Code")
		fmt.Fprintln(output, "  3) Gemini CLI")
		fmt.Fprint(output, "Choice [1]: ")
		choice, err := readInstallValue(reader)
		if err != nil {
			return err
		}
		switch choice {
		case "", "1", "codex":
			target = "codex"
		case "2", "claude":
			target = "claude"
		case "3", "gemini":
			target = "gemini"
		default:
			return fmt.Errorf("unsupported client %q (currently supported: codex, claude, gemini)", choice)
		}
	}
	switch target {
	case "codex":
		return installCodex(reader, output)
	case "claude":
		return installClaude(reader, output)
	case "gemini":
		return installGemini(reader, output)
	default:
		return fmt.Errorf("unsupported client %q (currently supported: codex, claude, gemini)", target)
	}
}

func installCodex(input io.Reader, output io.Writer) error {
	if os.Getenv("CODEX_HOME") == "" {
		fmt.Fprintln(output, "Configuring Codex in ~/.codex. Set CODEX_HOME to use another directory.")
	}
	baseURL, err := promptInstallValue(input, output, "One API Pro URL", defaultCodexURL)
	if err != nil {
		return err
	}
	model, err := promptInstallValue(input, output, "Default model", defaultCodexModel)
	if err != nil {
		return err
	}
	codexHome, err := getCodexHome()
	if err != nil {
		return err
	}
	configPath := filepath.Join(codexHome, "config.toml")
	if err := writeCodexConfig(configPath, baseURL, model); err != nil {
		return err
	}
	fmt.Fprintf(output, "Codex provider installed: %s\n", configPath)
	fmt.Fprintln(output, "Set your One API Pro token before starting Codex:")
	fmt.Fprintln(output, "  export ONE_API_PRO_API_KEY='your-token'")
	fmt.Fprintf(output, "  codex -c model_provider='\"%s\"' -c model='\"%s\"'\n", codexProviderName, model)
	return nil
}

func installClaude(input io.Reader, output io.Writer) error {
	fmt.Fprintln(output, "Configuring Claude Code to use One API Pro.")
	fmt.Fprintln(output, "This will set environment variables in your shell profile.")
	fmt.Fprintln(output, "")
	
	baseURL, err := promptInstallValue(input, output, "One API Pro URL", defaultClaudeURL)
	if err != nil {
		return err
	}
	model, err := promptInstallValue(input, output, "Default model", defaultClaudeModel)
	if err != nil {
		return err
	}
	
	// Determine shell profile
	shell := os.Getenv("SHELL")
	profilePath := ""
	if strings.Contains(shell, "zsh") {
		profilePath = filepath.Join(os.Getenv("HOME"), ".zshrc")
	} else if strings.Contains(shell, "bash") {
		profilePath = filepath.Join(os.Getenv("HOME"), ".bashrc")
	} else {
		profilePath = filepath.Join(os.Getenv("HOME"), ".profile")
	}
	
	fmt.Fprintf(output, "Shell profile: %s\n", profilePath)
	fmt.Fprintln(output, "")
	fmt.Fprintln(output, "Add the following lines to your shell profile:")
	fmt.Fprintln(output, "")
	fmt.Fprintf(output, "  export ANTHROPIC_BASE_URL='%s'\n", baseURL)
	fmt.Fprintf(output, "  export ANTHROPIC_AUTH_TOKEN='your-token'\n")
	fmt.Fprintf(output, "  export ANTHROPIC_MODEL='%s'\n", model)
	fmt.Fprintln(output, "")
	fmt.Fprintln(output, "Or add to ~/.claude/settings.json:")
	fmt.Fprintln(output, "")
	
	settings := map[string]interface{}{
		"env": map[string]string{
			"ANTHROPIC_BASE_URL":   baseURL,
			"ANTHROPIC_AUTH_TOKEN": "your-token",
			"ANTHROPIC_MODEL":      model,
		},
	}
	settingsJSON, _ := json.MarshalIndent(settings, "", "  ")
	fmt.Fprintf(output, "  %s\n", string(settingsJSON))
	fmt.Fprintln(output, "")
	fmt.Fprintln(output, "After setting the environment variables, restart Claude Code.")
	return nil
}

func installGemini(input io.Reader, output io.Writer) error {
	fmt.Fprintln(output, "Configuring Gemini CLI to use One API Pro.")
	fmt.Fprintln(output, "This will create/update ~/.gemini/settings.json")
	fmt.Fprintln(output, "")
	
	baseURL, err := promptInstallValue(input, output, "One API Pro URL", defaultGeminiURL)
	if err != nil {
		return err
	}
	model, err := promptInstallValue(input, output, "Default model", defaultGeminiModel)
	if err != nil {
		return err
	}
	
	// Get or create Gemini settings directory
	geminiHome, err := getGeminiHome()
	if err != nil {
		return err
	}
	
	settingsPath := filepath.Join(geminiHome, geminiSettingsFile)
	if err := writeGeminiConfig(settingsPath, baseURL, model); err != nil {
		return err
	}
	
	fmt.Fprintf(output, "Gemini CLI settings updated: %s\n", settingsPath)
	fmt.Fprintln(output, "")
	fmt.Fprintln(output, "Set your API key before starting Gemini CLI:")
	fmt.Fprintf(output, "  export GEMINI_API_KEY='your-api-key'\n")
	fmt.Fprintf(output, "  gemini\n")
	return nil
}

func getGeminiHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".gemini"), nil
}

func writeGeminiConfig(path, baseURL, model string) error {
	if strings.TrimSpace(baseURL) == "" || strings.TrimSpace(model) == "" {
		return fmt.Errorf("One API Pro URL and default model cannot be empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create Gemini configuration directory: %w", err)
	}
	
	// Read existing settings
	original, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read Gemini configuration: %w", err)
	}
	
	// Parse existing settings or create new
	var settings map[string]interface{}
	if len(original) > 0 {
		if err := json.Unmarshal(original, &settings); err != nil {
			return fmt.Errorf("parse Gemini configuration: %w", err)
		}
	} else {
		settings = make(map[string]interface{})
	}
	
	// Update settings
	if settings["apiKey"] == nil {
		settings["apiKey"] = "$GEMINI_API_KEY"
	}
	settings["baseUrl"] = baseURL
	if settings["model"] == nil {
		settings["model"] = model
	}
	
	// Backup original
	if len(original) > 0 {
		backup := fmt.Sprintf("%s.one-api-pro-%s.bak", path, time.Now().Format("20060102-150405"))
		if err := os.WriteFile(backup, original, 0600); err != nil {
			return fmt.Errorf("back up Gemini configuration: %w", err)
		}
	}
	
	// Write updated settings
	updated, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal Gemini configuration: %w", err)
	}
	if err := os.WriteFile(path, updated, 0600); err != nil {
		return fmt.Errorf("write Gemini configuration: %w", err)
	}
	return nil
}

func getCodexHome() (string, error) {
	if codexHome := os.Getenv("CODEX_HOME"); codexHome != "" {
		return codexHome, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".codex"), nil
}

func promptInstallValue(input io.Reader, output io.Writer, label, defaultValue string) (string, error) {
	fmt.Fprintf(output, "%s [%s]: ", label, defaultValue)
	value, err := readInstallValue(input)
	if err != nil {
		return "", err
	}
	if value == "" {
		return defaultValue, nil
	}
	return value, nil
}

func readInstallValue(input io.Reader) (string, error) {
	reader, ok := input.(*bufio.Reader)
	if !ok {
		reader = bufio.NewReader(input)
	}
	value, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("read input: %w", err)
	}
	return strings.TrimSpace(value), nil
}

func writeCodexConfig(path, baseURL, model string) error {
	if strings.TrimSpace(baseURL) == "" || strings.TrimSpace(model) == "" {
		return fmt.Errorf("One API Pro URL and default model cannot be empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create Codex configuration directory: %w", err)
	}
	original, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read Codex configuration: %w", err)
	}
	updated, changed, err := replaceInstallBlock(original, codexConfigBlock(baseURL))
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	if len(original) > 0 {
		backup := fmt.Sprintf("%s.one-api-pro-%s.bak", path, time.Now().Format("20060102-150405"))
		if err := os.WriteFile(backup, original, 0600); err != nil {
			return fmt.Errorf("back up Codex configuration: %w", err)
		}
	}
	if err := os.WriteFile(path, updated, 0600); err != nil {
		return fmt.Errorf("write Codex configuration: %w", err)
	}
	return nil
}

func codexConfigBlock(baseURL string) []byte {
	return []byte(fmt.Sprintf("%s\n# Managed by one-api-pro install codex. It only adds a provider and does not change Codex defaults.\n[model_providers.%s]\nname = \"One API Pro\"\nbase_url = %q\nenv_key = \"ONE_API_PRO_API_KEY\"\nwire_api = \"responses\"\n%s\n", installBlockStart, codexProviderName, baseURL, installBlockEnd))
}

func replaceInstallBlock(original, block []byte) ([]byte, bool, error) {
	start := bytes.Index(original, []byte(installBlockStart))
	if start >= 0 {
		end := bytes.Index(original[start:], []byte(installBlockEnd))
		if end < 0 {
			return nil, false, fmt.Errorf("Codex configuration has an incomplete %s block", installBlockStart)
		}
		end += start + len(installBlockEnd)
		return append(append([]byte{}, original[:start]...), append(block, original[end:]...)...), true, nil
	}
	if bytes.Contains(original, []byte("[model_providers."+codexProviderName+"]")) {
		return nil, false, fmt.Errorf("Codex configuration already defines provider %q; add the managed block manually or rename the provider", codexProviderName)
	}
	if len(original) == 0 {
		return block, true, nil
	}
	return append(append(append([]byte{}, original...), '\n'), block...), true, nil
}
