package modelrouter

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/modelbus/one-api-pro/model"
	schema "github.com/modelbus/one-api-pro/relay/schema"
)

func init() {
	Register("scoring", func() ModelRouter {
		return &ScoringModelRouter{}
	})
}

type ScoringModelRouter struct{}

func (r *ScoringModelRouter) Name() string {
	return "scoring"
}

func (r *ScoringModelRouter) SelectModel(ctx context.Context, group string, userID int, req *ModelSelectRequest) (string, error) {
	started := time.Now()
	models, err := model.CacheGetGroupModels(ctx, group)
	if err != nil || len(models) == 0 {
		routeErr := fmt.Errorf("no available models for group %s", group)
		RecordRoutingDecision(ctx, RoutingDecision{
			Strategy: r.Name(), Group: group, UserID: userID,
			Reason: "no candidates available", Error: routeErr.Error(), LatencyMs: time.Since(started).Milliseconds(),
		})
		return "", routeErr
	}
	models = filterModelsWithPricing(ctx, models)
	if len(models) == 0 {
		routeErr := fmt.Errorf("no models with pricing found for group %s", group)
		RecordRoutingDecision(ctx, RoutingDecision{
			Strategy: r.Name(), Group: group, UserID: userID,
			Reason: "no priced models available", Error: routeErr.Error(), LatencyMs: time.Since(started).Milliseconds(),
		})
		return "", routeErr
	}

	if req == nil {
		return r.recordFallback(ctx, group, userID, models, started, "request is nil"), nil
	}
	features := requestFeatures(req)
	turnType := DetectTurnType(features)
	if turnType != TurnTypeNormal {
		req.DisableSessionPin = true
		selected := selectSpecialModel(models, turnType)
		RecordRoutingDecision(ctx, RoutingDecision{
			Model: selected, Strategy: r.Name(), Group: group, UserID: userID,
			TurnType: turnType, Candidates: models,
			Reason: fmt.Sprintf("special turn type: %s", turnType), LatencyMs: time.Since(started).Milliseconds(),
		})
		return selected, nil
	}
	if len(req.Messages) == 0 {
		return r.recordFallback(ctx, group, userID, models, started, "request contains no messages"), nil
	}

	prompt := extractPrompt(req.Messages)
	if prompt == "" {
		return r.recordFallback(ctx, group, userID, models, started, "request contains no user prompt"), nil
	}

	profileProvider := NewHybridProfileProvider()
	modelProfiles := profileProvider.GetProfiles(ctx, models)
	candidates := filterByCapabilities(models, modelProfiles, features)

	if len(candidates) == 0 {
		candidates = models
	}

	scores := scoreModelsDynamic(prompt, candidates, modelProfiles)
	candidateScores := make(map[string]float64, len(candidates))
	for i, candidate := range candidates {
		candidateScores[candidate] = scores[i]
	}

	bestIdx := 0
	bestScore := scores[0]
	for i, s := range scores {
		if s > bestScore {
			bestScore = s
			bestIdx = i
		}
	}

	if bestScore == 0 {
		selected := candidates[rand.Intn(len(candidates))]
		RecordRoutingDecision(ctx, RoutingDecision{
			Model: selected, Strategy: r.Name(), Group: group, UserID: userID,
			Candidates: candidates, CandidateScores: candidateScores,
			Reason: "dynamic scoring produced no match; random fallback", LatencyMs: time.Since(started).Milliseconds(),
		})
		return selected, nil
	}
	selected := candidates[bestIdx]
	RecordRoutingDecision(ctx, RoutingDecision{
		Model: selected, Score: bestScore, Scores: map[string]float64{"dynamic": bestScore},
		Strategy: r.Name(), Group: group, UserID: userID, Candidates: candidates, CandidateScores: candidateScores,
		Reason: "highest dynamic score", LatencyMs: time.Since(started).Milliseconds(),
	})
	return selected, nil
}

