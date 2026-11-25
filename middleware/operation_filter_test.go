// operation_filter_test.go 多模态操作硬约束回归
// Ensures native text-only adapters never silently discard media inputs.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package middleware

import (
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/modelbus/one-api-pro/channelrouter"
	"github.com/modelbus/one-api-pro/common"
	"github.com/modelbus/one-api-pro/common/config"
	"github.com/modelbus/one-api-pro/common/ctxkey"
	"github.com/modelbus/one-api-pro/model"
	"gorm.io/gorm"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestDistribute_OperationRejectionSurvivesCandidateFiltering(t *testing.T) {
	oldDB, oldMemory, oldRedis := model.DB, config.MemoryCacheEnabled, common.RedisEnabled
	t.Cleanup(func() { model.DB = oldDB; config.MemoryCacheEnabled = oldMemory; common.RedisEnabled = oldRedis })
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { sqlDB.Close() })
	model.InitModelCapabilityCache(db)
	db.AutoMigrate(&model.User{}, &model.Channel{}, &model.ModelCatalog{})
	model.DB = db
	config.MemoryCacheEnabled = false
	common.RedisEnabled = false
	db.Create(&model.User{Id: 701, Username: "op-rejection", Group: "default"})
	realtime := model.Channel{Id: 701, Type: 17, Status: 1, Models: "shared-op", Group: "default", ModelMapping: opStringPtr(`{"shared-op":"qwen3.5-omni-flash-realtime"}`)}
	db.Create(&realtime)
	router := gin.New()
	router.POST("/v1/chat/completions", func(c *gin.Context) { c.Set(ctxkey.Id, 701); c.Set(ctxkey.RequestModel, "shared-op") }, Distribute(), func(c *gin.Context) { c.JSON(200, gin.H{"channel": c.GetInt(ctxkey.ChannelId)}) })
	request := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"shared-op","messages":[{"content":"hello"}]}`))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)
		return w
	}
	w := request()
	if w.Code != 400 || !strings.Contains(w.Body.String(), `"code":"model_operation_unsupported"`) || !strings.Contains(w.Body.String(), "GET /v1/realtime") {
		t.Fatalf("operation reason swallowed: %d %s", w.Code, w.Body.String())
	}
	db.Create(&model.Channel{Id: 702, Type: 1, Status: 1, Models: "shared-op", Group: "default", ModelMapping: opStringPtr(`{"shared-op":"gpt-4o"}`)})
	w = request()
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"channel":702`) {
		t.Fatalf("valid alternative channel wrongly rejected: %d %s", w.Code, w.Body.String())
	}
}

func opStringPtr(value string) *string { return &value }

func TestOperationCandidates_MemoryMissNegativeCacheAndInvalidation(t *testing.T) {
	oldDB, oldMemory := model.DB, config.MemoryCacheEnabled
	t.Cleanup(func() { model.DB = oldDB; config.MemoryCacheEnabled = oldMemory })
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { sqlDB.Close() })
	model.InitModelCapabilityCache(db)
	db.AutoMigrate(&model.Channel{}, &model.ModelCatalog{})
	model.DB = db
	config.MemoryCacheEnabled = true
	var reads atomic.Int64
	db.Callback().Query().Before("gorm:query").Register("test:negative_candidate_count", func(tx *gorm.DB) {
		if tx.Statement.Table == "channels" {
			reads.Add(1)
		}
	})
	request := func() *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{}`))
		return c
	}
	for i := 0; i < 3; i++ {
		if rows := OperationChannelCandidates(request(), "negative-group-unique", "negative-model-unique"); len(rows) != 0 {
			t.Fatal("unexpected candidate")
		}
	}
	if reads.Load() != 1 {
		t.Fatalf("negative miss caused %d DB reads", reads.Load())
	}
	db.Create(&model.Channel{Id: 801, Type: 1, Status: 1, Models: "negative-model-unique", Group: "negative-group-unique"})
	if rows := OperationChannelCandidates(request(), "negative-group-unique", "negative-model-unique"); len(rows) != 1 {
		t.Fatal("configuration write did not invalidate candidate miss")
	}
}

func TestOperationRequest_ParsesMediaAndLimitsOnce(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"stream":true,"n":2,"messages":[{"content":[{"type":"image_url","image_url":{"url":"https://example.com/a.png"}}]}]}`))
	first := operationRequest(c)
	second := operationRequest(c)
	if first != second || !first.HasMedia || !first.Stream || first.N != 2 {
		t.Fatal("request parsing was repeated or lost media/limits")
	}
}

