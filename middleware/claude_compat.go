// claude_compat.go 将 Anthropic Messages 请求标准化为内部 OpenAI 消息格式
// Normalize Anthropic Messages requests into the internal OpenAI-compatible schema.
// 版本: v0.0.16
// 日期: 2026-09-11
// 作者: Codex
package middleware

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/modelbus/one-api-pro/common"
	"github.com/modelbus/one-api-pro/common/ctxkey"
	relaymodel "github.com/modelbus/one-api-pro/relay/schema"
)

type claudeMessagesRequest struct {
	Model         string          `json:"model"`
	System        json.RawMessage `json:"system,omitempty"`
	Messages      []claudeMessage `json:"messages"`
	MaxTokens     int             `json:"max_tokens"`
	StopSequences []string        `json:"stop_sequences,omitempty"`
	Stream        bool            `json:"stream,omitempty"`
	Temperature   *float64        `json:"temperature,omitempty"`
	TopP          *float64        `json:"top_p,omitempty"`
	TopK          int             `json:"top_k,omitempty"`
	Tools         []claudeTool    `json:"tools,omitempty"`
	ToolChoice    json.RawMessage `json:"tool_choice,omitempty"`
}

type claudeMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type claudeTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type claudeContentBlock struct {
	Type      string             `json:"type"`
	Text      string             `json:"text,omitempty"`
	ID        string             `json:"id,omitempty"`
	Name      string             `json:"name,omitempty"`
	Input     json.RawMessage    `json:"input,omitempty"`
	ToolUseID string             `json:"tool_use_id,omitempty"`
	Content   json.RawMessage    `json:"content,omitempty"`
	Source    *claudeImageSource `json:"source,omitempty"`
}

type claudeImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
}

// NormalizeClaudeMessages rewrites the public Anthropic Messages payload into
// the canonical request consumed by the relay and model router.
// 版本: v0.0.16
// 日期: 2026-09-11
func NormalizeClaudeMessages() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.URL.Path != "/v1/messages" {
			c.Next()
			return
		}

		body, err := common.GetRequestBody(c)
		if err != nil {
			abortClaudeWithMessage(c, http.StatusBadRequest, "failed to read Anthropic Messages request")
			return
		}
		request, err := parseClaudeMessagesRequest(body)
		if err != nil {
			abortClaudeWithMessage(c, http.StatusBadRequest, err.Error())
			return
		}
		converted, err := json.Marshal(request)
		if err != nil {
			abortClaudeWithMessage(c, http.StatusInternalServerError, "failed to convert Anthropic Messages request")
			return
		}
		c.Set(ctxkey.KeyRequestBody, converted)
		c.Request.Body = io.NopCloser(bytes.NewReader(converted))
		c.Request.ContentLength = int64(len(converted))
		c.Next()
	}
}

func abortClaudeWithMessage(c *gin.Context, statusCode int, message string) {
	c.JSON(statusCode, gin.H{
		"type":  "error",
		"error": gin.H{"type": "invalid_request_error", "message": message},
	})
	c.Abort()
}

