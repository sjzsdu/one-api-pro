package modelrouter

import (
	"sync"
	"time"
)

// RoutingDecision is the complete, queryable explanation for one model-routing
// attempt. Strategies which do not use a field leave it at its zero value.
type RoutingDecision struct {
	Model          string             `json:"model"`
	Provider       string             `json:"provider,omitempty"`
	Score          float64            `json:"score"`
	Scores         map[string]float64 `json:"scores,omitempty"`
	FilteredOut    []string           `json:"filtered_out,omitempty"`
	TurnType       TurnType           `json:"turn_type"`
	Features       *RequestFeatures   `json:"features,omitempty"`
	ClusterMatches []ClusterMatch     `json:"cluster_matches,omitempty"`
	Reason         string             `json:"reason"`
	LatencyMs      int64              `json:"latency_ms"`

	// Additional diagnostic fields make the record self-contained without
	// exposing prompt content.
	Timestamp       time.Time          `json:"timestamp"`
	Strategy        string             `json:"strategy"`
	Group           string             `json:"group,omitempty"`
	UserID          int                `json:"user_id,omitempty"`
	Candidates      []string           `json:"candidates,omitempty"`
	CandidateScores map[string]float64 `json:"candidate_scores,omitempty"`
	FilterReasons   map[string]string  `json:"filter_reasons,omitempty"`
	Error           string             `json:"error,omitempty"`
}

// DecisionStore is a bounded in-memory ring of recent routing decisions. It
// keeps the debug endpoint useful without allowing observability data to grow
// with process uptime.
type DecisionStore struct {
	mu        sync.RWMutex
	decisions []RoutingDecision
	start     int
	capacity  int
}

func NewDecisionStore(capacity int) *DecisionStore {
	if capacity < 1 {
		capacity = 1
	}
	return &DecisionStore{
		decisions: make([]RoutingDecision, 0, capacity),
		capacity:  capacity,
	}
}

func (s *DecisionStore) Add(decision RoutingDecision) {
	decision = cloneDecision(decision)
	if decision.Timestamp.IsZero() {
		decision.Timestamp = time.Now().UTC()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.decisions) < s.capacity {
		s.decisions = append(s.decisions, decision)
		return
	}
	s.decisions[s.start] = decision
	s.start = (s.start + 1) % s.capacity
}

// Recent returns newest decisions first. A non-positive limit returns all
// retained decisions.
func (s *DecisionStore) Recent(limit int) []RoutingDecision {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := len(s.decisions)
	if limit > 0 && limit < count {
		count = limit
	}
	result := make([]RoutingDecision, 0, count)
	for offset := 0; offset < count; offset++ {
		logical := len(s.decisions) - 1 - offset
		idx := (s.start + logical) % len(s.decisions)
		result = append(result, cloneDecision(s.decisions[idx]))
	}
	return result
}

func cloneDecision(decision RoutingDecision) RoutingDecision {
	decision.Scores = cloneFloatMap(decision.Scores)
	decision.CandidateScores = cloneFloatMap(decision.CandidateScores)
	decision.FilterReasons = cloneStringMap(decision.FilterReasons)
	decision.FilteredOut = append([]string(nil), decision.FilteredOut...)
	decision.Candidates = append([]string(nil), decision.Candidates...)
	decision.ClusterMatches = append([]ClusterMatch(nil), decision.ClusterMatches...)
	if decision.Features != nil {
		features := *decision.Features
		decision.Features = &features
	}
	return decision
}

func cloneFloatMap(source map[string]float64) map[string]float64 {
	if source == nil {
		return nil
	}
	clone := make(map[string]float64, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

func cloneStringMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	clone := make(map[string]string, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}
