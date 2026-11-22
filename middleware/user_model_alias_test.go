// user_model_alias_test.go 别名解析及令牌权限集成测试
// Verifies alias resolution before token permissions and reusable request bodies.
// 版本: v0.0.28
// 日期: 2026-11-22
// 作者: Codex
package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/modelbus/one-api-pro/common"
	"github.com/modelbus/one-api-pro/common/ctxkey"
	"github.com/modelbus/one-api-pro/model"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestTokenAuth_UserAliasPermissions checks target permissions, account isolation and switching.
// 版本: v0.0.28
// 日期: 2026-11-21
func TestTokenAuth_UserAliasPermissions(t *testing.T) {
	seedAliasGroup(t)
	if err := model.DB.AutoMigrate(&model.UserModelAlias{}); err != nil {
		t.Fatal(err)
	}
	id, tid := seedUser(t, "alias-auth", model.RoleCommonUser, "aliasauthkey", true)
	id2, _ := seedUser(t, "alias-other", model.RoleCommonUser, "aliasotherkey", true)
	model.DB.Create(&model.UserModelAlias{UserId: id, Alias: "chat-tool", TargetModel: "deepseek"})
	model.DB.Create(&model.UserModelAlias{UserId: id2, Alias: "chat-tool", TargetModel: "glm"})
	allowed := "deepseek"
	model.DB.Model(&model.Token{}).Where("id = ?", tid).Update("models", allowed)
	r := gin.New()
	r.POST("/v1/chat/completions", TokenAuth(), func(c *gin.Context) {
		body, _ := common.GetRequestBody(c)
		var v map[string]any
		json.Unmarshal(body, &v)
		c.JSON(200, gin.H{"model": v["model"], "target": c.GetString(ctxkey.RequestModel), "alias": c.GetString(ctxkey.UserModelAlias)})
	})
	call := func(key string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"chat-tool","messages":[],"custom":{"keep":true}}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer sk-"+key)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	w := call("aliasauthkey")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"target":"deepseek"`) || !strings.Contains(w.Body.String(), `"model":"deepseek"`) || !strings.Contains(w.Body.String(), `"alias":"chat-tool"`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	w = call("aliasotherkey")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"target":"glm"`) {
		t.Fatal(w.Body.String())
	}
	if err := model.SaveUserModelAliases(context.Background(), id, []model.UserModelAlias{{Alias: "chat-tool", TargetModel: "glm"}}); err != nil {
		t.Fatal(err)
	}
	w = call("aliasauthkey")
	if w.Code != 403 {
		t.Fatalf("changed target bypassed token permission: %d %s", w.Code, w.Body.String())
	}
}

// TestTokenAuth_UserAliasMultipart rejects unsupported uploads before relay.
// 版本: v0.0.28
// 日期: 2026-11-21
func TestTokenAuth_UserAliasMultipart(t *testing.T) {
	seedAliasGroup(t)
	id, _ := seedUser(t, "alias-upload", model.RoleCommonUser, "aliasuploadkey", true)
	model.DB.Create(&model.UserModelAlias{UserId: id, Alias: "audio-alias", TargetModel: "whisper-1"})
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	form.WriteField("model", "audio-alias")
	form.Close()
	r := gin.New()
	r.POST("/v1/audio/transcriptions", TokenAuth(), func(c *gin.Context) { t.Error("unsupported upload reached relay") })
	req := httptest.NewRequest("POST", "/v1/audio/transcriptions", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.Header.Set("Authorization", "Bearer sk-aliasuploadkey")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "multipart") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}

// TestTokenAuth_UserAliasInFlight keeps a resolved target when its mapping changes.
// 版本: v0.0.28
// 日期: 2026-11-21
func TestTokenAuth_UserAliasInFlight(t *testing.T) {
	seedAliasGroup(t)
	id, _ := seedUser(t, "alias-flight", model.RoleCommonUser, "aliasflightkey", true)
	model.DB.Create(&model.UserModelAlias{UserId: id, Alias: "chat-flight", TargetModel: "deepseek"})
	r := gin.New()
	r.POST("/v1/chat/completions", TokenAuth(), func(c *gin.Context) {
		if err := model.SaveUserModelAliases(context.Background(), id, []model.UserModelAlias{{Alias: "chat-flight", TargetModel: "glm"}}); err != nil {
			t.Fatal(err)
		}
		body, _ := common.GetRequestBody(c)
		c.JSON(200, gin.H{"target": c.GetString(ctxkey.RequestModel), "body": string(body)})
	})
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"chat-flight"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer sk-aliasflightkey")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"target":"deepseek"`) || strings.Contains(w.Body.String(), "glm") {
		t.Fatalf("in-flight request changed: %d %s", w.Code, w.Body.String())
	}
}

