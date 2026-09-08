package modelrouter

import (
	"testing"

	schema "github.com/modelbus/one-api-pro/relay/schema"
)

func TestDetectTurnType(t *testing.T) {
	tests := []struct {
		name     string
		features *RequestFeatures
		want     TurnType
	}{
		{name: "normal", features: &RequestFeatures{Prompt: "explain this code"}, want: TurnTypeNormal},
		{name: "compression flag", features: &RequestFeatures{CompressionRequest: true}, want: TurnTypeCompression},
		{name: "compression marker", features: &RequestFeatures{SystemPrompt: "Create a conversation summary for continuation"}, want: TurnTypeCompression},
		{name: "sub-agent marker", features: &RequestFeatures{SystemPrompt: "You are a sub-agent working on tests"}, want: TurnTypeSubAgent},
		{name: "tool result", features: &RequestFeatures{HasToolResult: true}, want: TurnTypeToolResult},
		{name: "title marker", features: &RequestFeatures{Prompt: "Generate a concise title for this conversation"}, want: TurnTypeTitleGen},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := DetectTurnType(test.features); got != test.want {
				t.Fatalf("DetectTurnType() = %s, want %s", got, test.want)
			}
		})
	}
}

func TestExtractRequestFeatures(t *testing.T) {
	messages := []schema.Message{
		{Role: "system", Content: "route carefully"},
		{Role: "user", Content: []any{
			map[string]any{"type": schema.ContentTypeText, "text": "describe this"},
			map[string]any{"type": schema.ContentTypeImageURL, "image_url": map[string]any{"url": "data:image/png;base64,x"}},
		}},
		{Role: "tool", ToolCallId: "call-1", Content: "result"},
	}
	features := ExtractRequestFeatures(messages, []schema.Tool{{Type: "function"}}, 512)
	if !features.HasImages || !features.HasTools || !features.HasToolResult {
		t.Fatalf("capabilities not extracted: %+v", features)
	}
	if features.Prompt != "describe this" {
		t.Fatalf("Prompt = %q", features.Prompt)
	}
	if features.EstimatedTokens < 512 {
		t.Fatalf("EstimatedTokens = %d, want at least requested output", features.EstimatedTokens)
	}
}

func TestSpecialModelSelectionPrefersLowCost(t *testing.T) {
	expensive, cheap := 10.0, .1
	models := []string{"expensive", "cheap"}
	profiles := map[string]ModelProfile{
		"expensive": {Model: "expensive", InputCost: &expensive, OutputCost: &expensive, Confidence: 1},
		"cheap":     {Model: "cheap", InputCost: &cheap, OutputCost: &cheap, Confidence: 1},
	}
	if got := ScoreModelProfiles("", models, profiles, "economy").Selected; got != "cheap" {
		t.Fatalf("economy scoring selected %q, want cheap", got)
	}
}

func TestDetectTaskDifficulty(t *testing.T) {
	if got := DetectTaskDifficulty(&RequestFeatures{Prompt: "hello", EstimatedTokens: 20, MaxOutputTokens: 32}); got != TaskDifficultySimple {
		t.Fatalf("simple request difficulty = %q", got)
	}
	if got := DetectTaskDifficulty(&RequestFeatures{Prompt: "refactor this module step by step", EstimatedTokens: 100, MaxOutputTokens: 100}); got != TaskDifficultyComplex {
		t.Fatalf("reasoning request difficulty = %q", got)
	}
	if got := DetectTaskDifficulty(&RequestFeatures{Prompt: "summarize this", EstimatedTokens: 2_000, MaxOutputTokens: 200}); got != TaskDifficultyNormal {
		t.Fatalf("ordinary request difficulty = %q", got)
	}
}

func TestDifficultyChangesBalancedScoring(t *testing.T) {
	expensive, cheap := 10.0, .1
	profiles := map[string]ModelProfile{
		"quality": {Model: "quality", Quality: map[string]float64{"default": .95}, InputCost: &expensive, OutputCost: &expensive, Confidence: 1},
		"cheap":   {Model: "cheap", Quality: map[string]float64{"default": .55}, InputCost: &cheap, OutputCost: &cheap, Confidence: 1},
	}
	names := []string{"quality", "cheap"}
	if got := ScoreModelProfilesWithFeatures(&RequestFeatures{Prompt: "hello", EstimatedTokens: 10, MaxOutputTokens: 10}, names, profiles, nil, "balanced").Selected; got != "cheap" {
		t.Fatalf("simple request selected %q, want cheap", got)
	}
	if got := ScoreModelProfilesWithFeatures(&RequestFeatures{Prompt: "prove this step by step", EstimatedTokens: 10, MaxOutputTokens: 10}, names, profiles, nil, "balanced").Selected; got != "quality" {
		t.Fatalf("complex request selected %q, want quality", got)
	}
}
