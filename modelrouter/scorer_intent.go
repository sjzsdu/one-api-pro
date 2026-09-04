package modelrouter

import (
	"strings"
)

type IntentScorer struct{}

func (s *IntentScorer) Name() string { return "intent" }

var intentCategories = map[string][]string{
	"code":     {"代码", "code", "编程", "函数", "bug", "debug", "实现", "implement", "算法", "algorithm", "refactor", "重构", "编程语言", "syntax", "函数", "function", "class", "方法", "method", "接口", "interface", "api", "sdk"},
	"translate": {"翻译", "translate", "translation", "英译中", "中译英", "localize"},
	"math":     {"数学", "计算", "方程", "证明", "math", "calculate", "equation", "proof", "微积分", "线性代数", "统计"},
	"reason":   {"推理", "分析", "逻辑", "reason", "analyze", "logic", "为什么", "why", "对比", "compare", "评估", "evaluate", "思考", "think"},
	"creative": {"写", "创作", "故事", "诗", "write", "create", "story", "poem", "文案", "copywriting", "小说", "novel"},
	"chat":     {"你好", "hello", "hi", "聊天", "chat", "闲聊", "你是谁", "who are you"},
}

var intentModelPreference = map[string]map[string]float64{
	"code":     {"deepseek-coder": 0.95, "deepseek-chat": 0.85, "gpt-4": 0.8, "gpt-4o": 0.82, "claude-3.5-sonnet": 0.9},
	"translate": {"gpt-4": 0.9, "gpt-4o": 0.88, "claude-3.5-sonnet": 0.9, "deepseek-chat": 0.7},
	"math":     {"deepseek-reasoner": 0.95, "gpt-4": 0.85, "gpt-4o": 0.8, "o1": 0.95, "o1-mini": 0.85},
	"reason":   {"gpt-4": 0.85, "gpt-4o": 0.8, "claude-3.5-sonnet": 0.88, "deepseek-reasoner": 0.9},
	"creative": {"gpt-4": 0.85, "gpt-4o": 0.88, "claude-3.5-sonnet": 0.85, "deepseek-chat": 0.7},
	"chat":     {"gpt-4o-mini": 0.9, "gpt-3.5-turbo": 0.9, "deepseek-chat": 0.85, "gpt-4o": 0.7, "claude-3-haiku": 0.85},
}

func (s *IntentScorer) Score(model string, features *RequestFeatures, ctx *RoutingContext) float64 {
	if features.Prompt == "" {
		return 0.5
	}

	detectedCategory := s.detectIntent(features.Prompt)
	if detectedCategory == "" {
		return 0.5
	}

	prefs, ok := intentModelPreference[detectedCategory]
	if !ok {
		return 0.5
	}

	if score, ok := prefs[model]; ok {
		return score
	}
	return 0.5
}

func (s *IntentScorer) detectIntent(prompt string) string {
	lower := strings.ToLower(prompt)
	bestCategory := ""
	bestHits := 0

	for cat, keywords := range intentCategories {
		hits := 0
		for _, kw := range keywords {
			if strings.Contains(lower, kw) {
				hits++
			}
		}
		if hits > bestHits {
			bestHits = hits
			bestCategory = cat
		}
	}
	return bestCategory
}
