// text_ledger_test.go 既有文本协议持久结算与部分流响应回归测试
// Verifies legacy text relay uses one durable reservation without double charging.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package controller

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/modelbus/one-api-pro/common/ctxkey"
	dbmodel "github.com/modelbus/one-api-pro/model"
	"github.com/modelbus/one-api-pro/relay/adaptor"
	"github.com/modelbus/one-api-pro/relay/adaptor/openai"
	billingratio "github.com/modelbus/one-api-pro/relay/billing/ratio"
	"github.com/modelbus/one-api-pro/relay/meta"
	"github.com/modelbus/one-api-pro/relay/registry"
	schema "github.com/modelbus/one-api-pro/relay/schema"
)

func setupTextLedger(t *testing.T, channel string, stream bool) (int, int, *gin.Context) {
	t.Helper()
	uid, tid := setupAudioMedia(t, "gpt-4o", 2, 5)
	if err := dbmodel.DB.AutoMigrate(&dbmodel.InferenceSettlement{}); err != nil {
		t.Fatal(err)
	}
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`
	if stream {
		body = `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}],"stream":true}`
	}
	c, _ := newMultimodalContext([]byte(body), channel, "https://api.example", uid, tid)
	c.Request.URL.Path = "/v1/chat/completions"
	c.Set(ctxkey.RequestModel, "gpt-4o")
	return uid, tid, c
}

func readTextSettlement(t *testing.T) dbmodel.InferenceSettlement {
	t.Helper()
	var rows []dbmodel.InferenceSettlement
	if err := dbmodel.DB.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected one durable reservation, got %d", len(rows))
	}
	return rows[0]
}

// TestRelayText_DurableSettlement charges the same token price once and exposes its audit key.
// 版本: v0.0.28
// 日期: 2026-11-23
func TestRelayText_DurableSettlement(t *testing.T) {
	uid, tid, c := setupTextLedger(t, "openai", false)
	installAudioHTTP(t, func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"role":"assistant","content":"hello"}}],"usage":{"prompt_tokens":10,"completion_tokens":4,"total_tokens":14}}`)), Request: r}, nil
	})
	if err := RelayTextHelper(c); err != nil {
		t.Fatal(err)
	}
	row := readTextSettlement(t)
	if row.Status != "settled" || row.TotalQuota != 20 || c.Writer.Header().Get("X-Inference-Id") != row.Key {
		t.Fatalf("wrong settlement: %+v", row)
	}
	if readQuota(t, uid) != 9980 || readToken(t, tid).RemainQuota != 9999980 {
		t.Fatal("legacy and durable paths double charged")
	}
	if err := dbmodel.SettleInference(context.Background(), row.Key, row.TotalQuota, row.UsageJSON); err != nil {
		t.Fatal(err)
	}
	var u dbmodel.User
	dbmodel.DB.First(&u, uid)
	if readQuota(t, uid) != 9980 || u.RequestCount != 1 || u.UsedQuota != 20 {
		t.Fatalf("duplicate settlement: %+v", u)
	}
}

// TestRelayText_FailedAttemptRefunds persists failure recovery before another channel is attempted.
// 版本: v0.0.28
// 日期: 2026-11-23
func TestRelayText_FailedAttemptRefunds(t *testing.T) {
	uid, tid, c := setupTextLedger(t, "openai", false)
	installAudioHTTP(t, func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 400, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":{"message":"rejected"}}`)), Request: r}, nil
	})
	if err := RelayTextHelper(c); err == nil {
		t.Fatal("expected upstream failure")
	}
	if row := readTextSettlement(t); row.Status != "refunded" {
		t.Fatalf("failed request not refunded: %+v", row)
	}
	assertNoAudioConsumption(t, uid, tid)
}

type failedEmbeddingBody struct{}

func (failedEmbeddingBody) Read([]byte) (int, error) { return 0, errors.New("response truncated") }
func (failedEmbeddingBody) Close() error             { return nil }

// TestRelayMultimodalEmbedding_ReadFailureKeepsPendingAudit prevents retrying an unknown upstream result.
// 版本: v0.0.28
// 日期: 2026-11-23
func TestRelayMultimodalEmbedding_ReadFailureKeepsPendingAudit(t *testing.T) {
	uid, tid, _ := setupMultimodalFixture(t, 10000)
	c, _ := newMultimodalContext([]byte(`{"model":"qwen3-vl-embedding","input":{"contents":[{"text":"hello"}]}}`), "alibailian", "https://api.example", uid, tid)
	installAudioHTTP(t, func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: failedEmbeddingBody{}, Request: r}, nil
	})
	err := RelayMultimodalEmbeddingHelper(c)
	if err == nil || err.Error.Code != "outcome_unknown" {
		t.Fatalf("expected nonretryable unknown result: %+v", err)
	}
	row := readTextSettlement(t)
	if row.Status != "reserved" || !strings.Contains(row.UsageJSON, "upstream_response_read_failed") {
		t.Fatalf("lost pending audit: %+v", row)
	}
}

type partialTextAdaptor struct {
	openai.Adaptor
	usage *schema.Usage
}

func (a *partialTextAdaptor) DoResponse(c *gin.Context, r *http.Response, m *meta.Meta) (*schema.Usage, *schema.ErrorWithStatusCode) {
	r.Body.Close()
	c.Header("Content-Type", "text/event-stream")
	_, _ = c.Writer.WriteString("data: partial\n\n")
	return a.usage, openai.ErrorWrapper(errors.New("stream interrupted"), "stream_interrupted", 502)
}

