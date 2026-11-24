// outcome_test.go 上游执行结果不确定与成功响应交付回归
// Preserves holds after uncertain execution and delivers already generated results.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package controller

import (
	"errors"
	"gorm.io/gorm"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/modelbus/one-api-pro/common/ctxkey"
	dbmodel "github.com/modelbus/one-api-pro/model"
	"github.com/modelbus/one-api-pro/relay/relaymode"
	schema "github.com/modelbus/one-api-pro/relay/schema"
)

func assertPendingMediaReservation(t *testing.T, uid, tid int) {
	t.Helper()
	row := readTextSettlement(t)
	if row.Status != "reserved" || row.PendingReason == "" || readQuota(t, uid) != 10000-row.ReservedQuota || readToken(t, tid).RemainQuota != 10000000-row.ReservedQuota {
		t.Fatalf("unknown execution released its hold: %+v", row)
	}
}

func TestMedia_ExplicitRejectionRefunds(t *testing.T) {
	for _, kind := range []string{"typed", "audio", "image"} {
		t.Run(kind, func(t *testing.T) {
			name, body := "wan2.7-image", `{"model":"wan2.7-image","prompt":"scene","n":1}`
			if kind == "typed" {
				name, body = "gte-rerank-v2", `{"model":"gte-rerank-v2","query":"q","documents":["d"]}`
			}
			if kind == "audio" {
				name, body = "cosyvoice-v3-flash", `{"model":"cosyvoice-v3-flash","input":"hello","voice":"longanhuan_v3"}`
			}
			uid, tid := setupAudioMedia(t, name, 2, 0)
			installAudioHTTP(t, func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 400, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":{"message":"rejected"}}`)), Request: r}, nil
			})
			c, _ := newAudioMediaContext(body, "ali", uid, tid)
			c.Set(ctxkey.RequestModel, name)
			switch kind {
			case "typed":
				RelayOperationHelper(c, "rerank.score")
			case "audio":
				RelayAudioHelper(c, relaymode.AudioSpeech)
			case "image":
				c.Request.URL.Path = "/v1/images/generations"
				RelayImageHelper(c, relaymode.ImagesGenerations)
			}
			row := readTextSettlement(t)
			if row.Status != "refunded" {
				t.Fatalf("explicit rejection not refunded: %+v", row)
			}
			assertNoAudioConsumption(t, uid, tid)
		})
	}
}

func TestBufferedInferenceResponse_DefaultStatusAndBound(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	w := newBufferedInferenceResponse(c.Writer)
	if w.Status() != 200 || w.Written() {
		t.Fatal("invalid initial response state")
	}
	if _, err := w.Write(make([]byte, (64<<20)+1)); err == nil || w.Size() != 0 || w.Written() {
		t.Fatal("oversized response accepted or truncated")
	}
	if _, err := w.WriteString("ok"); err != nil || w.Status() != 200 || w.Size() != 2 {
		t.Fatal("default successful status lost")
	}
}

func TestRelayImage_LargeBase64ResponseIsDelivered(t *testing.T) {
	uid, tid := setupAudioMedia(t, "image-model", 2, 0)
	payload := `{"data":[{"b64_json":"` + strings.Repeat("A", 9<<20) + `"}]}`
	installAudioHTTP(t, func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(payload)), Request: r}, nil
	})
	c, w := newAudioMediaContext(`{"model":"image-model","prompt":"scene","n":1}`, "openai_compatible", uid, tid)
	c.Set(ctxkey.RequestModel, "image-model")
	c.Request.URL.Path = "/v1/images/generations"
	if err := RelayImageHelper(c, relaymode.ImagesGenerations); err != nil {
		t.Fatal(err)
	}
	if w.Body.String() != payload || readTextSettlement(t).Status != "settled" {
		t.Fatal("large base64 image was lost or refunded")
	}
}

