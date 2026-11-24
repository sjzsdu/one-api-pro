// audio_media_test.go 音频与图片中继失败退款及计费回归测试
// Verifies observable media delivery, balance, token and subscription accounting.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package controller

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/modelbus/one-api-pro/common/client"
	"github.com/modelbus/one-api-pro/common/config"
	"github.com/modelbus/one-api-pro/common/ctxkey"
	dbmodel "github.com/modelbus/one-api-pro/model"
	"github.com/modelbus/one-api-pro/relay/adaptor"
	"github.com/modelbus/one-api-pro/relay/adaptor/provider/ali"
	"github.com/modelbus/one-api-pro/relay/meta"
	"github.com/modelbus/one-api-pro/relay/registry"
	"github.com/modelbus/one-api-pro/relay/relaymode"
	schema "github.com/modelbus/one-api-pro/relay/schema"
)

type audioTestTransport func(*http.Request) (*http.Response, error)

func (f audioTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func setupAudioMedia(t *testing.T, model string, inputPrice, outputPrice float64) (int, int) {
	t.Helper()
	uid, tid, _ := setupMultimodalFixture(t, 10000)
	oldApprox := config.ApproximateTokenEnabled
	config.ApproximateTokenEnabled = true
	t.Cleanup(func() { config.ApproximateTokenEnabled = oldApprox })
	cache := dbmodel.GetModelPriceCacheForTest()
	cache.Lock()
	dbmodel.SetModelPriceMapForTest(map[string]*dbmodel.ModelPrice{model: {ModelName: model, InputPrice: inputPrice, OutputPrice: outputPrice, BillingType: dbmodel.BillingTypeToken, Enabled: true}})
	cache.Unlock()
	groupCache := dbmodel.GetGroupPriceCacheForTest()
	groupCache.Lock()
	dbmodel.SetGroupPriceMapForTest(map[string]map[string]float64{"default": {"": 0.5}})
	groupCache.Unlock()
	t.Cleanup(func() { groupCache.Lock(); dbmodel.SetGroupPriceMapForTest(nil); groupCache.Unlock() })
	return uid, tid
}

func newAudioMediaContext(body, channel string, uid, tid int) (*gin.Context, *httptest.ResponseRecorder) {
	c, w := newMultimodalContext([]byte(body), channel, "https://api.example", uid, tid)
	c.Request.URL.Path = "/v1/audio/speech"
	c.Set(ctxkey.RequestModel, "cosyvoice-v3-flash")
	return c, w
}

func installAudioHTTP(t *testing.T, fn audioTestTransport) {
	t.Helper()
	old := client.HTTPClient
	client.HTTPClient = &http.Client{Transport: fn}
	t.Cleanup(func() { client.HTTPClient = old })
}

func assertNoAudioConsumption(t *testing.T, uid, tid int) {
	t.Helper()
	if got := readQuota(t, uid); got != 10000 {
		t.Fatalf("user balance changed: %d", got)
	}
	var token dbmodel.Token
	if err := dbmodel.DB.First(&token, tid).Error; err != nil {
		t.Fatal(err)
	}
	if token.RemainQuota != 10000000 {
		t.Fatalf("token balance changed: %d", token.RemainQuota)
	}
	var user dbmodel.User
	_ = dbmodel.DB.First(&user, uid).Error
	if user.UsedQuota != 0 || user.RequestCount != 0 {
		t.Fatalf("user stats consumed: %+v", user)
	}
	var count int64
	dbmodel.DB.Model(&dbmodel.Log{}).Count(&count)
	if count != 0 {
		t.Fatalf("failed call created %d consumption logs", count)
	}
}

// TestRelayAudio_NativeFailuresKeepPending preserves unconfirmed provider execution.
// 版本: v0.0.28
// 日期: 2026-11-23
func TestRelayAudio_NativeFailuresKeepPending(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"invalid JSON", `broken`}, {"missing audio", `{"output":{}}`},
		{"download rejected", `{"output":{"audio":{"url":"http://127.0.0.1/audio"}},"usage":{"characters":2}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			uid, tid := setupAudioMedia(t, "cosyvoice-v3-flash", 2, 0)
			installAudioHTTP(t, func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.body)), Request: r}, nil
			})
			c, w := newAudioMediaContext(`{"model":"cosyvoice-v3-flash","input":"你好","voice":"longanhuan_v3"}`, "ali", uid, tid)
			if err := RelayAudioHelper(c, relaymode.AudioSpeech); err == nil {
				t.Fatal("expected failure")
			}
			if w.Body.Len() != 0 || c.Writer.Written() {
				t.Fatalf("failure committed client body: %q", w.Body.String())
			}
			assertPendingMediaReservation(t, uid, tid)
		})
	}
}

// TestRelayAudio_InvalidNativeRequestIs400 verifies the gateway media contract.
// 版本: v0.0.28
// 日期: 2026-11-23
func TestRelayAudio_InvalidNativeRequestIs400(t *testing.T) {
	uid, tid := setupAudioMedia(t, "cosyvoice-v3-flash", 2, 0)
	calls := 0
	installAudioHTTP(t, func(r *http.Request) (*http.Response, error) { calls++; return nil, errors.New("unexpected request") })
	c, _ := newAudioMediaContext(`{"model":"cosyvoice-v3-flash","input":"你好","voice":"longanhuan_v3","speed":0}`, "ali", uid, tid)
	err := RelayAudioHelper(c, relaymode.AudioSpeech)
	if err == nil || err.StatusCode != 400 {
		t.Fatalf("expected 400: %+v", err)
	}
	if calls != 0 {
		t.Fatal("invalid request sent upstream")
	}
	assertNoAudioConsumption(t, uid, tid)
}

// TestRelayAudio_SubscriptionFailureDoesNotConsume verifies the gateway media contract.
// 版本: v0.0.28
// 日期: 2026-11-23
func TestRelayAudio_SubscriptionFailureDoesNotConsume(t *testing.T) {
	uid, tid := setupAudioMedia(t, "cosyvoice-v3-flash", 2, 0)
	extendSubscriptionTestDB(t)
	upID := seedSubscription(t, uid, "cosyvoice-v3-flash", 100000)
	installAudioHTTP(t, func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"output":{"audio":{"url":"http://127.0.0.1/audio"}}}`)), Request: r}, nil
	})
	c, _ := newAudioMediaContext(`{"model":"cosyvoice-v3-flash","input":"你好","voice":"longanhuan_v3"}`, "ali", uid, tid)
	c.Set(ctxkey.PlanId, upID)
	if err := RelayAudioHelper(c, relaymode.AudioSpeech); err == nil {
		t.Fatal("expected failure")
	}
	assertNoAudioConsumption(t, uid, tid)
	used, err := dbmodel.GetUserPlanUsedAmount(upID)
	if err != nil || used != 0 {
		t.Fatalf("plan consumed: %d %v", used, err)
	}
	var count int64
	dbmodel.DB.Model(&dbmodel.PlanUsage{}).Count(&count)
	if count != 0 {
		t.Fatalf("plan windows consumed: %d", count)
	}
}

