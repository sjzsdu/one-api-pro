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
