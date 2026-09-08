package modelrouter

import (
	"context"
	"fmt"
)

// QuizResult is a dry-run view of the same candidate and scoring pipeline used
// by production routing.
type QuizResult struct {
	Prompt            string                    `json:"prompt"`
	Group             string                    `json:"group"`
	DetectedCategory  string                    `json:"detected_category"`
	SelectedModel     string                    `json:"selected_model"`
	ModelScores       map[string]float64        `json:"model_scores"`
	ScoreDetails      map[string]CandidateScore `json:"score_details"`
	ClusterMatches    []ClusterMatch            `json:"cluster_matches,omitempty"`
	Reason            string                    `json:"reason"`
	AvailableModels   []string                  `json:"available_models"`
	FilteredOutModels []string                  `json:"filtered_out_models"`
	FilterReasons     map[string]string         `json:"filter_reasons"`
	Strategy          string                    `json:"strategy"`
	TurnType          string                    `json:"turn_type"`
	Difficulty        TaskDifficulty            `json:"difficulty"`
}

func SimulateRouting(ctx context.Context, group, prompt, strategy string) (QuizResult, error) {
	features := &RequestFeatures{Prompt: prompt}
	candidates, err := ResolveCandidates(ctx, group, features)
	if err != nil {
		return QuizResult{}, err
	}
	if len(candidates.Models) == 0 {
		return QuizResult{}, fmt.Errorf("no compatible models for group %s", group)
	}
	if strategy == "embedding" {
		router, ok := DefaultRouter.(*EmbeddingModelRouter)
		if !ok {
			return QuizResult{}, fmt.Errorf("embedding router is not initialized")
		}
		scores, matches, err := router.ScoreCandidates(ctx, prompt, candidates.Models)
		if err != nil {
			return QuizResult{}, err
		}
		selected := candidates.Models[0]
		for _, model := range candidates.Models[1:] {
			if scores[model] > scores[selected] {
				selected = model
			}
		}
		return QuizResult{
			Prompt: prompt, Group: group, DetectedCategory: detectTaskCategory(prompt),
			SelectedModel: selected, ModelScores: scores,
			Reason:          "selected by the active embedding semantic router",
			AvailableModels: candidates.Models, FilteredOutModels: candidates.FilteredOut,
			FilterReasons: candidates.FilterReasons, Strategy: strategy,
			TurnType: DetectTurnType(features).String(), ClusterMatches: matches,
		}, nil
	}
	policy := "balanced"
	if _, ok := scorePolicies[strategy]; ok {
		policy = strategy
	}
	turnType := DetectTurnType(features)
	if turnType != TurnTypeNormal {
		policy = "economy"
	}
	scored := ScoreModelProfilesForRequest(features, candidates.Models, candidates.Profiles, candidates.Availability, policy)
	flat := make(map[string]float64, len(scored.Scores))
	for name, score := range scored.Scores {
		flat[name] = score.Total
	}
	return QuizResult{
		Prompt: prompt, Group: group, DetectedCategory: scored.Category,
		SelectedModel: scored.Selected, ModelScores: flat, ScoreDetails: scored.Scores,
		Reason:          "selected from the current group by dynamic profile score (" + policy + ")",
		AvailableModels: candidates.Models, FilteredOutModels: candidates.FilteredOut,
		FilterReasons: candidates.FilterReasons, Strategy: strategy, TurnType: turnType.String(), Difficulty: scored.Difficulty,
	}, nil
}

// buildQuizResult keeps the original local quiz helper available to callers
// while using the same dynamic profile scorer as production routing.
func buildQuizResult(prompt string, available, candidates []string) QuizResult {
	features := &RequestFeatures{Prompt: prompt}
	turnType := DetectTurnType(features)
	policy := "balanced"
	if turnType != TurnTypeNormal {
		policy = "economy"
	}
	profiles := make(map[string]ModelProfile, len(candidates))
	for _, model := range candidates {
		profiles[model] = genericProfile(model)
	}
	scored := ScoreModelProfilesForRequest(features, candidates, profiles, nil, policy)
	modelScores := make(map[string]float64, len(scored.Scores))
	for name, score := range scored.Scores {
		modelScores[name] = score.Total
	}
	return QuizResult{
		Prompt:            prompt,
		DetectedCategory:  scored.Category,
		SelectedModel:     scored.Selected,
		ModelScores:       modelScores,
		Reason:            "selected from the dynamic profile score (" + policy + ")",
		AvailableModels:   append([]string(nil), candidates...),
		FilteredOutModels: difference(available, candidates),
		Strategy:          "scoring", Difficulty: scored.Difficulty,
		TurnType: turnType.String(),
	}
}

// scoreModelsWithCategory is a compatibility view for the original quiz
// helper. Scores are still produced by dynamic profiles, never a model-name
// preference table.
func scoreModelsWithCategory(prompt string, models []string) (string, []float64) {
	profiles := make(map[string]ModelProfile, len(models))
	for _, model := range models {
		profiles[model] = genericProfile(model)
	}
	scored := ScoreModelProfiles(prompt, models, profiles, "balanced")
	scores := make([]float64, len(models))
	for i, model := range models {
		scores[i] = scored.Scores[model].Total
	}
	return scored.Category, scores
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
