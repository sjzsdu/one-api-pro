package controller

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/modelbus/one-api-pro/common/render"
	relaymodel "github.com/modelbus/one-api-pro/relay/schema"
)

func TestConvertToolsToChatFormat(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantNames []string
		wantNil   bool
	}{
		{
			name:      "nil tools",
			input:     `null`,
			wantNil:   true,
			wantNames: nil,
		},
		{
			name:      "empty tools",
			input:     `[]`,
			wantNil:   true,
			wantNames: nil,
		},
		{
			name:      "Responses API format tools",
			input:     `[{"type":"function","name":"get_weather","description":"Get weather","parameters":{"type":"object"}}]`,
			wantNames: []string{"get_weather"},
			wantNil:   false,
		},
		{
			name:      "already in Chat Completions format",
			input:     `[{"type":"function","function":{"name":"get_weather","description":"Get weather"}}]`,
			wantNames: []string{"get_weather"},
			wantNil:   false,
		},
		{
			name:      "multiple tools",
			input:     `[{"type":"function","name":"foo","description":"Foo"},{"type":"function","name":"bar","description":"Bar"}]`,
			wantNames: []string{"foo", "bar"},
			wantNil:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tools []relaymodel.Tool
			var rawTools json.RawMessage = []byte(tt.input)
			if tt.input != "null" {
				if err := json.Unmarshal(rawTools, &tools); err != nil {
					t.Fatalf("failed to unmarshal input: %v", err)
				}
			}

			result := convertToolsToChatFormat(tools, rawTools)

			if tt.wantNil {
				if result != nil {
					t.Errorf("expected nil, got %+v", result)
				}
				return
			}

			if len(result) != len(tt.wantNames) {
				t.Errorf("expected %d tools, got %d", len(tt.wantNames), len(result))
				return
			}

			for i, name := range tt.wantNames {
				if result[i].Function.Name != name {
					t.Errorf("tool[%d]: expected name %q, got %q", i, name, result[i].Function.Name)
				}
				if result[i].Type != "function" {
					t.Errorf("tool[%d]: expected type 'function', got %q", i, result[i].Type)
				}
			}
		})
	}
}

// TestResponsesStreamWriter_EmitsCompletion verifies Codex receives the required terminal event.
// 版本: v0.0.16
// 日期: 2026-09-10
func TestResponsesStreamWriter_EmitsCompletion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Writer = newResponsesStreamWriter(context.Writer, "auto", nil)
	render.StringData(context, `{"id":"chatcmpl_1","model":"example-model","choices":[{"delta":{"content":"Hello"}}]}`)
	render.StringData(context, `{"id":"chatcmpl_1","model":"example-model","choices":[{"delta":{"content":" world"}}]}`)
	render.Done(context)

	body := response.Body.String()
	for _, event := range []string{"response.created", "response.output_text.delta", "response.output_text.done", "response.completed"} {
		if !strings.Contains(body, "event: "+event) {
			t.Fatalf("missing %s in stream: %s", event, body)
		}
	}
	if !strings.Contains(body, `"text":"Hello world"`) || strings.Contains(body, "[DONE]") {
		t.Fatalf("unexpected transformed stream: %s", body)
	}
}

// TestResponsesStreamWriter_ConvertsToolCalls verifies Codex receives function-call events.
// 版本: v0.0.16
// 日期: 2026-09-10
func TestResponsesStreamWriter_ConvertsToolCalls(t *testing.T) {
	gin.SetMode(gin.TestMode)
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Writer = newResponsesStreamWriter(context.Writer, "auto", nil)
	render.StringData(context, `{"id":"chatcmpl_1","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"shell","arguments":"{\"command\":"}}]}}]}`)
	render.StringData(context, `{"id":"chatcmpl_1","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"pwd\"}"}}]}}]}`)
	render.Done(context)

	body := response.Body.String()
	for _, event := range []string{"response.function_call_arguments.delta", "response.function_call_arguments.done", "response.output_item.done", "response.completed"} {
		if !strings.Contains(body, "event: "+event) {
			t.Fatalf("missing %s in stream: %s", event, body)
		}
	}
	if !strings.Contains(body, `"name":"shell"`) || !strings.Contains(body, `\"command\"`) {
		t.Fatalf("unexpected tool conversion: %s", body)
	}
}

// TestConvertResponsesTools_FlattensCodexNamespaces verifies portable Chat functions retain Codex identity.
// 版本: v0.0.16
// 日期: 2026-09-10
func TestConvertResponsesTools_FlattensCodexNamespaces(t *testing.T) {
	tools, mappings := convertResponsesTools(json.RawMessage(`[{"type":"namespace","name":"functions","tools":[{"type":"custom","name":"exec"},{"type":"namespace","name":"collaboration","tools":[{"type":"function","name":"send_message","parameters":{"type":"object"}}]}]}]`))
	if len(tools) != 2 || tools[0].Function.Name != "exec" || tools[1].Function.Name != "collaboration__send_message" {
		t.Fatalf("unexpected flattened tools: %+v", tools)
	}
	if !mappings["exec"].Custom || mappings["collaboration__send_message"].Namespace != "collaboration" {
		t.Fatalf("unexpected tool mappings: %+v", mappings)
	}
}

