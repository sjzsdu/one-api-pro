// inference_probe_media_test.go 语音合成与生图探测回传媒体回归
// Verifies media probes call native adapters and return inline previewable artifacts.
// 版本: v0.0.28
// 日期: 2026-11-24
// 作者: opencode
package controller

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/modelbus/one-api-pro/common/client"
	"github.com/modelbus/one-api-pro/relay/meta"
	schema "github.com/modelbus/one-api-pro/relay/schema"
)

func TestEncodeAudioMedia(t *testing.T) {
	audio := []byte("RIFFfake-audio")
	media, err := encodeAudioMedia(&schema.AudioSpeechResponse{Body: audio, ContentType: "audio/wav"}, "audio/wav")
	if err != nil {
		t.Fatal(err)
	}
	if media.Kind != "audio" || media.MimeType != "audio/wav" {
		t.Fatalf("wrong media descriptor: %+v", media)
	}
	decoded, err := base64.StdEncoding.DecodeString(media.Base64)
	if err != nil || string(decoded) != string(audio) {
		t.Fatalf("audio not round-tripped: %v %q", err, decoded)
	}
	if !strings.HasSuffix(media.Filename, ".wav") {
		t.Fatalf("wrong extension: %s", media.Filename)
	}
	if _, err := encodeAudioMedia(&schema.AudioSpeechResponse{}, "audio/wav"); err == nil {
		t.Fatal("empty audio accepted")
	}
}

func TestMimeFromFormat(t *testing.T) {
	cases := map[string]string{"mp3": "audio/mpeg", "opus": "audio/ogg", "PCM": "audio/pcm", "": "audio/wav", "wav": "audio/wav"}
	for in, want := range cases {
		if got := mimeFromFormat(in); got != want {
			t.Fatalf("mimeFromFormat(%q)=%q want %q", in, got, want)
		}
	}
}

// TestBuildProbeImageBody_Passthrough verifies the OpenAI body when no rewriter applies.
func TestBuildProbeImageBody_Passthrough(t *testing.T) {
	body, err := buildProbeImageBody(nil, &schema.ImageRequest{Model: "x", Prompt: "a cat", N: 1, Size: "1024x1024", ResponseFormat: "b64_json"})
	if err != nil {
		t.Fatal(err)
	}
	var decoded schema.ImageRequest
	if err := json.Unmarshal(body, &decoded); err != nil || decoded.Prompt != "a cat" {
		t.Fatalf("bad body: %s %v", body, err)
	}
}

// mockProbeAdaptor is a minimal adaptor that produces a fixed OpenAI image JSON.
type mockProbeAdaptor struct {
	*probeMetaAdaptor
	response string
}

func (a *mockProbeAdaptor) DoRequest(c *gin.Context, m *meta.Meta, body io.Reader) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"output":{"choices":[]}}`))}, nil
}

func (a *mockProbeAdaptor) DoResponse(c *gin.Context, resp *http.Response, m *meta.Meta) (*schema.Usage, *schema.ErrorWithStatusCode) {
	c.Writer.Header().Set("Content-Type", "application/json")
	_, _ = c.Writer.Write([]byte(a.response))
	return &schema.Usage{}, nil
}

// RewriteImageRequest shadows the promoted (nil) ali implementation with a passthrough.
func (a *mockProbeAdaptor) RewriteImageRequest(req *schema.ImageRequest) (any, error) {
	return req, nil
}

func TestRunImageProbe_ReturnsInlineBase64(t *testing.T) {
	setupCapabilityCatalog(t)
	png := base64.StdEncoding.EncodeToString([]byte("fake-png-bytes"))
	response := `{"created":1,"data":[{"b64_json":"` + png + `"}]}`
	a := &mockProbeAdaptor{response: response}
	old := client.HTTPClient
	t.Cleanup(func() { client.HTTPClient = old })

	c, _ := capabilityContext("POST", "/api/channel/test_operation/91", "")
	m := &meta.Meta{}
	media, err := runImageProbe(c, a, m, "wan2.7-image", json.RawMessage(`{"prompt":"a cat"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(media) != 1 || media[0].Kind != "image" || media[0].Base64 != png {
		t.Fatalf("wrong media: %+v", media)
	}
}

func TestRunImageProbe_RejectsEmptyPrompt(t *testing.T) {
	setupCapabilityCatalog(t)
	a := &mockProbeAdaptor{response: `{"data":[]}`}
	c, _ := capabilityContext("POST", "/api/channel/test_operation/91", "")
	if _, err := runImageProbe(c, a, &meta.Meta{}, "wan2.7-image", nil); err == nil {
		t.Fatal("empty prompt accepted")
	}
}
