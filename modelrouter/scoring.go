package modelrouter

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	schema "github.com/modelbus/one-api-pro/relay/schema"
)

func init() { Register("scoring", func() ModelRouter { return &ScoringModelRouter{} }) }

type ScoringModelRouter struct{}

func (r *ScoringModelRouter) Name() string { return "scoring" }

type ScoreWeights struct {
	Quality, Reliability, Cost, Latency, Uncertainty float64
}

var scorePolicies = map[string]ScoreWeights{
	"balanced": {Quality: .45, Reliability: .20, Cost: .20, Latency: .15, Uncertainty: .10},
	"quality":  {Quality: .65, Reliability: .20, Cost: .05, Latency: .10, Uncertainty: .10},
	"economy":  {Quality: .20, Reliability: .15, Cost: .50, Latency: .15, Uncertainty: .10},
}

type CandidateScore struct {
	Total       float64            `json:"total"`
	Components  map[string]float64 `json:"components"`
	Confidence  float64            `json:"confidence"`
	ProfileData []string           `json:"profile_sources,omitempty"`
}

type ScoringResult struct {
	Category string
	Selected string
	Scores   map[string]CandidateScore
}

func (r *ScoringModelRouter) SelectModel(ctx context.Context, group string, userID int, req *ModelSelectRequest) (string, error) {
	started := time.Now()
	features := requestFeatures(req)
	candidates, err := ResolveCandidates(ctx, group, features)
	if err != nil || len(candidates.Models) == 0 {
		if err == nil {
			err = fmt.Errorf("no compatible models for group %s", group)
		}
		RecordRoutingDecision(ctx, RoutingDecision{
			Strategy: r.Name(), Group: group, UserID: userID, Features: features,
			FilteredOut: candidates.FilteredOut, FilterReasons: candidates.FilterReasons,
			Reason: "no candidates available", Error: err.Error(), LatencyMs: time.Since(started).Milliseconds(),
		})
		return "", err
	}

	policy := envOrDefault("MODEL_ROUTER_SCORING_POLICY", "balanced")
	if _, ok := scorePolicies[policy]; !ok {
		policy = "balanced"
	}
	if DetectTurnType(features) != TurnTypeNormal {
		policy = "economy"
		if req != nil {
			req.DisableSessionPin = true
		}
	}
	result := ScoreModelProfilesWithFeatures(features, candidates.Models, candidates.Profiles, candidates.Availability, policy)
	flatScores := make(map[string]float64, len(result.Scores))
	var selectedComponents map[string]float64
	for name, score := range result.Scores {
		flatScores[name] = score.Total
		if name == result.Selected {
			selectedComponents = score.Components
		}
	}
	turnType := DetectTurnType(features)
	RecordRoutingDecision(ctx, RoutingDecision{
		Model: result.Selected, Score: result.Scores[result.Selected].Total, Scores: selectedComponents,
		Strategy: r.Name(), Group: group, UserID: userID, TurnType: turnType, Features: features,
		Candidates: candidates.Models, CandidateScores: flatScores, FilteredOut: candidates.FilteredOut,
		FilterReasons: candidates.FilterReasons, Reason: "highest dynamic profile score (" + policy + ")",
		LatencyMs: time.Since(started).Milliseconds(),
	})
	return result.Selected, nil
}

func ScoreModelProfiles(prompt string, names []string, profiles map[string]ModelProfile, policy string) ScoringResult {
	return ScoreModelProfilesWithFeatures(&RequestFeatures{Prompt: prompt, TaskCategory: detectTaskCategory(prompt)}, names, profiles, nil, policy)
}

