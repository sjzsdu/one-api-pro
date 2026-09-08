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

// TaskDifficulty captures the amount of model capability a request needs.
// It is intentionally independent of TurnType: a code request can be normal
// or complex, while maintenance turns (titles, compression, tool results)
// should stay cheap regardless of their text length.
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

// DetectTaskDifficulty classifies the request using structured request facts
// first, then conservative prompt signals.  The thresholds avoid sending a
// short code question to an expensive reasoning model, while multi-step code
// and reasoning work receives the quality-oriented policy.
func DetectTaskDifficulty(features *RequestFeatures) TaskDifficulty {
	if features == nil {
		return TaskDifficultyNormal
	}
	if DetectTurnType(features) != TurnTypeNormal {
		return TaskDifficultySimple
	}
	if features.HasImages || features.HasTools || features.HasToolResult ||
		features.EstimatedTokens >= 32000 || features.MaxOutputTokens >= 8192 {
		return TaskDifficultyComplex
	}
	text := strings.ToLower(features.SystemPrompt + "\n" + features.Prompt)
	complexSignal := containsAny(text,
		"architecture", "multi-step", "step by step", "production", "refactor", "debug", "implement",
		"推理", "架构", "多步骤", "逐步", "生产环境", "重构", "调试", "实现")
	if complexSignal && (features.MaxOutputTokens >= 1024 || features.EstimatedTokens >= 4000) {
		return TaskDifficultyComplex
	}
	if features.EstimatedTokens <= 1000 && features.MaxOutputTokens <= 512 &&
		!complexSignal {
		return TaskDifficultySimple
	}
	return TaskDifficultyNormal
}

func (f *RequestFeatures) EnsureClassification() {
	if f == nil {
		return
	}
	if f.TaskCategory == "" {
		f.TaskCategory = detectTaskCategory(f.Prompt)
	}
	if f.Difficulty == "" {
		f.Difficulty = DetectTaskDifficulty(f)
	}
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
