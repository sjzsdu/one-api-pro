// model_capability_cache_test.go 能力缓存隔离、失效及并发回归
// Verifies database-scoped cache invalidation without shared mutable capability maps.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package model

import (
	"errors"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func capabilityCacheDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	InitModelCapabilityCache(db)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { sqlDB.Close() })
	if err = db.AutoMigrate(&ModelCatalog{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestGetModelCapabilities_CacheInvalidationAndDBIsolation(t *testing.T) {
	old := DB
	t.Cleanup(func() { DB = old })
	DB = capabilityCacheDB(t)
	var reads atomic.Int64
	DB.Callback().Query().Before("gorm:query").Register("test:count_capability_reads", func(tx *gorm.DB) {
		if tx.Statement.Table == "model_catalogs" {
			reads.Add(1)
		}
	})
	for i := 0; i < 2; i++ {
		if _, err := GetModelCapabilities("ali", "qwen-plus"); err != nil {
			t.Fatal(err)
		}
	}
	if reads.Load() != 1 {
		t.Fatalf("hot-path catalog reads=%d want 1", reads.Load())
	}
	DB.Create(&ModelCatalog{Provider: "ali", ModelName: "qwen-plus", CapabilityOverride: `{"primary_category":"embedding","operations":{"embedding.create":{"modes":["sync"]}}}`})
	got, err := GetModelCapabilities("ali", "qwen-plus")
	if err != nil || got.PrimaryCategory != "embedding" {
		t.Fatalf("stale override: %+v %v", got, err)
	}
	delete(got.Operations, "embedding.create")
	if fresh, _ := GetModelCapabilities("ali", "qwen-plus"); len(fresh.Operations) != 1 {
		t.Fatal("caller mutated cached declaration")
	}
	DB = capabilityCacheDB(t)
	if fresh, _ := GetModelCapabilities("ali", "qwen-plus"); fresh.PrimaryCategory != "text" {
		t.Fatalf("cache leaked across databases: %+v", fresh)
	}
}

func TestGetModelCapabilities_ConcurrentRefresh(t *testing.T) {
	old := DB
	t.Cleanup(func() { DB = old })
	DB = capabilityCacheDB(t)
	DB.Create(&ModelCatalog{Provider: "ali", ModelName: "qwen-plus"})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 15; j++ {
				if _, err := GetModelCapabilities("ali", "qwen-plus"); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	for j := 0; j < 10; j++ {
		if err := DB.Model(&ModelCatalog{}).Where("provider = ?", "ali").Update("description", "refreshed").Error; err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
}

func TestGetModelCapabilities_TTLAndCapacityBound(t *testing.T) {
	old := DB
	t.Cleanup(func() { DB = old })
	DB = capabilityCacheDB(t)
	if _, err := GetModelCapabilities("ali", "qwen-plus"); err != nil {
		t.Fatal(err)
	}
	// Raw SQL represents a change from another process: local callbacks cannot
	// observe it, so expiration must force a fresh catalog read.
	if err := DB.Exec(`INSERT INTO model_catalogs (provider,model_name,capability_override) VALUES (?,?,?)`, "ali", "qwen-plus", `{"primary_category":"embedding","operations":{"embedding.create":{"modes":["sync"]}}}`).Error; err != nil {
		t.Fatal(err)
	}
	cache := modelCapabilityCache(DB)
	cache.mu.Lock()
	entry := cache.entries["ali\x00qwen-plus"]
	entry.expires = time.Now().Add(-time.Second)
	cache.entries["ali\x00qwen-plus"] = entry
	cache.mu.Unlock()
	got, err := GetModelCapabilities("ali", "qwen-plus")
	if err != nil || got.PrimaryCategory != "embedding" {
		t.Fatalf("TTL did not recover external edit: %+v %v", got, err)
	}
	cache.mu.Lock()
	for i := 0; i < 4096; i++ {
		cache.entries[string(rune(i))] = capabilityCacheEntry{}
	}
	cache.mu.Unlock()
	if _, err = GetModelCapabilities("ali", "qwen-max"); err != nil {
		t.Fatal(err)
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if len(cache.entries) > 4096 {
		t.Fatalf("unbounded cache %d", len(cache.entries))
	}
}

func TestGetModelCapabilities_DatabaseFailureIsNotCached(t *testing.T) {
	old := DB
	t.Cleanup(func() { DB = old })
	DB = capabilityCacheDB(t)
	DB.Callback().Query().Before("gorm:query").Register("test:capability_failure", func(tx *gorm.DB) { tx.AddError(errors.New("temporary database failure")) })
	if _, err := GetModelCapabilities("ali", "qwen-plus"); err == nil {
		t.Fatal("database failure became unsupported metadata")
	}
	DB.Callback().Query().Remove("test:capability_failure")
	if got, err := GetModelCapabilities("ali", "qwen-plus"); err != nil || got.PrimaryCategory != "text" {
		t.Fatalf("database failure poisoned cache: %+v %v", got, err)
	}
}

func TestGetModelCapabilities_SlowMissDoesNotBlockWarmHit(t *testing.T) {
	old := DB
	t.Cleanup(func() { DB = old })
	DB = capabilityCacheDB(t)
	GetModelCapabilities("ali", "qwen-plus")
	blocked, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer func() { releaseOnce.Do(func() { close(release) }); <-done }()
	DB.Callback().Query().Before("gorm:query").Register("test:slow_capability_query", func(tx *gorm.DB) { close(blocked); <-release })
	go func() { defer close(done); GetModelCapabilities("ali", "qwen-max") }()
	<-blocked
	warm := make(chan error, 1)
	go func() { _, err := GetModelCapabilities("ali", "qwen-plus"); warm <- err }()
	select {
	case err := <-warm:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("slow miss blocked warm capability hit")
	}
	releaseOnce.Do(func() { close(release) })
}

func TestGetModelCapabilities_ChannelStatisticsDoNotInvalidate(t *testing.T) {
	old := DB
	t.Cleanup(func() { DB = old })
	DB = capabilityCacheDB(t)
	if err := DB.AutoMigrate(&Channel{}); err != nil {
		t.Fatal(err)
	}
	DB.Create(&Channel{Id: 1, Type: 17, Models: "qwen-plus", Status: 1})
	var reads atomic.Int64
	DB.Callback().Query().Before("gorm:query").Register("test:count_stat_invalidation", func(tx *gorm.DB) {
		if tx.Statement.Table == "model_catalogs" {
			reads.Add(1)
		}
	})
	GetModelCapabilities("ali", "qwen-plus")
	DB.Model(&Channel{}).Where("id = ?", 1).Updates(map[string]any{"used_quota": 300, "test_time": 10, "response_time": 5, "balance": 20, "last_error": "transient"})
	GetModelCapabilities("ali", "qwen-plus")
	if reads.Load() != 1 {
		t.Fatalf("settlement/health statistics invalidated capability cache: %d reads", reads.Load())
	}
	DB.Model(&Channel{}).Where("id = ?", 1).Update("models", "qwen-plus,qwen-max")
	GetModelCapabilities("ali", "qwen-plus")
	if reads.Load() != 2 {
		t.Fatalf("configuration update did not invalidate: %d reads", reads.Load())
	}
}
