// group_test.go 分组目录并集测试
// Tests for GetAllGroupNames merging price/channel/user group sources.
// 版本: v0.0.28
// 日期: 2026-11-18
package model

import (
	"reflect"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupGroupNamesTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&Channel{}, &User{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestGetAllGroupNamesMergesAllSources(t *testing.T) {
	db := setupGroupNamesTestDB(t)
	withTestDB(t, db)

	// 分组价格表只覆盖 price-only / default。
	oldGroupPriceMap := groupPriceMap
	SetGroupPriceMapForTest(map[string]map[string]float64{
		"price-only": {"gpt-4o": 0.8},
		"default":    {"gpt-4o": 1},
		"vip":        {"gpt-4o": 0.7},
	})
	t.Cleanup(func() { SetGroupPriceMapForTest(oldGroupPriceMap) })

	// 渠道里实际使用、但没配价格的分组。
	channels := []Channel{
		{Id: 1, Status: ChannelStatusEnabled, Group: "default,vip", Models: "gpt-4o"},
		{Id: 2, Status: ChannelStatusEnabled, Group: "channel-only", Models: "gpt-4o"},
	}
	for _, ch := range channels {
		if err := db.Create(&ch).Error; err != nil {
			t.Fatalf("seed channel: %v", err)
		}
	}

	// 用户分组：staff / user-only 都应被收集，已删除用户的分组必须排除。
	users := []User{
		{Id: 1, Username: "u1", Password: "12345678", AccessToken: "token-u1", AffCode: "aff-u1", Group: "vip,staff", Status: UserStatusEnabled},
		{Id: 2, Username: "u2", Password: "12345678", AccessToken: "token-u2", AffCode: "aff-u2", Group: "user-only", Status: UserStatusEnabled},
		{Id: 3, Username: "u3", Password: "12345678", AccessToken: "token-u3", AffCode: "aff-u3", Group: "ghost", Status: UserStatusDeleted},
	}
	for _, u := range users {
		if err := db.Create(&u).Error; err != nil {
			t.Fatalf("seed user: %v", err)
		}
	}

	want := []string{"channel-only", "default", "price-only", "staff", "user-only", "vip"}
	if got := GetAllGroupNames(); !reflect.DeepEqual(got, want) {
		t.Fatalf("GetAllGroupNames() = %v, want %v", got, want)
	}
}

func TestGetAllGroupNamesEmptyWhenNoSource(t *testing.T) {
	db := setupGroupNamesTestDB(t)
	withTestDB(t, db)

	oldGroupPriceMap := groupPriceMap
	SetGroupPriceMapForTest(map[string]map[string]float64{})
	t.Cleanup(func() { SetGroupPriceMapForTest(oldGroupPriceMap) })

	if got := GetAllGroupNames(); len(got) != 0 {
		t.Fatalf("GetAllGroupNames() = %v, want empty", got)
	}
}

// TestChannelUpdatePersistsGroupAndRebuildsAbilities 覆盖用户报的症状：
// 编辑渠道把分组从 default 改成 vip 后，channels.group 必须落库，
// 且 abilities 要按新分组重建（否则用户侧依然看不到模型）。
func TestChannelUpdatePersistsGroupAndRebuildsAbilities(t *testing.T) {
	db := setupGroupModelsTestDB(t)
	withTestDB(t, db)

	ch := Channel{Id: 1, Type: 1, Status: ChannelStatusEnabled, Group: "default", Models: "gpt-4o"}
	if err := db.Create(&ch).Error; err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	if err := ch.UpdateAbilities(); err != nil {
		t.Fatalf("seed abilities: %v", err)
	}

	ch.Group = "vip"
	if err := ch.Update(map[string]bool{"group": true}); err != nil {
		t.Fatalf("update channel: %v", err)
	}

	var stored Channel
	if err := db.First(&stored, "id = ?", 1).Error; err != nil {
		t.Fatalf("reload channel: %v", err)
	}
	if stored.Group != "vip" {
		t.Fatalf("stored group = %q, want vip", stored.Group)
	}

	var abilities []Ability
	if err := db.Where("channel_id = ?", 1).Find(&abilities).Error; err != nil {
		t.Fatalf("load abilities: %v", err)
	}
	if len(abilities) != 1 || abilities[0].Group != "vip" || abilities[0].Model != "gpt-4o" {
		t.Fatalf("abilities = %+v, want one (vip, gpt-4o) record", abilities)
	}
}
