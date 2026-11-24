// alias_pool_integration_test.go 全局模型池的真实转发和故障回归
// Exercises authorized global aliases, sticky failover, billing and API wrappers.
// 版本: v0.0.28
// 日期: 2026-11-22
// 作者: Codex
package controller_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/modelbus/one-api-pro/channelrouter"
	"github.com/modelbus/one-api-pro/common"
	"github.com/modelbus/one-api-pro/common/client"
	"github.com/modelbus/one-api-pro/common/config"
	api "github.com/modelbus/one-api-pro/controller"
	"github.com/modelbus/one-api-pro/middleware"
	"github.com/modelbus/one-api-pro/model"
	"gorm.io/gorm"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestAliasPool_RealRelay verifies the model-pool behavior described by this test.
// 版本: v0.0.28
// 日期: 2026-11-22
func TestAliasPool_RealRelay(t *testing.T) {
	db, e := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if e != nil {
		t.Fatal(e)
	}
	if e = db.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.Ability{}, &model.UserModelAlias{}, &model.Log{}, &model.ModelCatalog{}, &model.InferenceSettlement{}, &model.UserPlan{}); e != nil {
		t.Fatal(e)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	oldSQLite := common.UsingSQLite
	common.UsingSQLite = true
	defer func() { common.UsingSQLite = oldSQLite }()
	oldDB, oldLog, oldRedis, oldBatch, oldApprox, oldLogEnabled := model.DB, model.LOG_DB, common.RedisEnabled, config.BatchUpdateEnabled, config.ApproximateTokenEnabled, config.LogConsumeEnabled
	model.DB, model.LOG_DB, common.RedisEnabled, config.BatchUpdateEnabled, config.ApproximateTokenEnabled, config.LogConsumeEnabled = db, db, false, false, true, true
	defer func() {
		model.DB, model.LOG_DB, common.RedisEnabled, config.BatchUpdateEnabled, config.ApproximateTokenEnabled, config.LogConsumeEnabled = oldDB, oldLog, oldRedis, oldBatch, oldApprox, oldLogEnabled
		sqlDB.Close()
	}()
	user := model.User{Id: 8801, Username: "pool-integration", Status: 1, Group: "default", Quota: 10000, AccessToken: "poolapi", AffCode: "poolapi"}
	db.Create(&user)
	token := model.Token{UserId: user.Id, Key: "poolintegration", Status: 1, RemainQuota: 10000, ExpiredTime: -1}
	allowed := "chat-tool"
	token.Models = &allowed
	db.Create(&token)
	var mu sync.Mutex
	seen := []string{}
	failA := true
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		name := fmt.Sprint(body["model"])
		mu.Lock()
		seen = append(seen, name)
		fail := failA && name == "pool-a"
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if fail {
			// Only an explicit rejection is safe to retry on another target.
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"error":{"message":"request rejected","type":"rate_limit_error","code":"rate_limit_exceeded"}}`)
			return
		}
		fmt.Fprintf(w, `{"id":"pool-response","object":"chat.completion","model":%q,"choices":[{"index":0,"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`, name)
	}))
	defer upstream.Close()
	base := upstream.URL
	for i, name := range []string{"pool-a", "pool-b"} {
		ch := model.Channel{Id: 8801 + i, Type: 1, Status: 1, Key: "fake", Name: name, Models: name, Group: "default", BaseURL: &base}
		if e = ch.Insert(); e != nil {
			t.Fatal(e)
		}
	}
	cache := model.GetModelPriceCacheForTest()
	cache.Lock()
	model.SetModelPriceMapForTest(map[string]*model.ModelPrice{"pool-a": {ModelName: "pool-a", Enabled: true, InputPrice: 2, OutputPrice: 3, BillingType: model.BillingTypeToken}, "pool-b": {ModelName: "pool-b", Enabled: true, InputPrice: 4, OutputPrice: 5, BillingType: model.BillingTypeToken}})
	cache.Unlock()
	defer func() { cache.Lock(); model.SetModelPriceMapForTest(nil); cache.Unlock() }()
	oldClient := client.HTTPClient
	client.HTTPClient = upstream.Client()
	defer func() { client.HTTPClient = oldClient }()
	pools := []model.UserModelAlias{{Alias: "chat-tool", Targets: []model.UserModelAliasTarget{{Model: "pool-a", Weight: 1}, {Model: "pool-b", Weight: 1}}}}
	if e = model.SaveUserModelAliases(context.Background(), 0, pools); e != nil {
		t.Fatal(e)
	}
	key := model.AliasPoolKey(user.Id, pools[0], "conversation")
	model.ClaimAliasSession(key, model.AliasSessionBinding{Model: "pool-a"}, time.Hour)
	r := gin.New()
	r.POST("/v1/chat/completions", middleware.TokenAuth(), middleware.PlanQuotaCheck(), middleware.Distribute(), api.Relay)
	r.POST("/v1/messages", middleware.NormalizeClaudeMessages(), middleware.TokenAuth(), middleware.PlanQuotaCheck(), middleware.Distribute(), api.RelayMessages)
	r.POST("/v1/responses", middleware.TokenAuth(), middleware.PlanQuotaCheck(), middleware.Distribute(), api.RelayResponses)
	r.GET("/v1/models", middleware.TokenAuth(), api.ListModels)
	call := func(path, body, session string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer sk-poolintegration")
		req.Header.Set("X-Session-ID", session)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	w := call("/v1/chat/completions", `{"model":"chat-tool","messages":[{"role":"user","content":"OK"}],"max_tokens":8}`, "conversation")
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte("pool-b")) {
		t.Fatalf("failover %d %s", w.Code, w.Body.String())
	}
	binding, ok, e := model.ReadAliasSession(key)
	if e != nil || !ok || binding.Model != "pool-b" || binding.ChannelID != 8802 {
		t.Fatalf("binding %+v %v %v", binding, ok, e)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		var count int64
		db.Model(&model.Log{}).Where("user_id = ?", user.Id).Count(&count)
		if count == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("missing success log")
		}
		time.Sleep(10 * time.Millisecond)
	}
	var log model.Log
	db.Where("user_id = ?", user.Id).First(&log)
	if log.ModelName != "chat-tool" || log.ResponseModelName != "pool-b" || log.Quota != 65 {
		t.Fatalf("wrong bill/log %+v", log)
	}
	var updated model.User
	db.First(&updated, user.Id)
	if updated.Quota != 9935 {
		t.Fatalf("failed attempt retained a reservation: quota %d", updated.Quota)
	}
	mu.Lock()
	failA = false
	mu.Unlock()
	for _, test := range []struct{ path, body string }{{"/v1/messages", `{"model":"chat-tool","messages":[{"role":"user","content":"OK"}],"max_tokens":8}`}, {"/v1/responses", `{"model":"chat-tool","input":"OK","max_output_tokens":8}`}} {
		w = call(test.path, test.body, "conversation")
		if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte("OK")) {
			t.Fatalf("wrapper %s %d %s", test.path, w.Code, w.Body.String())
		}
	}
	// Wait for asynchronous successful settlement before restoring the shared DB.
	deadline = time.Now().Add(2 * time.Second)
	for {
		var count int64
		db.Model(&model.Log{}).Where("user_id = ?", user.Id).Count(&count)
		if count == 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("wrapper settlement incomplete")
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	if len(seen) != 4 || seen[0] != "pool-a" || seen[1] != "pool-b" || seen[2] != "pool-b" || seen[3] != "pool-b" {
		t.Fatalf("sticky routing %v", seen)
	}
	mu.Unlock()
	// A personal override cannot inherit the root alias's token grant.
	if e = model.SaveUserModelAliases(context.Background(), user.Id, []model.UserModelAlias{{Alias: "chat-tool", TargetModel: "pool-b"}}); e != nil {
		t.Fatal(e)
	}
	w = call("/v1/chat/completions", `{"model":"chat-tool","messages":[]}`, "personal")
	if w.Code != 403 {
		t.Fatalf("personal override inherited global grant: %d %s", w.Code, w.Body.String())
	}

	// A channel-level failure must not ban a different healthy model on that same channel.
	if e = model.SaveUserModelAliases(context.Background(), user.Id, nil); e != nil {
		t.Fatal(e)
	}
	model.DB.Model(&model.Channel{}).Where("id = ?", 8801).Update("models", "pool-a,pool-b")
	model.DB.Model(&model.Channel{}).Where("id = ?", 8802).Update("status", 0)
	shared := []model.UserModelAlias{{Alias: "shared-pool", Targets: []model.UserModelAliasTarget{{Model: "pool-a", Weight: 1}, {Model: "pool-b", Weight: 1}}}}
	if e = model.SaveUserModelAliases(context.Background(), 0, shared); e != nil {
		t.Fatal(e)
	}
	model.DB.Model(&model.Token{}).Where("id = ?", token.Id).Update("models", "shared-pool")
	oldRouter := channelrouter.DefaultRouter
	channelrouter.DefaultRouter = channelrouter.NewChannelRouter()
	defer func() { channelrouter.DefaultRouter = oldRouter }()
	mu.Lock()
	failA = true
	mu.Unlock()
	sharedKey := model.AliasPoolKey(user.Id, shared[0], "shared")
	model.ClaimAliasSession(sharedKey, model.AliasSessionBinding{Model: "pool-a"}, time.Hour)
	w = call("/v1/chat/completions", `{"model":"shared-pool","messages":[{"role":"user","content":"OK"}],"max_tokens":8}`, "shared")
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte("pool-b")) {
		t.Fatalf("same-channel model failover: %d %s", w.Code, w.Body.String())
	}
	deadline = time.Now().Add(2 * time.Second)
	for {
		var count int64
		db.Model(&model.Log{}).Where("user_id = ?", user.Id).Count(&count)
		if count == 4 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("shared-channel settlement incomplete")
		}
		time.Sleep(10 * time.Millisecond)
	}

}