func parseClaudeMessagesRequest(body []byte) (*relaymodel.GeneralOpenAIRequest, error) {
	var request claudeMessagesRequest
	if err := json.Unmarshal(body, &request); err != nil {
		return nil, fmt.Errorf("invalid Anthropic Messages request: %w", err)
	}
	if request.Model == "" {
		return nil, fmt.Errorf("model is required")
	}
	if len(request.Messages) == 0 {
		return nil, fmt.Errorf("messages is required")
	}

	result := &relaymodel.GeneralOpenAIRequest{
		Model:       request.Model,
		MaxTokens:   request.MaxTokens,
		Stop:        request.StopSequences,
		Stream:      request.Stream,
		Temperature: request.Temperature,
		TopP:        request.TopP,
		TopK:        request.TopK,
		ToolChoice:  convertClaudeToolChoice(request.ToolChoice),
	}
	if system := claudeText(request.System); system != "" {
		result.Messages = append(result.Messages, relaymodel.Message{Role: "system", Content: system})
	}
	for _, tool := range request.Tools {
		var parameters any
		if len(tool.InputSchema) > 0 {
			if err := json.Unmarshal(tool.InputSchema, &parameters); err != nil {
				return nil, fmt.Errorf("invalid input_schema for tool %q: %w", tool.Name, err)
			}
		}
		result.Tools = append(result.Tools, relaymodel.Tool{Type: "function", Function: relaymodel.Function{
			Name: tool.Name, Description: tool.Description, Parameters: parameters,
		}})
	}
	for _, message := range request.Messages {
		if err := appendClaudeMessage(result, message); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func appendClaudeMessage(result *relaymodel.GeneralOpenAIRequest, message claudeMessage) error {
	role := strings.ToLower(strings.TrimSpace(message.Role))
	if role != "user" && role != "assistant" {
		return fmt.Errorf("unsupported Anthropic message role %q", message.Role)
	}
	var textParts []string
	var contentParts []any
	var toolCalls []relaymodel.Tool
	var toolResults []relaymodel.Message

	var text string
	if json.Unmarshal(message.Content, &text) == nil {
		if text != "" {
			textParts = append(textParts, text)
		}
	} else {
		var blocks []claudeContentBlock
		if err := json.Unmarshal(message.Content, &blocks); err != nil {
			return fmt.Errorf("invalid content for %s message: %w", role, err)
		}
		for _, block := range blocks {
			switch block.Type {
			case "text":
				if block.Text != "" {
					textParts = append(textParts, block.Text)
				}
			case "image":
				if block.Source == nil {
					continue
				}
				url := block.Source.URL
				if block.Source.Type == "base64" && block.Source.Data != "" {
					url = "data:" + block.Source.MediaType + ";base64," + block.Source.Data
				}
				if url != "" {
					contentParts = append(contentParts, map[string]any{
						"type": "image_url", "image_url": map[string]any{"url": url},
					})
				}
			case "tool_use":
				if role != "assistant" {
					continue
				}
				var input any
				if len(block.Input) > 0 {
					if err := json.Unmarshal(block.Input, &input); err != nil {
						return fmt.Errorf("invalid input for tool %q: %w", block.Name, err)
					}
				}
				toolCalls = append(toolCalls, relaymodel.Tool{Id: block.ID, Type: "function", Function: relaymodel.Function{
					Name: block.Name, Arguments: input,
				}})
			case "tool_result":
				if role != "user" {
					continue
				}
				toolResults = append(toolResults, relaymodel.Message{
					Role: "tool", ToolCallId: block.ToolUseID, Content: claudeBlockText(block.Content),
				})
			}
		}
	}

	content := strings.Join(textParts, "\n")
	if len(contentParts) > 0 {
		if content != "" {
			contentParts = append([]any{map[string]any{"type": "text", "text": content}}, contentParts...)
		}
		result.Messages = append(result.Messages, relaymodel.Message{Role: role, Content: contentParts, ToolCalls: toolCalls})
	} else if content != "" || len(toolCalls) > 0 {
		result.Messages = append(result.Messages, relaymodel.Message{Role: role, Content: content, ToolCalls: toolCalls})
	}
	result.Messages = append(result.Messages, toolResults...)
	return nil
}

func claudeBlockText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var blocks []claudeContentBlock
	if json.Unmarshal(raw, &blocks) == nil {
		var parts []string
		for _, block := range blocks {
			if block.Type == "text" && block.Text != "" {
				parts = append(parts, block.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return string(raw)
}

func claudeText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	return claudeBlockText(raw)
}

func convertClaudeToolChoice(raw json.RawMessage) any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var choice struct {
		Type string `json:"type"`
		Name string `json:"name,omitempty"`
	}
	if json.Unmarshal(raw, &choice) != nil {
		return nil
	}
	switch choice.Type {
	case "auto":
		return "auto"
	case "none":
		return "none"
	case "any":
		return "required"
	case "tool":
		return map[string]any{"type": "function", "function": map[string]any{"name": choice.Name}}
	default:
		return nil
	}
}
