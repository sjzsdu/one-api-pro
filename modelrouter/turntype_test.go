package modelrouter

import (
	"testing"

	"github.com/modelbus/one-api-pro/channelrouter"
	"github.com/modelbus/one-api-pro/model"
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
	tests := []struct {
		name string
		in   *RequestFeatures
		want TaskDifficulty
	}{
		{"simple greeting", &RequestFeatures{Prompt: "hello"}, DifficultySimple},
		{"normal sized answer", &RequestFeatures{Prompt: "explain this", MaxOutputTokens: 2000}, DifficultyNormal},
		{"complex code", &RequestFeatures{Prompt: "implement and refactor this code", MaxOutputTokens: 5000}, DifficultyComplex},
		{"tool result is cheap", &RequestFeatures{Prompt: "analyze", HasToolResult: true, MaxOutputTokens: 8000}, DifficultySimple},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := DetectTaskDifficulty(test.in); got != test.want {
				t.Fatalf("DetectTaskDifficulty() = %s, want %s", got, test.want)
			}
		})
	}
}

func TestDifficultyChangesScoringPriorities(t *testing.T) {
	expensive, cheap := 10.0, .1
	profiles := map[string]ModelProfile{
		"quality": {Model: "quality", InputCost: &expensive, OutputCost: &expensive, Quality: map[string]float64{"default": .95}, Confidence: 1},
		"cheap":   {Model: "cheap", InputCost: &cheap, OutputCost: &cheap, Quality: map[string]float64{"default": .55}, Confidence: 1},
	}
	names := []string{"quality", "cheap"}
	if got := ScoreModelProfilesWithFeatures(&RequestFeatures{Prompt: "hello"}, names, profiles, nil, "balanced").Selected; got != "cheap" {
		t.Fatalf("simple request selected %q, want cheap", got)
	}
	if got := ScoreModelProfilesWithFeatures(&RequestFeatures{Prompt: "implement code", MaxOutputTokens: 5000}, names, profiles, nil, "balanced").Selected; got != "quality" {
		t.Fatalf("complex request selected %q, want quality", got)
	}
}

func TestAvailabilityLowersCandidateReliability(t *testing.T) {
	profiles := map[string]ModelProfile{
		"healthy": {Model: "healthy", Quality: map[string]float64{"default": .7}, Confidence: 1},
		"busy":    {Model: "busy", Quality: map[string]float64{"default": .7}, Confidence: 1},
	}
	result := ScoreModelProfilesWithFeatures(&RequestFeatures{Prompt: "explain this", MaxOutputTokens: 2000}, []string{"healthy", "busy"}, profiles, map[string]float64{"healthy": 1, "busy": 0}, "balanced")
	if result.Selected != "healthy" {
		t.Fatalf("availability did not downgrade busy candidate: %+v", result)
	}
	if result.Scores["busy"].Components["availability"] != 0 {
		t.Fatalf("availability component = %v, want 0", result.Scores["busy"].Components)
	}
}

func TestChannelAvailabilityUsesLiveLimits(t *testing.T) {
	previous := channelrouter.DefaultRouter
	router := channelrouter.NewChannelRouter()
	channelrouter.DefaultRouter = router
	t.Cleanup(func() { channelrouter.DefaultRouter = previous })

	rpm, concurrency := 2, 1
	channel := &model.Channel{Id: 42, Status: model.ChannelStatusEnabled, RPM: &rpm, MaxConcurrency: &concurrency}
	if got := channelAvailability(channel); got != 1 {
		t.Fatalf("healthy channel availability = %v, want 1", got)
	}
	router.IncrementRPM(channel.Id)
	router.IncrementRPM(channel.Id)
	if got := channelAvailability(channel); got != 0 {
		t.Fatalf("RPM-saturated availability = %v, want 0", got)
	}
	router.RPM.Cleanup(channel.Id)
	if !router.TryAcquireConcurrency(channel.Id, concurrency) {
		t.Fatal("could not occupy concurrency for test")
	}
	if got := channelAvailability(channel); got != 0 {
		t.Fatalf("concurrency-saturated availability = %v, want 0", got)
	}
	router.ReleaseConcurrency(channel.Id)
	router.SetCooldown(channel.Id, 60, "test", 429)
	if got := channelAvailability(channel); got != 0 {
		t.Fatalf("cooling channel availability = %v, want 0", got)
	}
}
