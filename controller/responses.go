package controller

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/modelbus/one-api-pro/common"
	"github.com/modelbus/one-api-pro/common/ctxkey"
	"github.com/modelbus/one-api-pro/common/logger"
	relaymodel "github.com/modelbus/one-api-pro/relay/schema"
)

type ResponsesRequest struct {
	Model             string          `json:"model"`
	Input             json.RawMessage `json:"input"`
	Instructions      string          `json:"instructions,omitempty"`
	Stream            bool            `json:"stream,omitempty"`
	MaxOutputTokens   int             `json:"max_output_tokens,omitempty"`
	Temperature       *float64        `json:"temperature,omitempty"`
	TopP              *float64        `json:"top_p,omitempty"`
	Tools             json.RawMessage `json:"tools,omitempty"`
	ToolChoice        any             `json:"tool_choice,omitempty"`
	ParallelToolCalls *bool           `json:"parallel_tool_calls,omitempty"`
	Reasoning         *Reasoning      `json:"reasoning,omitempty"`
}

type Reasoning struct {
	Effort string `json:"effort,omitempty"`
}

type responsesJSONWriter struct {
	gin.ResponseWriter
	model        string
	toolMappings map[string]responseToolMapping
	buffer       bytes.Buffer
	written      bool
}

type responsesChatResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Message      relaymodel.Message `json:"message"`
		FinishReason string             `json:"finish_reason"`
	} `json:"choices"`
	Usage *relaymodel.Usage `json:"usage,omitempty"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func newResponsesJSONWriter(writer gin.ResponseWriter, model string, toolMappings map[string]responseToolMapping) *responsesJSONWriter {
	return &responsesJSONWriter{ResponseWriter: writer, model: model, toolMappings: toolMappings}
}

func (w *responsesJSONWriter) Write(data []byte) (int, error) {
	if w.written {
		return len(data), nil
	}
	w.buffer.Write(data)
	var response responsesChatResponse
	if err := json.Unmarshal(w.buffer.Bytes(), &response); err != nil {
		return len(data), nil
	}
	converted, err := convertChatResponseToResponses(response, w.model, w.toolMappings)
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

func (w *responsesJSONWriter) WriteString(data string) (int, error) {
	return w.Write([]byte(data))
}

func convertChatResponseToResponses(response responsesChatResponse, model string, mappings map[string]responseToolMapping) ([]byte, error) {
	if response.Error != nil {
		return json.Marshal(gin.H{"error": gin.H{"type": response.Error.Type, "message": response.Error.Message}})
	}
	if model == "" {
		model = response.Model
	}
	id := response.ID
	if id == "" {
		id = "resp_oneapi_" + time.Now().Format("20060102150405.000000000")
	}
	output := make([]any, 0)
	finishReason := "stop"
	if len(response.Choices) > 0 {
		choice := response.Choices[0]
		if choice.FinishReason != "" {
			finishReason = choice.FinishReason
		}
		if text := choice.Message.StringContent(); text != "" {
			output = append(output, gin.H{
				"id": "msg_" + id, "type": "message", "status": "completed", "role": "assistant",
				"content": []any{gin.H{"type": "output_text", "text": text, "annotations": []any{}}},
			})
		}
		for _, toolCall := range choice.Message.ToolCalls {
			name := toolCall.Function.Name
			mapping := mappings[name]
			callID := toolCall.Id
			if mapping.Custom {
				input := toolArguments(toolCall.Function.Arguments)
				output = append(output, gin.H{
					"id": "ctc_" + callID, "type": "custom_tool_call", "status": "completed", "call_id": callID,
					"name": mapping.Name, "namespace": mapping.Namespace, "input": customToolInput(input),
				})
				continue
			}
			if mapping.Name != "" {
				name = mapping.Name
			}
			item := gin.H{
				"id": "fc_" + callID, "type": "function_call", "status": "completed", "call_id": callID,
				"name": name, "arguments": toolArguments(toolCall.Function.Arguments),
			}
			if mapping.Namespace != "" {
				item["namespace"] = mapping.Namespace
			}
			output = append(output, item)
		}
	}
	status := "completed"
	if finishReason == "error" {
		status = "failed"
	}
	usage := gin.H{}
	if response.Usage != nil {
		usage = gin.H{
			"input_tokens": response.Usage.PromptTokens, "output_tokens": response.Usage.CompletionTokens,
			"total_tokens": response.Usage.TotalTokens,
		}
	}
	return json.Marshal(gin.H{
		"id": id, "object": "response", "created_at": time.Now().Unix(), "status": status,
		"model": model, "output": output, "usage": usage,
	})
}

type rawInputItem struct {
	Type      string          `json:"type,omitempty"`
	Role      string          `json:"role,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	ID        string          `json:"id,omitempty"`
	CallID    string          `json:"call_id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Arguments string          `json:"arguments,omitempty"`
	Output    json.RawMessage `json:"output,omitempty"`
}

func RelayResponses(c *gin.Context) {
	body, err := common.GetRequestBody(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": "Failed to read request body", "type": "invalid_request_error"},
		})
		return
	}

	var req ResponsesRequest
	if err := json.Unmarshal(body, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": "Invalid request body", "type": "invalid_request_error"},
		})
		return
	}

	messages := convertResponsesInputToMessages(req.Input, req.Instructions)

	logger.Debugf(c.Request.Context(), "responses conversion: %d messages from input", len(messages))
	if len(messages) == 0 {
		logger.Warnf(c.Request.Context(), "responses conversion produced empty messages, input: %s", string(req.Input))
		messages = append(messages, relaymodel.Message{Role: "user", Content: "Continue."})
	} else {
		for i, msg := range messages {
			logger.Debugf(c.Request.Context(), "responses message[%d]: role=%s, content=%v", i, msg.Role, msg.Content)
		}
	}

	tools, toolMappings := convertResponsesTools(req.Tools)

	chatReq := relaymodel.GeneralOpenAIRequest{
		Model:            req.Model,
		Messages:         messages,
		Stream:           req.Stream,
		Temperature:      req.Temperature,
		TopP:             req.TopP,
		Tools:            tools,
		ToolChoice:       convertResponsesToolChoice(req.ToolChoice),
		ParallelTooCalls: req.ParallelToolCalls,
	}

	if req.MaxOutputTokens > 0 {
		chatReq.MaxTokens = req.MaxOutputTokens
	}

	if req.Reasoning != nil && req.Reasoning.Effort != "" {
		chatReq.ReasoningEffort = &req.Reasoning.Effort
	}
	if req.Stream {
		// Downstream adaptors emit Chat Completions SSE. Codex uses the Responses
		// event protocol and treats a stream that ends without response.completed
		// as a transport failure, so translate the rendered event stream here.
		c.Writer = newResponsesStreamWriter(c.Writer, req.Model, toolMappings)
	} else {
		c.Writer = newResponsesJSONWriter(c.Writer, req.Model, toolMappings)
	}

	convertedBody, err := json.Marshal(chatReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"message": "Failed to convert request", "type": "internal_error"},
		})
		return
	}

	logger.Debugf(c.Request.Context(), "responses -> chat completions: %s", string(convertedBody))

	c.Request.Body = io.NopCloser(bytes.NewBuffer(convertedBody))
	c.Request.ContentLength = int64(len(convertedBody))
	c.Set(ctxkey.KeyRequestBody, convertedBody)
	c.Request.URL.Path = "/v1/chat/completions"

	Relay(c)
}

func convertResponsesToolChoice(choice any) any {
	switch value := choice.(type) {
	case string:
		return value
	case map[string]any:
		if value["type"] == "function" {
			if name, ok := value["name"].(string); ok {
				return map[string]any{"type": "function", "function": map[string]any{"name": name}}
			}
		}
	}
	return choice
}

// responseToolMapping records how a flattened Chat function maps to Codex's
// Responses namespace and custom-tool protocol.
type responseToolMapping struct {
	Name      string
	Namespace string
	Custom    bool
}

func convertResponsesTools(rawTools json.RawMessage) ([]relaymodel.Tool, map[string]responseToolMapping) {
	mappings := make(map[string]responseToolMapping)
	if len(rawTools) == 0 || string(rawTools) == "null" {
		return nil, mappings
	}
	var items []json.RawMessage
	if err := json.Unmarshal(rawTools, &items); err != nil {
		return nil, mappings
	}
	return flattenResponsesTools(items, "", mappings), mappings
}

func flattenResponsesTools(items []json.RawMessage, namespace string, mappings map[string]responseToolMapping) []relaymodel.Tool {
	result := make([]relaymodel.Tool, 0, len(items))
	for _, rawItem := range items {
		var item struct {
			Type        string            `json:"type"`
			Name        string            `json:"name"`
			Description string            `json:"description"`
			Parameters  any               `json:"parameters"`
			Tools       []json.RawMessage `json:"tools"`
		}
		if json.Unmarshal(rawItem, &item) != nil {
			continue
		}
		if item.Type == "namespace" {
			childNamespace := item.Name
			if childNamespace == "functions" {
				childNamespace = ""
			}
			result = append(result, flattenResponsesTools(item.Tools, childNamespace, mappings)...)
			continue
		}
		if (item.Type != "function" && item.Type != "custom") || item.Name == "" {
			continue
		}
		alias := item.Name
		if namespace != "" {
			alias = namespace + "__" + item.Name
		}
		if _, exists := mappings[alias]; exists {
			continue
		}
		parameters := item.Parameters
		if item.Type == "custom" {
			parameters = map[string]any{"type": "object", "properties": map[string]any{"input": map[string]any{"type": "string"}}, "required": []string{"input"}, "additionalProperties": false}
		}
		result = append(result, relaymodel.Tool{Type: "function", Function: relaymodel.Function{Name: alias, Description: item.Description, Parameters: parameters}})
		mappings[alias] = responseToolMapping{Name: item.Name, Namespace: namespace, Custom: item.Type == "custom"}
	}
	return result
}

func convertResponsesInputToMessages(input json.RawMessage, instructions string) []relaymodel.Message {
	var messages []relaymodel.Message

	if instructions != "" {
		messages = append(messages, relaymodel.Message{Role: "system", Content: instructions})
	}

	var inputStr string
	if err := json.Unmarshal(input, &inputStr); err == nil && inputStr != "" {
		messages = append(messages, relaymodel.Message{Role: "user", Content: inputStr})
		return messages
	}

	var inputItems []json.RawMessage
	if err := json.Unmarshal(input, &inputItems); err != nil {
		logger.Errorf(nil, "failed to parse responses input: %v", err)
		return messages
	}

	var pendingToolCalls []relaymodel.Tool
	flushToolCalls := func() {
		if len(pendingToolCalls) == 0 {
			return
		}
		messages = append(messages, relaymodel.Message{Role: "assistant", ToolCalls: pendingToolCalls})
		pendingToolCalls = nil
	}

	for _, rawItem := range inputItems {
		var item rawInputItem
		if err := json.Unmarshal(rawItem, &item); err != nil {
			continue
		}

		switch item.Type {
		case "function_call":
			callID := item.CallID
			if callID == "" {
				callID = item.ID
			}
			pendingToolCalls = append(pendingToolCalls, relaymodel.Tool{
				Id: callID, Type: "function",
				Function: relaymodel.Function{Name: item.Name, Arguments: item.Arguments},
			})
		case "function_call_output":
			flushToolCalls()
			messages = append(messages, relaymodel.Message{
				Role:       "tool",
				Content:    responsesOutputText(item.Output),
				ToolCallId: item.CallID,
			})
		default:
			flushToolCalls()
			role := item.Role
			if role == "" {
				role = "user"
			}
			if role == "system" || role == "developer" {
				role = "system"
			}
			text := extractContentText(item.Content)
			if text == "" {
				continue
			}
			messages = append(messages, relaymodel.Message{Role: role, Content: text})
		}
	}
	flushToolCalls()

	return messages
}

func responsesOutputText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	return extractContentText(raw)
}

// ResponsesAPITool represents a tool in the Responses API format (flat structure)
type ResponsesAPITool struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
	Strict      bool   `json:"strict,omitempty"`
}

// convertToolsToChatFormat converts tools from Responses API format to Chat Completions format
// Responses API: {"type": "function", "name": "foo", "description": "...", "parameters": {...}}
// Chat Completions: {"type": "function", "function": {"name": "foo", "description": "...", "parameters": {...}}}
func convertToolsToChatFormat(tools []relaymodel.Tool, rawTools json.RawMessage) []relaymodel.Tool {
	if len(tools) == 0 {
		return nil
	}

	// Check if tools are already in Chat Completions format (have Function field)
	// or if they're in Responses API format (have Name at top level)
	needConversion := false
	for _, tool := range tools {
		// In Chat Completions format, Function.Name should be set
		// In Responses API format, Name is at top level and Function is empty
		if tool.Function.Name == "" && tool.Type == "function" {
			needConversion = true
			break
		}
	}

	if !needConversion {
		return tools
	}

	// Parse raw JSON directly as Responses API tools
	var responsesTools []ResponsesAPITool
	if err := json.Unmarshal(rawTools, &responsesTools); err != nil {
		logger.Errorf(nil, "failed to parse tools as Responses API format: %v", err)
		return tools
	}

	result := make([]relaymodel.Tool, 0, len(responsesTools))
	for _, rt := range responsesTools {
		if rt.Type != "function" || rt.Name == "" {
			continue
		}
		result = append(result, relaymodel.Tool{
			Type: "function",
			Function: relaymodel.Function{
				Name:        rt.Name,
				Description: rt.Description,
				Parameters:  rt.Parameters,
			},
		})
	}
	return result
}

func mustMarshal(v any) []byte {
	data, _ := json.Marshal(v)
	return data
}

func extractContentText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s)
	}

	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}

	var sb strings.Builder
	for _, b := range blocks {
		switch b.Type {
		case "input_text", "text", "output_text":
			if t := strings.TrimSpace(b.Text); t != "" {
				if sb.Len() > 0 {
					sb.WriteString("\n")
				}
				sb.WriteString(t)
			}
		}
	}
	return sb.String()
}