func TestMedia_UncertainExecutionKeepsReservation(t *testing.T) {
	for _, kind := range []string{"typed", "audio", "image", "text"} {
		for _, failure := range []string{"transport", "read", "invalid"} {
			t.Run(kind+"/"+failure, func(t *testing.T) {
				name, body := "wan2.7-image", `{"model":"wan2.7-image","prompt":"scene","n":1}`
				if kind == "typed" {
					name, body = "gte-rerank-v2", `{"model":"gte-rerank-v2","query":"hello","documents":["world"]}`
				}
				if kind == "audio" {
					name, body = "cosyvoice-v3-flash", `{"model":"cosyvoice-v3-flash","input":"hello","voice":"longanhuan_v3"}`
				}
				if kind == "text" {
					name, body = "gpt-4o", `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`
				}
				uid, tid := setupAudioMedia(t, name, 2, 0)
				installAudioHTTP(t, func(r *http.Request) (*http.Response, error) {
					if failure == "transport" {
						return nil, errors.New("connection lost after sending")
					}
					var reader io.ReadCloser = io.NopCloser(strings.NewReader(`broken`))
					if failure == "read" {
						reader = failedEmbeddingBody{}
					}
					return &http.Response{StatusCode: 200, Header: make(http.Header), Body: reader, Request: r}, nil
				})
				c, _ := newAudioMediaContext(body, "ali", uid, tid)
				if kind == "text" {
					c, _ = newAudioMediaContext(body, "openai", uid, tid)
					c.Request.URL.Path = "/v1/chat/completions"
				}
				c.Set(ctxkey.RequestModel, name)
				var err *schema.ErrorWithStatusCode
				switch kind {
				case "typed":
					err = RelayOperationHelper(c, "rerank.score")
				case "audio":
					err = RelayAudioHelper(c, relaymode.AudioSpeech)
				case "image":
					c.Request.URL.Path = "/v1/images/generations"
					err = RelayImageHelper(c, relaymode.ImagesGenerations)
				case "text":
					err = RelayTextHelper(c)
				}
				row := readTextSettlement(t)
				if err == nil || !schema.ExecutionMayHaveSucceeded(err) || row.Status != "reserved" || row.PendingReason == "" || c.Writer.Header().Get("X-Inference-Id") != row.Key || c.Writer.Header().Get("x-should-retry") != "false" {
					t.Fatalf("unsafe unknown execution: err=%+v ledger=%+v headers=%v", err, row, c.Writer.Header())
				}
			})
		}
	}
}