func TestOperationCandidates_NoCacheMissDoesNotScanAllChannels(t *testing.T) {
	oldDB, oldCache := model.DB, config.MemoryCacheEnabled
	t.Cleanup(func() { model.DB = oldDB; config.MemoryCacheEnabled = oldCache })
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { sqlDB.Close() })
	db.AutoMigrate(&model.Channel{})
	model.DB = db
	config.MemoryCacheEnabled = false
	var reads atomic.Int64
	db.Callback().Query().Before("gorm:query").Register("test:count_candidates", func(tx *gorm.DB) {
		if tx.Statement.Table == "channels" {
			reads.Add(1)
		}
	})
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{}`))
	if got := OperationChannelCandidates(c, "default", "missing"); len(got) != 0 {
		t.Fatal("unexpected candidates")
	}
	if reads.Load() != 1 {
		t.Fatalf("empty no-cache candidate lookup issued %d queries", reads.Load())
	}
}

func TestChannelOperation_AliLegacyASRWithExplicitCapabilityKeepsWirePath(t *testing.T) {
	old := model.DB
	t.Cleanup(func() { model.DB = old })
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { sqlDB.Close() })
	model.InitModelCapabilityCache(db)
	db.AutoMigrate(&model.ModelCatalog{})
	model.DB = db
	db.Create(&model.ModelCatalog{Provider: "ali", ModelName: "paraformer-v2", CapabilityOverride: `{"primary_category":"audio","operations":{"audio.transcribe":{"modes":["sync"]},"audio.translate":{"modes":["sync"]}}}`})
	for _, path := range []string{"/v1/audio/transcriptions", "/v1/audio/translations"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", path, strings.NewReader(`{}`))
		if err := channelOperationError(c, &model.Channel{Type: 17}, "paraformer-v2"); err != nil {
			t.Fatalf("legacy ASR blocked before controller: %v", err)
		}
	}
	db.Where("provider = ?", "ali").Delete(&model.ModelCatalog{})
	for _, name := range []string{"paraformer", "paraformer-v2", "fun-asr"} {
		for _, path := range []string{"/v1/audio/transcriptions", "/v1/audio/translations"} {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", path, strings.NewReader(`{}`))
			if err := channelOperationError(c, &model.Channel{Type: 17}, name); err != nil {
				t.Fatalf("legacy name %s requires unexpected manual override: %v", name, err)
			}
		}
	}
}

func TestChannelOperation_AliVisionCannotLoseImages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"qwen-vl","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/a.png"}}]}]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	if err := channelOperationError(c, &model.Channel{Type: 17}, "qwen-vl"); err == nil {
		t.Fatal("text-only Ali adapter accepted vision payload")
	}
}
func TestChatMediaDetection_DoesNotInspectPlainPromptText(t *testing.T) {
	if hasMediaChatInput([]byte(`{"messages":[{"content":"explain image_url and video"}]}`)) {
		t.Fatal("plain text treated as media")
	}
	if !hasMediaChatInput([]byte(`{"input":[{"content":[{"type":"input_audio","input_audio":{"data":"abc"}}]}]}`)) {
		t.Fatal("missed audio input")
	}
}

func TestChannelOperation_RealtimeModelGivesActionableSyncError(t *testing.T) {
	old := model.DB
	t.Cleanup(func() { model.DB = old })
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { sqlDB.Close() })
	model.InitModelCapabilityCache(db)
	db.AutoMigrate(&model.ModelCatalog{})
	model.DB = db
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"messages":[{"content":"hello"}]}`))
	err = channelOperationError(c, &model.Channel{Type: 17}, "qwen3.5-omni-flash-realtime-2025-09-08")
	if err == nil || !strings.Contains(err.Error(), "model_operation_unsupported") || !strings.Contains(err.Error(), "/v1/realtime") {
		t.Fatalf("unactionable realtime error: %v", err)
	}
}

// TestChannelOperation_TTSVoiceMustMatchModelFamily 覆盖线上事故：
// qwen-audio-3.1-tts-flash 用 cosyvoice 的音色名时，上游只回一句
// "[cosyvoice:]Engine error [411]"，无法判断是音色错了。
// 能力目录声明音色白名单后，网关应在本地拦下并列出可用音色。
func TestChannelOperation_TTSVoiceMustMatchModelFamily(t *testing.T) {
	old := model.DB
	t.Cleanup(func() { model.DB = old })
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { sqlDB.Close() })
	model.InitModelCapabilityCache(db)
	db.AutoMigrate(&model.ModelCatalog{})
	model.DB = db

	newCtx := func(voice string) *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/v1/audio/speech",
			strings.NewReader(`{"model":"qwen-audio-3.1-tts-flash","input":"你好","voice":"`+voice+`"}`))
		c.Request.Header.Set("Content-Type", "application/json")
		return c
	}

	err = channelOperationError(newCtx("longanhuan_v3"), &model.Channel{Type: 17}, "qwen-audio-3.1-tts-flash")
	if err == nil {
		t.Fatal("cosyvoice voice accepted for a qwen-audio model")
	}
	if !strings.Contains(err.Error(), "longanhuan_v3.1") {
		t.Fatalf("error should list the allowed voices, got: %v", err)
	}
	if err := channelOperationError(newCtx("longanhuan_v3.1"), &model.Channel{Type: 17}, "qwen-audio-3.1-tts-flash"); err != nil {
		t.Fatalf("valid voice rejected: %v", err)
	}
}

// TestNoChannelMessage_区分没有渠道与渠道暂时不可用 锁定对外文案：
// 候选被冷却/并发滤掉时要带原因；确实没有渠道时保持原句不编造原因。
func TestNoChannelMessage_区分没有渠道与渠道暂时不可用(t *testing.T) {
	plain := noChannelMessage("default", "qwen-plus", errors.New("no channel"))
	if !strings.Contains(plain, "无可用渠道") {
		t.Fatalf("plain message = %q", plain)
	}
	if strings.Contains(plain, "暂时") {
		t.Fatalf("plain message should not claim a temporary condition: %q", plain)
	}

	dropped := &channelrouter.NoAvailableChannelError{
		Group: "default",
		Model: "qwen-plus",
		Drops: []channelrouter.ChannelDrop{{
			Filter:   "cooldown",
			Channels: []*model.Channel{{Id: 5, LastError: "[429] quota exceeded"}},
		}},
	}
	detailed := noChannelMessage("default", "qwen-plus", dropped)
	if !strings.Contains(detailed, "暂时无可用渠道") {
		t.Fatalf("detailed message = %q", detailed)
	}
	if !strings.Contains(detailed, "冷却中") {
		t.Fatalf("detailed message should mention the filter reason: %q", detailed)
	}
	if !strings.Contains(detailed, "quota exceeded") {
		t.Fatalf("detailed message should carry the upstream error: %q", detailed)
	}
}
