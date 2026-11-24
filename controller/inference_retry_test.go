// inference_retry_test.go 不确定执行结果禁止跨渠道重放
// Prevents provider fallback from regenerating output while billing or usage is unresolved.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package controller

import (
	"github.com/gin-gonic/gin"
	"github.com/modelbus/one-api-pro/common/client"
	"github.com/modelbus/one-api-pro/common/config"
	"github.com/modelbus/one-api-pro/common/ctxkey"
	"github.com/modelbus/one-api-pro/model"
	"github.com/modelbus/one-api-pro/relay/adaptor"
	"github.com/modelbus/one-api-pro/relay/adaptor/provider/ali"
	"github.com/modelbus/one-api-pro/relay/meta"
	"github.com/modelbus/one-api-pro/relay/registry"
	schema "github.com/modelbus/one-api-pro/relay/schema"
	"io"
	"net/http"
	"strings"
	"testing"
)

type uncertainRelayAdaptor struct {
	ali.Adaptor
	code string
}

func (a *uncertainRelayAdaptor) DoResponse(c *gin.Context, resp *http.Response, m *meta.Meta) (*schema.Usage, *schema.ErrorWithStatusCode) {
	resp.Body.Close()
	return nil, &schema.ErrorWithStatusCode{StatusCode: 503, Error: schema.Error{Code: a.code, Message: "execution awaiting reconciliation"}}
}
func TestRelay_UncertainOutcomeDoesNotRetryProvider(t *testing.T) {
	for _, code := range []string{"settlement_pending", "usage_pending", "submission_outcome_unknown"} {
		t.Run(code, func(t *testing.T) {
			setupJobFixture(t)
			cache := model.GetModelPriceCacheForTest()
			cache.Lock()
			model.SetModelPriceMapForTest(map[string]*model.ModelPrice{"uncertain-model": {ModelName: "uncertain-model", BillingType: model.BillingTypeToken, Enabled: true}})
			cache.Unlock()
			oldRetries := config.RetryTimes
			config.RetryTimes = 3
			t.Cleanup(func() { config.RetryTimes = oldRetries })
			registry.Register(registry.ChannelMeta{ID: "uncertain-test"}, func() adaptor.Adaptor { return &uncertainRelayAdaptor{code: code} })
			calls := 0
			client.HTTPClient = &http.Client{Transport: jobRoundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`))}, nil
			})}
			c, w := jobContext(`{"model":"uncertain-model","messages":[{"role":"user","content":"hello"}]}`, 1)
			c.Request.URL.Path = "/v1/chat/completions"
			c.Set(ctxkey.ChannelID, "uncertain-test")
			c.Set(ctxkey.RequestModel, "uncertain-model")
			c.Set(ctxkey.OriginalModel, "uncertain-model")
			Relay(c)
			if calls != 1 {
				t.Fatalf("uncertain result executed %d times", calls)
			}
			if w.Code != 503 {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if retryableAliasFailure(&schema.ErrorWithStatusCode{StatusCode: 503, Error: schema.Error{Code: code}}) {
				t.Fatal("alias fallback would replay uncertain execution")
			}
		})
	}
}

func TestRelay_HTTP503DoesNotRepeatGeneration(t *testing.T) {
	setupJobFixture(t)
	cache := model.GetModelPriceCacheForTest()
	cache.Lock()
	model.SetModelPriceMapForTest(map[string]*model.ModelPrice{"uncertain-model": {ModelName: "uncertain-model", BillingType: model.BillingTypeToken, InputPrice: 2, OutputPrice: 5, Enabled: true}})
	cache.Unlock()
	oldRetries := config.RetryTimes
	config.RetryTimes = 3
	t.Cleanup(func() { config.RetryTimes = oldRetries })
	calls := 0
	client.HTTPClient = &http.Client{Transport: jobRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 503, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":{"message":"generation outcome lost"}}`))}, nil
	})}
	c, w := jobContext(`{"model":"uncertain-model","messages":[{"role":"user","content":"hello"}]}`, 1)
	c.Request.URL.Path = "/v1/chat/completions"
	c.Set(ctxkey.ChannelID, "openai")
	c.Set(ctxkey.RequestModel, "uncertain-model")
	c.Set(ctxkey.OriginalModel, "uncertain-model")
	Relay(c)
	var ledger model.InferenceSettlement
	if calls != 1 || w.Code != 502 || !strings.Contains(w.Body.String(), "outcome_unknown") || w.Header().Get("x-should-retry") != "false" || model.DB.First(&ledger).Error != nil || ledger.Status != "reserved" || ledger.PendingReason == "" {
		t.Fatalf("503 replayed or refunded: calls=%d status=%d body=%s ledger=%+v", calls, w.Code, w.Body.String(), ledger)
	}
}
