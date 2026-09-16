package modelrouter

import (
	"context"
	"encoding/json"
	"time"

	"github.com/modelbus/one-api-pro/common/logger"
	"github.com/modelbus/one-api-pro/model"
)

const defaultDecisionHistorySize = 200

var defaultDecisionStore = NewDecisionStore(defaultDecisionHistorySize)

// RecordRoutingDecision retains the decision and emits it as one JSON log
// record. Recording avoids prompt content and uses a fixed-size in-memory ring.
func RecordRoutingDecision(ctx context.Context, decision RoutingDecision) {
	if decision.Timestamp.IsZero() {
		decision.Timestamp = time.Now().UTC()
	}
	defaultDecisionStore.Add(decision)
	if err := persistRoutingDecision(ctx, decision); err != nil {
		// Observability must never make a user request fail.
		logger.Errorf(ctx, "model router: persist routing decision: %v", err)
	}
	payload, err := json.Marshal(decision)
	if err != nil {
		logger.Errorf(ctx, "model router: failed to encode routing decision: %v", err)
		return
	}
	logger.Infof(ctx, "model_router_decision=%s", payload)
}

func persistRoutingDecision(ctx context.Context, decision RoutingDecision) error {
	features := decision.Features
	record := &model.ModelRoutingRecord{
		UserId: decision.UserID, UserGroup: decision.Group, Strategy: decision.Strategy,
		SelectedModel: decision.Model, Score: decision.Score, TurnType: decision.TurnType.String(),
		Difficulty: string(decision.Difficulty), RouterLatencyMs: decision.LatencyMs,
		Candidates:          marshalRoutingField(decision.Candidates),
		CandidateScores:     marshalRoutingField(decision.CandidateScores),
		SelectedScoreDetail: marshalRoutingField(decision.Scores),
		FilteredOut:         marshalRoutingField(decision.FilteredOut),
		FilterReasons:       marshalRoutingField(decision.FilterReasons),
		ClusterMatches:      marshalRoutingField(decision.ClusterMatches),
		Reason:              decision.Reason, Error: decision.Error,
	}
	if features != nil {
		record.Category = detectTaskCategory(features.Prompt)
		record.EstimatedTokens = features.EstimatedTokens
		record.MaxOutputTokens = features.MaxOutputTokens
		record.HasImages = features.HasImages
		record.HasTools = features.HasTools
		record.HasToolResult = features.HasToolResult
	}
	return model.RecordModelRoutingRecord(ctx, record)
}

func marshalRoutingField(value any) string {
	if value == nil {
		return ""
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func RecentRoutingDecisions(limit int) []RoutingDecision {
	return defaultDecisionStore.Recent(limit)
}
