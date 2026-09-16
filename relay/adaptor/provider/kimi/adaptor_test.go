package kimi

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/modelbus/one-api-pro/relay/adaptor"
	"github.com/modelbus/one-api-pro/relay/channeltype"
	"github.com/modelbus/one-api-pro/relay/meta"
	"github.com/modelbus/one-api-pro/relay/registry"
)

var _ adaptor.Adaptor = (*Adaptor)(nil)

func TestRegistration(t *testing.T) {
	registered, ok := registry.GetChannelMeta("kimi")
	if !ok {
		t.Fatal("kimi provider is not registered")
	}
	if registered.Name != "Kimi" {
		t.Fatalf("unexpected provider name: %q", registered.Name)
	}
	if registered.DefaultBaseURL != "https://api.moonshot.cn" {
		t.Fatalf("unexpected base URL: %q", registered.DefaultBaseURL)
	}
	if registered.LegacyType != channeltype.Kimi {
		t.Fatalf("unexpected legacy type: %d", registered.LegacyType)
	}
	if registry.IDByLegacyType(channeltype.Kimi) != "kimi" {
		t.Fatal("kimi legacy type is not mapped to the provider ID")
	}

	registeredAdaptor := registry.GetAdaptorByLegacyType(channeltype.Kimi)
	if registeredAdaptor == nil {
		t.Fatal("kimi adaptor factory returned nil")
	}
	if _, ok := registeredAdaptor.(*Adaptor); !ok {
		t.Fatalf("unexpected adaptor type: %T", registeredAdaptor)
	}
}

func TestRequestURLAndHeaders(t *testing.T) {
	provider := registry.GetAdaptor("kimi")
	requestMeta := &meta.Meta{
		BaseURL:        "https://api.moonshot.cn",
		APIKey:         "test-key",
		ChannelID:      "kimi",
		RequestURLPath: "/v1/chat/completions",
	}

	url, err := provider.GetRequestURL(requestMeta)
	if err != nil {
		t.Fatalf("GetRequestURL returned an error: %v", err)
	}
	if url != "https://api.moonshot.cn/v1/chat/completions" {
		t.Fatalf("unexpected request URL: %q", url)
	}

	gin.SetMode(gin.TestMode)
	incoming := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	incoming.Header.Set("Content-Type", "application/json")
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = incoming
	outgoing := httptest.NewRequest(http.MethodPost, url, nil)
	if err := provider.SetupRequestHeader(ctx, outgoing, requestMeta); err != nil {
		t.Fatalf("SetupRequestHeader returned an error: %v", err)
	}
	if got := outgoing.Header.Get("Authorization"); got != "Bearer test-key" {
		t.Fatalf("unexpected authorization header: %q", got)
	}
	if got := outgoing.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("unexpected content type: %q", got)
	}
}

func TestModelList(t *testing.T) {
	models := registry.GetAdaptor("kimi").GetModelList()
	for _, expected := range []string{"kimi-k2.7-code", "kimi-k2.6", "moonshot-v1-8k"} {
		if !slices.Contains(models, expected) {
			t.Errorf("model list does not contain %q", expected)
		}
	}
}
