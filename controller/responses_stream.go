// responses_stream.go 将 Chat Completions 流转换为 Responses API SSE 事件。
// Translates Chat Completions streams into Responses API SSE events.
// 版本: v0.0.16
// 日期: 2026-09-10
// 作者: Codex
package controller

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// responsesStreamWriter converts the Chat Completions SSE written by relay adaptors.
type responsesStreamWriter struct {
	gin.ResponseWriter
	model        string
	responseID   string
	messageID    string
	started      bool
	textStarted  bool
	text         strings.Builder
	toolCalls    map[int]*responsesToolCall
	toolMappings map[string]responseToolMapping
}

type responsesToolCall struct {
	index     int
	itemID    string
	callID    string
	name      string
	namespace string
	custom    bool
	arguments strings.Builder
}

type chatCompletionChunk struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Delta struct {
			Content   json.RawMessage `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
}

func newResponsesStreamWriter(writer gin.ResponseWriter, model string, toolMappings map[string]responseToolMapping) *responsesStreamWriter {
	identifier := fmt.Sprintf("resp_oneapi_%d", time.Now().UnixNano())
	return &responsesStreamWriter{
		ResponseWriter: writer,
		model:          model,
		responseID:     identifier,
		messageID:      "msg_" + identifier,
		toolCalls:      make(map[int]*responsesToolCall),
		toolMappings:   toolMappings,
	}
}

func (w *responsesStreamWriter) Write(data []byte) (int, error) {
	originalLength := len(data)
	text := string(data)
	if !strings.HasPrefix(text, "data: ") {
		return w.ResponseWriter.Write(data)
	}

	payload := strings.TrimSpace(strings.TrimPrefix(text, "data: "))
	if payload == "[DONE]" {
		if err := w.complete(); err != nil {
			return 0, err
		}
		return originalLength, nil
	}

	var chunk chatCompletionChunk
	if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
		// Keep malformed provider events observable instead of silently dropping
		// them. A later [DONE] still emits response.completed for Codex.
		return w.ResponseWriter.Write(data)
	}
	if chunk.Model != "" {
		w.model = chunk.Model
	}
	if chunk.ID != "" {
		w.responseID = "resp_" + chunk.ID
		w.messageID = "msg_" + chunk.ID
	}
	w.start()
	for _, choice := range chunk.Choices {
		delta := responseText(choice.Delta.Content)
		if delta != "" {
			w.startText()
			w.text.WriteString(delta)
			if err := w.event("response.output_text.delta", gin.H{
				"type":          "response.output_text.delta",
				"item_id":       w.messageID,
				"output_index":  0,
				"content_index": 0,
				"delta":         delta,
			}); err != nil {
				return 0, err
			}
		}
		for _, toolCall := range choice.Delta.ToolCalls {
			if err := w.toolDelta(toolCall.Index, toolCall.ID, toolCall.Function.Name, toolCall.Function.Arguments); err != nil {
				return 0, err
			}
		}
	}
	return originalLength, nil
}

func (w *responsesStreamWriter) toolDelta(index int, callID, name, arguments string) error {
	toolCall, found := w.toolCalls[index]
	if !found {
		if callID == "" {
			callID = fmt.Sprintf("call_oneapi_%d", index)
		}
		mapping, mapped := w.toolMappings[name]
		if mapped {
			name = mapping.Name
		}
		prefix := "fc_"
		if mapped && mapping.Custom {
			prefix = "ctc_"
		}
		toolCall = &responsesToolCall{index: index, itemID: prefix + callID, callID: callID, name: name}
		if mapped {
			toolCall.namespace, toolCall.custom = mapping.Namespace, mapping.Custom
		}
		w.toolCalls[index] = toolCall
		itemType, argumentName := "function_call", "arguments"
		if toolCall.custom {
			itemType, argumentName = "custom_tool_call", "input"
		}
		item := gin.H{"id": toolCall.itemID, "type": itemType, "status": "in_progress", "call_id": callID, "name": name, argumentName: ""}
		if toolCall.namespace != "" {
			item["namespace"] = toolCall.namespace
		}
		if err := w.event("response.output_item.added", gin.H{
			"type": "response.output_item.added", "output_index": index,
			"item": item,
		}); err != nil {
			return err
		}
	}
	if callID != "" {
		toolCall.callID = callID
	}
	if name != "" {
		toolCall.name = name
	}
	if arguments == "" {
		return nil
	}
	toolCall.arguments.WriteString(arguments)
	if toolCall.custom {
		return nil
	}
	return w.event("response.function_call_arguments.delta", gin.H{
		"type": "response.function_call_arguments.delta", "item_id": toolCall.itemID, "output_index": toolCall.index, "delta": arguments,
	})
}

func (w *responsesStreamWriter) WriteString(data string) (int, error) {
	return w.Write([]byte(data))
}

func (w *responsesStreamWriter) start() {
	if w.started {
		return
	}
	w.started = true
	_ = w.event("response.created", gin.H{
		"type": "response.created",
		"response": gin.H{
			"id":         w.responseID,
			"object":     "response",
			"created_at": time.Now().Unix(),
			"status":     "in_progress",
			"model":      w.model,
			"output":     []any{},
		},
	})
}

func (w *responsesStreamWriter) startText() {
	if w.textStarted {
		return
	}
	w.textStarted = true
	_ = w.event("response.output_item.added", gin.H{
		"type":         "response.output_item.added",
		"output_index": 0,
		"item": gin.H{
			"id":      w.messageID,
			"type":    "message",
			"status":  "in_progress",
			"role":    "assistant",
			"content": []any{},
		},
	})
	_ = w.event("response.content_part.added", gin.H{
		"type":          "response.content_part.added",
		"item_id":       w.messageID,
		"output_index":  0,
		"content_index": 0,
		"part": gin.H{
			"type":        "output_text",
			"text":        "",
			"annotations": []any{},
		},
	})
}

func (w *responsesStreamWriter) complete() error {
	w.start()
	var output []any
	if w.textStarted {
		text := w.text.String()
		if err := w.event("response.output_text.done", gin.H{
			"type":          "response.output_text.done",
			"item_id":       w.messageID,
			"output_index":  0,
			"content_index": 0,
			"text":          text,
		}); err != nil {
			return err
		}
		part := gin.H{"type": "output_text", "text": text, "annotations": []any{}}
		item := gin.H{"id": w.messageID, "type": "message", "status": "completed", "role": "assistant", "content": []any{part}}
		if err := w.event("response.content_part.done", gin.H{
			"type": "response.content_part.done", "item_id": w.messageID, "output_index": 0, "content_index": 0, "part": part,
		}); err != nil {
			return err
		}
		if err := w.event("response.output_item.done", gin.H{"type": "response.output_item.done", "output_index": 0, "item": item}); err != nil {
			return err
		}
		output = append(output, item)
	}
	for _, toolCall := range w.sortedToolCalls() {
		arguments := toolCall.arguments.String()
		input := ""
		if toolCall.custom {
			input = customToolInput(arguments)
			if input != "" {
				if err := w.event("response.custom_tool_call_input.delta", gin.H{"type": "response.custom_tool_call_input.delta", "item_id": toolCall.itemID, "call_id": toolCall.callID, "output_index": toolCall.index, "delta": input}); err != nil {
					return err
				}
			}
		} else if err := w.event("response.function_call_arguments.done", gin.H{
			"type": "response.function_call_arguments.done", "item_id": toolCall.itemID, "output_index": toolCall.index, "arguments": arguments,
		}); err != nil {
			return err
		}
		itemType, argumentName, value := "function_call", "arguments", arguments
		if toolCall.custom {
			itemType, argumentName, value = "custom_tool_call", "input", input
		}
		item := gin.H{"id": toolCall.itemID, "type": itemType, "status": "completed", "call_id": toolCall.callID, "name": toolCall.name, argumentName: value}
		if toolCall.namespace != "" {
			item["namespace"] = toolCall.namespace
		}
		if err := w.event("response.output_item.done", gin.H{"type": "response.output_item.done", "output_index": toolCall.index, "item": item}); err != nil {
			return err
		}
		output = append(output, item)
	}
	return w.event("response.completed", gin.H{
		"type": "response.completed",
		"response": gin.H{
			"id":           w.responseID,
			"object":       "response",
			"created_at":   time.Now().Unix(),
			"completed_at": time.Now().Unix(),
			"status":       "completed",
			"model":        w.model,
			"output":       output,
			"error":        nil,
		},
	})
}

func customToolInput(arguments string) string {
	var wrapper struct {
		Input string `json:"input"`
	}
	if json.Unmarshal([]byte(arguments), &wrapper) != nil {
		return ""
	}
	return wrapper.Input
}

func (w *responsesStreamWriter) sortedToolCalls() []*responsesToolCall {
	result := make([]*responsesToolCall, 0, len(w.toolCalls))
	for _, toolCall := range w.toolCalls {
		result = append(result, toolCall)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].index < result[j].index })
	return result
}

func (w *responsesStreamWriter) event(name string, body any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	_, err = w.ResponseWriter.Write([]byte("event: " + name + "\ndata: " + string(payload) + "\n\n"))
	return err
}

func responseText(raw json.RawMessage) string {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var parts []struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err != nil {
		return ""
	}
	var builder strings.Builder
	for _, part := range parts {
		builder.WriteString(part.Text)
	}
	return builder.String()
}
