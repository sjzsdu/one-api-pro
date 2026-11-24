// model_capability_test.go 模型目录权限与供应商映射回归测试
// Verifies account-scoped discovery and provider-specific capability overrides.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package controller

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/modelbus/one-api-pro/common/ctxkey"
	"github.com/modelbus/one-api-pro/model"
	"gorm.io/gorm"
)

func TestListModelOptions_CachedDirectoryInvalidatesOnChannelAndCatalogWrites(t *testing.T) {
	setupCapabilityCatalog(t)
	var reads atomic.Int64
	model.DB.Callback().Query().Before("gorm:query").Register("test:count_directory_reads", func(tx *gorm.DB) {
		if tx.Statement.Table == "channels" || tx.Statement.Table == "model_catalogs" {
			reads.Add(1)
		}
	})
	request := func() string {
		c, w := capabilityContext("GET", "/api/model/options", "")
		ListModelOptions(c)
		return w.Body.String()
	}
	request()
	first := reads.Load()
	request()
	if reads.Load() != first {
		t.Fatalf("directory re-read on cache hit: %d -> %d", first, reads.Load())
	}
	model.DB.Model(&model.Channel{}).Where("id = ?", 91).Update("status", 2)
	if strings.Contains(request(), "friendly-voice") {
		t.Fatal("disabled channel remained cached")
	}
	model.DB.Model(&model.Channel{}).Where("id = ?", 91).Update("status", 1)
	model.DB.Model(&model.ModelCatalog{}).Where("provider = ?", "ali").Update("display_name", "Refreshed voice")
	if !strings.Contains(request(), "Refreshed voice") {
		t.Fatal("catalog edit remained cached")
	}
}

func setupCapabilityCatalog(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:capability-%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	model.InitModelCapabilityCache(db)
	if err = db.AutoMigrate(&model.User{}, &model.Channel{}, &model.ModelCatalog{}, &model.UserModelAlias{}, &model.Ability{}, &model.Log{}); err != nil {
		t.Fatal(err)
	}
	old := model.DB
	oldLog := model.LOG_DB
	model.DB = db
	model.LOG_DB = db
	t.Cleanup(func() { model.DB = old; model.LOG_DB = oldLog; sqlDB, _ := db.DB(); sqlDB.Close() })
	db.Create(&model.User{Id: 91, Username: "cap-user", Group: "default"})
	db.Create(&model.Channel{Id: 91, Type: 17, Name: "private-channel-name", Key: "secret", Status: 1, Models: "friendly-voice", Group: "default", ModelMapping: ptrCapability(`{"friendly-voice":"cosyvoice-v3-flash"}`)})
	db.Create(&model.Channel{Id: 92, Type: 17, Status: 1, Models: "private-model", Group: "vip"})
	db.Create(&model.Channel{Id: 93, Type: 17, Status: 2, Models: "disabled-model", Group: "default"})
	db.Create(&model.ModelCatalog{Provider: "ali", ModelName: "cosyvoice-v3-flash", DisplayName: "Custom Voice", CapabilityOverride: `{"primary_category":"audio","operations":{"audio.speech":{"modes":["sync"],"limits":{"formats":["wav"]}}}}`})
}

// TestListModelOptions_AliasRequiresCommonOperations preserves pool guarantees during filtered discovery.
// 版本: v0.0.28
// 日期: 2026-11-23
func TestListModelOptions_AliasRequiresCommonOperations(t *testing.T) {
	setupCapabilityCatalog(t)
	model.DB.Create(&model.Channel{Id: 94, Type: 17, Status: 1, Models: "gpt-4o", Group: "default"})
	model.DB.Create(&model.UserModelAlias{UserId: 91, Alias: "mixed-pool", TargetModel: "friendly-voice", TargetsJSON: `[{"model":"friendly-voice","weight":1},{"model":"gpt-4o","weight":1}]`})
	c, w := capabilityContext("GET", "/api/model/options?operation=audio.speech", "")
	ListModelOptions(c)
	if strings.Contains(w.Body.String(), "mixed-pool") {
		t.Fatalf("pool with incompatible target advertised speech support: %s", w.Body.String())
	}
}
func ptrCapability(v string) *string { return &v }

func TestListModelOptions_AccountScopeAndNoTopology(t *testing.T) {
	setupCapabilityCatalog(t)
	c, w := capabilityContext("GET", "/api/model/options", "")
	ListModelOptions(c)
	var envelope struct {
		Success bool          `json:"success"`
		Data    []modelOption `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.Success || len(envelope.Data) != 1 || envelope.Data[0].ModelName != "friendly-voice" || envelope.Data[0].Capabilities.Source != "override" {
		t.Fatalf("wrong options: %s", w.Body.String())
	}
	for _, secret := range []string{"private-channel-name", "secret", "channel_id", "provider", "cosyvoice-v3-flash", "private-model", "disabled-model"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatalf("discovery leaked %s: %s", secret, w.Body.String())
		}
	}
	c, w = capabilityContext("GET", "/api/model/options?scope=all", "")
	ListModelOptions(c)
	if strings.Contains(w.Body.String(), `"success":true`) {
		t.Fatalf("non-admin obtained global options: %s", w.Body.String())
	}
}

func capabilityContext(method, path, body string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, path, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(ctxkey.Id, 91)
	return c, w
}

func TestUpdateModelCapabilities_PreservesProviderMetadata(t *testing.T) {
	setupCapabilityCatalog(t)
	c, w := capabilityContext("PUT", "/api/model/capabilities", `{"provider":"ali","model_name":"cosyvoice-v3-flash","capabilities":{"primary_category":"audio","operations":{"audio.speech":{"modes":["sync"]}}}}`)
	c.Set("role", model.RoleAdminUser)
	UpdateModelCapabilities(c)
	if !strings.Contains(w.Body.String(), `"success":true`) {
		t.Fatal(w.Body.String())
	}
	var row model.ModelCatalog
	model.DB.Where("provider = ? AND model_name = ?", "ali", "cosyvoice-v3-flash").First(&row)
	if row.DisplayName != "Custom Voice" || row.CapabilityOverride == "" {
		t.Fatalf("metadata overwritten: %+v", row)
	}
}

// TestModelCatalogRefresh_PreservesOverride isolates operator choices from discovery refreshes.
// 版本: v0.0.28
// 日期: 2026-11-23
func TestModelCatalogRefresh_PreservesOverride(t *testing.T) {
	setupCapabilityCatalog(t)
	if err := model.UpsertModelCatalog([]model.ModelCatalog{{Provider: "ali", ModelName: "cosyvoice-v3-flash", DisplayName: "Updated Voice", Capabilities: `{"primary_category":"text","operations":{"chat.generate":{"modes":["sync"]}}}`}}); err != nil {
		t.Fatal(err)
	}
	var row model.ModelCatalog
	if err := model.DB.Where("provider = ? AND model_name = ?", "ali", "cosyvoice-v3-flash").First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if got := model.ResolveModelCapabilities("ali", "cosyvoice-v3-flash", &row); got.Source != "override" || got.PrimaryCategory != "audio" {
		t.Fatalf("refresh erased override: %+v", got)
	}
}
