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
	Reason            string                    `json:"reason"`
	AvailableModels   []string                  `json:"available_models"`
	FilteredOutModels []string                  `json:"filtered_out_models"`
	FilterReasons     map[string]string         `json:"filter_reasons"`
	Strategy          string                    `json:"strategy"`
	TurnType          string                    `json:"turn_type"`
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
	policy := "balanced"
	if _, ok := scorePolicies[strategy]; ok {
		policy = strategy
	}
	turnType := DetectTurnType(features)
	if turnType != TurnTypeNormal {
		policy = "economy"
	}
	scored := ScoreModelProfiles(prompt, candidates.Models, candidates.Profiles, policy)
	flat := make(map[string]float64, len(scored.Scores))
	for name, score := range scored.Scores {
		flat[name] = score.Total
	}
	return QuizResult{
		Prompt: prompt, Group: group, DetectedCategory: scored.Category,
		SelectedModel: scored.Selected, ModelScores: flat, ScoreDetails: scored.Scores,
		Reason:          "selected from the current group by dynamic profile score (" + policy + ")",
		AvailableModels: candidates.Models, FilteredOutModels: candidates.FilteredOut,
		FilterReasons: candidates.FilterReasons, Strategy: strategy, TurnType: turnType.String(),
	}, nil
}
