// user_model_alias_test.go 用户模型别名隔离与校验测试
// Tests account isolation, switching, and validation of model aliases.
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

// TestUserModelAliases_IsolationAndSwitch verifies account-local replacement without stale resolution.
// 版本: v0.0.28
// 日期: 2026-11-21
func TestUserModelAliases_IsolationAndSwitch(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&User{}, &UserModelAlias{}, &Ability{}, &Channel{}); err != nil {
		t.Fatal(err)
	}
	withTestDB(t, db)
	for _, u := range []User{{Id: 301, Username: "alias1", Group: "default", AccessToken: "a1", AffCode: "a1"}, {Id: 302, Username: "alias2", Group: "default", AccessToken: "a2", AffCode: "a2"}} {
		if err = db.Create(&u).Error; err != nil {
			t.Fatal(err)
		}
	}
	db.Create(&Channel{Status: ChannelStatusEnabled, Group: "default", Models: "deepseek,glm"})
	for _, id := range []int{301, 302} {
		if err = SaveUserModelAliases(context.Background(), id, []UserModelAlias{{Alias: "chat-tool", TargetModel: "deepseek"}}); err != nil {
			t.Fatal(err)
		}
	}
	if err = SaveUserModelAliases(context.Background(), 301, []UserModelAlias{{Alias: "chat-tool", TargetModel: "glm"}}); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[int]string{301: "glm", 302: "deepseek"} {
		got, err := ResolveUserModelAlias(id, "chat-tool")
		if err != nil || got != want {
			t.Fatalf("user %d: %s %v", id, got, err)
		}
	}
	for _, items := range [][]UserModelAlias{
		{{Alias: "auto", TargetModel: "glm"}}, {{Alias: "glm", TargetModel: "deepseek"}},
		{{Alias: "x", TargetModel: "missing"}}, {{Alias: "x", TargetModel: "y"}, {Alias: "y", TargetModel: "glm"}},
		{{Alias: "x", TargetModel: "glm"}, {Alias: "x", TargetModel: "deepseek"}},
	} {
		if err := SaveUserModelAliases(context.Background(), 301, items); err == nil {
			t.Fatalf("accepted invalid aliases: %+v", items)
		}
	}
	got, _ := ResolveUserModelAlias(301, "chat-tool")
	if got != "glm" {
		t.Fatal("invalid save changed previous mapping")
	}
	if err = SaveUserModelAliases(context.Background(), 301, nil); err != nil {
		t.Fatal(err)
	}
	got, _ = ResolveUserModelAlias(301, "chat-tool")
	if got != "chat-tool" {
		t.Fatal("delete failed")
	}
}

// TestUserModelAliases_CacheAndNormalization checks negative caching, save invalidation and portable names.
// 版本: v0.0.28
// 日期: 2026-11-22
func TestUserModelAliases_CacheAndNormalization(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&User{}, &UserModelAlias{}, &Ability{}, &Channel{}); err != nil {
		t.Fatal(err)
	}
	withTestDB(t, db)
	db.Create(&User{Id: 401, Username: "alias-cache", Group: "default", AccessToken: "cache", AffCode: "cache"})
	db.Create(&Channel{Status: ChannelStatusEnabled, Group: "default", Models: "deepseek,glm,CaseSensitiveModel"})
	queries := 0
	db.Callback().Query().After("gorm:query").Register("alias-query-count", func(tx *gorm.DB) {
		if tx.Statement.Table == "user_model_aliases" {
			queries++
		}
	})
	for i := 0; i < 3; i++ {
		if target, e := ResolveUserModelAlias(401, "ordinary-model"); e != nil || target != "ordinary-model" {
			t.Fatalf("%s %v", target, e)
		}
	}
	if queries != 2 {
		t.Fatalf("empty mapping queried database %d times", queries)
	}
	if err = SaveUserModelAliases(context.Background(), 401, []UserModelAlias{{Alias: "Chat-Tool", TargetModel: "deepseek"}}); err != nil {
		t.Fatal(err)
	}
	items, err := GetUserModelAliases(401)
	if err != nil || len(items) != 1 || items[0].Alias != "chat-tool" {
		t.Fatalf("not normalized: %+v %v", items, err)
	}
	for _, name := range []string{"chat-tool", "Chat-Tool", "CHAT-TOOL"} {
		if got, e := ResolveUserModelAlias(401, name); e != nil || got != "deepseek" {
			t.Fatalf("%s => %s %v", name, got, e)
		}
	}
	before := queries
	if _, err = GetUserModelAliases(401); err != nil {
		t.Fatal(err)
	}
	if queries != before {
		t.Fatal("warm mapping queried database")
	}
	if err = SaveUserModelAliases(context.Background(), 401, []UserModelAlias{{Alias: "chat-tool", TargetModel: "glm"}}); err != nil {
		t.Fatal(err)
	}
	if got, e := ResolveUserModelAlias(401, "CHAT-TOOL"); e != nil || got != "glm" {
		t.Fatalf("stale mapping: %s %v", got, e)
	}
	for _, items := range [][]UserModelAlias{
		{{Alias: "chat-tool", TargetModel: "glm"}, {Alias: "Chat-Tool", TargetModel: "deepseek"}},
		{{Alias: "casesensitivemodel", TargetModel: "glm"}},
	} {
		if err = SaveUserModelAliases(context.Background(), 401, items); err == nil {
			t.Fatalf("accepted case collision: %+v", items)
		}
	}
	if got, e := ResolveUserModelAlias(401, "CaseSensitiveModel"); e != nil || got != "CaseSensitiveModel" {
		t.Fatal("ordinary model case changed")
	}
}

// TestUserModelAliases_LegacyCase reads pre-normalization names consistently across database collations.
// 版本: v0.0.28
// 日期: 2026-11-21
func TestUserModelAliases_LegacyCase(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&UserModelAlias{}); err != nil {
		t.Fatal(err)
	}
	withTestDB(t, db)
	db.Create(&UserModelAlias{UserId: 402, Alias: "Chat-Tool", TargetModel: "CaseSensitiveModel"})
	for _, name := range []string{"chat-tool", "Chat-Tool"} {
		if got, e := ResolveUserModelAlias(402, name); e != nil || got != "CaseSensitiveModel" {
			t.Fatalf("%s => %s %v", name, got, e)
		}
	}
}
