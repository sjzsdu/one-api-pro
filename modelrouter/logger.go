package modelrouter

import (
	"context"
	"encoding/json"
	"time"

	"github.com/modelbus/one-api-pro/common/logger"
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
	payload, err := json.Marshal(decision)
	if err != nil {
		logger.Errorf(ctx, "model router: failed to encode routing decision: %v", err)
		return
	}
	logger.Infof(ctx, "model_router_decision=%s", payload)
}

func RecentRoutingDecisions(limit int) []RoutingDecision {
	return defaultDecisionStore.Recent(limit)
}
