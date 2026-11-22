// user_model_alias_cache.go 用户模型别名的共享及有界本地缓存
// Caches empty mappings and prevents stale refills after account settings change.
// 版本: v0.0.28
// 日期: 2026-11-21
// 作者: Codex
package model

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/modelbus/one-api-pro/common"
	"gorm.io/gorm"
)

const userModelAliasCacheTTL = time.Minute
const userModelAliasCacheLimit = 4096

var userModelAliasLocks [64]sync.Mutex
var userModelAliasCache = struct {
	sync.Mutex
	database *gorm.DB
	entries  map[int]userModelAliasCacheEntry
}{entries: make(map[int]userModelAliasCacheEntry)}

type userModelAliasCacheEntry struct {
	items   []UserModelAlias
	expires time.Time
}

func userAliasLock(userId int) *sync.Mutex {
	return &userModelAliasLocks[uint(userId)%uint(len(userModelAliasLocks))]
}

func readUserModelAliases(userId int) ([]UserModelAlias, error) {
	items := []UserModelAlias{}
	if err := DB.Where("user_id = ?", userId).Order("alias ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for i := range items {
		items[i].Alias = strings.ToLower(items[i].Alias)
		// Older SQLite/PostgreSQL settings may contain names differing only by case.
		// Refuse ambiguous configuration instead of choosing a database-dependent winner.
		if seen[items[i].Alias] {
			return nil, fmt.Errorf("模型别名存在大小写冲突，请重新保存配置：%s", items[i].Alias)
		}
		seen[items[i].Alias] = true
	}
	return items, nil
}

func cloneUserModelAliases(items []UserModelAlias) []UserModelAlias {
	return append([]UserModelAlias{}, items...)
}

func userAliasRedisPrefix(userId int) string {
	// The hash tag keeps generation and data keys on the same Redis Cluster slot.
	return fmt.Sprintf("{user_model_aliases:%d}", userId)
}

func cachedUserModelAliases(userId int) ([]UserModelAlias, error) {
	if common.RedisEnabled && common.RDB != nil {
		return redisUserModelAliases(userId)
	}
	lock := userAliasLock(userId)
	lock.Lock()
	defer lock.Unlock()
	userModelAliasCache.Lock()
	if userModelAliasCache.database != DB {
		userModelAliasCache.database = DB
		userModelAliasCache.entries = make(map[int]userModelAliasCacheEntry)
	}
	entry, ok := userModelAliasCache.entries[userId]
	userModelAliasCache.Unlock()
	if ok && time.Now().Before(entry.expires) {
		return cloneUserModelAliases(entry.items), nil
	}
	items, err := readUserModelAliases(userId)
	if err != nil {
		return nil, err
	}
	userModelAliasCache.Lock()
	defer userModelAliasCache.Unlock()
	if len(userModelAliasCache.entries) >= userModelAliasCacheLimit {
		var oldestId int
		oldest := time.Now().Add(userModelAliasCacheTTL)
		for id, entry := range userModelAliasCache.entries {
			if entry.expires.Before(oldest) {
				oldestId, oldest = id, entry.expires
			}
		}
		delete(userModelAliasCache.entries, oldestId)
	}
	userModelAliasCache.entries[userId] = userModelAliasCacheEntry{items: cloneUserModelAliases(items), expires: time.Now().Add(userModelAliasCacheTTL)}
	return items, nil
}

func redisUserModelAliases(userId int) ([]UserModelAlias, error) {
	ctx := context.Background()
	prefix := userAliasRedisPrefix(userId)
	revision, err := common.RDB.Get(ctx, prefix+":generation").Result()
	if err == redis.Nil {
		revision = "0"
	} else if err != nil {
		return readUserModelAliases(userId)
	}
	if _, err = strconv.ParseUint(revision, 10, 64); err != nil {
		return readUserModelAliases(userId)
	}
	key := prefix + ":data:" + revision
	data, err := common.RDB.Get(ctx, key).Result()
	if err == nil {
		var items []UserModelAlias
		if json.Unmarshal([]byte(data), &items) == nil && items != nil {
			return items, nil
		}
	}
	items, err := readUserModelAliases(userId)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(items)
	if err == nil {
		// An older read can only fill its original generation. A successful save
		// increments the generation, so this value cannot overwrite the new mapping.
		_ = common.RDB.Set(ctx, key, string(encoded), userModelAliasCacheTTL).Err()
	}
	return items, nil
}

func invalidateUserModelAliases(userId int) error {
	userModelAliasCache.Lock()
	delete(userModelAliasCache.entries, userId)
	userModelAliasCache.Unlock()
	if common.RedisEnabled && common.RDB != nil {
		// Generation keys intentionally do not expire: resetting one to zero could
		// revive a delayed pre-save fill. Data keys (including empty maps) expire.
		if err := common.RDB.Incr(context.Background(), userAliasRedisPrefix(userId)+":generation").Err(); err != nil {
			return fmt.Errorf("模型别名已保存，但缓存刷新失败，请重试：%w", err)
		}
	}
	return nil
}
