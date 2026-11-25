// provider_test.go 供应商聚合与免费模型清单单测
// Unit tests for provider aggregation and the free model catalogue.
// 版本: v0.0.28
// 日期: 2026-11-25
// 作者: MCode
package model

import (
	"fmt"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// setupProviderTestDB 建立内存库并迁移供应商相关表。
// Builds an in-memory database with the provider-related tables migrated.
func setupProviderTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	if err := db.AutoMigrate(&Log{}, &Channel{}, &ProviderProfile{}, &ModelFreeModel{}, &ModelPrice{}); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}
	oldDB, oldLogDB := DB, LOG_DB
	DB, LOG_DB = db, db
	t.Cleanup(func() { DB, LOG_DB = oldDB, oldLogDB })
	return db
}

// TestSumUsageByChannelIdAndModel 锁定聚合口径：
// 同一渠道类型下的同名模型要合并，不同渠道类型要分开。
// Locks the aggregation contract: rows of the same model under one channel
// type are merged, different channel types stay separate.
func TestSumUsageByChannelIdAndModel(t *testing.T) {
	db := setupProviderTestDB(t)
	now := int64(1795600000)
	rows := []*Log{
		{UserId: 1, Type: LogTypeConsume, ModelName: "qwen-plus", ChannelId: 4, Quota: 10, PromptTokens: 100, CompletionTokens: 20, CreatedAt: now},
		{UserId: 1, Type: LogTypeConsume, ModelName: "qwen-plus", ChannelId: 4, Quota: 5, PromptTokens: 50, CompletionTokens: 10, CreatedAt: now},
		{UserId: 1, Type: LogTypeConsume, ModelName: "qwen-plus", ChannelId: 6, Quota: 7, PromptTokens: 70, CompletionTokens: 7, CreatedAt: now},
		{UserId: 1, Type: LogTypeConsume, ModelName: "glm-5.2", ChannelId: 7, Quota: 3, PromptTokens: 30, CompletionTokens: 5, CreatedAt: now},
		// 非消费日志必须被排除。
		{UserId: 1, Type: LogTypeSystem, ModelName: "qwen-plus", ChannelId: 4, Quota: 999, CreatedAt: now},
		// 时间窗外的日志必须被排除。
		{UserId: 1, Type: LogTypeConsume, ModelName: "qwen-plus", ChannelId: 4, Quota: 888, CreatedAt: now - 86400*7},
	}
	for _, row := range rows {
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("seed log: %v", err)
		}
	}

	got, err := SumUsageByChannelIdAndModel(0, now-3600, now+3600)
	if err != nil {
		t.Fatalf("SumUsageByChannelIdAndModel: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("rows = %d, want 3 (got %+v)", len(got), got)
	}
	byKey := map[string]*ChannelUsageRow{}
	for _, row := range got {
		byKey[fmt.Sprintf("%d|%s", row.ChannelId, row.ModelName)] = row
	}
	merged := byKey["4|qwen-plus"]
	if merged == nil {
		t.Fatal("channel type 4 qwen-plus row missing")
	}
	if merged.RequestCount != 2 || merged.Quota != 15 || merged.PromptTokens != 150 || merged.CompletionTokens != 30 {
		t.Fatalf("merged row = %+v, want count=2 quota=15 pt=150 ct=30", merged)
	}
	other := byKey["6|qwen-plus"]
	if other == nil || other.Quota != 7 {
		t.Fatalf("channel type 6 row = %+v, want quota=7", other)
	}
}

