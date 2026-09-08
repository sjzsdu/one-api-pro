package modelrouter

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCandidateSetUsesOnlyProvidedGroupModels(t *testing.T) {
	provider := staticProfileProvider{
		"new-model": {Model: "new-model", Confidence: .2},
		"known":     {Model: "known", Confidence: 1},
	}
	got, err := resolveCandidateNames(context.Background(), []string{"new-model", "known", "known", "batch:batch"}, nil, provider)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Models) != 2 || got.Models[0] != "known" || got.Models[1] != "new-model" {
		t.Fatalf("candidate models = %v", got.Models)
	}
	if _, exists := got.Profiles["outside-model"]; exists {
		t.Fatal("candidate resolver introduced a model outside the group")
	}
}

func TestProfileStoreLoadsArbitraryModels(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profiles.json")
	data := `[{"model":"brand-new","context_window":64000,"vision":"supported","quality":{"code":0.8},"confidence":0.9}]`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := LoadProfileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	profile := store.Snapshot([]string{"brand-new"})["brand-new"]
	if profile.ContextWindow != 64000 || profile.Vision != CapabilitySupported || profile.Quality["code"] != .8 {
		t.Fatalf("loaded profile = %+v", profile)
	}
}

func TestDynamicScoringUsesTaskQualityWithoutModelNames(t *testing.T) {
	profiles := map[string]ModelProfile{
		"alpha": {Model: "alpha", Quality: map[string]float64{"code": .9}, Confidence: 1},
		"beta":  {Model: "beta", Quality: map[string]float64{"code": .4}, Confidence: 1},
	}
	result := ScoreModelProfiles("implement this code", []string{"beta", "alpha"}, profiles, "quality")
	if result.Category != "code" || result.Selected != "alpha" {
		t.Fatalf("scoring result = %+v", result)
	}
}

func TestUnknownModelsUseNeutralPriorAndStableTieBreak(t *testing.T) {
	profiles := map[string]ModelProfile{
		"zeta":  genericProfile("zeta"),
		"alpha": genericProfile("alpha"),
	}
	result := ScoreModelProfiles("unclassified request", []string{"zeta", "alpha"}, profiles, "balanced")
	if result.Selected != "alpha" {
		t.Fatalf("stable tie-break selected %q, want alpha", result.Selected)
	}
	if result.Scores["alpha"].Components["quality"] != .5 {
		t.Fatal("unknown model did not receive neutral quality prior")
	}
}

func TestCanonicalModelName(t *testing.T) {
	for input, want := range map[string]string{
		"openai/gpt-4o":          "gpt-4o",
		"deepseek/deepseek-chat": "deepseek-chat",
		"~openai/gpt-4o-mini":    "gpt-4o-mini",
		"gpt-4o":                 "gpt-4o",
	} {
		if got := CanonicalModelName(input); got != want {
			t.Errorf("CanonicalModelName(%q) = %q, want %q", input, got, want)
		}
	}
}
