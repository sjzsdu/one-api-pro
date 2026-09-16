// claude.go 将内部 OpenAI 响应转换为 Anthropic Messages 响应
// Translate internal OpenAI-compatible responses into Anthropic Messages responses.
// 版本: v0.0.16
// 日期: 2026-09-11
// 作者: Codex
package controller

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/modelbus/one-api-pro/common"
	schema "github.com/modelbus/one-api-pro/relay/schema"
)

type claudeMessagesWriter struct {
	gin.ResponseWriter
	stream  bool
	buffer  bytes.Buffer
	written bool
	pending string
	state   claudeStreamState
}

type claudeStreamState struct {
	started    bool
	finished   bool
	id         string
	model      string
	finish     string
	usage      *schema.Usage
	textBlock  *claudeStreamBlock
	toolBlocks map[int]*claudeStreamBlock
	blockOrder []*claudeStreamBlock
}

type claudeStreamBlock struct {
	index int
	type_ string
	id    string
	name  string
}

type openAIChunk struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Delta        schema.Message `json:"delta"`
		Message      schema.Message `json:"message"`
		FinishReason string         `json:"finish_reason"`
	} `json:"choices"`
	Usage *schema.Usage `json:"usage,omitempty"`
}

type openAIResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Message      schema.Message `json:"message"`
		FinishReason string         `json:"finish_reason"`
	} `json:"choices"`
	Usage *schema.Usage `json:"usage,omitempty"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// RelayMessages adapts the public Anthropic Messages endpoint to the common
// relay pipeline and converts the result back to Anthropic's wire format.
// 版本: v0.0.16
// 日期: 2026-09-11
func RelayMessages(c *gin.Context) {
	body, err := common.GetRequestBody(c)
	if err != nil {
		c.JSON(400, gin.H{"type": "error", "error": gin.H{"type": "invalid_request_error", "message": err.Error()}})
		return
	}
	var request struct {
		Stream bool `json:"stream"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		c.JSON(400, gin.H{"type": "error", "error": gin.H{"type": "invalid_request_error", "message": err.Error()}})
		return
	}
	c.Writer = &claudeMessagesWriter{ResponseWriter: c.Writer, stream: request.Stream, state: claudeStreamState{toolBlocks: make(map[int]*claudeStreamBlock)}}
	c.Request.URL.Path = "/v1/chat/completions"
	Relay(c)
}

func (w *claudeMessagesWriter) Write(data []byte) (int, error) {
	if !w.stream {
		if w.written {
			return len(data), nil
		}
		w.buffer.Write(data)
		var response openAIResponse
		if json.Unmarshal(w.buffer.Bytes(), &response) != nil {
			return len(data), nil
		}
		converted, err := convertOpenAIResponseToClaude(response)
		if err != nil {
			return 0, err
		}
		w.ResponseWriter.Header().Set("Content-Type", "application/json")
		_, err = w.ResponseWriter.Write(converted)
		if err == nil {
			w.written = true
		}
		return len(data), err
	}

	w.pending += string(data)
	for {
		lineEnd := strings.IndexByte(w.pending, '\n')
		if lineEnd < 0 {
			break
		}
		line := strings.TrimSpace(w.pending[:lineEnd])
		w.pending = w.pending[lineEnd+1:]
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			if err := w.completeStream(); err != nil {
				return 0, err
			}
			continue
		}
		var chunk openAIChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		if err := w.writeChunk(chunk); err != nil {
			return 0, err
		}
	}
	return len(data), nil
}

func (w *claudeMessagesWriter) WriteString(data string) (int, error) {
	return w.Write([]byte(data))
}

func (w *claudeMessagesWriter) writeChunk(chunk openAIChunk) error {
	if chunk.ID != "" {
		w.state.id = "msg_" + strings.TrimPrefix(chunk.ID, "chatcmpl-")
	}
	if w.state.id == "" {
		w.state.id = fmt.Sprintf("msg_oneapi_%d", time.Now().UnixNano())
	}
	if chunk.Model != "" {
		w.state.model = chunk.Model
	}
	if chunk.Usage != nil {
		w.state.usage = chunk.Usage
	}
	if err := w.startStream(); err != nil {
		return err
	}
	for _, choice := range chunk.Choices {
		text := choice.Delta.StringContent()
		if text != "" {
			if w.state.textBlock == nil {
				block := &claudeStreamBlock{index: len(w.state.blockOrder), type_: "text"}
				w.state.textBlock = block
				w.state.blockOrder = append(w.state.blockOrder, block)
				if err := w.event("content_block_start", gin.H{"type": "content_block_start", "index": block.index, "content_block": gin.H{"type": "text", "text": ""}}); err != nil {
					return err
				}
			}
			if err := w.event("content_block_delta", gin.H{"type": "content_block_delta", "index": w.state.textBlock.index, "delta": gin.H{"type": "text_delta", "text": text}}); err != nil {
				return err
			}
		}
		for _, toolCall := range choice.Delta.ToolCalls {
			block := w.state.toolBlocks[toolCall.Index]
			if block == nil {
				block = &claudeStreamBlock{index: len(w.state.blockOrder), type_: "tool_use", id: toolCall.Id, name: toolCall.Function.Name}
				w.state.toolBlocks[toolCall.Index] = block
				w.state.blockOrder = append(w.state.blockOrder, block)
				if err := w.event("content_block_start", gin.H{"type": "content_block_start", "index": block.index, "content_block": gin.H{"type": "tool_use", "id": block.id, "name": block.name, "input": gin.H{}}}); err != nil {
					return err
				}
			}
			if block.id == "" {
				block.id = toolCall.Id
			}
			if block.name == "" {
				block.name = toolCall.Function.Name
			}
			arguments := toolArguments(toolCall.Function.Arguments)
			if arguments != "" {
				if err := w.event("content_block_delta", gin.H{"type": "content_block_delta", "index": block.index, "delta": gin.H{"type": "input_json_delta", "partial_json": arguments}}); err != nil {
					return err
				}
			}
		}
		if choice.FinishReason != "" {
			w.state.finish = choice.FinishReason
		}
	}
	return nil
}

