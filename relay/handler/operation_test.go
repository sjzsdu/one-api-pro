// operation_test.go 强类型操作的预留和账务回归
// Checks exact image reservation, fixed-point metering and durable legacy media settlement.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package controller

import (
	"context"
	"encoding/json"
	"github.com/modelbus/one-api-pro/common/client"
	"github.com/modelbus/one-api-pro/common/ctxkey"
	dbmodel "github.com/modelbus/one-api-pro/model"
	"github.com/modelbus/one-api-pro/relay/adaptor"
	"github.com/modelbus/one-api-pro/relay/adaptor/provider/ali"
	ratio "github.com/modelbus/one-api-pro/relay/billing/ratio"
	"github.com/modelbus/one-api-pro/relay/meta"
	"github.com/modelbus/one-api-pro/relay/registry"
	"github.com/modelbus/one-api-pro/relay/relaymode"
	schema "github.com/modelbus/one-api-pro/relay/schema"
	"io"
	"net/http"
	"strings"
	"testing"
)

type typedEditAdaptor struct{ ali.Adaptor }

func (a *typedEditAdaptor) EditImage(ctx context.Context, m *meta.Meta, r *schema.ImageEditRequest) (*schema.OperationResult, error) {
	return &schema.OperationResult{Body: json.RawMessage(`{"data":[{"url":"https://example.com/image.png"}]}`), Usage: []schema.UsageMetric{{Metric: "image.generated", Quantity: 1, Scale: 1, Unit: "image"}}}, nil
}

func TestLegacyMedia_FreePerRequestNeedsNoBalance(t *testing.T) {
	for _, kind := range []string{"audio", "image"} {
		t.Run(kind, func(t *testing.T) {
			name := "tts-free"
			if kind == "image" {
				name = "wan2.7-image"
			}
			uid, tid := setupAudioMedia(t, name, 0, 0)
			dbmodel.DB.Model(&dbmodel.User{}).Where("id = ?", uid).Update("quota", 0)
			cache := dbmodel.GetModelPriceCacheForTest()
			cache.Lock()
			dbmodel.SetModelPriceMapForTest(map[string]*dbmodel.ModelPrice{name: {ModelName: name, BillingType: dbmodel.BillingTypePerRequest, PerRequestPrice: 0, Enabled: true}})
			cache.Unlock()
			old := client.HTTPClient
			client.HTTPClient = &http.Client{Transport: audioTestTransport(func(r *http.Request) (*http.Response, error) {
				body := "audio"
				if kind == "image" {
					body = `{"output":{"choices":[{"message":{"content":[{"image":"https://example.com/a.png"}]}}]}}`
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			t.Cleanup(func() { client.HTTPClient = old })
			provider := "openai_compatible"
			body := `{"model":"tts-free","input":"hi","voice":"alloy"}`
			if kind == "image" {
				provider = "ali"
				body = `{"model":"wan2.7-image","prompt":"scene","n":1}`
			}
			c, _ := newAudioMediaContext(body, provider, uid, tid)
			c.Set(ctxkey.RequestModel, name)
			var err *schema.ErrorWithStatusCode
			if kind == "audio" {
				err = RelayAudioHelper(c, relaymode.AudioSpeech)
			} else {
				c.Request.URL.Path = "/v1/images/generations"
				err = RelayImageHelper(c, relaymode.ImagesGenerations)
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := readQuota(t, uid); got != 0 {
				t.Fatalf("free call charged %d", got)
			}
			var ledger dbmodel.InferenceSettlement
			if dbmodel.DB.First(&ledger).Error != nil || ledger.TotalQuota != 0 || ledger.Status != "settled" {
				t.Fatalf("free ledger=%+v", ledger)
			}
		})
	}
}
func TestRelayOperation_ImageReserveUsesRequestedCount(t *testing.T) {
	uid, tid := setupAudioMedia(t, "wan2.7-image", 0, 0)
	cache := dbmodel.GetModelPriceCacheForTest()
	cache.Lock()
	dbmodel.SetModelPriceMapForTest(map[string]*dbmodel.ModelPrice{"wan2.7-image": {ModelName: "wan2.7-image", PerRequestPrice: .02, BillingType: dbmodel.BillingTypePerRequest, Enabled: true}})
	cache.Unlock()
	registry.Register(registry.ChannelMeta{ID: "typed-edit-test"}, func() adaptor.Adaptor { return &typedEditAdaptor{} })
	c, w := newAudioMediaContext(`{"model":"wan2.7-image","prompt":"edit","images":["https://example.com/ref.png"],"n":1}`, "typed-edit-test", uid, tid)
	c.Set(ctxkey.RequestModel, "wan2.7-image")
	c.Request.URL.Path = "/v1/images/edits"
	if err := RelayOperationHelper(c, "image.edit"); err != nil {
		t.Fatal(err)
	}
	if readQuota(t, uid) != 0 || w.Body.Len() == 0 {
		t.Fatal("one image was not charged exactly once")
	}
	var ledger dbmodel.InferenceSettlement
	if err := dbmodel.DB.First(&ledger).Error; err != nil || ledger.ReservedQuota != 10000 || ledger.TotalQuota != 10000 || ledger.Status != "settled" {
		t.Fatalf("ledger=%+v %v", ledger, err)
	}
}
func TestMeteredQuota_ExplicitFractionalUnitsAndFreePrice(t *testing.T) {
	p := &ratio.PriceResult{InputPrice: 2, OutputPrice: 9, BillingType: dbmodel.BillingTypeToken}
	q, err := MeteredQuota(p, .5, []schema.UsageMetric{{Metric: "audio.input_duration", Quantity: 1250, Scale: 1000, Unit: "second"}})
	if err != nil || q != 1 {
		t.Fatalf("fractional metering=%d %v", q, err)
	}
	p.InputPrice = 0
	p.OutputPrice = 0
	q, err = MeteredQuota(p, 1, []schema.UsageMetric{{Metric: "audio.input_duration", Quantity: 300, Scale: 1, Unit: "second"}})
	if err != nil || q != 0 {
		t.Fatal("zero price charged")
	}
}
