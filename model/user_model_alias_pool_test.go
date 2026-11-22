// user_model_alias_pool_test.go 全局别名与多候选兼容测试
// Verifies global fallback, personal overrides and pool validation.
// 版本: v0.0.28
// 日期: 2026-11-22
// 作者: Codex
package model

import (
	"context"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"testing"
)

// TestUserModelAliases_GlobalPool verifies the model-pool behavior described by this test.
// 版本: v0.0.28
// 日期: 2026-11-22
func TestUserModelAliases_GlobalPool(t *testing.T) {
	db, e := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if e != nil {
		t.Fatal(e)
	}
	if e = db.AutoMigrate(&User{}, &UserModelAlias{}, &Channel{}, &Ability{}); e != nil {
		t.Fatal(e)
	}
	withTestDB(t, db)
	db.Create(&User{Id: 701, Username: "pool", Group: "default", AccessToken: "pool", AffCode: "pool"})
	db.Create(&Channel{Status: 1, Group: "default", Models: "deepseek,glm"})
	global := []UserModelAlias{{Alias: "chat-tool", Targets: []UserModelAliasTarget{{Model: "deepseek", Weight: 3}, {Model: "glm", Weight: 1}}}}
	if e = SaveUserModelAliases(context.Background(), 0, global); e != nil {
		t.Fatal(e)
	}
	items, e := GetEffectiveUserModelAliases(701)
	if e != nil || len(items) != 1 || items[0].Source != "global" || len(items[0].Targets) != 2 {
		t.Fatalf("%+v %v", items, e)
	}
	if e = SaveUserModelAliases(context.Background(), 701, []UserModelAlias{{Alias: "chat-tool", TargetModel: "glm"}}); e != nil {
		t.Fatal(e)
	}
	items, e = GetEffectiveUserModelAliases(701)
	if e != nil || items[0].Source != "personal" || items[0].Targets[0].Model != "glm" {
		t.Fatalf("%+v %v", items, e)
	}
	if e = SaveUserModelAliases(context.Background(), 701, []UserModelAlias{{Alias: "bad", Targets: []UserModelAliasTarget{{Model: "glm", Weight: -1}}}}); e == nil {
		t.Fatal("negative weight accepted")
	}
}
