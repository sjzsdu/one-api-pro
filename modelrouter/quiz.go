package modelrouter

import (
	"context"
	"fmt"
	"math/rand"

	"github.com/modelbus/one-api-pro/model"
)

// QuizResult describes a scoring preview. It never sends the prompt to a model
// or records a production routing decision.
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

// SimulateRouting previews the existing scoring strategy against the models
// available to a group. It performs local analysis only and never calls an LLM.
func SimulateRouting(ctx context.Context, group, prompt string) (QuizResult, error) {
	available, err := model.CacheGetGroupModels(ctx, group)
	if err != nil {
		return QuizResult{}, fmt.Errorf("load available models: %w", err)
	}
	if len(available) == 0 {
		return QuizResult{}, fmt.Errorf("no available models for group %s", group)
	}

	candidates := filterModelsWithPricing(ctx, available)
	if len(candidates) == 0 {
		return QuizResult{}, fmt.Errorf("no models with pricing found for group %s", group)
	}

	return buildQuizResult(prompt, available, candidates), nil
}

func buildQuizResult(prompt string, available, candidates []string) QuizResult {
	result := QuizResult{
		Prompt:            prompt,
		AvailableModels:   append([]string(nil), candidates...),
		FilteredOutModels: difference(available, candidates),
		Strategy:          "scoring",
	}

	features := &RequestFeatures{Prompt: prompt}
	turnType := DetectTurnType(features)
	result.TurnType = turnType.String()
	if turnType != TurnTypeNormal {
		result.SelectedModel = selectSpecialModel(candidates, turnType)
		result.ModelScores = specialScores(candidates, turnType)
		result.Reason = fmt.Sprintf("检测到特殊请求类型 %s，优先选择低成本、轻量的适用模型。", turnType)
		return result
	}

	category, scores := scoreModelsWithCategory(prompt, candidates)
	result.DetectedCategory = category
	result.ModelScores = make(map[string]float64, len(candidates))
	bestIndex := 0
	bestScore := scores[0]
	for i, candidate := range candidates {
		result.ModelScores[candidate] = scores[i]
		if scores[i] > bestScore {
			bestIndex, bestScore = i, scores[i]
		}
	}

	if bestScore == 0 {
		bestIndex = rand.Intn(len(candidates))
		result.Reason = "未匹配到明确的任务类别，生产路由会从可用模型中随机选择。"
	} else {
		result.Reason = fmt.Sprintf("识别为 %s 类任务，%s 的偏好分最高（%.1f）。", category, candidates[bestIndex], bestScore)
	}
	result.SelectedModel = candidates[bestIndex]
	return result
}

func specialScores(models []string, turnType TurnType) map[string]float64 {
	scores := make(map[string]float64, len(models))
	for _, candidate := range models {
		scores[candidate] = float64(specialModelScore(candidate, turnType))
	}
	return scores
}

func difference(all, kept []string) []string {
	keptSet := make(map[string]struct{}, len(kept))
	for _, item := range kept {
		keptSet[item] = struct{}{}
	}
	filtered := make([]string, 0, len(all)-len(kept))
	for _, item := range all {
		if _, ok := keptSet[item]; !ok {
			filtered = append(filtered, item)
		}
	}
	return filtered
}