// TestRelayAudio_CompatibleBytesAndCharacterBilling verifies the gateway media contract.
// 版本: v0.0.28
// 日期: 2026-11-23
func TestRelayAudio_CompatibleBytesAndCharacterBilling(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		price       float64
		want        int64
	}{{"Chinese", "你好", 4, 4}, {"ASCII", "hi", 4, 4}, {"free", "你好", 0, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			uid, tid := setupAudioMedia(t, "tts-test", tc.price, 99)
			body := `{"model":"tts-test","input":"` + tc.input + `","voice":"alloy","speed":1.1,"response_format":"mp3","extra":"preserve"}`
			installAudioHTTP(t, func(r *http.Request) (*http.Response, error) {
				data, _ := io.ReadAll(r.Body)
				if string(data) != body {
					t.Fatalf("passthrough changed: %s", data)
				}
				if r.URL.Path != "/audio/speech" || r.Header.Get("Authorization") != "Bearer channel-secret-key" {
					t.Fatalf("wrong compatible request: %s", r.URL)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"audio/mpeg"}}, Body: io.NopCloser(strings.NewReader("mp3bytes")), Request: r}, nil
			})
			c, w := newAudioMediaContext(body, "openai_compatible", uid, tid)
			if err := RelayAudioHelper(c, relaymode.AudioSpeech); err != nil {
				t.Fatal(err)
			}
			waitForQuota(t, uid, 10000-tc.want)
			if w.Body.String() != "mp3bytes" || w.Header().Get("Content-Type") != "audio/mpeg" {
				t.Fatalf("wrong binary output %q", w.Body.String())
			}
		})
	}
}

