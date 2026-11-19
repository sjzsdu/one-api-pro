// channel-test-native_test.go 渠道测试对原生多模态向量端点的支持单测
// 版本: v0.0.28
// 日期: 2026-11-19
// 作者: opencode
//
// 覆盖：qwen3-vl-embedding 的渠道测试走 DashScope 原生端点而非 compatible-mode，
// 请求体为 input.contents 原生格式，响应按 output.embeddings 解析；
// 普通 embedding（text-embedding-v3）仍走 /v1/embeddings 不受影响。
package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/modelbus/one-api-pro/common/client"
	dbmodel "github.com/modelbus/one-api-pro/model"
	"github.com/modelbus/one-api-pro/relay/channeltype"
	relaymodel "github.com/modelbus/one-api-pro/relay/schema"
)

func strPtr(s string) *string { return &s }

// setupTestLogDB gives testChannel's deferred RecordTestLog goroutine a valid
// isolated DB. Without this it would write to whatever global LOG_DB the last
// test left behind (or a closed one), polluting other tests.
func setupTestLogDB(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&dbmodel.Log{}); err != nil {
		t.Fatalf("migrate log: %v", err)
	}
	old := dbmodel.LOG_DB
	dbmodel.LOG_DB = db
	t.Cleanup(func() { dbmodel.LOG_DB = old })
	// Let the deferred goroutine finish before the next test grabs the global.
	t.Cleanup(func() { time.Sleep(30 * time.Millisecond) })
}

// newTestBailianChannel builds an alibailian channel pointing at baseURL.
func newTestBailianChannel(baseURL, models string, mapping *string) *dbmodel.Channel {
	return &dbmodel.Channel{
		Type:         channeltype.AliBailian,
		Name:         "Aliyun Bailian",
		Key:          "sk-test-key",
		Models:       models,
		BaseURL:      strPtr(baseURL),
		ModelMapping: mapping,
		Status:       1,
	}
}

func TestTestChannel_NativeMultimodalEmbedding(t *testing.T) {
	setupTestLogDB(t)
	var gotPath string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		if r.Header.Get("Authorization") != "Bearer sk-test-key" {
			t.Errorf("Authorization = %q, want Bearer sk-test-key", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"output":{"embeddings":[{"embedding":[0.1,0.2,0.3]}]},"usage":{"total_tokens":5},"request_id":"req-test"}`)
	}))
	defer srv.Close()

	old := client.HTTPClient
	client.HTTPClient = srv.Client()
	defer func() { client.HTTPClient = old }()

	channel := newTestBailianChannel(srv.URL+"/compatible-mode", "qwen3-vl-embedding", nil)
	req := buildTestRequest("qwen3-vl-embedding")

	message, err, _ := testChannel(context.Background(), channel, req)
	if err != nil {
		t.Fatalf("testChannel error: %v", err)
	}
	if message != "embedding ok, dimensions: 3" {
		t.Fatalf("message = %q, want embedding ok, dimensions: 3", message)
	}
	if gotPath != "/api/v1/services/embeddings/multimodal-embedding/multimodal-embedding" {
		t.Fatalf("path = %q, want native multimodal path", gotPath)
	}
	if strings.Contains(gotPath, "embeddings/v1/embeddings") || strings.Contains(gotPath, "/v1/embeddings") {
		t.Fatalf("path = %q must not be compatible-mode", gotPath)
	}
	var native struct {
		Model string `json:"model"`
		Input struct {
			Contents []map[string]string `json:"contents"`
		} `json:"input"`
	}
	if jerr := json.Unmarshal(gotBody, &native); jerr != nil {
		t.Fatalf("unmarshal request body: %v (body=%s)", jerr, gotBody)
	}
	if native.Model != "qwen3-vl-embedding" {
		t.Fatalf("request model = %q", native.Model)
	}
	if len(native.Input.Contents) != 1 || native.Input.Contents[0]["text"] == "" {
		t.Fatalf("request body missing input.contents text: %s", gotBody)
	}
}

func TestTestChannel_TextEmbeddingStillCompatibleMode(t *testing.T) {
	setupTestLogDB(t)
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"embedding":[0.1,0.2]}],"usage":{"prompt_tokens":1,"total_tokens":1}}`)
	}))
	defer srv.Close()

	old := client.HTTPClient
	client.HTTPClient = srv.Client()
	defer func() { client.HTTPClient = old }()

	channel := newTestBailianChannel(srv.URL+"/compatible-mode", "text-embedding-v3", nil)
	req := buildTestRequest("text-embedding-v3")

	message, err, _ := testChannel(context.Background(), channel, req)
	if err != nil {
		t.Fatalf("testChannel error: %v", err)
	}
	if message != "embedding ok, dimensions: 2" {
		t.Fatalf("message = %q", message)
	}
	if gotPath != "/compatible-mode/v1/embeddings" {
		t.Fatalf("path = %q, want compatible-mode /v1/embeddings", gotPath)
	}
}

func TestIsNativeMultimodalTestModel(t *testing.T) {
	cases := map[string]bool{
		"qwen3-vl-embedding":      true,
		" Qwen3-VL-Embedding ":    true,
		"text-embedding-v3":       false,
		"multimodal-embedding-v1": false,
		"bge-large-zh":            false,
	}
	for model, want := range cases {
		if got := isNativeMultimodalTestModel(model); got != want {
			t.Errorf("isNativeMultimodalTestModel(%q) = %v, want %v", model, got, want)
		}
	}
}

var _ = relaymodel.GeneralOpenAIRequest{}
