// multimodal_embedding_test.go 多模态向量中继端到端单测（httptest 假上游）
// 版本: v0.0.28
// 日期: 2026-11-19
// 作者: opencode
//
// 覆盖：请求体/响应体字节相等、转发路径、渠道 key 覆盖客户端 key、
// usage 解析与预扣/结算、缺 usage 全额退回、上游错误原文保留、
// 未实现可选接口的渠道返回 400。
package controller

import (
	"bytes"
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

	"github.com/modelbus/one-api-pro/common"
	"github.com/modelbus/one-api-pro/common/client"
	"github.com/modelbus/one-api-pro/common/ctxkey"
	dbmodel "github.com/modelbus/one-api-pro/model"
	"github.com/modelbus/one-api-pro/relay/adaptor/provider/alibailian"
)

const multimodalTestModel = "qwen3-vl-embedding"

// setupMultimodalFixture installs an in-memory DB, a price entry for the
// model, and a fake HTTP client. Returns the user/token/channel ids.
func setupMultimodalFixture(t *testing.T, userQuota int64) (int, int, int) {
	t.Helper()
	// A named shared in-memory DSN plus a single connection keeps every query on
	// the same database, including the goroutines spawned by refund/post-consume.
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql.DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&dbmodel.User{}, &dbmodel.Token{}, &dbmodel.Log{}, &dbmodel.Channel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	dbmodel.DB = db
	// Consume logs go through a separate LOG_DB global; share the same in-memory
	// instance so postConsumeQuota can persist them.
	dbmodel.LOG_DB = db
	common.RedisEnabled = false

	cache := dbmodel.GetModelPriceCacheForTest()
	cache.Lock()
	dbmodel.SetModelPriceMapForTest(map[string]*dbmodel.ModelPrice{
		multimodalTestModel: {
			ModelName:   multimodalTestModel,
			InputPrice:  1.0,
			OutputPrice: 1.0,
			BillingType: dbmodel.BillingTypeToken,
			Enabled:     true,
		},
	})
	cache.Unlock()
	t.Cleanup(func() {
		cache.Lock()
		dbmodel.SetModelPriceMapForTest(nil)
		cache.Unlock()
	})

	uid := seedUser(t, userQuota)
	tid := seedToken(t, uid, 10_000_000)
	ch := &dbmodel.Channel{Id: 7, Name: "bailian", Type: 49, Status: dbmodel.ChannelStatusEnabled}
	if err := db.Create(ch).Error; err != nil {
		t.Fatalf("create channel: %v", err)
	}
	return uid, tid, ch.Id
}

func newMultimodalContext(body []byte, channelID, baseURL string, uid, tid int) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodPost, "/v1/multimodal/embeddings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// Distribute rewrites this header to the channel key before the helper runs.
	req.Header.Set("Authorization", "Bearer channel-secret-key")
	c.Request = req
	c.Set(ctxkey.ChannelID, channelID)
	c.Set(ctxkey.Channel, 49)
	c.Set(ctxkey.ChannelId, 7)
	c.Set(ctxkey.ChannelName, "bailian")
	c.Set(ctxkey.BaseURL, baseURL)
	c.Set(ctxkey.TokenId, tid)
	c.Set(ctxkey.Id, uid)
	c.Set(ctxkey.Group, "default")
	c.Set(ctxkey.RequestModel, multimodalTestModel)
	c.Set(ctxkey.Config, dbmodel.ChannelConfig{})
	return c, w
}

func waitForQuota(t *testing.T, uid int, want int64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		got := readQuota(t, uid)
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("quota did not reach %d, got %d", want, got)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func readQuota(t *testing.T, uid int) int64 {
	t.Helper()
	var u dbmodel.User
	if err := dbmodel.DB.First(&u, "id = ?", uid).Error; err != nil {
		t.Fatalf("read user: %v", err)
	}
	return u.Quota
}

func TestRelayMultimodalEmbedding_PassthroughAndBilling(t *testing.T) {
	uid, tid, _ := setupMultimodalFixture(t, 10_000)

	reqBody := []byte(`{"model":"qwen3-vl-embedding","input":{"contents":[{"text":"涤纶面料"}]},"parameters":{"dimension":1024}}`)
	upstreamBody := []byte(`{"output":{"embeddings":[{"embedding":[0.1,0.2]}]},"usage":{"image_tokens":0,"input_tokens":23,"total_tokens":23},"request_id":"req-abc-123"}`)

	var gotPath, gotAuth, gotContentType string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("X-Request-Id", "req-abc-123")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(upstreamBody)
	}))
	defer srv.Close()

	oldClient := client.HTTPClient
	client.HTTPClient = srv.Client()
	defer func() { client.HTTPClient = oldClient }()

	// The channel base URL carries /compatible-mode; the native URL builder
	// must strip it. This exercises the real alibailian adaptor end-to-end.
	c, w := newMultimodalContext(reqBody, "alibailian", srv.URL+"/compatible-mode", uid, tid)
	before := readQuota(t, uid)
	if bizErr := RelayMultimodalEmbeddingHelper(c); bizErr != nil {
		t.Fatalf("helper returned error: status=%d code=%v message=%s", bizErr.StatusCode, bizErr.Error.Code, bizErr.Error.Message)
	}

	if gotPath != alibailian.MultimodalEmbeddingPath {
		t.Fatalf("upstream path = %q, want %q", gotPath, alibailian.MultimodalEmbeddingPath)
	}
	if gotAuth != "Bearer channel-secret-key" {
		t.Fatalf("upstream Authorization = %q, want channel key", gotAuth)
	}
	if gotContentType != "application/json" {
		t.Fatalf("upstream Content-Type = %q", gotContentType)
	}
	if !bytes.Equal(gotBody, reqBody) {
		t.Fatalf("request body not byte-equal:\ngot  %q\nwant %q", gotBody, reqBody)
	}
	if w.Code != http.StatusOK {
		t.Fatalf("response status = %d, want 200", w.Code)
	}
	if !bytes.Equal(w.Body.Bytes(), upstreamBody) {
		t.Fatalf("response body not byte-equal:\ngot  %q\nwant %q", w.Body.Bytes(), upstreamBody)
	}
	if got := w.Header().Get("X-Request-Id"); got != "req-abc-123" {
		t.Fatalf("X-Request-Id = %q", got)
	}
	if got := w.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("response Content-Type = %q", got)
	}

	// 1 text char / 4 = 1 estimated token; ratio = (1+1)/2 = 1.0,
	// so pre-consume = 500 + 1 = 501. Upstream reports 23 tokens, so the
	// settlement returns the unused 478 and the net charge is 23.
	after := readQuota(t, uid)
	if want := before - 23; after != want {
		t.Fatalf("user quota after settlement = %d, want %d (before=%d)", after, want, before)
	}
}

