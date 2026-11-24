// audio_test.go 火山豆包 V3 语音合成协议转换单测
// Unit tests for Volcengine V3 speech request and NDJSON response conversion.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: opencode
package doubao

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/modelbus/one-api-pro/relay/meta"
	"github.com/modelbus/one-api-pro/relay/relaymode"
	schema "github.com/modelbus/one-api-pro/relay/schema"
)

func TestConvertAudioSpeechRequest(t *testing.T) {
	speed := 1.07
	a := &Adaptor{}
	converted, err := a.ConvertAudioSpeechRequest(&schema.AudioSpeechRequest{
		Model:          "seed-tts-2.0",
		Input:          "你好，欢迎使用语音合成。",
		Voice:          "zh_female_vv_uranus_bigtts",
		ResponseFormat: "mp3",
		Speed:          &speed,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	body, ok := converted.(*volcTTSRequest)
	if !ok {
		t.Fatalf("unexpected type %T", converted)
	}
	if body.ReqParams.Speaker != "zh_female_vv_uranus_bigtts" {
		t.Fatalf("speaker mismatch: %q", body.ReqParams.Speaker)
	}
	if body.ReqParams.AudioParams.Format != "mp3" {
		t.Fatalf("format mismatch: %q", body.ReqParams.AudioParams.Format)
	}
	if body.ReqParams.AudioParams.SpeechRate != 7 {
		t.Fatalf("speech rate mismatch: %d", body.ReqParams.AudioParams.SpeechRate)
	}
	if body.User["uid"] == "" {
		t.Fatal("user uid is empty")
	}
}

func TestConvertAudioSpeechRequestRejectsMissingVoice(t *testing.T) {
	a := &Adaptor{}
	if _, err := a.ConvertAudioSpeechRequest(&schema.AudioSpeechRequest{Input: "hi"}); err == nil {
		t.Fatal("expected error for missing voice")
	}
}

func TestSeedTTSFormatAndSpeechRate(t *testing.T) {
	if got := seedTTSFormat("ogg"); got != "ogg_opus" {
		t.Fatalf("ogg mapping mismatch: %q", got)
	}
	if got := seedTTSFormat(""); got != "wav" {
		t.Fatalf("default format mismatch: %q", got)
	}
	zero := 0.5
	two := 2.0
	if got := seedTTSSpeechRate(&zero); got != -50 {
		t.Fatalf("min rate mismatch: %d", got)
	}
	if got := seedTTSSpeechRate(&two); got != 100 {
		t.Fatalf("max rate mismatch: %d", got)
	}
	if got := seedTTSSpeechRate(nil); got != 0 {
		t.Fatalf("nil rate mismatch: %d", got)
	}
}

func TestVolcTTSRequestIDIsUUID(t *testing.T) {
	first, second := NewVolcTTSRequestID(), NewVolcTTSRequestID()
	if first == "" || second == "" || first == second {
		t.Fatalf("request ids must be unique non-empty: %q %q", first, second)
	}
}

func TestAudioSpeechResponseParsesNDJSON(t *testing.T) {
	// NDJSON: 元数据行(data=null) → 两个音频块 → 结束码。
	lines := []string{
		`{"code":0,"data":null,"sequence":-1}`,
		`{"code":0,"data":"` + "UklGRg==" + `"}`, // "RIFF" base64
		`{"code":0,"data":"` + "ZmFrZQ==" + `"}`, // "fake" base64
		`{"code":20000000,"message":"Success"}`,
		`{"code":0,"data":"should-be-ignored"}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(strings.Join(lines, "\n")))
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	a := &Adaptor{}
	result, respErr := a.AudioSpeechResponse(c, resp, &meta.Meta{})
	if respErr != nil {
		t.Fatalf("unexpected response error: %v", respErr)
	}
	if string(result.Body) != "RIFFfake" {
		t.Fatalf("audio bytes mismatch: %q", string(result.Body))
	}
	if result.ContentType != "application/octet-stream" {
		t.Fatalf("content type mismatch: %q", result.ContentType)
	}
}

func TestAudioSpeechResponseSurfacesUpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":3050,"message":"音色不存在","header":{"message":"voice not found"}}`))
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	a := &Adaptor{}
	_, respErr := a.AudioSpeechResponse(c, resp, &meta.Meta{})
	if respErr == nil {
		t.Fatal("expected error for upstream code 3050")
	}
	var payload map[string]any
	if jsonErr := json.Unmarshal([]byte(respErr.Error.Message), &payload); jsonErr == nil {
		_ = payload
	}
}

func TestGetRequestURLAudioSpeech(t *testing.T) {
	a := &Adaptor{}
	url, err := a.GetRequestURL(&meta.Meta{Mode: relaymode.AudioSpeech, BaseURL: "https://ark.cn-beijing.volces.com/api/plan"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != volcTTSEndpoint {
		t.Fatalf("unexpected TTS endpoint: %q", url)
	}
}
