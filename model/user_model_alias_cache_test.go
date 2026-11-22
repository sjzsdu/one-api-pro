// user_model_alias_cache_test.go 别名缓存失效及并发回填回归测试
// Verifies empty Redis caches and generation isolation during concurrent saves.
// 版本: v0.0.28
// 日期: 2026-11-21
// 作者: Codex
package model

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/modelbus/one-api-pro/common"
	"gorm.io/gorm"
)

type aliasTestRedis struct {
	redis.Cmdable
	sync.Mutex
	values    map[string]string
	beforeSet func(string)
}

func (r *aliasTestRedis) Get(ctx context.Context, key string) *redis.StringCmd {
	r.Lock()
	defer r.Unlock()
	value, ok := r.values[key]
	if !ok {
		return redis.NewStringResult("", redis.Nil)
	}
	return redis.NewStringResult(value, nil)
}

func (r *aliasTestRedis) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) *redis.StatusCmd {
	if r.beforeSet != nil {
		r.beforeSet(key)
	}
	r.Lock()
	defer r.Unlock()
	r.values[key] = fmt.Sprint(value)
	return redis.NewStatusResult("OK", nil)
}

func (r *aliasTestRedis) Incr(ctx context.Context, key string) *redis.IntCmd {
	r.Lock()
	defer r.Unlock()
	var generation int64
	fmt.Sscan(r.values[key], &generation)
	generation++
	r.values[key] = fmt.Sprint(generation)
	return redis.NewIntResult(generation, nil)
}

// TestUserModelAliases_RedisGeneration rejects stale refills without dropping negative-cache entries.
// 版本: v0.0.28
// 日期: 2026-11-21
func TestUserModelAliases_RedisGeneration(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&User{}, &UserModelAlias{}, &Ability{}, &Channel{}); err != nil {
		t.Fatal(err)
	}
	withTestDB(t, db)
	db.Create(&User{Id: 451, Username: "alias-generation", Group: "default", AccessToken: "generation", AffCode: "generation"})
	db.Create(&Channel{Status: 1, Group: "default", Models: "deepseek,glm"})
	r := &aliasTestRedis{values: make(map[string]string)}
	previousRedis, previousEnabled := common.RDB, common.RedisEnabled
	common.RDB, common.RedisEnabled = r, true
	t.Cleanup(func() { common.RDB, common.RedisEnabled = previousRedis, previousEnabled })
	var queries atomic.Int32
	db.Callback().Query().After("gorm:query").Register("alias-generation-query", func(tx *gorm.DB) {
		if tx.Statement.Table == "user_model_aliases" {
			queries.Add(1)
		}
	})
	for i := 0; i < 3; i++ {
		if _, err = GetUserModelAliases(452); err != nil {
			t.Fatal(err)
		}
	}
	if queries.Load() != 1 {
		t.Fatalf("empty Redis mapping loaded %d times", queries.Load())
	}
	db.Create(&UserModelAlias{UserId: 451, Alias: "chat-tool", TargetModel: "deepseek"})
	loaded, release := make(chan struct{}), make(chan struct{})
	r.beforeSet = func(key string) {
		if key == userAliasRedisPrefix(451)+":data:0" {
			close(loaded)
			<-release
		}
	}
	result := make(chan string, 1)
	go func() {
		got, e := ResolveUserModelAlias(451, "chat-tool")
		if e != nil {
			result <- e.Error()
		} else {
			result <- got
		}
	}()
	select {
	case <-loaded:
	case <-time.After(time.Second):
		t.Fatal("read did not reach cache fill")
	}
	// This save represents a different process: the Redis path must not rely on
	// the local per-user lock to stop the already-loaded old value being published.
	if err = SaveUserModelAliases(context.Background(), 451, []UserModelAlias{{Alias: "chat-tool", TargetModel: "glm"}}); err != nil {
		t.Fatal(err)
	}
	if got, e := ResolveUserModelAlias(451, "CHAT-TOOL"); e != nil || got != "glm" {
		t.Fatalf("new request saw stale target: %s %v", got, e)
	}
	close(release)
	if got := <-result; got != "deepseek" {
		t.Fatalf("in-flight read changed: %s", got)
	}
	before := queries.Load()
	if got, e := ResolveUserModelAlias(451, "chat-tool"); e != nil || got != "glm" {
		t.Fatalf("old fill polluted new cache: %s %v", got, e)
	}
	if queries.Load() != before {
		t.Fatal("warm Redis mapping queried database")
	}
	if err = SaveUserModelAliases(context.Background(), 451, nil); err != nil {
		t.Fatal(err)
	}
	if got, e := ResolveUserModelAlias(451, "chat-tool"); e != nil || got != "chat-tool" {
		t.Fatalf("deleted alias remained cached: %s %v", got, e)
	}
}