func TestRelayMultimodalEmbedding_MissingUsageRefundsPreConsume(t *testing.T) {
	uid, tid, _ := setupMultimodalFixture(t, 10_000)
	reqBody := []byte(`{"model":"qwen3-vl-embedding","input":{"contents":[{"text":"abc"}]}}`)
	upstreamBody := []byte(`{"output":{"embeddings":[{"embedding":[0.1]}]}}`)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(upstreamBody)
	}))
	defer srv.Close()
	oldClient := client.HTTPClient
	client.HTTPClient = srv.Client()
	defer func() { client.HTTPClient = oldClient }()

	c, w := newMultimodalContext(reqBody, "alibailian", srv.URL, uid, tid)
	before := readQuota(t, uid)
	if bizErr := RelayMultimodalEmbeddingHelper(c); bizErr != nil {
		t.Fatalf("helper returned error: %+v", bizErr)
	}
	if w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), upstreamBody) {
		t.Fatalf("response not passed through: status=%d body=%q", w.Code, w.Body.Bytes())
	}
	// ReturnPreConsumedQuota refunds in a goroutine; wait for the DB write.
	waitForQuota(t, uid, before)
}

func TestRelayMultimodalEmbedding_UpstreamErrorKeepsRawMessage(t *testing.T) {
	uid, tid, _ := setupMultimodalFixture(t, 10_000)
	reqBody := []byte(`{"model":"qwen3-vl-embedding","input":{"contents":[{"text":"abc"}]}}`)
	const rawMessage = "dimension 999 is not supported for qwen3-vl-embedding"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"` + rawMessage + `","type":"invalid_request_error","code":"invalid_parameter"}}`))
	}))
	defer srv.Close()
	oldClient := client.HTTPClient
	client.HTTPClient = srv.Client()
	defer func() { client.HTTPClient = oldClient }()

	c, _ := newMultimodalContext(reqBody, "alibailian", srv.URL, uid, tid)
	before := readQuota(t, uid)
	bizErr := RelayMultimodalEmbeddingHelper(c)
	if bizErr == nil {
		t.Fatal("expected upstream error")
	}
	if bizErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", bizErr.StatusCode)
	}
	if !strings.Contains(bizErr.Error.Message, rawMessage) {
		t.Fatalf("upstream message lost: %q", bizErr.Error.Message)
	}
	waitForQuota(t, uid, before) // upstream error must refund pre-consume
}

func TestRelayMultimodalEmbedding_UnsupportedChannel(t *testing.T) {
	uid, tid, _ := setupMultimodalFixture(t, 10_000)
	reqBody := []byte(`{"model":"qwen3-vl-embedding","input":{"contents":[{"text":"abc"}]}}`)
	// The stock openai adaptor does not implement MultimodalEmbeddingURLer.
	c, _ := newMultimodalContext(reqBody, "openai", "https://example.com", uid, tid)
	before := readQuota(t, uid)
	bizErr := RelayMultimodalEmbeddingHelper(c)
	if bizErr == nil {
		t.Fatal("expected unsupported channel error")
	}
	if bizErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", bizErr.StatusCode)
	}
	if bizErr.Error.Code != "unsupported_channel_for_multimodal_embedding" {
		t.Fatalf("code = %v", bizErr.Error.Code)
	}
	if after := readQuota(t, uid); after != before {
		t.Fatalf("unsupported channel must not pre-consume: before=%d after=%d", before, after)
	}
}

func TestRelayMultimodalEmbedding_MissingModel(t *testing.T) {
	uid, tid, _ := setupMultimodalFixture(t, 10_000)
	c, _ := newMultimodalContext([]byte(`{"input":{"contents":[{"text":"abc"}]}}`), "alibailian", "https://dashscope.aliyuncs.com", uid, tid)
	bizErr := RelayMultimodalEmbeddingHelper(c)
	if bizErr == nil || bizErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing model must return 400, got %+v", bizErr)
	}
}