// TestRelayText_InterruptedStreamDoesNotBecomeFree preserves known usage or an explicit pending hold.
// 版本: v0.0.28
// 日期: 2026-11-23
func TestRelayText_InterruptedStreamDoesNotBecomeFree(t *testing.T) {
	for _, known := range []bool{true, false} {
		t.Run(map[bool]string{true: "known_usage", false: "unknown_usage"}[known], func(t *testing.T) {
			id := "text-partial-known"
			var usage *schema.Usage
			if known {
				usage = &schema.Usage{PromptTokens: 10, CompletionTokens: 4, TotalTokens: 14}
			} else {
				id = "text-partial-unknown"
			}
			registry.Register(registry.ChannelMeta{ID: id}, func() adaptor.Adaptor { return &partialTextAdaptor{usage: usage} })
			uid, _, c := setupTextLedger(t, id, true)
			installAudioHTTP(t, func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
			})
			if err := RelayTextHelper(c); err == nil {
				t.Fatal("interrupted stream silently succeeded")
			}
			row := readTextSettlement(t)
			if known {
				if row.Status != "settled" || row.TotalQuota != 20 || readQuota(t, uid) != 9980 || !strings.Contains(row.UsageJSON, "gateway.partial_response") {
					t.Fatalf("known usage refunded: %+v", row)
				}
			} else if row.Status != "reserved" || readQuota(t, uid) >= 10000 || !strings.Contains(row.UsageJSON, "interrupted_stream_usage_missing") {
				t.Fatalf("unknown partial stream silently became free: %+v", row)
			}
		})
	}
}

// TestRelayText_SubscriptionLedgerKeepsTokenWindows preserves cached-token and plan accounting.
// 版本: v0.0.28
// 日期: 2026-11-23
func TestRelayText_SubscriptionLedgerKeepsTokenWindows(t *testing.T) {
	uid, tid, c := setupTextLedger(t, "openai", false)
	extendSubscriptionTestDB(t)
	planID := seedSubscription(t, uid, "gpt-4o", 100000)
	c.Set(ctxkey.PlanId, planID)
	installAudioHTTP(t, func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"role":"assistant","content":"hello"}}],"usage":{"prompt_tokens":10,"completion_tokens":4,"total_tokens":14,"prompt_tokens_details":{"cached_tokens":3}}}`)), Request: r}, nil
	})
	if err := RelayTextHelper(c); err != nil {
		t.Fatal(err)
	}
	row := readTextSettlement(t)
	if row.Status != "settled" || row.TotalQuota != 17 || readQuota(t, uid) != 10000 || readToken(t, tid).RemainQuota != 10000000 {
		t.Fatalf("subscription double charged balance: %+v", row)
	}
	used, err := dbmodel.GetUserPlanUsedAmount(planID)
	if err != nil || used != 17 {
		t.Fatalf("plan amount=%d err=%v", used, err)
	}
	rows, err := dbmodel.GetPlanUsageByUserPlanId(planID)
	if err != nil || len(rows) != 3 {
		t.Fatalf("plan windows=%+v err=%v", rows, err)
	}
	for _, window := range rows {
		if window.Requests != 1 || window.PromptTokens != 10 || window.CompletionTokens != 4 || window.CachedTokens != 3 {
			t.Fatalf("usage lost: %+v", window)
		}
	}
	m := &meta.Meta{ReservationKey: row.Key, UserId: uid, TokenId: tid, PlanId: planID, ChannelId: 7, OriginModelName: "gpt-4o", ActualModelName: "gpt-4o"}
	usage := &schema.Usage{PromptTokens: 10, CompletionTokens: 4, TotalTokens: 14, PromptTokensDetails: &schema.PromptTokensDetails{CachedTokens: 3}}
	price := &billingratio.PriceResult{InputPrice: 2, OutputPrice: 5, BillingType: dbmodel.BillingTypeToken}
	if err := postConsumeQuota(context.Background(), usage, m, &schema.GeneralOpenAIRequest{}, row.ReservedQuota, price, .5, false); err != nil {
		t.Fatal(err)
	}
	used, _ = dbmodel.GetUserPlanUsedAmount(planID)
	if used != 17 {
		t.Fatal("duplicate plan settlement")
	}
}

// TestRelayText_ZeroPriceNeedsNoBalance preserves explicit free models in the shared ledger.
// 版本: v0.0.28
// 日期: 2026-11-23
func TestRelayText_ZeroPriceNeedsNoBalance(t *testing.T) {
	uid, tid := setupAudioMedia(t, "gpt-4o", 0, 0)
	if err := dbmodel.DB.AutoMigrate(&dbmodel.InferenceSettlement{}); err != nil {
		t.Fatal(err)
	}
	dbmodel.DB.Model(&dbmodel.User{}).Where("id = ?", uid).Update("quota", 0)
	c, _ := newMultimodalContext([]byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`), "openai", "https://api.example", uid, tid)
	c.Request.URL.Path = "/v1/chat/completions"
	c.Set(ctxkey.RequestModel, "gpt-4o")
	installAudioHTTP(t, func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"role":"assistant","content":"hello"}}],"usage":{"prompt_tokens":10,"completion_tokens":4,"total_tokens":14}}`)), Request: r}, nil
	})
	if err := RelayTextHelper(c); err != nil {
		t.Fatal(err)
	}
	if row := readTextSettlement(t); row.Status != "settled" || row.TotalQuota != 0 || readQuota(t, uid) != 0 {
		t.Fatalf("free call charged: %+v", row)
	}
}