func TestRelayImage_ActualAdditionalResultsAreCharged(t *testing.T) {
	uid, tid := setupAudioMedia(t, "wan2.7-image", 2, 0)
	installAudioHTTP(t, func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"output":{"choices":[{"message":{"content":[{"image":"https://cdn.example/1.png"},{"image":"https://cdn.example/2.png"}]}}]}}`)), Request: r}, nil
	})
	c, w := newAudioMediaContext(`{"model":"wan2.7-image","prompt":"scene","n":1}`, "ali", uid, tid)
	c.Request.URL.Path = "/v1/images/generations"
	if err := RelayImageHelper(c, relaymode.ImagesGenerations); err != nil {
		t.Fatal(err)
	}
	row := readTextSettlement(t)
	if row.Status != "settled" || !strings.Contains(row.UsageJSON, `"quantity":2`) || !strings.Contains(w.Body.String(), "2.png") {
		t.Fatalf("actual output lost: %+v %s", row, w.Body.String())
	}
}

func TestRelayText_SettlementFailureStillDeliversGeneratedResult(t *testing.T) {
	uid, _, c := setupTextLedger(t, "openai", false)
	installAudioHTTP(t, func(r *http.Request) (*http.Response, error) {
		// The provider has consumed tokens; concurrent account depletion makes
		// the first atomic finalization fail, but must not hide generated output.
		dbmodel.DB.Model(&dbmodel.User{}).Where("id = ?", uid).UpdateColumn("quota", 0)
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"role":"assistant","content":"generated answer"}}],"usage":{"prompt_tokens":10,"completion_tokens":40000,"total_tokens":40010}}`)), Request: r}, nil
	})
	if err := RelayTextHelper(c); err != nil {
		t.Fatalf("generated output turned into error: %+v", err)
	}
	row := readTextSettlement(t)
	if row.Status != "reserved" || row.PendingReason == "" || row.PendingQuota == nil || row.PendingUsage == "" || !strings.Contains(string(row.ResultJSON), "generated answer") || !c.Writer.Written() {
		t.Fatalf("missing pending result: %+v", row)
	}
	if c.Writer.Header().Get("X-Inference-Status") != "settlement_pending" {
		t.Fatal("missing pending status header")
	}
	dbmodel.DB.Model(&dbmodel.User{}).Where("id = ?", uid).UpdateColumn("quota", 1000000)
	if err := dbmodel.RetryPendingInferenceSettlements(c.Request.Context(), 4); err != nil {
		t.Fatal(err)
	}
	if err := dbmodel.RetryPendingInferenceSettlements(c.Request.Context(), 4); err != nil {
		t.Fatal(err)
	}
	row = readTextSettlement(t)
	if row.Status != "settled" || row.PendingQuota != nil || row.PendingReason != "" {
		t.Fatalf("retry failed: %+v", row)
	}
}

func TestRelayText_StreamWithoutUsageUsesEstimatedTokens(t *testing.T) {
	_, _, c := setupTextLedger(t, "openai", true)
	installAudioHTTP(t, func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"generated answer\"}}]}\n\ndata: [DONE]\n\n")), Request: r}, nil
	})
	if err := RelayTextHelper(c); err != nil {
		t.Fatal(err)
	}
	row := readTextSettlement(t)
	input, output := dbmodel.InferenceUsageTokens(row.UsageJSON)
	if row.Status != "settled" || input <= 0 || output <= 0 || row.TotalQuota <= 0 {
		t.Fatalf("estimated usage lost: %+v", row)
	}
}

func TestRelayText_PriorReadFailureQueuesExactCharge(t *testing.T) {
	_, _, c := setupTextLedger(t, "openai", false)
	failed, deadlineBounded := false, false
	installAudioHTTP(t, func(r *http.Request) (*http.Response, error) {
		if err := dbmodel.DB.Callback().Query().Before("gorm:query").Register("test:prior_read_failure", func(tx *gorm.DB) {
			if _, ok := tx.Statement.Dest.(*dbmodel.InferenceSettlement); ok && !failed {
				failed = true
				_, deadlineBounded = tx.Statement.Context.Deadline()
				tx.AddError(errors.New("transient prior observation read failure"))
			}
		}); err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"role":"assistant","content":"generated answer"}}],"usage":{"prompt_tokens":10,"completion_tokens":4,"total_tokens":14}}`)), Request: r}, nil
	})
	if err := RelayTextHelper(c); err != nil {
		t.Fatal(err)
	}
	row := readTextSettlement(t)
	if !failed || !deadlineBounded || row.Status != "reserved" || row.PendingQuota == nil || *row.PendingQuota != 20 || row.PendingUsage == "" || !strings.Contains(string(row.ResultJSON), "generated answer") || c.Writer.Header().Get("X-Inference-Status") != "settlement_pending" {
		t.Fatalf("prior failure lost exact settlement: failed=%v bounded=%v row=%+v", failed, deadlineBounded, row)
	}
	if err := dbmodel.RetryPendingInferenceSettlements(c.Request.Context(), 4); err != nil {
		t.Fatal(err)
	}
	row = readTextSettlement(t)
	if row.Status != "settled" || row.TotalQuota != 20 {
		t.Fatalf("exact charge retry failed: %+v", row)
	}
}
