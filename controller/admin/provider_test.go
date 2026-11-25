// provider_test.go 供应商统计聚合单测
// Unit tests for provider-level statistics aggregation.
// 版本: v0.0.28
// 日期: 2026-11-25
// 作者: MCode
package admin

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	// 引入 relay 以触发所有 provider 包的 init() 注册，registry 才有内容。
	// Importing relay triggers every provider package's init() registration.
	"github.com/modelbus/one-api-pro/model"
	_ "github.com/modelbus/one-api-pro/relay"
	"github.com/modelbus/one-api-pro/relay/registry"
)

// setupProviderAdminTestDB 建立内存库并迁移供应商相关表。
// Builds an in-memory database with the provider-related tables migrated.
func setupProviderAdminTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Log{}, &model.Channel{}, &model.ProviderProfile{},
		&model.ModelFreeModel{}, &model.ModelPrice{}); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}
	oldDB, oldLogDB := model.DB, model.LOG_DB
	model.DB, model.LOG_DB = db, db
	t.Cleanup(func() { model.DB, model.LOG_DB = oldDB, oldLogDB })
	return db
}

// TestRegistryKnowsProviderChannelTypes 确认 registry 已注册常用渠道类型，
// 统计聚合依赖这层映射。
// Confirms the registry knows the common channel types the aggregation relies on.
func TestRegistryKnowsProviderChannelTypes(t *testing.T) {
	cases := map[int]string{
		49: "alibailian",
		16: "zhipu",
		17: "ali",
		40: "doubao",
		36: "deepseek",
		20: "openrouter",
	}
	for channelType, want := range cases {
		if got := registry.IDByLegacyType(channelType); got != want {
			t.Fatalf("IDByLegacyType(%d) = %q, want %q", channelType, got, want)
		}
	}
}

// TestBuildProviderUsageGroupsByProvider 锁定统计口径：
// 同一 provider 下的多个渠道要合并，同名模型挂在不同 provider 下要分开。
// Locks the aggregation contract: channels of one provider are merged, while the
// same model name under different providers stays separate.
func TestBuildProviderUsageGroupsByProvider(t *testing.T) {
	db := setupProviderAdminTestDB(t)
	now := int64(1795600000)

	// 两条 alibailian 渠道（49），一条 zhipu 渠道（16）。
	db.Create(&model.Channel{Id: 4, Name: "bailian-a", Type: 49, Status: model.ChannelStatusEnabled,
		Models: "qwen-plus,deepseek-v4-pro", Group: "default"})
	db.Create(&model.Channel{Id: 5, Name: "bailian-b", Type: 49, Status: model.ChannelStatusEnabled,
		Models: "qwen-plus", Group: "default"})
	db.Create(&model.Channel{Id: 7, Name: "zhipu", Type: 16, Status: model.ChannelStatusEnabled,
		Models: "glm-5.2", Group: "default"})

	db.Create(&model.Log{UserId: 1, Type: model.LogTypeConsume, ModelName: "qwen-plus", ChannelId: 4,
		Quota: 100, PromptTokens: 1000, CompletionTokens: 200, CreatedAt: now})
	db.Create(&model.Log{UserId: 1, Type: model.LogTypeConsume, ModelName: "qwen-plus", ChannelId: 5,
		Quota: 50, PromptTokens: 500, CompletionTokens: 100, CreatedAt: now})
	db.Create(&model.Log{UserId: 1, Type: model.LogTypeConsume, ModelName: "deepseek-v4-pro", ChannelId: 4,
		Quota: 30, PromptTokens: 300, CompletionTokens: 60, CreatedAt: now})
	db.Create(&model.Log{UserId: 1, Type: model.LogTypeConsume, ModelName: "glm-5.2", ChannelId: 7,
		Quota: 20, PromptTokens: 200, CompletionTokens: 40, CreatedAt: now})

	// 只有 qwen-plus 有定价，deepseek-v4-pro 与 glm-5.2 走兜底价。
	db.Create(&model.ModelPrice{ModelName: "qwen-plus", InputPrice: 0.8, OutputPrice: 0.8, BillingType: "token", Enabled: true})

	freeModels := map[string][]*model.ModelFreeModel{
		"alibailian": {{ProviderID: "alibailian", ModelName: "deepseek-v4-pro", FreeType: model.FreeTypeQuota, Enabled: true}},
	}
	prices := map[string]*model.ModelPrice{
		"qwen-plus": {ModelName: "qwen-plus", InputPrice: 0.8, OutputPrice: 0.8},
	}

	rows, err := model.SumUsageByChannelIdAndModel(0, now-3600, now+3600)
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	channels, err := model.GetAllChannels(0, 0, "all")
	if err != nil {
		t.Fatalf("channels: %v", err)
	}
	index := buildProviderIndex(channels)
	usage := buildProviderUsage(rows, index, freeModels, prices)
	if len(usage) != 2 {
		t.Fatalf("providers = %d, want 2 (got %+v)", len(usage), usage)
	}

	byID := map[string]*providerUsageDTO{}
	for _, item := range usage {
		byID[item.ProviderID] = item
	}
	bailian := byID["alibailian"]
	if bailian == nil {
		t.Fatal("alibailian provider missing")
	}
	if bailian.Quota != 180 {
		t.Fatalf("alibailian quota = %d, want 180", bailian.Quota)
	}
	if bailian.RequestCount != 3 {
		t.Fatalf("alibailian request count = %d, want 3", bailian.RequestCount)
	}
	if bailian.ChannelCount != 2 || bailian.EnabledChannels != 2 {
		t.Fatalf("alibailian channels = %d/%d, want 2/2", bailian.ChannelCount, bailian.EnabledChannels)
	}
	if bailian.TotalTokens != 2160 {
		t.Fatalf("alibailian total tokens = %d, want 2160", bailian.TotalTokens)
	}
	if bailian.ModelCount != 2 {
		t.Fatalf("alibailian model count = %d, want 2", bailian.ModelCount)
	}
	// deepseek-v4-pro 没有定价条目，应被标记为未定价。
	if len(bailian.UnpricedModels) != 1 || bailian.UnpricedModels[0] != "deepseek-v4-pro" {
		t.Fatalf("alibailian unpriced = %v, want [deepseek-v4-pro]", bailian.UnpricedModels)
	}
	if len(bailian.FreeModels) != 1 || bailian.FreeModels[0] != "deepseek-v4-pro" {
		t.Fatalf("alibailian free = %v, want [deepseek-v4-pro]", bailian.FreeModels)
	}

	zhipu := byID["zhipu"]
	if zhipu == nil {
		t.Fatal("zhipu provider missing")
	}
	if zhipu.Quota != 20 || zhipu.ChannelCount != 1 {
		t.Fatalf("zhipu usage = %+v, want quota=20 channels=1", zhipu)
	}
	// 占比：180 / 200 = 0.9
	if bailian.Share < 0.89 || bailian.Share > 0.91 {
		t.Fatalf("alibailian share = %v, want ~0.9", bailian.Share)
	}
}

// TestProviderDisplayNamePrefersCuratedTitle 验证策展标题优先于 registry 名称。
// Verifies the curated title takes precedence over the registry name.
func TestProviderDisplayNamePrefersCuratedTitle(t *testing.T) {
	if got := providerDisplayName("alibailian", nil); got != "Ali Bailian" {
		t.Fatalf("display name = %q, want registry name", got)
	}
	profile := &model.ProviderProfile{ProviderID: "alibailian", Title: "阿里云百炼"}
	if got := providerDisplayName("alibailian", profile); got != "阿里云百炼" {
		t.Fatalf("display name = %q, want curated title", got)
	}
}
