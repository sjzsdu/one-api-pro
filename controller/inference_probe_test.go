// inference_probe_test.go 操作探测目录及输入分发回归
// Verifies probe discovery, input validation and operation-specific upstream requests.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package controller

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/modelbus/one-api-pro/common/client"
	"github.com/modelbus/one-api-pro/model"
	"github.com/modelbus/one-api-pro/relay/adaptor/provider/ali"
	"github.com/modelbus/one-api-pro/relay/meta"
)

type probeMetaAdaptor struct {
	ali.Adaptor
	initialized *meta.Meta
}

func (a *probeMetaAdaptor) Init(m *meta.Meta) { a.initialized = m }

func TestOperationProbe_InitUsesExecutionMeta(t *testing.T) {
	a := &probeMetaAdaptor{}
	m, err := initOperationProbeAdaptor(a, &model.Channel{Id: 71, Type: 17, Key: "secret", Config: `{"plugin":"test-plugin"}`}, "ali", "friendly", "qwen3-asr-flash")
	if err != nil || a.initialized != m || m.ChannelId != 71 || m.ActualModelName != "qwen3-asr-flash" || m.Config.Plugin != "test-plugin" {
		t.Fatalf("adapter initialized with unrelated metadata: %+v %v", m, err)
	}
}

// TestOperationProbe_DeclaredProbeContract separates executable inference from supported probes.
// 版本: v0.0.28
// 日期: 2026-11-23
func TestOperationProbe_DeclaredProbeContract(t *testing.T) {
	// 语音合成与文生图现在支持带输入的调试探测（回传媒体供页面预览）；
	// 视频等仍需显式异步/realtime 端点的生成类操作保持不可探测。
	if _, ok := ProbeOperations("ali", "cosyvoice-v3-flash")["audio.speech"]; !ok {
		t.Fatal("speech synthesis probe not declared for cosyvoice")
	}
	if len(ProbeOperations("ali", "wan2.6-t2v")) != 0 {
		t.Fatal("video generation advertised as probeable")
	}
	if _, ok := ProbeOperations("ali", "paraformer-v2")["audio.transcribe"]; ok {
		t.Fatal("legacy wire ASR falsely advertised as native probe")
	}
	if !ProbeOperations("ali", "qwen3-asr-flash")["audio.transcribe"].RequiresInput {
		t.Fatal("ASR manual input not declared")
	}
	if !ProbeOperations("ali", "wan2.7-image")["image.edit"].RequiresInput {
		t.Fatal("editing input not declared")
	}
	if !ProbeOperations("ali", "wan2.7-image")["image.generate"].RequiresInput {
		t.Fatal("image generation input not declared")
	}
	setupCapabilityCatalog(t)
	rows, err := configuredModelItems([]*model.Channel{{Id: 1, Type: 17, Models: "qwen3-asr-flash,cosyvoice-v3-flash", Status: 1}})
	if err != nil || len(rows) != 2 {
		t.Fatalf("catalog error: %v", err)
	}
	if !rows[0].ProbeOperations["audio.transcribe"].RequiresInput || len(rows[1].ProbeOperations) == 0 {
		t.Fatal("catalog omitted explicit probe contract")
	}
}

// TestOperationProbe_InputValidationAndDispatch never sends chat or incomplete media requests.
// 版本: v0.0.28
// 日期: 2026-11-23
func TestOperationProbe_InputValidationAndDispatch(t *testing.T) {
	setupCapabilityCatalog(t)
	if err := model.DB.Model(&model.Channel{}).Where("id = ?", 91).Update("models", "qwen3-asr-flash,wan2.7-image,cosyvoice-v3-flash").Error; err != nil {
		t.Fatal(err)
	}
	old := client.HTTPClient
	t.Cleanup(func() { client.HTTPClient = old })
	for _, tc := range []struct {
		name, model, op, input string
		wantCall               bool
	}{
		{"missing_audio", "qwen3-asr-flash", "audio.transcribe", "", false},
		{"empty_audio", "qwen3-asr-flash", "audio.transcribe", `{}`, false},
		{"invalid_audio", "qwen3-asr-flash", "audio.transcribe", `{"audio":"bad-url"}`, false},
		{"missing_image", "wan2.7-image", "image.edit", `{"prompt":"hello"}`, false},
		{"missing_prompt", "wan2.7-image", "image.edit", `{"images":["https://media.example/input.png"]}`, false},
		{"unsupported", "cosyvoice-v3-flash", "audio.speech", "", false},
		{"asr", "qwen3-asr-flash", "audio.transcribe", `{"audio":"https://media.example/input.wav"}`, true},
		{"edit", "wan2.7-image", "image.edit", `{"images":["https://media.example/input.png"],"prompt":"make it blue"}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client.HTTPClient = &http.Client{Transport: jobRoundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				body, _ := io.ReadAll(r.Body)
				if strings.Contains(r.URL.Path, "chat/completions") || !strings.Contains(r.URL.Path, "multimodal-generation/generation") {
					t.Fatalf("wrong endpoint %s", r.URL.Path)
				}
				if !strings.Contains(string(body), map[string]string{"audio.transcribe": "https://media.example/input.wav", "image.edit": "make it blue"}[tc.op]) {
					t.Fatalf("input not forwarded %s", body)
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"output":{"choices":[{"message":{"content":[{"text":"hello","image":"https://media.example/output.png"}]}}]},"usage":{"seconds":1}}`))}, nil
			})}
			payload := map[string]any{"model": tc.model, "operation": tc.op, "confirm_cost": true}
			if tc.input != "" {
				payload["input"] = json.RawMessage(tc.input)
			}
			body, _ := json.Marshal(payload)
			c, w := capabilityContext("POST", "/api/channel/test_operation/91", string(body))
			c.Params = gin.Params{{Key: "id", Value: "91"}}
			OperationProbe(c)
			if (calls > 0) != tc.wantCall || strings.Contains(w.Body.String(), `"success":true`) != tc.wantCall {
				t.Fatalf("calls=%d response=%s", calls, w.Body.String())
			}
			if tc.wantCall {
				var audit model.Log
				if err := model.DB.Where("type = ? AND channel_id = ?", model.LogTypeTest, 91).Order("id DESC").First(&audit).Error; err != nil {
					t.Fatalf("missing administrative audit: %v", err)
				}
				if audit.UserId != 91 || !strings.Contains(audit.Content, tc.op) || strings.Contains(audit.Content, "media.example") || strings.Contains(audit.Content, "secret") {
					t.Fatalf("incomplete or sensitive audit: %+v", audit)
				}
			}
		})
	}
}
