package modelrouter

import (
	"context"
	"fmt"
	"math"
	"sync"
)

// ClusterModelRouter is the strict, artifact-backed semantic router. A model
// must be covered by the offline evaluation artifact (or an explicit local
// override) to be eligible for selection.
//
// The ranking artifacts are generated offline from evaluations. At request
// time this router only embeds the request, finds its nearest clusters, and
// aggregates the pre-computed per-cluster ranks for models that are actually
// available to the caller.
type ClusterModelRouter struct {
	once   sync.Once
	scorer *WeaveClusterScorer
	err    error
}

func init() {
	Register("cluster", func() ModelRouter { return &ClusterModelRouter{} })
}

func (r *ClusterModelRouter) Name() string { return "cluster" }

func (r *ClusterModelRouter) Prewarm(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.once.Do(func() { r.scorer, r.err = newWeaveClusterScorerFromEnv() })
	return r.err
}

func (r *ClusterModelRouter) ScoreCandidateDetails(ctx context.Context, prompt string, models []string) (map[string]CandidateScore, []ClusterMatch, error) {
	if err := r.Prewarm(ctx); err != nil {
		return nil, nil, err
	}
	return r.scorer.Score(ctx, prompt, models)
}

func (r *ClusterModelRouter) scoreCandidateProfiles(ctx context.Context, prompt string, candidates CandidateSet, features *RequestFeatures) (map[string]CandidateScore, []ClusterMatch, error) {
	if err := r.Prewarm(ctx); err != nil {
		return nil, nil, err
	}
	return r.scorer.ScoreWithCandidateProfiles(ctx, prompt, candidates.Models, features, candidates.Profiles)
}

func (r *ClusterModelRouter) SelectModel(ctx context.Context, group string, userID int, req *ModelSelectRequest) (string, error) {
	features := requestFeatures(req)
	candidates, err := ResolveCandidates(ctx, group, features)
	if err != nil || len(candidates.Models) == 0 {
		if err != nil {
			return "", err
		}
		return "", fmt.Errorf("no compatible models for group %s", group)
	}
	prompt := extractPrompt(req.Messages)
	if prompt == "" {
		return "", fmt.Errorf("cluster routing requires a non-empty prompt")
	}
	details, matches, err := r.scoreCandidateProfiles(ctx, prompt, candidates, features)
	if err != nil {
		return "", fmt.Errorf("cluster model scoring: %w", err)
	}

	best := ""
	bestScore := math.Inf(-1)
	scores := make(map[string]float64, len(details))
	for _, candidate := range candidates.Models {
		detail, ranked := details[candidate]
		if !ranked {
			continue
		}
		scores[candidate] = detail.Total
		if detail.Total > bestScore {
			best, bestScore = candidate, detail.Total
		}
	}
	if best == "" {
		return "", fmt.Errorf("cluster artifact contains no ranking for available models")
	}

	RecordRoutingDecision(ctx, RoutingDecision{
		Model: best, Score: bestScore, Scores: details[best].Components,
		Strategy: r.Name(), Group: group, UserID: userID, TurnType: DetectTurnType(features), Features: features,
		Candidates: candidates.Models, CandidateScores: scores, FilteredOut: candidates.FilteredOut,
		FilterReasons: candidates.FilterReasons, ClusterMatches: matches,
		Reason: "highest offline-evaluated cluster ranking",
	})
	return best, nil
}