func (r *ScoringModelRouter) recordFallback(ctx context.Context, group string, userID int, models []string, started time.Time, reason string) string {
	selected := models[rand.Intn(len(models))]
	RecordRoutingDecision(ctx, RoutingDecision{
		Model: selected, Strategy: r.Name(), Group: group, UserID: userID,
		Candidates: models, Reason: reason + "; random fallback", LatencyMs: time.Since(started).Milliseconds(),
	})
	return selected
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

func selectSpecialModel(models []string, turnType TurnType) string {
	best := models[0]
	bestScore := specialModelScore(best, turnType)
	for _, candidate := range models[1:] {
		score := specialModelScore(candidate, turnType)
		if score > bestScore || score == bestScore && candidate < best {
			best, bestScore = candidate, score
		}
	}
	return best
}

func specialModelScore(name string, turnType TurnType) int {
	profile := inferModelProfile(name)
	score := 100 - profile.costTier*20
	if profile.lightweight {
		score += 30
	}
	if turnType == TurnTypeSubAgent && containsAny(strings.ToLower(name), "coder", "code", "deepseek") {
		score += 10
	}
	return score
}

func extractPrompt(messages []schema.Message) string {
	var sb strings.Builder
	for _, msg := range messages {
		if msg.Role == "user" {
			sb.WriteString(msg.StringContent())
			sb.WriteByte(' ')
		}
	}
	return strings.TrimSpace(sb.String())
}

// scoreModelsDynamic scores models using profile-based heuristics.
// No hardcoded model names — scoring uses cost tier, capability flags, and name patterns.
func scoreModelsDynamic(prompt string, models []string, profiles map[string]*ModelProfile) []float64 {
	scores := make([]float64, len(models))
	lower := strings.ToLower(prompt)

	categoryWeights := detectCategory(lower)

	for i, m := range models {
		profile, ok := profiles[m]
		if !ok {
			scores[i] = 0.5
			continue
		}

		score := computeModelScore(profile, categoryWeights)
		scores[i] = score
	}
	return scores
}

type categoryWeights struct {
	code      float64
	translate float64
	math      float64
	reason    float64
	creative  float64
	chat      float64
}

func detectCategory(lower string) categoryWeights {
	var w categoryWeights

	categories := map[string][]string{
		"code":      {"代码", "code", "编程", "函数", "bug", "debug", "实现", "implement", "算法", "algorithm", "refactor", "重构", "编程语言", "syntax"},
		"translate": {"翻译", "translate", "translation", "英译中", "中译英", "localize"},
		"math":      {"数学", "计算", "方程", "证明", "math", "calculate", "equation", "proof", "微积分", "线性代数", "统计"},
		"reason":    {"推理", "分析", "逻辑", "reason", "analyze", "logic", "为什么", "why", "对比", "compare", "评估", "evaluate"},
		"creative":  {"写", "创作", "故事", "诗", "write", "create", "story", "poem", "文案", "copywriting", "小说", "novel"},
		"chat":      {"你好", "hello", "hi", "聊天", "chat", "闲聊", "你是谁", "who are you"},
	}

	hits := map[string]int{}
	for cat, keywords := range categories {
		for _, kw := range keywords {
			if strings.Contains(lower, kw) {
				hits[cat]++
			}
		}
	}

	if hits["code"] > 0 {
		w.code = float64(hits["code"])
	}
	if hits["translate"] > 0 {
		w.translate = float64(hits["translate"])
	}
	if hits["math"] > 0 {
		w.math = float64(hits["math"])
	}
	if hits["reason"] > 0 {
		w.reason = float64(hits["reason"])
	}
	if hits["creative"] > 0 {
		w.creative = float64(hits["creative"])
	}
	if hits["chat"] > 0 {
		w.chat = float64(hits["chat"])
	}

	return w
}

func computeModelScore(profile *ModelProfile, w categoryWeights) float64 {
	score := 1.0

	if w.code > 0 || w.math > 0 || w.reason > 0 {
		if profile.Tools {
			score += 0.3
		}
		if profile.CostTier >= 3 {
			score += 0.2
		}
	}

	if w.translate > 0 || w.creative > 0 {
		if profile.Vision {
			score += 0.1
		}
		if profile.CostTier >= 2 {
			score += 0.15
		}
	}

	if w.chat > 0 {
		if profile.Lightweight {
			score += 0.3
		}
		if profile.CostTier <= 1 {
			score += 0.2
		}
	}

	if profile.Confidence < 0.5 {
		score *= 0.8
	}

	return score
}
