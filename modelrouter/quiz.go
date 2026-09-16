package modelrouter

import (
	"context"
	"fmt"
)

// QuizResult is a dry-run view of the production cluster-routing pipeline.
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
	UnrankedModels    []string                  `json:"unranked_models,omitempty"`
	Strategy          string                    `json:"strategy"`
	TurnType          string                    `json:"turn_type"`
	Difficulty        TaskDifficulty            `json:"difficulty"`
}

func SimulateRouting(ctx context.Context, group, prompt, strategy string) (QuizResult, error) {
	if strategy != "cluster" {
		return QuizResult{}, fmt.Errorf("model routing strategy %q is unavailable; use cluster", strategy)
	}
	features := &RequestFeatures{Prompt: prompt, Category: detectTaskCategory(prompt)}
	features.EstimatedTokens = estimateTokens(prompt)
	features.Difficulty = DetectTaskDifficulty(features)
	candidates, err := ResolveCandidates(ctx, group, features)
	if err != nil {
		return QuizResult{}, err
	}
	if len(candidates.Models) == 0 {
		return QuizResult{}, fmt.Errorf("no compatible models for group %s", group)
	}
	router, ok := DefaultRouter.(*ClusterModelRouter)
	if !ok {
		return QuizResult{}, fmt.Errorf("cluster router is not initialized; set MODEL_ROUTER_STRATEGY=cluster and restart")
	}
	details, matches, err := router.scoreCandidateProfiles(ctx, prompt, candidates, features)
	if err != nil {
		return QuizResult{}, err
	}
	scores := make(map[string]float64, len(details))
	selected := ""
	unranked := make([]string, 0)
	for _, model := range candidates.Models {
		detail, ranked := details[model]
		if !ranked {
			unranked = append(unranked, model)
			continue
		}
		scores[model] = detail.Total
		if selected == "" || detail.Total > scores[selected] {
			selected = model
		}
	}
	reason := "selected from offline-evaluated semantic cluster rankings"
	if selected == "" {
		reason = "no available model is covered by the current offline-evaluation artifact; cluster routing would reject this request"
	} else if len(unranked) > 0 {
		reason += "; unranked models were excluded"
	}
	return QuizResult{
		Prompt: prompt, Group: group, DetectedCategory: detectTaskCategory(prompt),
		SelectedModel: selected, ModelScores: scores, ScoreDetails: details,
		Reason:          reason,
		AvailableModels: candidates.Models, FilteredOutModels: candidates.FilteredOut,
		FilterReasons: candidates.FilterReasons, UnrankedModels: unranked, Strategy: strategy,
		TurnType: DetectTurnType(features).String(), Difficulty: DetectTaskDifficulty(features), ClusterMatches: matches,
	}, nil
}