func (w *claudeMessagesWriter) startStream() error {
	if w.state.started {
		return nil
	}
	w.state.started = true
	return w.event("message_start", gin.H{"type": "message_start", "message": gin.H{
		"id": w.state.id, "type": "message", "role": "assistant", "content": []any{}, "model": w.state.model,
		"stop_reason": nil, "stop_sequence": nil, "usage": gin.H{"input_tokens": 0, "output_tokens": 0},
	}})
}

func (w *claudeMessagesWriter) completeStream() error {
	if w.state.finished {
		return nil
	}
	if err := w.startStream(); err != nil {
		return err
	}
	for _, block := range w.state.blockOrder {
		if err := w.event("content_block_stop", gin.H{"type": "content_block_stop", "index": block.index}); err != nil {
			return err
		}
	}
	stopReason := "end_turn"
	if w.state.finish == "tool_calls" {
		stopReason = "tool_use"
	} else if w.state.finish == "length" {
		stopReason = "max_tokens"
	} else if w.state.finish == "stop_sequence" {
		stopReason = "stop_sequence"
	}
	outputTokens := 0
	if w.state.usage != nil {
		outputTokens = w.state.usage.CompletionTokens
	}
	if err := w.event("message_delta", gin.H{"type": "message_delta", "delta": gin.H{"stop_reason": stopReason, "stop_sequence": nil}, "usage": gin.H{"output_tokens": outputTokens}}); err != nil {
		return err
	}
	if err := w.event("message_stop", gin.H{"type": "message_stop"}); err != nil {
		return err
	}
	w.state.finished = true
	return nil
}

func (w *claudeMessagesWriter) event(name string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := w.ResponseWriter.Write([]byte("event: " + name + "\ndata: " + string(data) + "\n\n")); err != nil {
		return err
	}
	w.ResponseWriter.Flush()
	return nil
}

func convertOpenAIResponseToClaude(response openAIResponse) ([]byte, error) {
	if response.Error != nil {
		return json.Marshal(gin.H{"type": "error", "error": gin.H{"type": response.Error.Type, "message": response.Error.Message}})
	}
	var choice struct {
		Message      schema.Message `json:"message"`
		FinishReason string         `json:"finish_reason"`
	}
	if len(response.Choices) > 0 {
		choice = response.Choices[0]
	}
	content := make([]any, 0)
	if text := choice.Message.StringContent(); text != "" {
		content = append(content, gin.H{"type": "text", "text": text})
	}
	for _, toolCall := range choice.Message.ToolCalls {
		var input any
		arguments := toolArguments(toolCall.Function.Arguments)
		if arguments == "" {
			input = gin.H{}
		} else if json.Unmarshal([]byte(arguments), &input) != nil {
			input = gin.H{"input": arguments}
		}
		content = append(content, gin.H{"type": "tool_use", "id": toolCall.Id, "name": toolCall.Function.Name, "input": input})
	}
	stopReason := "end_turn"
	if choice.FinishReason == "tool_calls" {
		stopReason = "tool_use"
	} else if choice.FinishReason == "length" {
		stopReason = "max_tokens"
	}
	usage := gin.H{"input_tokens": 0, "output_tokens": 0}
	if response.Usage != nil {
		usage["input_tokens"] = response.Usage.PromptTokens
		usage["output_tokens"] = response.Usage.CompletionTokens
	}
	return json.Marshal(gin.H{
		"id": response.ID, "type": "message", "role": "assistant", "content": content,
		"model": response.Model, "stop_reason": stopReason, "stop_sequence": nil, "usage": usage,
	})
}

func toolArguments(arguments any) string {
	if arguments == nil {
		return ""
	}
	if text, ok := arguments.(string); ok {
		return text
	}
	data, err := json.Marshal(arguments)
	if err != nil {
		return ""
	}
	return string(data)
}
