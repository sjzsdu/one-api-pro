package openaicodex

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/modelbus/one-api-pro/relay/meta"
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

func TestStreamCodexResponse_ForwardsToolCalls(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	responseBody := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}`,
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","call_id":"call_1","name":"shell","arguments":""}}`,
		`data: {"type":"response.function_call_arguments.delta","output_index":0,"call_id":"call_1","delta":"{\"command\":"}`,
		`data: {"type":"response.function_call_arguments.delta","output_index":0,"call_id":"call_1","delta":"\"pwd\"}"}`,
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","call_id":"call_1","name":"shell","arguments":"{\"command\":\"pwd\"}"}}`,
		`data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[{"type":"function_call","call_id":"call_1","name":"shell","arguments":"{\"command\":\"pwd\"}"}]}}`,
	}, "\n") + "\n"
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(responseBody))}
	_, relayErr := streamCodexResponse(context, resp, &meta.Meta{ActualModelName: "gpt-test", PromptTokens: 1})
	if relayErr != nil {
		t.Fatalf("streamCodexResponse returned error: %+v", relayErr)
	}
	output := recorder.Body.String()
	if !strings.Contains(output, `"tool_calls"`) || !strings.Contains(output, `"finish_reason":"tool_calls"`) {
		t.Fatalf("tool call was not forwarded in stream: %s", output)
	}
	if !strings.Contains(output, `"arguments":"{\"command\":"`) || !strings.Contains(output, `"arguments":"\"pwd\"}"`) {
		t.Fatalf("tool arguments were not streamed: %s", output)
	}
}
