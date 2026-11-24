// user_model_alias_test.go 用户别名 API、真实转发与计费日志集成测试
// Exercises account-scoped APIs and a real relay against a local fake upstream.
// 版本: v0.0.28
// 日期: 2026-11-21
// 作者: Codex
package controller_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/modelbus/one-api-pro/common"
	"github.com/modelbus/one-api-pro/common/client"
	"github.com/modelbus/one-api-pro/common/config"
	"github.com/modelbus/one-api-pro/common/ctxkey"
	api "github.com/modelbus/one-api-pro/controller"
	"github.com/modelbus/one-api-pro/middleware"
	"github.com/modelbus/one-api-pro/model"
	handler "github.com/modelbus/one-api-pro/relay/handler"
	"gorm.io/gorm"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestUserModelAlias_APIAndRelay verifies owner scope, concrete billing and alias/actual logs.
// 版本: v0.0.28
// 日期: 2026-11-21
func TestUserModelAlias_APIAndRelay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldApprox := config.ApproximateTokenEnabled
	config.ApproximateTokenEnabled = true
	defer func() { config.ApproximateTokenEnabled = oldApprox }()
	db, err := gorm.Open(sqlite.Open("file:alias-relay?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err = db.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.Ability{}, &model.UserModelAlias{}, &model.Log{}, &model.ModelCatalog{}, &model.InferenceSettlement{}); err != nil {
		t.Fatal(err)
	}
	oldDB, oldLog, oldRedis, oldLogEnabled, oldBatch := model.DB, model.LOG_DB, common.RedisEnabled, config.LogConsumeEnabled, config.BatchUpdateEnabled
	model.DB, model.LOG_DB, common.RedisEnabled, config.LogConsumeEnabled, config.BatchUpdateEnabled = db, db, false, true, false
	defer func() {
		model.DB, model.LOG_DB, common.RedisEnabled, config.LogConsumeEnabled, config.BatchUpdateEnabled = oldDB, oldLog, oldRedis, oldLogEnabled, oldBatch
		sqlDB.Close()
	}()
	user := model.User{Id: 991, Username: "alias-integration", Status: model.UserStatusEnabled, Group: "default", Quota: 10000000, AccessToken: "aliasacc", AffCode: "aliasaff"}
	if err = db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	token := model.Token{UserId: user.Id, Key: "aliasintegrationkey", Status: model.TokenStatusEnabled, UnlimitedQuota: true, ExpiredTime: -1}
	if err = db.Create(&token).Error; err != nil {
		t.Fatal(err)
	}
	db.Create(&model.Channel{Id: 991, Status: model.ChannelStatusEnabled, Models: "deepseek,glm", Group: "default"})
	cache := model.GetModelPriceCacheForTest()
	cache.Lock()
	model.SetModelPriceMapForTest(map[string]*model.ModelPrice{
		"deepseek":  {ModelName: "deepseek", Enabled: true, InputPrice: 2, OutputPrice: 3, BillingType: model.BillingTypeToken},
		"glm":       {ModelName: "glm", Enabled: true, InputPrice: 4, OutputPrice: 5, BillingType: model.BillingTypeToken},
		"chat-tool": {ModelName: "chat-tool", Enabled: true, InputPrice: 999, OutputPrice: 999, BillingType: model.BillingTypeToken},
	})
	cache.Unlock()
	defer func() { cache.Lock(); model.SetModelPriceMapForTest(nil); cache.Unlock() }()
	modelsSeen := make(chan string, 4)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		modelsSeen <- fmt.Sprint(body["model"])
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id":"test","object":"chat.completion","model":%q,"choices":[{"index":0,"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120}}`, body["model"])
	}))
	defer upstream.Close()
	oldClient := client.HTTPClient
	client.HTTPClient = upstream.Client()
	defer func() { client.HTTPClient = oldClient }()
	r := gin.New()
	r.PUT("/api/user/model_aliases", func(c *gin.Context) { c.Set(ctxkey.Id, user.Id) }, api.UpdateUserModelAliases)
	r.GET("/api/user/model_aliases", func(c *gin.Context) { c.Set(ctxkey.Id, user.Id) }, api.GetUserModelAliases)
	r.GET("/v1/models", middleware.TokenAuth(), api.ListModels)
	r.POST("/v1/chat/completions", middleware.TokenAuth(), func(c *gin.Context) {
		c.Set(ctxkey.ChannelID, "openai")
		c.Set(ctxkey.ChannelId, 991)
		c.Set(ctxkey.Group, "default")
		c.Set(ctxkey.BaseURL, upstream.URL)
		c.Set(ctxkey.ModelMapping, map[string]string{"deepseek": "deepseek-upstream", "glm": "glm-upstream"})
		c.Set(ctxkey.Config, model.ChannelConfig{})
		if e := handler.RelayTextHelper(c); e != nil {
			c.JSON(e.StatusCode, e)
		}
	})
	call := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer sk-aliasintegrationkey")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	for i, target := range []string{"deepseek", "glm"} {
		// A spoofed owner field must not move settings into another account.
		w := call("PUT", "/api/user/model_aliases", fmt.Sprintf(`{"aliases":[{"alias":"chat-tool","target_model":%q,"user_id":992}]}`, target))
		if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"success":true`)) {
			t.Fatal(w.Body.String())
		}
		if got, _ := model.ResolveUserModelAlias(992, "chat-tool"); got != "chat-tool" {
			t.Fatal("owner spoofing succeeded")
		}
		w = call("POST", "/v1/chat/completions", `{"model":"chat-tool","messages":[{"role":"user","content":"hello"}],"max_tokens":32}`)
		if w.Code != 200 {
			t.Fatalf("relay %d: %s", w.Code, w.Body.String())
		}
		if got := <-modelsSeen; got != target+"-upstream" {
			t.Fatal(got)
		}
		deadline := time.Now().Add(2 * time.Second)
		var logs []model.Log
		for {
			db.Order("id").Find(&logs)
			if len(logs) == i+1 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("consume log missing")
			}
			time.Sleep(10 * time.Millisecond)
		}
		log := logs[i]
		want := 260
		if i == 1 {
			want = 500
		}
		if log.ModelName != "chat-tool" || log.ResponseModelName != target+"-upstream" || log.Quota != want {
			t.Fatalf("unexpected billing/log: %+v, quota want %d", log, want)
		}
	}
	// Discovery exposes an alias only when its current target passes token limits.
	w := call("GET", "/v1/models", "")
	if !bytes.Contains(w.Body.Bytes(), []byte(`"id":"chat-tool"`)) {
		t.Fatalf("alias missing from models: %s", w.Body.String())
	}
	db.Model(&model.Token{}).Where("id = ?", token.Id).Update("models", "deepseek")
	w = call("GET", "/v1/models", "")
	if bytes.Contains(w.Body.Bytes(), []byte(`"id":"chat-tool"`)) {
		t.Fatal("discovery exposed forbidden alias target")
	}
	db.Model(&model.Token{}).Where("id = ?", token.Id).Update("models", "")
	// Empty object must not accidentally delete settings; explicit [] remains supported.
	w = call("PUT", "/api/user/model_aliases", `{}`)
	if bytes.Contains(w.Body.Bytes(), []byte(`"success":true`)) {
		t.Fatal("missing aliases accepted")
	}
	// Non-alias requests retain the normal request-model log value.
	w = call("POST", "/v1/chat/completions", `{"model":"deepseek","messages":[{"role":"user","content":"hello"}]}`)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	<-modelsSeen
	deadline := time.Now().Add(2 * time.Second)
	for {
		var log model.Log
		db.Order("id DESC").First(&log)
		if log.ModelName == "deepseek" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("ordinary log changed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err = model.SaveUserModelAliases(context.Background(), user.Id, []model.UserModelAlias{}); err != nil {
		t.Fatal(err)
	}
}

