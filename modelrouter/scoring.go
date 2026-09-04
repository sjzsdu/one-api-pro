package modelrouter

import (
	"context"
	"encoding/json"
	"strings"
)

type ScoringModelRouter struct {
	scorers  []ModelScorer
	fallback ModelRouter
}

func NewScoringModelRouter(fallback ModelRouter, scorers ...ModelScorer) *ScoringModelRouter {
	if fallback == nil {
		fallback = NewRandomModelRouter()
	}
	if len(scorers) == 0 {
		scorers = []ModelScorer{KeywordModelScorer{}}
	}
	return &ScoringModelRouter{scorers: scorers, fallback: fallback}
}

func (r *ScoringModelRouter) Name() string {
	return "scoring"
}

func (r *ScoringModelRouter) SelectModel(ctx context.Context, group string, userId int, request *ModelSelectRequest) (string, error) {
	candidates, err := availableCandidates(request)
	if err != nil {
		return "", err
	}

	scores := make(map[string]float64, len(candidates))
	for _, scorer := range r.scorers {
		if scorer == nil {
			continue
		}
		for candidate, score := range scorer.Score(ctx, request, candidates) {
			scores[candidate] += score
		}
	}

	var selected string
	var highest float64
	for _, candidate := range candidates {
		if score := scores[candidate]; score > highest {
			selected = candidate
			highest = score
		}
	}
	if selected == "" {
		return r.fallback.SelectModel(ctx, group, userId, request)
	}
	return selected, nil
}

// KeywordModelScorer is a fast local classifier. A future embedding or LLM
// classifier can be added by implementing ModelScorer without changing the
// routing contract.
type KeywordModelScorer struct{}

type keywordRule struct {
	keywords []string
	models   []modelPreference
}

type modelPreference struct {
	pattern string
	score   float64
}

var keywordRules = []keywordRule{
	{
		keywords: []string{"code", "coding", "debug", "program", "function", "算法", "代码", "编程", "调试"},
		models:   []modelPreference{{"deepseek-chat", 100}, {"gpt-4", 85}, {"claude", 70}},
	},
	{
		keywords: []string{"translate", "translation", "translator", "翻译", "译成", "英文", "中文"},
		models:   []modelPreference{{"gpt-4", 100}, {"claude", 90}, {"qwen", 65}},
	},
	{
		keywords: []string{"math", "calculate", "equation", "proof", "reason", "数学", "计算", "方程", "证明", "推理"},
		models:   []modelPreference{{"deepseek-reasoner", 110}, {"o1", 100}, {"o3", 100}, {"gpt-4", 85}},
	},
	{
		keywords: []string{"creative", "story", "poem", "brainstorm", "创意", "故事", "诗", "文案"},
		models:   []modelPreference{{"claude", 100}, {"gpt-4", 90}, {"qwen", 65}},
	},
	{
		keywords: []string{"hello", "hi", "chat", "你好", "聊天", "闲聊"},
		models:   []modelPreference{{"gpt-3.5-turbo", 100}, {"deepseek-chat", 90}},
	},
}

func (KeywordModelScorer) Score(_ context.Context, request *ModelSelectRequest, candidates []string) map[string]float64 {
	prompt := strings.ToLower(requestText(request))
	scores := make(map[string]float64)
	for _, rule := range keywordRules {
		if !containsAny(prompt, rule.keywords) {
			continue
		}
		for _, candidate := range candidates {
			candidateLower := strings.ToLower(candidate)
			for _, preference := range rule.models {
				if strings.Contains(candidateLower, preference.pattern) {
					scores[candidate] += preference.score
				}
			}
		}
	}
	return scores
}

func requestText(request *ModelSelectRequest) string {
	if request == nil {
		return ""
	}
	var parts []string
	for _, message := range request.Messages {
		parts = append(parts, valueText(message.Content))
	}
	parts = append(parts, valueText(request.Prompt), valueText(request.Input))
	return strings.Join(parts, " ")
}

func valueText(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case nil:
		return ""
	default:
		data, err := json.Marshal(typed)
		if err != nil {
			return ""
		}
		return string(data)
	}
}

func containsAny(text string, keywords []string) bool {
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}
