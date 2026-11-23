// download_test.go 媒体下载信任边界与响应回归测试
// Verifies credential isolation, private-address rejection, redirects and media limits.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package ali

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/modelbus/one-api-pro/relay/meta"
)

type mediaRoundTrip func(*http.Request) (*http.Response, error)

func (f mediaRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func installMediaTransport(t *testing.T, f mediaRoundTrip) {
	t.Helper()
	old := mediaHTTPClient
	c := *old
	c.Transport = f
	mediaHTTPClient = &c
	t.Cleanup(func() { mediaHTTPClient = old })
}

// TestDownloadMedia_RejectsUnsafeTargets verifies the gateway media contract.
// 版本: v0.0.28
// 日期: 2026-11-23
func TestDownloadMedia_RejectsUnsafeTargets(t *testing.T) {
	called := false
	installMediaTransport(t, func(r *http.Request) (*http.Response, error) { called = true; return nil, nil })
	for _, raw := range []string{"http://127.0.0.1/a", "http://169.254.169.254/latest", "http://[::1]/a", "file:///etc/passwd", "https://user:password@example.com/a"} {
		if _, _, err := downloadMedia(context.Background(), raw); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	if called {
		t.Fatal("unsafe target reached transport")
	}
	if _, err := dialPublicMedia(context.Background(), "tcp", "127.0.0.1:80"); err == nil {
		t.Fatal("dial accepted loopback")
	}
}

// TestDownloadMedia_CredentialsAndRedirects verifies the gateway media contract.
// 版本: v0.0.28
// 日期: 2026-11-23
func TestDownloadMedia_CredentialsAndRedirects(t *testing.T) {
	installMediaTransport(t, func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Fatal("credentials sent to media host")
		}
		if r.URL.Path == "/redirect" {
			return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"http://127.0.0.1/private"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"audio/wav"}}, Body: io.NopCloser(strings.NewReader("RIFFaudio")), Request: r}, nil
	})
	data, ct, err := downloadMedia(context.Background(), "https://cdn.example/audio?secret=signed")
	if err != nil || string(data) != "RIFFaudio" || ct != "audio/wav" {
		t.Fatalf("download: %q %q %v", data, ct, err)
	}
	if _, _, err := downloadMedia(context.Background(), "https://cdn.example/redirect?secret=signed"); err == nil {
		t.Fatal("followed private redirect")
	} else if strings.Contains(err.Error(), "secret") {
		t.Fatalf("signed URL leaked: %v", err)
	}
}

// TestDownloadMedia_FailureAndSize verifies the gateway media contract.
// 版本: v0.0.28
// 日期: 2026-11-23
func TestDownloadMedia_FailureAndSize(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		length int64
		body   string
	}{
		{"not found", 404, 0, "missing"}, {"empty", 200, 0, ""}, {"oversized", 200, maxMediaBytes + 1, "data"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			installMediaTransport(t, func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, ContentLength: tc.length, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.body)), Request: r}, nil
			})
			if _, _, err := downloadMedia(context.Background(), "https://cdn.example/media"); err == nil {
				t.Fatal("accepted invalid media")
			}
		})
	}
}

// TestAudioSpeechResponse_BufferedUsageAndNoWrites verifies the gateway media contract.
// 版本: v0.0.28
// 日期: 2026-11-23
func TestAudioSpeechResponse_BufferedUsageAndNoWrites(t *testing.T) {
	installMediaTransport(t, func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "" {
			t.Fatal("API key leaked")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"audio/wav"}}, Body: io.NopCloser(strings.NewReader("RIFFaudio")), Request: r}, nil
	})
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/audio/speech", nil)
	a := &Adaptor{}
	m := &meta.Meta{ActualModelName: "qwen-audio-3.1-tts-flash", APIKey: "must-not-leak"}
	a.Init(m)
	response := func(body string) *http.Response {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}
	}
	result, err := a.AudioSpeechResponse(c, response(`{"output":{"audio":{"url":"https://cdn.example/a.wav"}},"usage":{"input_tokens":12,"output_tokens":40}}`), m)
	if err != nil {
		t.Fatal(err)
	}
	if string(result.Body) != "RIFFaudio" || result.TokenUsage.PromptTokens != 12 || result.TokenUsage.CompletionTokens != 40 {
		t.Fatalf("wrong result: %+v", result)
	}
	if c.Writer.Written() {
		t.Fatal("normalizer committed HTTP response before settlement")
	}
	for _, body := range []string{`not-json`, `{"code":"BadInput"}`, `{"output":{"audio":{"url":"https://cdn.example/a.wav"}}}`} {
		if _, err := a.AudioSpeechResponse(c, response(body), m); err == nil {
			t.Errorf("accepted invalid speech response %s", body)
		}
	}
}

// TestWanImage_ResponseContract verifies the gateway media contract.
// 版本: v0.0.28
// 日期: 2026-11-23
func TestWanImage_ResponseContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, format := range []string{"url", "b64_json"} {
		t.Run(format, func(t *testing.T) {
			installMediaTransport(t, func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("png")), Request: r}, nil
			})
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", "/v1/images/generations", nil)
			c.Set("response_format", format)
			resp := &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"output":{"choices":[{"message":{"content":[{"image":"https://cdn.example/image.png"}]}}]}}`))}
			err, _ := wanImageHandler(c, resp)
			if err != nil {
				t.Fatal(err)
			}
			if format == "url" && !strings.Contains(w.Body.String(), `"url":"https://cdn.example/image.png"`) {
				t.Fatalf("wrong url result %s", w.Body.String())
			}
			if format == "b64_json" && !strings.Contains(w.Body.String(), `"b64_json":"cG5n"`) {
				t.Fatalf("wrong b64 result %s", w.Body.String())
			}
		})
	}
}
