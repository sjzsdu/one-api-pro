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

func TestReconcileAbilitiesFromChannelsFillsEmptyTable(t *testing.T) {
	db := setupGroupModelsTestDB(t)
	withTestDB(t, db)

	db.Create(&Channel{Id: 1, Status: ChannelStatusEnabled, Group: "default,vip", Models: "gpt-4o, claude-3"})
	db.Create(&Channel{Id: 2, Status: ChannelStatusManuallyDisabled, Group: "default", Models: "old-model"})

	added, err := ReconcileAbilitiesFromChannels()
	if err != nil {
		t.Fatalf("ReconcileAbilitiesFromChannels: %v", err)
	}
	// 渠道1：2 模型 × 2 分组 = 4；渠道2：1 模型 × 1 分组 = 1，共 5 条。
	if added != 5 {
		t.Errorf("added = %d, want 5", added)
	}

	var count int64
	db.Model(&Ability{}).Count(&count)
	if count != 5 {
		t.Errorf("abilities count = %d, want 5", count)
	}
	// 停用渠道写入的 ability 应为 disabled。
	var disabled Ability
	if err := db.Where("channel_id = ? AND model = ?", 2, "old-model").First(&disabled).Error; err != nil {
		t.Fatalf("query disabled ability: %v", err)
	}
	if disabled.Enabled {
		t.Errorf("ability for disabled channel should have Enabled=false")
	}

	// 补齐后 GetGroupModels 应直接命中 abilities，返回具体模型。
	got, err := GetGroupModels(context.Background(), "default")
	if err != nil {
		t.Fatalf("GetGroupModels: %v", err)
	}
	want := []string{"claude-3", "gpt-4o"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetGroupModels(default) = %v, want %v", got, want)
	}
}

func TestReconcileAbilitiesFromChannelsIsIdempotent(t *testing.T) {
	db := setupGroupModelsTestDB(t)
	withTestDB(t, db)

	db.Create(&Channel{Id: 1, Status: ChannelStatusEnabled, Group: "default", Models: "gpt-4o"})

	if _, err := ReconcileAbilitiesFromChannels(); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	// 第二次不应再插入任何记录。
	added, err := ReconcileAbilitiesFromChannels()
	if err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if added != 0 {
		t.Errorf("second reconcile added = %d, want 0 (idempotent)", added)
	}
	var count int64
	db.Model(&Ability{}).Count(&count)
	if count != 1 {
		t.Errorf("abilities count = %d, want 1", count)
	}
}

func TestReconcileAbilitiesPreservesExistingSettings(t *testing.T) {
	db := setupGroupModelsTestDB(t)
	withTestDB(t, db)

	db.Create(&Channel{Id: 1, Status: ChannelStatusEnabled, Group: "default", Models: "gpt-4o"})
	// 预置一条手工调整过 priority 的 ability。
	priority := int64(99)
	db.Create(&Ability{Group: "default", Model: "gpt-4o", ChannelId: 1, Enabled: true, Priority: &priority})

	added, err := ReconcileAbilitiesFromChannels()
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if added != 0 {
		t.Errorf("added = %d, want 0 (existing record must not be duplicated)", added)
	}
	var got Ability
	db.Where("channel_id = ? AND model = ?", 1, "gpt-4o").First(&got)
	if got.Priority == nil || *got.Priority != 99 {
		t.Errorf("existing ability priority overwritten: got %v, want 99", got.Priority)
	}
}