func TestConvertChatResponseToResponses_ToolCall(t *testing.T) {
	response := responsesChatResponse{ID: "chatcmpl-1", Model: "gpt-test"}
	response.Choices = append(response.Choices, struct {
		Message      relaymodel.Message `json:"message"`
		FinishReason string             `json:"finish_reason"`
	}{Message: relaymodel.Message{Role: "assistant", ToolCalls: []relaymodel.Tool{{
		Id: "call_1", Type: "function", Function: relaymodel.Function{Name: "shell", Arguments: `{"command":"pwd"}`},
	}}}, FinishReason: "tool_calls"})
	body, err := convertChatResponseToResponses(response, response.Model, nil)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Object string `json:"object"`
		Output []struct {
			Type      string `json:"type"`
			CallID    string `json:"call_id"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"output"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Object != "response" || len(decoded.Output) != 1 {
		t.Fatalf("unexpected Responses response: %s", body)
	}
	if decoded.Output[0].Type != "function_call" || decoded.Output[0].CallID != "call_1" || decoded.Output[0].Name != "shell" || decoded.Output[0].Arguments != `{"command":"pwd"}` {
		t.Fatalf("unexpected function call output: %+v", decoded.Output[0])
	}
}

func TestConvertResponsesInputToMessages_PreservesFunctionCallHistory(t *testing.T) {
	messages := convertResponsesInputToMessages(json.RawMessage(`[
		{"role":"user","content":"run pwd"},
		{"type":"function_call","id":"fc_1","call_id":"call_1","name":"shell","arguments":"{\"command\":\"pwd\"}"},
		{"type":"function_call_output","call_id":"call_1","output":"/tmp"}
	]`), "")
	if len(messages) != 3 {
		t.Fatalf("expected user, assistant function call, and tool result, got %d: %+v", len(messages), messages)
	}
	if messages[1].Role != "assistant" || len(messages[1].ToolCalls) != 1 || messages[1].ToolCalls[0].Id != "call_1" {
		t.Fatalf("function call history was not preserved: %+v", messages[1])
	}
	if messages[2].Role != "tool" || messages[2].ToolCallId != "call_1" || messages[2].Content != "/tmp" {
		t.Fatalf("function call output was not preserved: %+v", messages[2])
	}
}

// TestResponsesStreamWriter_RestoresCustomToolCall verifies Codex receives its custom-tool event shape.
// 版本: v0.0.16
// 日期: 2026-09-10
func TestResponsesStreamWriter_RestoresCustomToolCall(t *testing.T) {
	gin.SetMode(gin.TestMode)
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Writer = newResponsesStreamWriter(context.Writer, "auto", map[string]responseToolMapping{"exec": {Name: "exec", Custom: true}})
	render.StringData(context, `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_exec","function":{"name":"exec","arguments":"{\"input\":\"pwd\"}"}}]}}]}`)
	render.Done(context)
	body := response.Body.String()
	if !strings.Contains(body, `"type":"custom_tool_call"`) || !strings.Contains(body, "response.custom_tool_call_input.delta") || !strings.Contains(body, `"input":"pwd"`) {
		t.Fatalf("unexpected custom tool conversion: %s", body)
	}
}

func TestConvertResponsesInputToMessages(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		instructions string
		wantMessages int
		wantContent  string
	}{
		{
			name:         "simple string input",
			input:        `"Hello world"`,
			instructions: "",
			wantMessages: 1,
			wantContent:  "Hello world",
		},
		{
			name:         "with instructions",
			input:        `"Hello"`,
			instructions: "You are a helpful assistant",
			wantMessages: 2,
			wantContent:  "Hello",
		},
		{
			name:         "array with string content",
			input:        `[{"role":"user","content":"Hello"}]`,
			instructions: "",
			wantMessages: 1,
			wantContent:  "Hello",
		},
		{
			name:         "array with content blocks",
			input:        `[{"role":"user","content":[{"type":"input_text","text":"Hello world"}]}]`,
			instructions: "",
			wantMessages: 1,
			wantContent:  "Hello world",
		},
		{
			name:         "empty input gets Continue",
			input:        `""`,
			instructions: "",
			wantMessages: 0,
			wantContent:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var input json.RawMessage = []byte(tt.input)
			messages := convertResponsesInputToMessages(input, tt.instructions)

			if tt.wantMessages == 0 {
				// Empty input should return empty, caller adds "Continue."
				if len(messages) != 0 {
					t.Errorf("expected 0 messages, got %d", len(messages))
				}
				return
			}

			if len(messages) != tt.wantMessages {
				t.Errorf("expected %d messages, got %d: %+v", tt.wantMessages, len(messages), messages)
				return
			}

			// Check the last message (user message)
			lastMsg := messages[len(messages)-1]
			if lastMsg.Role != "user" {
				t.Errorf("expected role 'user', got %q", lastMsg.Role)
			}

			content, ok := lastMsg.Content.(string)
			if !ok {
				t.Errorf("expected string content, got %T", lastMsg.Content)
				return
			}

			if content != tt.wantContent {
				t.Errorf("expected content %q, got %q", tt.wantContent, content)
			}
		})
	}
}
