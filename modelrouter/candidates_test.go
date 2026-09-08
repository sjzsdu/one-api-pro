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
	if result.Scores["alpha"].Components["quality"] != .55 {
		t.Fatal("unknown model did not receive the operational default prior")
	}
}

func TestNewModelGetsNameDerivedProfile(t *testing.T) {
	profile := genericProfile("vendor/new-coder-pro")
	if profile.Quality["code"] <= .8 || profile.Confidence <= .5 {
		t.Fatalf("derived profile did not set an operational code tier: %+v", profile)
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
func TestDifficultyChangesBalancedSelection(t *testing.T) {
	cheap, expensive := .1, 10.0
	profiles := map[string]ModelProfile{
		"cheap":   {Model: "cheap", InputCost: &cheap, OutputCost: &cheap, Quality: map[string]float64{"default": .5}, Confidence: 1},
		"quality": {Model: "quality", InputCost: &expensive, OutputCost: &expensive, Quality: map[string]float64{"default": .95}, Confidence: 1},
	}
	if got := ScoreRequestProfiles(&RequestFeatures{Prompt: "hello", Difficulty: TaskDifficultySimple}, []string{"cheap", "quality"}, profiles, nil, "balanced").Selected; got != "cheap" {
		t.Fatalf("simple selected %s", got)
	}
	if got := ScoreRequestProfiles(&RequestFeatures{Prompt: "analyze", Difficulty: TaskDifficultyComplex}, []string{"cheap", "quality"}, profiles, nil, "balanced").Selected; got != "quality" {
		t.Fatalf("complex selected %s", got)
	}
}

func TestAvailabilityLowersCandidateScore(t *testing.T) {
	profiles := map[string]ModelProfile{
		"healthy":   {Model: "healthy", Quality: map[string]float64{"default": .8}, Reliability: floatPtr(.9), Confidence: 1},
		"unhealthy": {Model: "unhealthy", Quality: map[string]float64{"default": .9}, Reliability: floatPtr(.9), Confidence: 1},
	}
	result := ScoreRequestProfiles(&RequestFeatures{Prompt: "analyze", Difficulty: TaskDifficultyComplex}, []string{"healthy", "unhealthy"}, profiles, map[string]float64{"healthy": 1, "unhealthy": 0}, "balanced")
	if result.Selected != "healthy" || result.Scores["unhealthy"].Components["availability"] != 0 {
		t.Fatalf("availability was not applied: %+v", result)
	}
}

func floatPtr(v float64) *float64 { return &v }
