// protocol_test.go 百炼媒体协议回归测试
// Tests provider-visible image and speech request contracts.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package ali

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/modelbus/one-api-pro/model"
	"github.com/modelbus/one-api-pro/relay/meta"
	"github.com/modelbus/one-api-pro/relay/relaymode"
	schema "github.com/modelbus/one-api-pro/relay/schema"
)

// TestConvertAudioSpeechRequest_FormatAndRate verifies the gateway media contract.
// 版本: v0.0.28
// 日期: 2026-11-23
func TestConvertAudioSpeechRequest_FormatAndRate(t *testing.T) {
	for _, format := range []string{"mp3", "pcm", "wav", "opus"} {
		t.Run(format, func(t *testing.T) {
			speed := 1.1
			got, err := (&Adaptor{}).ConvertAudioSpeechRequest(&schema.AudioSpeechRequest{Model: "cosyvoice-v3-flash", Input: "你好", Voice: "longanhuan_v3", ResponseFormat: format, Speed: &speed})
			if err != nil {
				t.Fatal(err)
			}
			body, _ := json.Marshal(got)
			var wire struct {
				Input struct {
					Format string  `json:"format"`
					Rate   float64 `json:"rate"`
				} `json:"input"`
			}
			_ = json.Unmarshal(body, &wire)
			if wire.Input.Format != format || wire.Input.Rate != speed {
				t.Fatalf("wrong protocol: %s", body)
			}
		})
	}
	for _, format := range []string{"flac", "ogg", "aac"} {
		if _, err := (&Adaptor{}).ConvertAudioSpeechRequest(&schema.AudioSpeechRequest{Model: "cosyvoice-v3-flash", Input: "hi", Voice: "voice", ResponseFormat: format}); err == nil {
			t.Errorf("accepted %s", format)
		}
	}
	for _, speed := range []float64{0, -1, 2.1} {
		if _, err := (&Adaptor{}).ConvertAudioSpeechRequest(&schema.AudioSpeechRequest{Model: "cosyvoice-v3-flash", Input: "hi", Voice: "voice", Speed: &speed}); err == nil {
			t.Errorf("accepted speed %v", speed)
		}
	}
}

// TestWanImage_RequestContract verifies the gateway media contract.
// 版本: v0.0.28
// 日期: 2026-11-23
func TestWanImage_RequestContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	a := &Adaptor{}
	m := &meta.Meta{Mode: relaymode.ImagesGenerations, ActualModelName: "wan2.7-image", BaseURL: "https://workspace.example", Config: model.ChannelConfig{}}
	a.Init(m)
	url, err := a.GetRequestURL(m)
	if err != nil || url != "https://workspace.example/api/v1/services/aigc/multimodal-generation/generation" {
		t.Fatalf("url=%s err=%v", url, err)
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	req, _ := http.NewRequest(http.MethodPost, url, nil)
	if err := a.SetupRequestHeader(c, req, m); err != nil {
		t.Fatal(err)
	}
	if req.Header.Get("X-DashScope-Async") != "" {
		t.Fatal("Wan synchronous request marked async")
	}
	wire, err := a.ConvertImageRequest(&schema.ImageRequest{Model: "wan2.7-image", Prompt: "mountain", Size: "1080x1440", N: 1})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(wire)
	var got struct {
		Input struct {
			Messages []struct {
				Role    string `json:"role"`
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"messages"`
		} `json:"input"`
	}
	_ = json.Unmarshal(body, &got)
	if len(got.Input.Messages) != 1 || len(got.Input.Messages[0].Content) != 1 || got.Input.Messages[0].Content[0].Text != "mountain" {
		t.Fatalf("wrong request: %s", body)
	}
	m.ActualModelName = "wanx-v1"
	url, _ = a.GetRequestURL(m)
	if url != "https://workspace.example/api/v1/services/aigc/text2image/image-synthesis" {
		t.Fatalf("legacy changed: %s", url)
	}
}