type usageSpeechAdaptor struct {
	ali.Adaptor
	result *schema.AudioSpeechResponse
}

func (a *usageSpeechAdaptor) AudioSpeechResponse(c *gin.Context, resp *http.Response, m *meta.Meta) (*schema.AudioSpeechResponse, *schema.ErrorWithStatusCode) {
	resp.Body.Close()
	return a.result, nil
}

// TestRelayAudio_NativeActualUsageBilling verifies the gateway media contract.
// 版本: v0.0.28
// 日期: 2026-11-23
func TestRelayAudio_NativeActualUsageBilling(t *testing.T) {
	for _, tokenUsage := range []bool{false, true} {
		name := "characters"
		if tokenUsage {
			name = "tokens"
		}
		t.Run(name, func(t *testing.T) {
			uid, tid := setupAudioMedia(t, "speech-native", 2, 5)
			characters := 8
			result := &schema.AudioSpeechResponse{Body: []byte("audio"), ContentType: "audio/wav", Characters: &characters}
			want := int64(8)
			if tokenUsage {
				result.TokenUsage = &schema.Usage{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30}
				want = 60
			}
			id := "speech-test-" + name
			registry.Register(registry.ChannelMeta{ID: id}, func() adaptor.Adaptor { return &usageSpeechAdaptor{result: result} })
			installAudioHTTP(t, func(r *http.Request) (*http.Response, error) {
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewBufferString("{}")), Request: r}, nil
			})
			c, w := newAudioMediaContext(`{"model":"speech-native","input":"你好","voice":"longanhuan_v3"}`, id, uid, tid)
			if err := RelayAudioHelper(c, relaymode.AudioSpeech); err != nil {
				t.Fatal(err)
			}
			waitForQuota(t, uid, 10000-want)
			if w.Body.String() != "audio" {
				t.Fatalf("wrong binary output: %s", w.Body.String())
			}
		})
	}
}

