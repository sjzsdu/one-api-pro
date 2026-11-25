// cache_dialect_test.go 候选查询列名方言引用回归测试
// Regression test: channel candidate SQL must quote the reserved "group" column per dialect.
// 版本: v0.0.28
// 日期: 2026-11-25
// 作者: opencode
package model

import (
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/modelbus/one-api-pro/common"
	"github.com/modelbus/one-api-pro/common/config"
)

// TestChannelCandidateQueryQuotesGroupPerDialect 覆盖线上 503 事故：
// PostgreSQL 部署（MEMORY_CACHE_ENABLED=false）下候选查询硬编码 MySQL 反引号，
// 整条 SQL 语法错误 → 候选恒为空 → 所有真实模型名 503「无可用渠道」。
// 这里用 DryRun 打印真实方言的 SQL，断言 PG 用 "group"、MySQL 用 `group`。
func TestChannelCandidateQueryQuotesGroupPerDialect(t *testing.T) {
	oldPG := common.UsingPostgreSQL
	t.Cleanup(func() { common.UsingPostgreSQL = oldPG })

	pg, err := gorm.Open(postgres.New(postgres.Config{
		DSN: "postgres://dummy:dummy@127.0.0.1:5432/dummy",
	}), &gorm.Config{DisableAutomaticPing: true, DryRun: true})
	if err != nil {
		t.Fatalf("open dry-run postgres dialector: %v", err)
	}

	common.UsingPostgreSQL = true
	captured := channelCandidateQuery(pg, "default", "qwen-plus").
		Find(&[]Channel{}).Statement.SQL.String()
	if !strings.Contains(captured, `"group"`) {
		t.Fatalf("postgres SQL should quote group as \"group\", got: %s", captured)
	}
	if strings.Contains(captured, "`group`") {
		t.Fatalf("postgres SQL must not contain MySQL backticks, got: %s", captured)
	}

	common.UsingPostgreSQL = false
	if got := groupColumnRef(); got != "`group`" {
		t.Fatalf("mysql groupColumnRef() = %q, want `group`", got)
	}
}

// TestGetChannelCandidatesReturnsEnabledChannel 锁定候选查询的行为：
// 分组 default 下的启用渠道必须能被查到（SQLite 语义与 PostgreSQL 一致）。
// Locks candidate lookup behaviour: an enabled channel in group default must be found.
func TestGetChannelCandidatesReturnsEnabledChannel(t *testing.T) {
	oldDB, oldMemory := DB, config.MemoryCacheEnabled
	t.Cleanup(func() { DB, config.MemoryCacheEnabled = oldDB, oldMemory })

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { sqlDB.Close() })
	if err := db.AutoMigrate(&Channel{}); err != nil {
		t.Fatal(err)
	}
	DB = db
	config.MemoryCacheEnabled = false

	if err := db.Create(&Channel{
		Id: 9101, Name: "cand", Type: 1, Status: ChannelStatusEnabled,
		Group: "default", Models: "qwen-plus,deepseek-v4-pro",
	}).Error; err != nil {
		t.Fatal(err)
	}

	candidates := GetChannelCandidates("default", "qwen-plus")
	if len(candidates) != 1 {
		t.Fatalf("candidates = %d, want 1", len(candidates))
	}
	if candidates[0].Id != 9101 {
		t.Fatalf("candidate id = %d, want 9101", candidates[0].Id)
	}
	if got := GetChannelCandidates("default", "cosyvoice-v3-flash"); len(got) != 0 {
		t.Fatalf("unlisted model candidates = %d, want 0", len(got))
	}
}

// TestSystemSettingKeyQueryQuotesPerDialect 保证 options/system_settings 的
// key 列（保留字）同样按方言引用。
// Ensures the reserved "key" column is quoted per dialect as well.
func TestSystemSettingKeyQueryQuotesPerDialect(t *testing.T) {
	oldPG := common.UsingPostgreSQL
	t.Cleanup(func() { common.UsingPostgreSQL = oldPG })

	pg, err := gorm.Open(postgres.New(postgres.Config{
		DSN: "postgres://dummy:dummy@127.0.0.1:5432/dummy",
	}), &gorm.Config{DisableAutomaticPing: true, DryRun: true})
	if err != nil {
		t.Fatalf("open dry-run postgres dialector: %v", err)
	}

	common.UsingPostgreSQL = true
	sql := pg.Where(common.QuoteIdentifier("key")+" = ?", "topup.enabled").
		First(&SystemSetting{}).Statement.SQL.String()
	if !strings.Contains(sql, `"key"`) {
		t.Fatalf("postgres SQL should quote key as \"key\", got: %s", sql)
	}
	if strings.Contains(sql, "`key`") {
		t.Fatalf("postgres SQL must not contain MySQL backticks, got: %s", sql)
	}
}
