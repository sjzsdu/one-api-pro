// operations_test.go 原生重排、识别和编辑协议回归
// Tests provider-visible operation paths, payloads and measured usage.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package ali

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/modelbus/one-api-pro/common/client"
	"github.com/modelbus/one-api-pro/relay/meta"
	schema "github.com/modelbus/one-api-pro/relay/schema"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestNativeJSON_ErrorClassificationAndLargeResponse(t *testing.T) {
	for _, status := range []int{400, 429, 408, 504, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			old := client.HTTPClient
			t.Cleanup(func() { client.HTTPClient = old })
			client.HTTPClient = &http.Client{Transport: mediaRoundTrip(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`))}, nil
			})}
			err := nativeJSON(context.Background(), &meta.Meta{BaseURL: "https://api.example"}, "POST", "/infer", nil, false, &struct{}{})
			var coded interface{ HTTPStatusCode() int }
			if err == nil || !errors.As(err, &coded) || coded.HTTPStatusCode() != status || schema.ExecutionWasRejected(err) != (status == 400 || status == 429) {
				t.Fatalf("unsafe status classification: %v", err)
			}
		})
	}
	t.Run("large JSON", func(t *testing.T) {
		old := client.HTTPClient
		t.Cleanup(func() { client.HTTPClient = old })
		payload := strings.Repeat("A", 9<<20)
		client.HTTPClient = &http.Client{Transport: mediaRoundTrip(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"image":"` + payload + `"}`))}, nil
		})}
		var result struct {
			Image string `json:"image"`
		}
		if err := nativeJSON(context.Background(), &meta.Meta{BaseURL: "https://api.example"}, "POST", "/infer", nil, false, &result); err != nil || result.Image != payload {
			t.Fatalf("large image response truncated: %v", err)
		}
	})
}

func TestNativeOperations_DateSuffixPreservesWireModel(t *testing.T) {
	old := client.HTTPClient
	t.Cleanup(func() { client.HTTPClient = old })
	client.HTTPClient = &http.Client{Transport: mediaRoundTrip(func(r *http.Request) (*http.Response, error) {
		raw, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(raw), "-2026-11-23") {
			t.Fatal("date suffix removed from wire")
		}
		response := `{"output":{"results":[{"index":0,"relevance_score":0.5}]},"usage":{"total_tokens":1}}`
		if strings.Contains(string(raw), "qwen3-asr") {
			response = `{"output":{"choices":[{"message":{"content":[{"text":"hello"}]}}]},"usage":{"seconds":1}}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response))}, nil
	})}
	a := &Adaptor{}
	m := &meta.Meta{BaseURL: "https://api.example"}
	if _, err := a.Rerank(context.Background(), m, &schema.RerankRequest{Model: "gte-rerank-v2-2026-11-23", Query: "q", Documents: []string{"d"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Transcribe(context.Background(), m, &schema.TranscriptionRequest{Model: "qwen3-asr-flash-2026-11-23", Audio: "https://cdn.example/audio.wav"}); err != nil {
		t.Fatal(err)
	}
}

func TestNativeOperations_WireContracts(t *testing.T) {
	old := client.HTTPClient
	t.Cleanup(func() { client.HTTPClient = old })
	client.HTTPClient = &http.Client{Transport: mediaRoundTrip(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatal("missing auth")
		}
		var v map[string]any
		if err := json.Unmarshal(body, &v); err != nil {
			t.Fatal(err)
		}
		input := v["input"].(map[string]any)
		result := ""
		switch r.URL.Path {
		case "/api/v1/services/rerank/text-rerank/text-rerank":
			if input["query"] != "find" {
				t.Fatal(string(body))
			}
			result = `{"output":{"results":[{"index":0,"relevance_score":0.9}]},"usage":{"total_tokens":8}}`
		case "/api/v1/services/aigc/multimodal-generation/generation":
			if v["model"] == "qwen3-asr-flash" {
				result = `{"output":{"choices":[{"message":{"content":[{"text":"hello"}]}}]},"usage":{"seconds":1.25}}`
			} else {
				if !strings.Contains(string(body), `"image":"https://example.com/ref.png"`) {
					t.Fatal(string(body))
				}
				result = `{"output":{"choices":[{"message":{"content":[{"image":"https://example.com/result.png"}]}}]}}`
			}
		default:
			t.Fatal(r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(result))}, nil
	})}
	a := &Adaptor{}
	m := &meta.Meta{BaseURL: "https://api.example", APIKey: "secret"}
	out, err := a.Rerank(context.Background(), m, &schema.RerankRequest{Model: "gte-rerank-v2", Query: "find", Documents: []string{"doc"}, TopN: 1})
	if err != nil || out.Usage[0].Quantity != 8 {
		t.Fatalf("rerank %v %+v", err, out)
	}
	out, err = a.Transcribe(context.Background(), m, &schema.TranscriptionRequest{Model: "qwen3-asr-flash", Audio: "https://example.com/audio.wav"})
	if err != nil || out.Usage[0].Quantity != 1250 {
		t.Fatalf("asr %v %+v", err, out)
	}
	out, err = a.EditImage(context.Background(), m, &schema.ImageEditRequest{Model: "wan2.7-image", Prompt: "change", Images: []string{"https://example.com/ref.png"}, N: 1})
	if err != nil || !strings.Contains(string(out.Body), "result.png") {
		t.Fatalf("edit %v %+v", err, out)
	}
}
