package openaicodex

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelbus/one-api-pro/relay/relaymode"
	relaymodel "github.com/modelbus/one-api-pro/relay/schema"
)

func TestConvertRequestStripsUnsupportedCompatibilityParams(t *testing.T) {
	temperature := 0.2
	topP := 0.9
	maxCompletionTokens := 1024
	converted, err := (&Adaptor{}).ConvertRequest(nil, relaymode.ChatCompletions, &relaymodel.GeneralOpenAIRequest{
		Model:     "gpt-5.4",
		Messages:  []relaymodel.Message{{Role: "user", Content: "Reply with OK only."}},
		MaxTokens: maxCompletionTokens, MaxCompletionTokens: &maxCompletionTokens,
		Temperature: &temperature, TopP: &topP,
	})
	if err != nil {
		t.Fatal(err)
	}
	req, ok := converted.(codexRequest)
	if !ok {
		t.Fatalf("converted request type is %T", converted)
	}
	if req.MaxOutputTokens != 0 || req.Temperature != nil || req.TopP != nil {
		t.Fatalf("unsupported fields were retained: %+v", req)
	}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"max_output_tokens", "temperature", "top_p"} {
		if strings.Contains(string(body), field) {
			t.Fatalf("request contains %q: %s", field, body)
		}
	}
}

func TestKnownCodexModelsReturnsLatestCatalogueCopy(t *testing.T) {
	models := KnownCodexModels()
	if len(models) == 0 {
		t.Fatal("Codex model catalogue is empty")
	}
	if models[0] != "gpt-5.6" {
		t.Fatalf("latest Codex model should be first, got %q", models[0])
	}
	if !containsModel(models, "gpt-5.3-codex") {
		t.Fatalf("Codex catalogue is missing gpt-5.3-codex: %v", models)
	}

	models[0] = "changed"
	if KnownCodexModels()[0] == "changed" {
		t.Fatal("KnownCodexModels returned the shared backing slice")
	}
}

func containsModel(models []string, target string) bool {
	for _, model := range models {
		if model == target {
			return true
		}
	}
	return false
}
