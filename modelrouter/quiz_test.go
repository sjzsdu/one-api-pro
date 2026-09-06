package modelrouter

import "testing"

func TestBuildQuizResultUsesScoringStrategy(t *testing.T) {
	available := []string{"deepseek-coder", "gpt-4o", "unpriced-model"}
	candidates := []string{"deepseek-coder", "gpt-4o"}

	result := buildQuizResult("Please implement and debug this code", available, candidates)

	if result.Strategy != "scoring" {
		t.Fatalf("strategy = %q, want scoring", result.Strategy)
	}
	if result.DetectedCategory != "code" {
		t.Fatalf("category = %q, want code", result.DetectedCategory)
	}
	if result.SelectedModel != "deepseek-coder" {
		t.Fatalf("selected model = %q, want deepseek-coder", result.SelectedModel)
	}
	if len(result.FilteredOutModels) != 1 || result.FilteredOutModels[0] != "unpriced-model" {
		t.Fatalf("filtered models = %#v", result.FilteredOutModels)
	}
}

func TestScoreModelsCategoryTieIsDeterministic(t *testing.T) {
	category, _ := scoreModelsWithCategory("write code", []string{"gpt-4o"})
	if category != "code" {
		t.Fatalf("category = %q, want first matching category code", category)
	}
}

func TestBuildQuizResultHandlesSpecialTurns(t *testing.T) {
	models := []string{"gpt-4", "gpt-4o-mini"}
	result := buildQuizResult("Generate a concise title for this conversation", models, models)

	if result.TurnType != "title_generation" {
		t.Fatalf("turn type = %q, want title_generation", result.TurnType)
	}
	if result.SelectedModel == "" {
		t.Fatal("selected model must not be empty")
	}
	if len(result.ModelScores) != len(models) {
		t.Fatalf("scores = %#v", result.ModelScores)
	}
}