// TestRelayImage_WanFailureDoesNotConsume verifies the gateway media contract.
// 版本: v0.0.28
// 日期: 2026-11-23
func TestRelayImage_WanFailureDoesNotConsume(t *testing.T) {
	uid, tid := setupAudioMedia(t, "wan2.7-image", 2, 0)
	installAudioHTTP(t, func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"output":{"choices":[]}}`)), Request: r}, nil
	})
	c, w := newAudioMediaContext(`{"model":"wan2.7-image","prompt":"mountain","n":1}`, "ali", uid, tid)
	c.Request.URL.Path = "/v1/images/generations"
	if err := RelayImageHelper(c, relaymode.ImagesGenerations); err == nil {
		t.Fatal("expected failure")
	}
	if w.Body.Len() != 0 {
		t.Fatal("failure wrote client output")
	}
	assertPendingMediaReservation(t, uid, tid)
}

// TestRelayImage_WanSuccessContractAndBilling verifies the gateway media contract.
// 版本: v0.0.28
// 日期: 2026-11-23
func TestRelayImage_WanSuccessContractAndBilling(t *testing.T) {
	uid, tid := setupAudioMedia(t, "wan2.7-image", 0, 0)
	cache := dbmodel.GetModelPriceCacheForTest()
	cache.Lock()
	dbmodel.SetModelPriceMapForTest(map[string]*dbmodel.ModelPrice{"wan2.7-image": {ModelName: "wan2.7-image", PerRequestPrice: 0.002, BillingType: dbmodel.BillingTypePerRequest, Enabled: true}})
	cache.Unlock()
	installAudioHTTP(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/v1/services/aigc/multimodal-generation/generation" || r.Header.Get("X-DashScope-Async") != "" {
			t.Fatalf("wrong Wan endpoint or async mode: %s", r.URL)
		}
		body, _ := io.ReadAll(r.Body)
		if !bytes.Contains(body, []byte(`"messages"`)) || !bytes.Contains(body, []byte(`"text":"mountain"`)) || !bytes.Contains(body, []byte(`"size":"1080*1440"`)) {
			t.Fatalf("wrong native request: %s", body)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"output":{"choices":[{"message":{"content":[{"image":"https://cdn.example/wan.png"}]}}]}}`)), Request: r}, nil
	})
	c, w := newAudioMediaContext(`{"model":"wan2.7-image","prompt":"mountain","size":"1080x1440","n":1}`, "ali", uid, tid)
	c.Request.URL.Path = "/v1/images/generations"
	if err := RelayImageHelper(c, relaymode.ImagesGenerations); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(w.Body.String(), `"url":"https://cdn.example/wan.png"`) {
		t.Fatalf("wrong output %s", w.Body.String())
	}
	if got := readQuota(t, uid); got != 9000 {
		t.Fatalf("wrong image charge: %d", got)
	}
	var token dbmodel.Token
	_ = dbmodel.DB.First(&token, tid).Error
	if token.RemainQuota != 9999000 {
		t.Fatalf("wrong token charge: %d", token.RemainQuota)
	}
	var count int64
	dbmodel.DB.Model(&dbmodel.Log{}).Count(&count)
	if count != 1 {
		t.Fatalf("expected one log, got %d", count)
	}
}

// TestRelayAudio_MissingTokenUsageKeepsPending requires actual usage for Qwen Flash.
// 版本: v0.0.28
// 日期: 2026-11-23
func TestRelayAudio_MissingTokenUsageKeepsPending(t *testing.T) {
	uid, tid := setupAudioMedia(t, "qwen-audio-3.1-tts-flash", 2, 5)
	installAudioHTTP(t, func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"output":{"audio":{"url":"https://cdn.example/audio.wav"}},"usage":{}}`)), Request: r}, nil
	})
	c, w := newAudioMediaContext(`{"model":"qwen-audio-3.1-tts-flash","input":"你好","voice":"longanhuan_v3.1"}`, "ali", uid, tid)
	err := RelayAudioHelper(c, relaymode.AudioSpeech)
	if err == nil || err.Error.Code != "outcome_unknown" {
		t.Fatalf("missing usage accepted: %+v", err)
	}
	if w.Body.Len() != 0 || c.Writer.Written() {
		t.Fatal("missing usage committed response")
	}
	assertPendingMediaReservation(t, uid, tid)
}

// TestRelayAudio_FreeNativeRequiresNoBalance preserves intentional zero-price models.
// 版本: v0.0.28
// 日期: 2026-11-23
func TestRelayAudio_FreeNativeRequiresNoBalance(t *testing.T) {
	uid, tid := setupAudioMedia(t, "speech-free", 0, 0)
	if err := dbmodel.DB.Model(&dbmodel.User{}).Where("id = ?", uid).Update("quota", 0).Error; err != nil {
		t.Fatal(err)
	}
	characters := 2
	registry.Register(registry.ChannelMeta{ID: "speech-free-test"}, func() adaptor.Adaptor {
		return &usageSpeechAdaptor{result: &schema.AudioSpeechResponse{Body: []byte("audio"), ContentType: "audio/wav", Characters: &characters}}
	})
	installAudioHTTP(t, func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{}")), Request: r}, nil
	})
	c, w := newAudioMediaContext(`{"model":"speech-free","input":"你好","voice":"voice"}`, "speech-free-test", uid, tid)
	if err := RelayAudioHelper(c, relaymode.AudioSpeech); err != nil {
		t.Fatal(err)
	}
	if w.Body.String() != "audio" || readQuota(t, uid) != 0 {
		t.Fatalf("free speech failed: %q", w.Body.String())
	}
}
