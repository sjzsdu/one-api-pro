// alias_pool_prefix_integration_test.go 无会话请求头的消息追加与真实故障切换
// Verifies inferred conversation bindings survive failover through the real relay.
// 版本: v0.0.28
// 日期: 2026-11-22
// 作者: Codex
package controller_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/modelbus/one-api-pro/common"
	"github.com/modelbus/one-api-pro/common/client"
	"github.com/modelbus/one-api-pro/common/config"
	api "github.com/modelbus/one-api-pro/controller"
	"github.com/modelbus/one-api-pro/middleware"
	"github.com/modelbus/one-api-pro/model"
	"gorm.io/gorm"
)

// TestAliasPrefix_RealRelayFailover verifies automatic prefix routing behavior.
// 版本: v0.0.28
// 日期: 2026-11-22
func TestAliasPrefix_RealRelayFailover(t *testing.T) {
	db, e := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if e != nil {
		t.Fatal(e)
	}
	if e = db.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.Ability{}, &model.UserModelAlias{}, &model.Log{}, &model.ModelCatalog{}, &model.InferenceSettlement{}, &model.UserPlan{}); e != nil {
		t.Fatal(e)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	oldDB, oldLog, oldRedis, oldBatch, oldApprox, oldLogging, oldSQLite := model.DB, model.LOG_DB, common.RedisEnabled, config.BatchUpdateEnabled, config.ApproximateTokenEnabled, config.LogConsumeEnabled, common.UsingSQLite
	model.DB, model.LOG_DB, common.RedisEnabled, config.BatchUpdateEnabled, config.ApproximateTokenEnabled, config.LogConsumeEnabled, common.UsingSQLite = db, db, false, false, true, true, true
	defer func() {
		model.DB, model.LOG_DB, common.RedisEnabled, config.BatchUpdateEnabled, config.ApproximateTokenEnabled, config.LogConsumeEnabled, common.UsingSQLite = oldDB, oldLog, oldRedis, oldBatch, oldApprox, oldLogging, oldSQLite
		sqlDB.Close()
	}()
	user := model.User{Id: 8901, Username: "auto-relay", Status: 1, Group: "default", Quota: 1000000, AccessToken: "auto-relay", AffCode: "auto-relay"}
	db.Create(&user)
	token := model.Token{UserId: user.Id, Key: "autorelaykey", Status: 1, UnlimitedQuota: true, ExpiredTime: -1}
	db.Create(&token)
	var mu sync.Mutex
	failA := false
	seen := []string{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		name := fmt.Sprint(body["model"])
		mu.Lock()
		seen = append(seen, name)
		fail := failA && name == "auto-a"
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if fail {
			// A 5xx may have consumed tokens; failover requires a confirmed rejection.
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"error":{"message":"request rejected","type":"rate_limit_error","code":"rate_limit_exceeded"}}`)
			return
		}
		fmt.Fprintf(w, `{"id":"auto-response","object":"chat.completion","model":%q,"choices":[{"index":0,"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`, name)
	}))
	defer upstream.Close()
	base := upstream.URL
	ch := model.Channel{Id: 8901, Type: 1, Status: 1, Key: "fake", Name: "auto", Models: "auto-a,auto-b", Group: "default", BaseURL: &base}
	if e = ch.Insert(); e != nil {
		t.Fatal(e)
	}
	cache := model.GetModelPriceCacheForTest()
	cache.Lock()
	model.SetModelPriceMapForTest(map[string]*model.ModelPrice{"auto-a": {ModelName: "auto-a", Enabled: true, InputPrice: 2, OutputPrice: 3, BillingType: model.BillingTypeToken}, "auto-b": {ModelName: "auto-b", Enabled: true, InputPrice: 4, OutputPrice: 5, BillingType: model.BillingTypeToken}})
	cache.Unlock()
	defer func() { cache.Lock(); model.SetModelPriceMapForTest(nil); cache.Unlock() }()
	oldClient := client.HTTPClient
	client.HTTPClient = upstream.Client()
	defer func() { client.HTTPClient = oldClient }()
	pools := []model.UserModelAlias{{Alias: "auto-pool", Targets: []model.UserModelAliasTarget{{Model: "auto-a", Weight: 1}, {Model: "auto-b", Weight: 1}}}}
	if e = model.SaveUserModelAliases(context.Background(), 0, pools); e != nil {
		t.Fatal(e)
	}
	capturedKey := ""
	r := gin.New()
	r.POST("/v1/chat/completions", middleware.TokenAuth(), middleware.PlanQuotaCheck(), middleware.Distribute(), func(c *gin.Context) { capturedKey = middleware.GetAliasPool(c).SessionKey }, api.Relay)
	call := func(messages []map[string]any) (string, string) {
		raw, _ := json.Marshal(map[string]any{"model": "auto-pool", "messages": messages, "max_tokens": 8})
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(string(raw)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer sk-autorelaykey")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		var result map[string]any
		json.Unmarshal(w.Body.Bytes(), &result)
		if w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		return fmt.Sprint(result["model"]), capturedKey
	}
	first := []map[string]any{{"role": "user", "content": "Please explain the architecture and tradeoffs of this application."}}
	firstModel, key := call(first)
	if key == "" {
		t.Fatal("missing inferred binding")
	}
	// Establish the failure scene deterministically regardless of weighted initial selection.
	if e = model.UpdateAliasSession(key, firstModel, model.AliasSessionBinding{Model: "auto-a", ChannelID: ch.Id}, time.Hour); e != nil {
		t.Fatal(e)
	}
	mu.Lock()
	failA = true
	mu.Unlock()
	next := append(append([]map[string]any{}, first...), map[string]any{"role": "assistant", "content": "An explanation"}, map[string]any{"role": "user", "content": "Continue with implementation details."})
	name, nextKey := call(next)
	if name != "auto-b" || nextKey != key {
		t.Fatalf("automatic failover %s %s", name, nextKey)
	}
	mu.Lock()
	failA = false
	mu.Unlock()
	third := append(append([]map[string]any{}, next...), map[string]any{"role": "assistant", "content": "More details"}, map[string]any{"role": "user", "content": "What about reliability?"})
	name, nextKey = call(third)
	if name != "auto-b" || nextKey != key {
		t.Fatal("next turn did not continue replacement")
	}
	name, nextKey = call(first)
	if name != "auto-b" || nextKey != key {
		t.Fatal("old prefix retained stale model after failover")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		var count int64
		db.Model(&model.Log{}).Where("user_id = ?", user.Id).Count(&count)
		if count == 4 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("settlement incomplete")
		}
		time.Sleep(time.Millisecond * 10)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 5 || seen[1] != "auto-a" || seen[2] != "auto-b" || seen[3] != "auto-b" || seen[4] != "auto-b" {
		t.Fatalf("unexpected upstream routes %v", seen)
	}
}
