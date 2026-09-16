// claude_test.go 验证 Anthropic Messages 协议转换
// Test Anthropic Messages protocol conversion behavior.
// 版本: v0.0.16
// 日期: 2026-09-11
// 作者: Codex
package controller

import (
	"encoding/json"
	"testing"

	relaymodel "github.com/modelbus/one-api-pro/relay/schema"
)

func TestConvertOpenAIResponseToClaude_ToolUse(t *testing.T) {
	response := openAIResponse{ID: "chatcmpl-1", Model: "test-model"}
	response.Choices = append(response.Choices, struct {
		Message      relaymodel.Message `json:"message"`
		FinishReason string             `json:"finish_reason"`
	}{Message: relaymodel.Message{Role: "assistant", ToolCalls: []relaymodel.Tool{{
		Id: "call_1", Type: "function", Function: relaymodel.Function{Name: "shell", Arguments: `{"command":"pwd"}`},
	}}}, FinishReason: "tool_calls"})
	body, err := convertOpenAIResponseToClaude(response)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		StopReason string `json:"stop_reason"`
		Content    []struct {
			Type  string         `json:"type"`
			ID    string         `json:"id"`
			Name  string         `json:"name"`
			Input map[string]any `json:"input"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.StopReason != "tool_use" || len(decoded.Content) != 1 {
		t.Fatalf("unexpected Claude response: %s", body)
	}
	if decoded.Content[0].Type != "tool_use" || decoded.Content[0].ID != "call_1" || decoded.Content[0].Name != "shell" || decoded.Content[0].Input["command"] != "pwd" {
		t.Fatalf("unexpected tool block: %+v", decoded.Content[0])
	}
}
