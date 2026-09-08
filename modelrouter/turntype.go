package modelrouter

import (
	"strings"
	"unicode/utf8"

	schema "github.com/modelbus/one-api-pro/relay/schema"
)

// TurnType identifies requests which should not use the normal quality scorer.
type TurnType int

const (
	TurnTypeNormal TurnType = iota
	TurnTypeCompression
	TurnTypeSubAgent
	TurnTypeToolResult
	TurnTypeTitleGen
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

// TaskDifficulty expresses the amount of reasoning capacity a request needs.
// It is deliberately a small, explainable tier rather than a model-specific
// label so policy changes remain operable by administrators.
type TaskDifficulty string

const (
	TaskDifficultySimple  TaskDifficulty = "simple"
	TaskDifficultyNormal  TaskDifficulty = "normal"
	TaskDifficultyComplex TaskDifficulty = "complex"
)

// RequestFeatures contains routing-relevant facts extracted from a request.
// The explicit flags let callers avoid relying on prompt heuristics.
type RequestFeatures struct {
	Prompt             string         `json:"-"`
	SystemPrompt       string         `json:"-"`
	EstimatedTokens    int            `json:"estimated_tokens,omitempty"`
	MaxOutputTokens    int            `json:"max_output_tokens,omitempty"`
	HasImages          bool           `json:"has_images,omitempty"`
	HasTools           bool           `json:"has_tools,omitempty"`
	HasToolResult      bool           `json:"has_tool_result,omitempty"`
	CompressionRequest bool           `json:"compression_request,omitempty"`
	SubAgentRequest    bool           `json:"sub_agent_request,omitempty"`
	TitleRequest       bool           `json:"title_request,omitempty"`
	TaskCategory       string         `json:"task_category,omitempty"`
	Difficulty         TaskDifficulty `json:"difficulty,omitempty"`
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
	features.TaskCategory = detectTaskCategory(features.Prompt)
	features.Difficulty = DetectTaskDifficulty(features)
	return features
}

// DetectTaskDifficulty derives a stable tier from request shape before using
// prompt signals. Capability-heavy and large-context requests are complex;
// short social/translation requests are simple; everything else is normal.
func DetectTaskDifficulty(features *RequestFeatures) TaskDifficulty {
	if features == nil {
		return TaskDifficultyNormal
	}
	if DetectTurnType(features) != TurnTypeNormal || features.HasImages || features.HasTools || features.HasToolResult ||
		features.EstimatedTokens >= 32_000 || features.MaxOutputTokens >= 4_096 {
		return TaskDifficultyComplex
	}
	text := strings.ToLower(features.SystemPrompt + "\n" + features.Prompt)
	if containsAny(text, "step by step", "multi-step", "prove", "architecture", "refactor", "debug", "algorithm", "reasoning", "chain of thought", "逐步", "多步骤", "证明", "架构", "重构", "调试", "算法", "推理") {
		return TaskDifficultyComplex
	}
	category := features.TaskCategory
	if category == "" {
		category = detectTaskCategory(features.Prompt)
	}
	if features.EstimatedTokens <= 1_024 && features.MaxOutputTokens <= 1_024 &&
		(category == "chat" || category == "translate" || strings.TrimSpace(features.Prompt) == "") {
		return TaskDifficultySimple
	}
	return TaskDifficultyNormal
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

// DetectTurnType gives explicit feature flags precedence, then uses narrow
// prompt markers for compatibility with clients which cannot send metadata.
func DetectTurnType(features *RequestFeatures) TurnType {
	if features == nil {
		return TurnTypeNormal
	}
	if features.CompressionRequest {
		return TurnTypeCompression
	}
	if features.SubAgentRequest {
		return TurnTypeSubAgent
	}
	if features.HasToolResult {
		return TurnTypeToolResult
	}
	if features.TitleRequest {
		return TurnTypeTitleGen
	}

	text := strings.ToLower(features.SystemPrompt + "\n" + features.Prompt)
	if containsAny(text,
		"context compression", "compress the conversation", "conversation summary for continuation",
		"上下文压缩", "压缩以下对话", "压缩会话") {
		return TurnTypeCompression
	}
	if containsAny(text, "sub-agent", "subagent", "sub agent", "子 agent", "子智能体") {
		return TurnTypeSubAgent
	}
	if containsAny(text,
		"generate a concise title", "generate a short title", "title for this conversation",
		"生成一个简短标题", "生成会话标题", "生成对话标题") {
		return TurnTypeTitleGen
	}
	return TurnTypeNormal
}

func estimateTokens(text string) int {
	if text == "" {
		return 0
	}
	// This is deliberately conservative and tokenizer-independent.
	return (utf8.RuneCountInString(text) + 2) / 3
}

func containsAny(text string, values ...string) bool {
	for _, value := range values {
		if strings.Contains(text, value) {
			return true
		}
	}
	return false
}
