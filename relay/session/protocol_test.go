// protocol_test.go Qwen 实时会话协议与预算边界测试
// Verifies pinned-model configuration and actual per-response usage parsing.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package session

import "testing"

func TestSupportedModel_ValidatedDateVersions(t *testing.T) {
	for name, want := range map[string]bool{
		"qwen3.5-omni-flash-realtime":            true,
		"qwen3.5-omni-plus-realtime-2026-11-23":  true,
		"qwen3.5-omni-flash-realtime-20261123":   true,
		"qwen3.5-omni-flash-realtime-20custom":   false,
		"qwen3.5-omni-flash-realtime-2026-02-30": false,
		"qwen3.5-omni-flash-realtime-custom":     false,
	} {
		if got := SupportedModel(name); got != want {
			t.Errorf("SupportedModel(%q)=%v, want %v", name, got, want)
		}
	}
}

func TestValidateClientEvent_ManualBoundedAndPinned(t *testing.T) {
	for _, raw := range []string{
		`{"type":"session.update","session":{"model":"other"}}`,
		`{"type":"session.update","session":{"voice":"unknown"}}`,
		`{"type":"session.update","session":{"max_tokens":4097}}`,
		`{"type":"session.update","session":{"turn_detection":{"type":"server_vad"}}}`,
		`{"type":"session.update","session":{"enable_search":true}}`,
		`{"type":"response.create","response":{"model":"other"}}`,
	} {
		if _, err := ValidateClientEvent([]byte(raw), "qwen3.5-omni-flash-realtime", 4096); err == nil {
			t.Errorf("accepted unsafe configuration: %s", raw)
		}
	}
	for _, raw := range []string{
		`{"type":"session.update","session":{"voice":"Ethan","max_tokens":1024,"turn_detection":null,"tools":[{"type":"function","function":{"name":"weather"}}]}}`,
		`{"type":"conversation.item.create","item":{"type":"function_call_output","call_id":"opaque-call","output":"ok"}}`,
		`{"type":"input_audio_buffer.append","audio":"AAAA"}`,
		`{"type":"response.create"}`,
		`{"type":"response.cancel"}`,
	} {
		if _, err := ValidateClientEvent([]byte(raw), "qwen3.5-omni-flash-realtime", 4096); err != nil {
			t.Errorf("valid Qwen event rejected: %s: %v", raw, err)
		}
	}
}

func TestParseCompletion_RequiresActualUsage(t *testing.T) {
	for _, raw := range []string{
		`{"type":"response.done","response":{"id":"r"}}`,
		`{"type":"response.done","response":{"id":"r","usage":{"input_tokens":1,"output_tokens":-1}}}`,
		`{"type":"response.done","response":{"usage":{"input_tokens":1,"output_tokens":2}}}`,
	} {
		if _, err := ParseCompletion([]byte(raw)); err == nil {
			t.Errorf("accepted missing or invalid accounting: %s", raw)
		}
	}
	c, err := ParseCompletion([]byte(`{"type":"response.done","response":{"id":"r1","usage":{"input_tokens":12,"output_tokens":4,"input_tokens_details":{"text_tokens":2,"audio_tokens":10},"output_tokens_details":{"audio_tokens":4}}}}`))
	if err != nil || c.ID != "r1" || c.InputTokens != 12 || c.OutputTokens != 4 || len(c.UsageJSON) == 0 {
		t.Fatalf("actual usage lost: %+v %v", c, err)
	}
}
