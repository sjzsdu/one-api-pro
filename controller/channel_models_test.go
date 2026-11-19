package controller

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/modelbus/one-api-pro/common/client"
	"github.com/stretchr/testify/require"
)

func TestBuildOpenRouterModelURLsKeepsCustomEndpointScoped(t *testing.T) {
	urls := buildOpenRouterModelURLs("https://gateway.example.com/api")
	require.Equal(t, []string{
		"https://gateway.example.com/api/v1/models",
		"https://gateway.example.com/api/models",
	}, urls)
	for _, url := range urls {
		require.False(t, strings.Contains(url, "openrouter.ai"))
	}
}

func TestBuildOpenRouterModelURLsUsesOfficialEndpointOnlyWhenDefaulted(t *testing.T) {
	urls := buildOpenRouterModelURLs("")
	require.Equal(t, []string{
		"https://openrouter.ai/api/v1/models",
		"https://openrouter.ai/api/models",
	}, urls)
}

func TestParseModelMappingKeys(t *testing.T) {
	keys := parseModelMappingKeys(`{"deepseek-v4-flash":"deepseek-v4-flash-0731"," qwen3-vl-embedding ":"qwen3-vl-embedding"}`)
	require.ElementsMatch(t, []string{"deepseek-v4-flash", "qwen3-vl-embedding"}, keys)
}

func TestParseModelMappingKeysIgnoresEmptyAndMalformed(t *testing.T) {
	for _, raw := range []string{"", "   ", "{}", "not json", `["a","b"]`} {
		require.Empty(t, parseModelMappingKeys(raw), "raw=%q", raw)
	}
}

// A model_mapping key is a gateway-side alias that never appears in the
// upstream /models list. The cross-check must keep it as valid.
func TestValidateChannelModelsKeepsModelMappingAliases(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"deepseek-v4-flash-0731"}]}`))
	}))
	defer upstream.Close()

	// Use the default HTTP client so the test server is always reachable.
	previousClient := client.ImpatientHTTPClient
	client.ImpatientHTTPClient = nil
	defer func() { client.ImpatientHTTPClient = previousClient }()

	payload := map[string]any{
		"type":          1,
		"key":           "sk-test",
		"base_url":      upstream.URL,
		"models":        "deepseek-v4-flash,deepseek-v4-flash-0731,ghost-model",
		"model_mapping": `{"deepseek-v4-flash":"deepseek-v4-flash-0731"}`,
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/channel/models/validate", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	ValidateChannelModels(c)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			ValidModels         []string `json:"valid_models"`
			InvalidModels       []string `json:"invalid_models"`
			VerificationEnabled bool     `json:"verification_enabled"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.True(t, resp.Success)
	require.True(t, resp.Data.VerificationEnabled)
	require.ElementsMatch(t, []string{"deepseek-v4-flash", "deepseek-v4-flash-0731"}, resp.Data.ValidModels)
	require.Equal(t, []string{"ghost-model"}, resp.Data.InvalidModels)
}
