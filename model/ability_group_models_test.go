// ability_group_models_test.go abilities 为空时从 channels 回退推导可用模型测试
// Tests for group models fallback to channels when abilities table is empty.
// 版本: v0.0.28
// 日期: 2026-11-18
// 作者: opencode
package model

import (
	"context"
	"reflect"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// setupGroupModelsTestDB 初始化内存库，仅建 channels + abilities 两张表。
func setupGroupModelsTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&Channel{}, &Ability{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// withTestDB 临时替换全局 DB，测试结束后恢复。
func withTestDB(t *testing.T, db *gorm.DB) {
	t.Helper()
	old := DB
	DB = db
	t.Cleanup(func() { DB = old })
}

func TestGetGroupModelsFallsBackToChannelsWhenAbilitiesEmpty(t *testing.T) {
	db := setupGroupModelsTestDB(t)
	withTestDB(t, db)

	// abilities 表保持为空；channels 提供 default 组的三个模型（含重复/空白需清理）。
	channels := []Channel{
		{Id: 1, Status: ChannelStatusEnabled, Group: "default", Models: "gpt-4o, gpt-4o-mini, claude-3"},
		{Id: 2, Status: ChannelStatusEnabled, Group: "default,vip", Models: "gpt-4o, ,llama-3"},
		{Id: 3, Status: ChannelStatusManuallyDisabled, Group: "default", Models: "disabled-only-model"},
		{Id: 4, Status: ChannelStatusEnabled, Group: "default", Models: "*"},
		{Id: 5, Status: ChannelStatusEnabled, Group: "default-vip", Models: "wrong-group-model"},
	}
	for _, ch := range channels {
		if err := db.Create(&ch).Error; err != nil {
			t.Fatalf("seed channel: %v", err)
		}
	}

	got, err := GetGroupModels(context.Background(), "default")
	if err != nil {
		t.Fatalf("GetGroupModels: %v", err)
	}
	want := []string{"claude-3", "gpt-4o", "gpt-4o-mini", "llama-3"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetGroupModels(default) = %v, want %v", got, want)
	}
}

func TestGetGroupModelsPrefersAbilitiesWhenPresent(t *testing.T) {
	db := setupGroupModelsTestDB(t)
	withTestDB(t, db)

	// abilities 有数据时不得回退到 channels。
	db.Create(&Channel{Id: 1, Status: ChannelStatusEnabled, Group: "default", Models: "from-channel"})
	db.Create(&Ability{Group: "default", Model: "from-ability", ChannelId: 1, Enabled: true})

	got, err := GetGroupModels(context.Background(), "default")
	if err != nil {
		t.Fatalf("GetGroupModels: %v", err)
	}
	want := []string{"from-ability"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetGroupModels(default) = %v, want %v", got, want)
	}
}

func TestGetGroupModelsFromChannelsExactGroupMatch(t *testing.T) {
	db := setupGroupModelsTestDB(t)
	withTestDB(t, db)

	db.Create(&Channel{Id: 1, Status: ChannelStatusEnabled, Group: "default", Models: "gpt-4o"})
	db.Create(&Channel{Id: 2, Status: ChannelStatusEnabled, Group: "default-vip", Models: "secret-model"})
	db.Create(&Channel{Id: 3, Status: ChannelStatusEnabled, Group: "vip", Models: "vip-model"})

	got := GetGroupModelsFromChannels("default")
	want := []string{"gpt-4o"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetGroupModelsFromChannels(default) = %v, want %v", got, want)
	}
}

func TestGetGroupModelsFromChannelsWildcardOnlyReturnsEmpty(t *testing.T) {
	db := setupGroupModelsTestDB(t)
	withTestDB(t, db)

	// 只有通配渠道时无法枚举具体模型名，应返回空而非 ["*"]。
	db.Create(&Channel{Id: 1, Status: ChannelStatusEnabled, Group: "default", Models: "*"})

	got := GetGroupModelsFromChannels("default")
	if len(got) != 0 {
		t.Errorf("GetGroupModelsFromChannels(default) = %v, want empty for wildcard-only group", got)
	}
}

func TestGetGroupModelsFromChannelsEmptyGroup(t *testing.T) {
	db := setupGroupModelsTestDB(t)
	withTestDB(t, db)

	db.Create(&Channel{Id: 1, Status: ChannelStatusEnabled, Group: "default", Models: "gpt-4o"})

	if got := GetGroupModelsFromChannels(""); len(got) != 0 {
		t.Errorf("GetGroupModelsFromChannels(\"\") = %v, want empty", got)
	}
}
