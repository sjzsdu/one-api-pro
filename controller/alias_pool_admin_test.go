// alias_pool_admin_test.go 全局模型池管理权限测试
// Restricts publishing to root and isolates ordinary users' configuration.
// 版本: v0.0.28
// 日期: 2026-11-22
// 作者: Codex
package controller_test

import (
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/modelbus/one-api-pro/common"
	api "github.com/modelbus/one-api-pro/controller"
	"github.com/modelbus/one-api-pro/middleware"
	"github.com/modelbus/one-api-pro/model"
	"gorm.io/gorm"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAliasPool_RootPublishing verifies the model-pool behavior described by this test.
// 版本: v0.0.28
// 日期: 2026-11-22
func TestAliasPool_RootPublishing(t *testing.T) {
	db, e := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if e != nil {
		t.Fatal(e)
	}
	if e = db.AutoMigrate(&model.User{}, &model.Channel{}, &model.Ability{}, &model.UserModelAlias{}); e != nil {
		t.Fatal(e)
	}
	oldDB, oldRedis := model.DB, common.RedisEnabled
	model.DB, common.RedisEnabled = db, false
	defer func() { model.DB, common.RedisEnabled = oldDB, oldRedis }()
	for _, u := range []model.User{{Id: 8901, Username: "pool-root", Role: 100, Status: 1, AccessToken: "rootpublish", AffCode: "rootpub"}, {Id: 8902, Username: "pool-user", Role: 1, Status: 1, AccessToken: "userpublish", AffCode: "userpub"}, {Id: 8903, Username: "pool-admin", Role: 10, Status: 1, AccessToken: "adminpublish", AffCode: "adminpub"}} {
		db.Create(&u)
	}
	db.Create(&model.Channel{Status: 1, Models: "model-a,model-b", Group: "default"})
	r := gin.New()
	r.Use(sessions.Sessions("test", cookie.NewStore([]byte("test-model-pool-cookie-key-123456"))))
	r.PUT("/api/model_alias/global", middleware.RootAuth(), api.UpdateGlobalModelAliases)
	for _, key := range []string{"userpublish", "adminpublish", "rootpublish"} {
		req := httptest.NewRequest("PUT", "/api/model_alias/global", strings.NewReader(`{"aliases":[{"alias":"shared","targets":[{"model":"model-a","weight":1},{"model":"model-b","weight":2}]}]}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", key)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		success := strings.Contains(w.Body.String(), `"success":true`)
		if success != (key == "rootpublish") {
			t.Fatalf("%s: %d %s", key, w.Code, w.Body.String())
		}
	}
	items, e := model.GetEffectiveUserModelAliases(8902)
	if e != nil || len(items) != 1 || items[0].Source != "global" {
		t.Fatalf("global publication %+v %v", items, e)
	}
}
