package modelrouter

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecisionStoreReturnsNewestWithinCapacity(t *testing.T) {
	store := NewDecisionStore(2)
	store.Add(RoutingDecision{Model: "first"})
	store.Add(RoutingDecision{Model: "second"})
	store.Add(RoutingDecision{Model: "third"})

	got := store.Recent(0)
	if len(got) != 2 {
		t.Fatalf("Recent() returned %d decisions, want 2", len(got))
	}
	if got[0].Model != "third" || got[1].Model != "second" {
		t.Fatalf("Recent() models = %q, %q; want third, second", got[0].Model, got[1].Model)
	}
	if got[0].Timestamp.IsZero() {
		t.Fatal("stored decision has a zero timestamp")
	}
}

func TestRoutingDecisionJSONDoesNotExposePrompts(t *testing.T) {
	encoded, err := json.Marshal(RoutingDecision{
		Features: &RequestFeatures{
			Prompt:       "private user prompt",
			SystemPrompt: "private system prompt",
			HasTools:     true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	jsonText := string(encoded)
	if strings.Contains(jsonText, "private") {
		t.Fatalf("decision JSON exposed prompt content: %s", jsonText)
	}
	if !strings.Contains(jsonText, `"has_tools":true`) {
		t.Fatalf("decision JSON omitted safe feature details: %s", jsonText)
	}
}

func TestDecisionStoreClonesMutableFields(t *testing.T) {
	store := NewDecisionStore(1)
	decision := RoutingDecision{
		Scores:         map[string]float64{"quality": 0.8},
		Candidates:     []string{"model-a"},
		FilterReasons:  map[string]string{"model-b": "no tools"},
		Features:       &RequestFeatures{HasTools: true},
		ClusterMatches: []ClusterMatch{{Cluster: 3, Similarity: 0.9}},
	}
	store.Add(decision)

	decision.Scores["quality"] = 0
	decision.Candidates[0] = "changed"
	decision.FilterReasons["model-b"] = "changed"
	decision.Features.HasTools = false
	decision.ClusterMatches[0].Cluster = 9

	got := store.Recent(1)[0]
	if got.Scores["quality"] != 0.8 || got.Candidates[0] != "model-a" || got.FilterReasons["model-b"] != "no tools" || !got.Features.HasTools || got.ClusterMatches[0].Cluster != 3 {
		t.Fatalf("stored decision was mutated through caller-owned data: %#v", got)
	}

	got.Scores["quality"] = 0
	if store.Recent(1)[0].Scores["quality"] != 0.8 {
		t.Fatal("Recent() exposed mutable store state")
	}
}
