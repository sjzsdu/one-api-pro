package modelrouter

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelbus/one-api-pro/channelrouter"
	"github.com/modelbus/one-api-pro/model"
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

func TestDifficultyChangesProfileSelection(t *testing.T) {
	expensive, cheap := 10.0, .1
	profiles := map[string]ModelProfile{
		"quality": {Model: "quality", InputCost: &expensive, OutputCost: &expensive, Quality: map[string]float64{"default": .95}, Confidence: 1},
		"cheap":   {Model: "cheap", InputCost: &cheap, OutputCost: &cheap, Quality: map[string]float64{"default": .30}, Confidence: 1},
	}
	names := []string{"quality", "cheap"}
	simple := ScoreModelProfilesForRequest(&RequestFeatures{Prompt: "hello", EstimatedTokens: 10, MaxOutputTokens: 100}, names, profiles, nil, "balanced")
	if simple.Difficulty != TaskDifficultySimple || simple.Selected != "cheap" {
		t.Fatalf("simple result = %+v, want cheap simple selection", simple)
	}
	complex := ScoreModelProfilesForRequest(&RequestFeatures{Prompt: "implement architecture", MaxOutputTokens: 2048}, names, profiles, nil, "balanced")
	if complex.Difficulty != TaskDifficultyComplex || complex.Selected != "quality" {
		t.Fatalf("complex result = %+v, want quality complex selection", complex)
	}
}

func TestBestChannelAvailabilityHonorsLiveLimits(t *testing.T) {
	router := channelrouter.NewChannelRouter()
	now := time.Now()
	good := &model.Channel{Id: 1, Status: model.ChannelStatusEnabled}
	if score, ok := bestChannelAvailability([]*model.Channel{good}, router, now); !ok || score != 1 {
		t.Fatalf("healthy channel = (%v, %v), want (1, true)", score, ok)
	}
	recentFailure := &model.Channel{Id: 2, Status: model.ChannelStatusEnabled, LastErrorTime: now.Unix()}
	if score, ok := bestChannelAvailability([]*model.Channel{recentFailure}, router, now); !ok || score >= .31 {
		t.Fatalf("recent failure score = (%v, %v), want a strong penalty", score, ok)
	}
	router.SetCooldown(1, 60, "test", 429)
	if _, ok := bestChannelAvailability([]*model.Channel{good}, router, now); ok {
		t.Fatal("cooling-down channel should not be serviceable")
	}
	rpm := 1
	rpmBound := &model.Channel{Id: 3, Status: model.ChannelStatusEnabled, RPM: &rpm}
	router.IncrementRPM(3)
	if _, ok := bestChannelAvailability([]*model.Channel{rpmBound}, router, now); ok {
		t.Fatal("RPM-saturated channel should not be serviceable")
	}
	maxConcurrency := 1
	concurrent := &model.Channel{Id: 4, Status: model.ChannelStatusEnabled, MaxConcurrency: &maxConcurrency}
	if !router.TryAcquireConcurrency(4, maxConcurrency) {
		t.Fatal("could not acquire test concurrency slot")
	}
	defer router.ReleaseConcurrency(4)
	if _, ok := bestChannelAvailability([]*model.Channel{concurrent}, router, now); ok {
		t.Fatal("concurrency-saturated channel should not be serviceable")
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