// TestTokenAuth_UserAliasNullBody rejects null JSON for a model alias in an engine URL.
// 版本: v0.0.28
// 日期: 2026-11-21
func TestTokenAuth_UserAliasNullBody(t *testing.T) {
	seedAliasGroup(t)
	id, _ := seedUser(t, "alias-null", model.RoleCommonUser, "aliasnullkey", true)
	model.DB.Create(&model.UserModelAlias{UserId: id, Alias: "embedding-alias", TargetModel: "embedding-target"})
	r := gin.New()
	r.POST("/v1/engines/:model/embeddings", TokenAuth(), func(c *gin.Context) {
		t.Error("null body reached relay")
	})
	req := httptest.NewRequest("POST", "/v1/engines/embedding-alias/embeddings", strings.NewReader("null"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer sk-aliasnullkey")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "JSON object") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}

// seedAliasGroup provides real group availability for alias authorization tests.
func seedAliasGroup(t *testing.T) {
	t.Helper()
	if err := model.DB.AutoMigrate(&model.Channel{}, &model.Ability{}); err != nil {
		t.Fatal(err)
	}
	if err := model.DB.Where("name = ?", "alias-test-models").Delete(&model.Channel{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := model.DB.Create(&model.Channel{Name: "alias-test-models", Status: model.ChannelStatusEnabled, Group: "default", Models: "deepseek,glm,whisper-1,embedding-target"}).Error; err != nil {
		t.Fatal(err)
	}
}

// TestTokenAuth_UserAliasUnavailable rejects stale targets before routing, without blocking ordinary models.
// 版本: v0.0.28
// 日期: 2026-11-21
func TestTokenAuth_UserAliasUnavailable(t *testing.T) {
	seedAliasGroup(t)
	id, _ := seedUser(t, "alias-lost", model.RoleCommonUser, "aliaslostkey", true)
	model.DB.Create(&model.UserModelAlias{UserId: id, Alias: "chat-lost", TargetModel: "deepseek"})
	model.DB.Model(&model.User{}).Where("id = ?", id).Update("group", "no-models")
	r := gin.New()
	r.POST("/v1/chat/completions", TokenAuth(), func(c *gin.Context) { c.Status(204) })
	for _, tc := range []struct {
		name   string
		status int
	}{{"chat-lost", 403}, {"ordinary", 204}} {
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"`+tc.name+`"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer sk-aliaslostkey")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != tc.status || (tc.status == 403 && !strings.Contains(w.Body.String(), "别名目标模型已不可用")) {
			t.Fatalf("%s: %d %s", tc.name, w.Code, w.Body.String())
		}
	}
}

// TestTokenAuth_UserAliasContentType describes the actual unsupported request format.
// 版本: v0.0.28
// 日期: 2026-11-21
func TestTokenAuth_UserAliasContentType(t *testing.T) {
	seedAliasGroup(t)
	id, _ := seedUser(t, "alias-form", model.RoleCommonUser, "aliasformkey", true)
	model.DB.Create(&model.UserModelAlias{UserId: id, Alias: "form-alias", TargetModel: "deepseek"})
	r := gin.New()
	r.POST("/v1/engines/:model/embeddings", TokenAuth(), func(c *gin.Context) { t.Error("unsupported format reached relay") })
	for _, contentType := range []string{"application/x-www-form-urlencoded", ""} {
		req := httptest.NewRequest("POST", "/v1/engines/form-alias/embeddings", strings.NewReader("model=form-alias"))
		req.Header.Set("Content-Type", contentType)
		req.Header.Set("Authorization", "Bearer sk-aliasformkey")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 400 || strings.Contains(w.Body.String(), "multipart") || !strings.Contains(w.Body.String(), "application/json") {
			t.Fatalf("%q: %d %s", contentType, w.Code, w.Body.String())
		}
	}
}

// TestTokenAuth_UserAliasOpaqueReferences keeps explicit rejection while allowing absent references.
// 版本: v0.0.28
// 日期: 2026-11-22
func TestTokenAuth_UserAliasOpaqueReferences(t *testing.T) {
	seedAliasGroup(t)
	id, _ := seedUser(t, "alias-opaque", model.RoleCommonUser, "aliasopaquekey", true)
	if err := model.SaveUserModelAliases(context.Background(), id, []model.UserModelAlias{{Alias: "opaque-test", TargetModel: "deepseek"}}); err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.POST("/v1/chat/completions", TokenAuth(), func(c *gin.Context) { c.Status(204) })
	for _, test := range []struct {
		field  string
		status int
	}{
		{`"previous_response_id":"resp-previous"`, 400},
		{`"conversation":{"id":"conv-previous"}`, 400},
		{`"previous_response_id":""`, 204},
		{`"conversation":null`, 204},
	} {
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"opaque-test",`+test.field+`}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer sk-aliasopaquekey")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != test.status || (test.status == 400 && !strings.Contains(w.Body.String(), "请发送完整聊天历史")) {
			t.Fatalf("%s: %d %s", test.field, w.Code, w.Body.String())
		}
	}
}