// TestSumUsageByChannelIdAndModelByUser 验证用户维度过滤只统计该用户。
// Verifies the user filter only counts that user's logs.
func TestSumUsageByChannelIdAndModelByUser(t *testing.T) {
	db := setupProviderTestDB(t)
	now := int64(1795600000)
	db.Create(&Log{UserId: 1, Type: LogTypeConsume, ModelName: "m", ChannelId: 4, Quota: 10, CreatedAt: now})
	db.Create(&Log{UserId: 2, Type: LogTypeConsume, ModelName: "m", ChannelId: 4, Quota: 20, CreatedAt: now})

	got, err := SumUsageByChannelIdAndModel(1, now-3600, now+3600)
	if err != nil {
		t.Fatalf("SumUsageByChannelIdAndModel: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("rows = %d, want 1", len(got))
	}
	if got[0].Quota != 10 {
		t.Fatalf("quota = %d, want 10", got[0].Quota)
	}
}

// TestDetectFreeModelSuffix 锁定免费后缀识别规则。
// Locks the free-suffix detection rules.
func TestDetectFreeModelSuffix(t *testing.T) {
	cases := []struct {
		model  string
		free   bool
		suffix string
	}{
		{"openrouter/free", true, "/free"},
		{"cohere/north-mini-code:free", true, ":free"},
		{"vendor/model-free", true, "-free"},
		{"vendor/model_free", true, "_free"},
		{"qwen-plus", false, ""},
		{"", false, ""},
		{"free-model", false, ""},
	}
	for _, tc := range cases {
		free, suffix := DetectFreeModelSuffix(tc.model)
		if free != tc.free || suffix != tc.suffix {
			t.Fatalf("DetectFreeModelSuffix(%q) = (%v, %q), want (%v, %q)",
				tc.model, free, suffix, tc.free, tc.suffix)
		}
	}
}

// TestUpsertProviderProfile 验证策展信息按 provider_id 幂等写入。
// Verifies profiles are upserted idempotently by provider ID.
func TestUpsertProviderProfile(t *testing.T) {
	setupProviderTestDB(t)
	item := &ProviderProfile{ProviderID: "alibailian", Title: "阿里云百炼", Summary: "通义千问"}
	if err := UpsertProviderProfile(item); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	item.Summary = "通义千问 / DeepSeek"
	if err := UpsertProviderProfile(item); err != nil {
		t.Fatalf("upsert again: %v", err)
	}
	items, err := GetProviderProfiles()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("profiles = %d, want 1", len(items))
	}
	if items[0].Summary != "通义千问 / DeepSeek" {
		t.Fatalf("summary = %q, want updated value", items[0].Summary)
	}
	if err := DeleteProviderProfile("alibailian"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	items, _ = GetProviderProfiles()
	if len(items) != 0 {
		t.Fatalf("profiles after delete = %d, want 0", len(items))
	}
}

// TestUpsertProviderProfileRejectsEmptyID 验证缺少 provider_id 会被拒绝。
// Verifies a profile without a provider ID is rejected.
func TestUpsertProviderProfileRejectsEmptyID(t *testing.T) {
	setupProviderTestDB(t)
	if err := UpsertProviderProfile(&ProviderProfile{}); err == nil {
		t.Fatal("expected error for empty provider_id")
	}
}

// TestUpsertModelFreeModel 验证免费模型按 (provider_id, model_name) 幂等写入。
// Verifies free models are upserted idempotently by (provider, model).
func TestUpsertModelFreeModel(t *testing.T) {
	setupProviderTestDB(t)
	item := &ModelFreeModel{ProviderID: "openrouter", ModelName: "openrouter/free", FreeType: FreeTypeSuffix, Enabled: true}
	if err := UpsertModelFreeModel(item); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	item.Note = "上游 0 元"
	item.ZeroPrice = true
	if err := UpsertModelFreeModel(item); err != nil {
		t.Fatalf("upsert again: %v", err)
	}
	items, err := GetModelFreeModels("openrouter")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("free models = %d, want 1", len(items))
	}
	if items[0].Note != "上游 0 元" || !items[0].ZeroPrice {
		t.Fatalf("free model = %+v, want updated values", items[0])
	}
	if err := DeleteModelFreeModel("openrouter", "openrouter/free"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	items, _ = GetModelFreeModels("")
	if len(items) != 0 {
		t.Fatalf("free models after delete = %d, want 0", len(items))
	}
}
