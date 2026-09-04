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

	if req == nil || len(req.Messages) == 0 {
		return r.recordFallback(ctx, group, userID, models, started, "request contains no messages"), nil
	}

	prompt := extractPrompt(req.Messages)
	if prompt == "" {
		return r.recordFallback(ctx, group, userID, models, started, "request contains no user prompt"), nil
	}

	scores := scoreModels(prompt, models)
	candidateScores := make(map[string]float64, len(models))
	for i, candidate := range models {
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
		selected := models[rand.Intn(len(models))]
		RecordRoutingDecision(ctx, RoutingDecision{
			Model: selected, Strategy: r.Name(), Group: group, UserID: userID,
			Candidates: models, CandidateScores: candidateScores,
			Reason: "keyword scoring produced no match; random fallback", LatencyMs: time.Since(started).Milliseconds(),
		})
		return selected, nil
	}
	selected := models[bestIdx]
	RecordRoutingDecision(ctx, RoutingDecision{
		Model: selected, Score: bestScore, Scores: map[string]float64{"keyword": bestScore},
		Strategy: r.Name(), Group: group, UserID: userID, Candidates: models, CandidateScores: candidateScores,
		Reason: "highest keyword-category score", LatencyMs: time.Since(started).Milliseconds(),
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

func scoreModels(prompt string, models []string) []float64 {
	scores := make([]float64, len(models))
	lower := strings.ToLower(prompt)

	categories := map[string][]string{
		"code":      {"代码", "code", "编程", "函数", "bug", "debug", "实现", "implement", "算法", "algorithm", "refactor", "重构", "编程语言", "syntax"},
		"translate": {"翻译", "translate", "translation", "英译中", "中译英", "localize"},
		"math":      {"数学", "计算", "方程", "证明", "math", "calculate", "equation", "proof", "微积分", "线性代数", "统计"},
		"reason":    {"推理", "分析", "逻辑", "reason", "analyze", "logic", "为什么", "why", "对比", "compare", "评估", "evaluate"},
		"creative":  {"写", "创作", "故事", "诗", "write", "create", "story", "poem", "文案", "copywriting", "小说", "novel"},
		"chat":      {"你好", "hello", "hi", "聊天", "chat", "闲聊", "你是谁", "who are you"},
	}

	modelPreference := map[string]map[string]float64{
		"code":      {"deepseek-chat": 3, "gpt-4": 2, "gpt-4o": 2, "claude-3.5-sonnet": 2.5, "deepseek-coder": 3},
		"translate": {"gpt-4": 3, "gpt-4o": 2.5, "claude-3.5-sonnet": 3, "deepseek-chat": 1.5},
		"math":      {"deepseek-reasoner": 3, "gpt-4": 2.5, "gpt-4o": 2, "o1": 3, "o1-mini": 2.5},
		"reason":    {"gpt-4": 2.5, "gpt-4o": 2, "claude-3.5-sonnet": 2.5, "deepseek-reasoner": 2.5},
		"creative":  {"gpt-4": 2.5, "gpt-4o": 3, "claude-3.5-sonnet": 2.5, "deepseek-chat": 1.5},
		"chat":      {"gpt-4o-mini": 3, "gpt-3.5-turbo": 3, "deepseek-chat": 2.5, "gpt-4o": 1.5},
	}

	detectedCategory := ""
	maxHits := 0
	for cat, keywords := range categories {
		hits := 0
		for _, kw := range keywords {
			if strings.Contains(lower, kw) {
				hits++
			}
		}
		if hits > maxHits {
			maxHits = hits
			detectedCategory = cat
		}
	}

	if detectedCategory == "" {
		return scores
	}

	prefs := modelPreference[detectedCategory]
	for i, m := range models {
		if score, ok := prefs[m]; ok {
			scores[i] = score
		} else {
			scores[i] = 0.5
		}
	}
	return scores
}
