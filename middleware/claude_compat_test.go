// claude_compat_test.go 验证 Anthropic Messages 请求标准化
// Test Anthropic Messages request normalization.
// 版本: v0.0.16
// 日期: 2026-09-11
// 作者: Codex
package middleware

import "testing"

func TestParseClaudeMessagesRequest_ToolUseAndResult(t *testing.T) {
	request, err := parseClaudeMessagesRequest([]byte(`{
		"model":"claude-test",
		"max_tokens":128,
		"system":"You are helpful",
		"tools":[{"name":"shell","description":"run a command","input_schema":{"type":"object","properties":{"command":{"type":"string"}}}}],
		"messages":[
			{"role":"user","content":"run pwd"},
			{"role":"assistant","content":[{"type":"tool_use","id":"call_1","name":"shell","input":{"command":"pwd"}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":"/tmp"}]}
		]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(request.Messages) != 4 {
		t.Fatalf("expected system, user, assistant, and tool messages, got %d", len(request.Messages))
	}
	if request.Messages[2].Role != "assistant" || len(request.Messages[2].ToolCalls) != 1 {
		t.Fatalf("assistant tool call was not preserved: %+v", request.Messages[2])
	}
	if request.Messages[2].ToolCalls[0].Id != "call_1" || request.Messages[2].ToolCalls[0].Function.Name != "shell" {
		t.Fatalf("unexpected tool call: %+v", request.Messages[2].ToolCalls[0])
	}
	if request.Messages[3].Role != "tool" || request.Messages[3].ToolCallId != "call_1" || request.Messages[3].Content != "/tmp" {
		t.Fatalf("tool result was not preserved: %+v", request.Messages[3])
	}
	if len(request.Tools) != 1 || request.Tools[0].Function.Name != "shell" {
		t.Fatalf("tool definition was not converted: %+v", request.Tools)
	}
}
