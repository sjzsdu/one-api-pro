package modelrouter

import (
	"strings"
	"unicode/utf8"

	schema "github.com/modelbus/one-api-pro/relay/schema"
)

func (t TurnType) String() string {
	switch t {
	case TurnTypeCompression:
		return "compression"
	case TurnTypeSubAgent:
		return "sub_agent"
	case TurnTypeToolResult:
		return "tool_result"
	case TurnTypeTitleGen:
		return "title_generation"
	default:
		return "normal"
	}
}

// ExtractRequestFeatures normalizes the OpenAI-compatible request fields used
// by turn detection and fallback capability filtering.
func ExtractRequestFeatures(messages []schema.Message, tools []schema.Tool, maxTokens int) *RequestFeatures {
	features := &RequestFeatures{
		HasTools:        len(tools) > 0,
		MaxOutputTokens: maxTokens,
	}
	var prompts []string
	for _, message := range messages {
		content := message.StringContent()
		features.EstimatedTokens += estimateTokens(content)
		switch message.Role {
		case "system", "developer":
			if content != "" {
				features.SystemPrompt += content + "\n"
			}
		case "user":
			if content != "" {
				prompts = append(prompts, content)
			}
		case "tool", "function":
			features.HasToolResult = true
		}
		if message.ToolCallId != "" {
			features.HasToolResult = true
		}
		if len(message.ToolCalls) > 0 {
			features.HasTools = true
		}
		if hasImageContent(message.Content) {
			features.HasImages = true
		}
	}
	features.Prompt = strings.Join(prompts, "\n")
	features.SystemPrompt = strings.TrimSpace(features.SystemPrompt)
	features.EstimatedTokens += maxTokens
	return features
}

func hasImageContent(content any) bool {
	parts, ok := content.([]any)
	if !ok {
		return false
	}
	for _, part := range parts {
		value, ok := part.(map[string]any)
		if ok && value["type"] == schema.ContentTypeImageURL {
			return true
		}
	}
	return false
}

func estimateTokens(text string) int {
	if text == "" {
		return 0
	}
	runeCount := utf8.RuneCountInString(text)
	return (runeCount + 3) / 4
}

// DetectTurnType identifies requests which should bypass the normal quality scorer.
func DetectTurnType(features *RequestFeatures) TurnType {
	if features == nil {
		return TurnTypeNormal
	}
	lower := strings.ToLower(features.Prompt + " " + features.SystemPrompt)

	if features.CompressionRequest || containsAny(lower, "compress this conversation", "summarize the conversation", "conversation summary", "压缩上下文", "压缩对话") {
		return TurnTypeCompression
	}
	if features.SubAgentRequest || containsAny(lower, "sub-agent", "subagent", "child agent", "子agent", "子任务代理") {
		return TurnTypeSubAgent
	}
	if features.HasToolResult && !features.HasTools {
		return TurnTypeToolResult
	}
	if features.TitleRequest || containsAny(lower, "generate a title", "generate title", "对话标题", "生成标题", "title for this conversation") {
		return TurnTypeTitleGen
	}
	return TurnTypeNormal
}

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
