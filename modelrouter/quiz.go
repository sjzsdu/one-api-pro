package modelrouter

import (
	"strconv"
	"strings"
)

// QuizResult represents the simulated routing result for the quiz.
type QuizResult struct {
	Prompt            string             `json:"prompt"`
	DetectedCategory  string             `json:"detected_category"`
	SelectedModel     string             `json:"selected_model"`
	ModelScores       map[string]float64 `json:"model_scores"`
	Reason            string             `json:"reason"`
	AvailableModels   []string           `json:"available_models"`
	FilteredOutModels []string           `json:"filtered_out_models"`
	Strategy          string             `json:"strategy"`
	TurnType          string             `json:"turn_type"`
}

// SimulateRouting simulates the model routing logic without making real LLM calls.
// It uses the scoring strategy to analyze the prompt and show which model would be selected.
func SimulateRouting(prompt string, strategy string) QuizResult {
	// Default models for demo purposes (in real system, these come from the database)
	defaultModels := []string{
		"gpt-4",
		"gpt-4o",
		"gpt-4o-mini",
		"gpt-3.5-turbo",
		"claude-3.5-sonnet",
		"deepseek-chat",
		"deepseek-coder",
		"deepseek-reasoner",
		"o1",
		"o1-mini",
	}

	result := QuizResult{
		Prompt:          prompt,
		AvailableModels: defaultModels,
		Strategy:        strategy,
	}

	// Detect turn type from prompt
	features := &RequestFeatures{
		Prompt: prompt,
	}
	turnType := DetectTurnType(features)
	result.TurnType = turnType.String()

	// Use scoring logic for the quiz
	lower := strings.ToLower(prompt)

	// Categories and their keywords (from scoring.go)
	categories := map[string][]string{
		"code":      {"代码", "code", "编程", "函数", "bug", "debug", "实现", "implement", "算法", "algorithm", "refactor", "重构", "编程语言", "syntax"},
		"translate": {"翻译", "translate", "translation", "英译中", "中译英", "localize"},
		"math":      {"数学", "计算", "方程", "证明", "math", "calculate", "equation", "proof", "微积分", "线性代数", "统计"},
		"reason":    {"推理", "分析", "逻辑", "reason", "analyze", "logic", "为什么", "why", "对比", "compare", "评估", "evaluate"},
		"creative":  {"写", "创作", "故事", "诗", "write", "create", "story", "poem", "文案", "copywriting", "小说", "novel"},
		"chat":      {"你好", "hello", "hi", "聊天", "chat", "闲聊", "你是谁", "who are you"},
	}

	// Model preferences per category
	modelPreference := map[string]map[string]float64{
		"code":      {"deepseek-chat": 3, "gpt-4": 2, "gpt-4o": 2, "claude-3.5-sonnet": 2.5, "deepseek-coder": 3},
		"translate": {"gpt-4": 3, "gpt-4o": 2.5, "claude-3.5-sonnet": 3, "deepseek-chat": 1.5},
		"math":      {"deepseek-reasoner": 3, "gpt-4": 2.5, "gpt-4o": 2, "o1": 3, "o1-mini": 2.5},
		"reason":    {"gpt-4": 2.5, "gpt-4o": 2, "claude-3.5-sonnet": 2.5, "deepseek-reasoner": 2.5},
		"creative":  {"gpt-4": 2.5, "gpt-4o": 3, "claude-3.5-sonnet": 2.5, "deepseek-chat": 1.5},
		"chat":      {"gpt-4o-mini": 3, "gpt-3.5-turbo": 3, "deepseek-chat": 2.5, "gpt-4o": 1.5},
	}

	// Detect category
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

	result.DetectedCategory = detectedCategory

	// Calculate scores
	scores := make(map[string]float64, len(defaultModels))
	if detectedCategory != "" {
		prefs := modelPreference[detectedCategory]
		for _, m := range defaultModels {
			if score, ok := prefs[m]; ok {
				scores[m] = score
			} else {
				scores[m] = 0.5
			}
		}
	} else {
		// No category detected, all models get low scores
		for _, m := range defaultModels {
			scores[m] = 0.5
		}
	}

	result.ModelScores = scores

	// Find the best model
	bestModel := defaultModels[0]
	bestScore := scores[bestModel]
	for _, m := range defaultModels[1:] {
		if scores[m] > bestScore {
			bestModel = m
			bestScore = scores[m]
		}
	}

	result.SelectedModel = bestModel

	// Build reason
	if detectedCategory != "" {
		result.Reason = "Detected category: " + detectedCategory + " (matched " + strings.ToUpper(detectedCategory) + " keywords). " +
			"Model '" + bestModel + "' has the highest preference score (" + strconv.FormatFloat(bestScore, 'f', -1, 64) + ") for this category."
	} else {
		result.Reason = "No specific category detected. All models have equal base scores. Random selection would be used in production."
	}

	return result
}