// ScoreModelProfilesWithFeatures scores candidates with the full request shape
// and their best currently serviceable channel health. The legacy prompt-only
// entrypoint remains available for callers which cannot supply those signals.
func ScoreModelProfilesWithFeatures(features *RequestFeatures, names []string, profiles map[string]ModelProfile, availability map[string]float64, policy string) ScoringResult {
	weights, ok := scorePolicies[policy]
	if !ok {
		weights = scorePolicies["balanced"]
	}
	if features == nil {
		features = &RequestFeatures{}
	}
	category := features.TaskCategory
	if category == "" {
		category = detectTaskCategory(features.Prompt)
	}
	difficulty := features.Difficulty
	if difficulty == "" {
		copy := *features
		copy.TaskCategory = category
		difficulty = DetectTaskDifficulty(&copy)
	}
	if policy == "balanced" {
		weights = weightsForDifficulty(weights, difficulty)
	}
	costs := make(map[string]*float64, len(names))
	latencies := make(map[string]*float64, len(names))
	for _, name := range names {
		profile := profiles[name]
		costs[name] = averageCost(profile.InputCost, profile.OutputCost)
		latencies[name] = profile.Latency
	}
	costScores := normalizeLowerIsBetter(names, costs)
	latencyScores := normalizeLowerIsBetter(names, latencies)

	result := ScoringResult{Category: category, Scores: make(map[string]CandidateScore, len(names))}
	ordered := append([]string(nil), names...)
	sort.Strings(ordered)
	bestScore := math.Inf(-1)
	for _, name := range ordered {
		profile := profiles[name]
		quality := .5
		if value, exists := profile.Quality[category]; exists {
			quality = clamp01(value)
		} else if value, exists := profile.Quality["default"]; exists {
			quality = clamp01(value)
		}
		reliability := .5
		if profile.Reliability != nil {
			reliability = clamp01(*profile.Reliability)
		}
		channelAvailability := 1.0
		if availability != nil {
			channelAvailability = clamp01(availability[name])
		}
		// Profile reliability is historical; availability is the live channel
		// signal. Blend both so a healthy route does not hide a poor model.
		reliability = .4*reliability + .6*channelAvailability
		confidence := clamp01(profile.Confidence)
		uncertainty := 1 - confidence
		components := map[string]float64{
			"quality": quality, "reliability": reliability,
			"cost": costScores[name], "latency": latencyScores[name],
			"availability":        channelAvailability,
			"uncertainty_penalty": uncertainty,
		}
		total := weights.Quality*quality + weights.Reliability*reliability + weights.Cost*costScores[name] + weights.Latency*latencyScores[name] - weights.Uncertainty*uncertainty
		result.Scores[name] = CandidateScore{Total: total, Components: components, Confidence: confidence, ProfileData: append([]string(nil), profile.Sources...)}
		if total > bestScore {
			bestScore, result.Selected = total, name
		}
	}
	return result
}

func weightsForDifficulty(base ScoreWeights, difficulty TaskDifficulty) ScoreWeights {
	// Explicit quality/economy policies remain intentional operator overrides.
	// Difficulty modifies the balanced default used by normal auto routing.
	switch difficulty {
	case TaskDifficultySimple:
		return ScoreWeights{Quality: .25, Reliability: .20, Cost: .35, Latency: .20, Uncertainty: .10}
	case TaskDifficultyComplex:
		return ScoreWeights{Quality: .65, Reliability: .20, Cost: .05, Latency: .10, Uncertainty: .10}
	default:
		return base
	}
}

func normalizeLowerIsBetter(names []string, values map[string]*float64) map[string]float64 {
	result := make(map[string]float64, len(names))
	minValue, maxValue := math.Inf(1), math.Inf(-1)
	for _, name := range names {
		if value := values[name]; value != nil {
			minValue, maxValue = min(minValue, *value), max(maxValue, *value)
		}
	}
	for _, name := range names {
		value := values[name]
		switch {
		case value == nil:
			result[name] = .5
		case maxValue <= minValue:
			result[name] = 1
		default:
			result[name] = 1 - (*value-minValue)/(maxValue-minValue)
		}
	}
	return result
}

func averageCost(input, output *float64) *float64 {
	if input == nil && output == nil {
		return nil
	}
	if input == nil {
		value := *output
		return &value
	}
	if output == nil {
		value := *input
		return &value
	}
	value := (*input + *output) / 2
	return &value
}

func clamp01(value float64) float64 { return max(0, min(1, value)) }

func detectTaskCategory(prompt string) string {
	lower := strings.ToLower(prompt)
	categories := map[string][]string{
		"code":      {"代码", "code", "编程", "函数", "bug", "debug", "实现", "implement", "算法", "algorithm", "refactor", "重构", "syntax"},
		"translate": {"翻译", "translate", "translation", "英译中", "中译英", "localize"},
		"math":      {"数学", "计算", "方程", "证明", "math", "calculate", "equation", "proof", "微积分", "线性代数", "统计"},
		"reason":    {"推理", "分析", "逻辑", "reason", "analyze", "logic", "为什么", "why", "对比", "compare", "评估", "evaluate"},
		"creative":  {"写", "创作", "故事", "诗", "write", "create", "story", "poem", "文案", "copywriting", "小说", "novel"},
		"chat":      {"你好", "hello", "hi", "聊天", "chat", "闲聊", "你是谁", "who are you"},
	}
	best, maxHits := "default", 0
	for category, keywords := range categories {
		hits := 0
		for _, keyword := range keywords {
			if strings.Contains(lower, keyword) {
				hits++
			}
		}
		if hits > maxHits || hits == maxHits && hits > 0 && category < best {
			best, maxHits = category, hits
		}
	}
	return best
}

func requestFeatures(req *ModelSelectRequest) *RequestFeatures {
	if req == nil {
		return &RequestFeatures{}
	}
	if req.Features == nil {
		req.Features = ExtractRequestFeatures(req.Messages, req.Tools, req.MaxTokens)
	}
	return req.Features
}

func extractPrompt(messages []schema.Message) string {
	var builder strings.Builder
	for _, message := range messages {
		if message.Role == "user" {
			builder.WriteString(message.StringContent())
			builder.WriteByte(' ')
		}
	}
	return strings.TrimSpace(builder.String())
}