// TestUserModelAlias_Discovery verifies real-token detail access, permissions and optional discovery failure.
// 版本: v0.0.28
// 日期: 2026-11-21
func TestUserModelAlias_Discovery(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.Ability{}, &model.UserModelAlias{}); err != nil {
		t.Fatal(err)
	}
	oldDB, oldRedis := model.DB, common.RedisEnabled
	model.DB, common.RedisEnabled = db, false
	defer func() { model.DB, common.RedisEnabled = oldDB, oldRedis }()
	db.Create(&model.User{Id: 552, Username: "alias-discovery", Status: 1, Group: "default", AccessToken: "discoveryacc", AffCode: "discoveryaff"})
	db.Create(&model.Token{Id: 552, UserId: 552, Key: "aliasdiscoverykey", Status: 1, UnlimitedQuota: true, ExpiredTime: -1})
	db.Create(&model.Channel{Status: 1, Group: "default", Models: "deepseek,glm"})
	db.Create(&model.UserModelAlias{UserId: 552, Alias: "chat-tool", TargetModel: "deepseek"})
	db.Create(&model.UserModelAlias{UserId: 552, Alias: "gpt-4", TargetModel: "deepseek"})
	r := gin.New()
	r.GET("/v1/models", middleware.TokenAuth(), api.ListModels)
	r.GET("/v1/models/:model", middleware.TokenAuth(), api.RetrieveModel)
	call := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Authorization", "Bearer sk-aliasdiscoverykey")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	for _, path := range []string{"/v1/models", "/v1/models/chat-tool", "/v1/models/Chat-Tool"} {
		w := call(path)
		if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"id":"chat-tool"`)) {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
	for _, path := range []string{"/v1/models/gpt-4", "/v1/models/GPT-4"} {
		w := call(path)
		if !bytes.Contains(w.Body.Bytes(), []byte(`"owned_by":"custom"`)) {
			t.Fatalf("static catalog shadowed alias: %s", w.Body.String())
		}
	}
	db.Model(&model.Token{}).Where("id = ?", 552).Update("models", "chat-tool,gpt-4,CHAT-TOOL,GPT-4")
	if w := call("/v1/models"); bytes.Contains(w.Body.Bytes(), []byte(`"id":"chat-tool"`)) || bytes.Contains(w.Body.Bytes(), []byte(`"id":"gpt-4"`)) || bytes.Contains(w.Body.Bytes(), []byte(`"id":"CHAT-TOOL"`)) || bytes.Contains(w.Body.Bytes(), []byte(`"id":"GPT-4"`)) {
		t.Fatalf("alias-only token discovered forbidden target: %s", w.Body.String())
	}
	if w := call("/v1/models/gpt-4"); bytes.Contains(w.Body.Bytes(), []byte(`"id":"gpt-4"`)) {
		t.Fatal("static catalog bypassed alias target permissions")
	}
	db.Model(&model.Token{}).Where("id = ?", 552).Update("models", "glm")
	if w := call("/v1/models/chat-tool"); bytes.Contains(w.Body.Bytes(), []byte(`"id":"chat-tool"`)) {
		t.Fatal("detail exposed forbidden alias")
	}
	db.Model(&model.Token{}).Where("id = ?", 552).Update("models", "")
	db.Model(&model.User{}).Where("id = ?", 552).Update("group", "empty")
	if w := call("/v1/models/chat-tool"); bytes.Contains(w.Body.Bytes(), []byte(`"id":"chat-tool"`)) {
		t.Fatal("detail exposed stale group target")
	}
	db.Model(&model.User{}).Where("id = ?", 552).Update("group", "default")
	// An alias-cache miss against an unavailable optional table must retain base models.
	db.Create(&model.User{Id: 553, Username: "alias-unavailable", Status: 1, Group: "default", AccessToken: "unavailableacc", AffCode: "unavailableaff"})
	db.Create(&model.Token{Id: 553, UserId: 553, Key: "aliasunavailablekey", Status: 1, UnlimitedQuota: true, ExpiredTime: -1})
	db.Migrator().DropTable(&model.UserModelAlias{})
	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer sk-aliasunavailablekey")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"id":"deepseek"`)) {
		t.Fatalf("optional aliases broke model list: %d %s", w.Code, w.Body.String())
	}
}
