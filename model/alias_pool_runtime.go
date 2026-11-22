// alias_pool_runtime.go 模型池会话固定、权重分配与故障冷却
// Coordinates bounded local or shared Redis session bindings and cooldowns.
// 版本: v0.0.28
// 日期: 2026-11-22
// 作者: Codex
package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/go-redis/redis/v8"
	"github.com/modelbus/one-api-pro/common"
	"github.com/modelbus/one-api-pro/common/config"
	"gorm.io/gorm/clause"
	"math/rand"
	"sync"
	"time"
)

// AliasSessionBinding records the selected concrete model and last successful channel.
// 版本: v0.0.28
// 日期: 2026-11-22
type AliasSessionBinding struct {
	Model     string `json:"model"`
	ChannelID int    `json:"channel_id"`
}
type aliasRuntimeEntry struct {
	Binding AliasSessionBinding
	Expires time.Time
}

var aliasRuntime = struct {
	sync.Mutex
	Sessions  map[string]aliasRuntimeEntry
	Cooldowns map[string]time.Time
}{Sessions: map[string]aliasRuntimeEntry{}, Cooldowns: map[string]time.Time{}}

// AliasPoolKey isolates personal/global aliases, accounts, and client-supplied sessions.
// 版本: v0.0.28
// 日期: 2026-11-22
func AliasPoolKey(userID int, alias UserModelAlias, sessionID string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%s:%s", userID, alias.UserId, alias.Alias, sessionID)))
	return "alias_session:" + hex.EncodeToString(sum[:])
}

// WeightedAliasTargets draws a weighted order without replacement.
// 版本: v0.0.28
// 日期: 2026-11-22
func WeightedAliasTargets(targets []UserModelAliasTarget) []UserModelAliasTarget {
	remaining := append([]UserModelAliasTarget{}, targets...)
	result := make([]UserModelAliasTarget, 0, len(targets))
	for len(remaining) > 0 {
		total := 0
		for _, t := range remaining {
			total += t.Weight
		}
		if total <= 0 {
			break
		}
		draw := rand.Intn(total)
		index := 0
		for i, t := range remaining {
			draw -= t.Weight
			if draw < 0 {
				index = i
				break
			}
		}
		result = append(result, remaining[index])
		remaining = append(remaining[:index], remaining[index+1:]...)
	}
	return result
}

// ReadAliasSession retrieves a non-expired binding without changing its target.
// 版本: v0.0.28
// 日期: 2026-11-22
func ReadAliasSession(key string) (AliasSessionBinding, bool, error) {
	if key == "" {
		return AliasSessionBinding{}, false, nil
	}
	if common.RedisEnabled && common.RDB != nil {
		raw, err := common.RDB.Get(context.Background(), key).Result()
		if err == redis.Nil {
			return AliasSessionBinding{}, false, nil
		}
		if err != nil {
			return AliasSessionBinding{}, false, err
		}
		var binding AliasSessionBinding
		err = json.Unmarshal([]byte(raw), &binding)
		return binding, err == nil, err
	}
	aliasRuntime.Lock()
	defer aliasRuntime.Unlock()
	entry, ok := aliasRuntime.Sessions[key]
	if ok && time.Now().Before(entry.Expires) {
		return entry.Binding, true, nil
	}
	delete(aliasRuntime.Sessions, key)
	return AliasSessionBinding{}, false, nil
}

// ClaimAliasSession atomically makes concurrent first requests agree on one model.
// 版本: v0.0.28
// 日期: 2026-11-22
func ClaimAliasSession(key string, proposed AliasSessionBinding, ttl time.Duration) (AliasSessionBinding, error) {
	if key == "" {
		return proposed, nil
	}
	if common.RedisEnabled && common.RDB != nil {
		raw, _ := json.Marshal(proposed)
		ok, err := common.RDB.SetNX(context.Background(), key, string(raw), ttl).Result()
		if err != nil {
			return proposed, err
		}
		if ok {
			return proposed, nil
		}
		existing, found, err := ReadAliasSession(key)
		if err != nil {
			return proposed, err
		}
		if found {
			return existing, nil
		}
		return ClaimAliasSession(key, proposed, ttl)
	}
	aliasRuntime.Lock()
	defer aliasRuntime.Unlock()
	if old, ok := aliasRuntime.Sessions[key]; ok && time.Now().Before(old.Expires) {
		return old.Binding, nil
	}
	if len(aliasRuntime.Sessions) >= 8192 {
		for k, v := range aliasRuntime.Sessions {
			if time.Now().After(v.Expires) {
				delete(aliasRuntime.Sessions, k)
			}
		}
		if len(aliasRuntime.Sessions) >= 8192 {
			for k := range aliasRuntime.Sessions {
				delete(aliasRuntime.Sessions, k)
				break
			}
		}
	}
	aliasRuntime.Sessions[key] = aliasRuntimeEntry{proposed, time.Now().Add(ttl)}
	return proposed, nil
}

// UpdateAliasSession changes a binding only while its model still matches the caller's attempt.
// 版本: v0.0.28
// 日期: 2026-11-22
func UpdateAliasSession(key, expectedModel string, next AliasSessionBinding, ttl time.Duration) error {
	if key == "" {
		return nil
	}
	if common.RedisEnabled && common.RDB != nil {
		raw, _ := json.Marshal(next)
		return common.RDB.Eval(context.Background(), `local old=redis.call('GET',KEYS[1]); if not old then return 0 end; local obj=cjson.decode(old); if obj.model~=ARGV[1] then return 0 end; redis.call('SET',KEYS[1],ARGV[2],'PX',ARGV[3]); return 1`, []string{key}, expectedModel, string(raw), ttl.Milliseconds()).Err()
	}
	aliasRuntime.Lock()
	defer aliasRuntime.Unlock()
	old, ok := aliasRuntime.Sessions[key]
	if ok && old.Binding.Model == expectedModel && time.Now().Before(old.Expires) {
		aliasRuntime.Sessions[key] = aliasRuntimeEntry{next, time.Now().Add(ttl)}
	}
	return nil
}

// AliasPoolCooling reports whether a group's pool candidate is temporarily unhealthy.
// 版本: v0.0.28
// 日期: 2026-11-22
func AliasPoolCooling(key string) bool {
	if common.RedisEnabled && common.RDB != nil {
		n, e := common.RDB.Exists(context.Background(), "alias_cooldown:"+key).Result()
		return e == nil && n > 0
	}
	aliasRuntime.Lock()
	defer aliasRuntime.Unlock()
	until := aliasRuntime.Cooldowns[key]
	if time.Now().Before(until) {
		return true
	}
	delete(aliasRuntime.Cooldowns, key)
	return false
}

// CoolAliasPoolCandidate pauses only a fully exhausted group's candidate for thirty seconds.
// 版本: v0.0.28
// 日期: 2026-11-22
func CoolAliasPoolCandidate(key string) {
	if common.RedisEnabled && common.RDB != nil {
		_ = common.RDB.Set(context.Background(), "alias_cooldown:"+key, "1", 30*time.Second).Err()
		return
	}
	aliasRuntime.Lock()
	defer aliasRuntime.Unlock()
	if len(aliasRuntime.Cooldowns) >= 8192 {
		for k := range aliasRuntime.Cooldowns {
			delete(aliasRuntime.Cooldowns, k)
			break
		}
	}
	aliasRuntime.Cooldowns[key] = time.Now().Add(30 * time.Second)
}

// GetAliasPoolChannels returns exact group/model matches, avoiding substring or wildcard routing surprises.
// 版本: v0.0.28
// 日期: 2026-11-22
func GetAliasPoolChannels(group, name string) []*Channel {
	var channels []*Channel
	if config.MemoryCacheEnabled {
		channels = GetChannelCandidates(group, name)
	} else {
		if DB.Where(clause.Eq{Column: "status", Value: ChannelStatusEnabled}).Where(clause.Or(clause.Like{Column: "models", Value: "%" + name + "%"}, clause.Eq{Column: "models", Value: "*"})).Where(clause.Like{Column: "group", Value: "%" + group + "%"}).Find(&channels).Error != nil {
			return nil
		}
	}
	exact := []*Channel{}
	for _, ch := range channels {
		if ch.Status == ChannelStatusEnabled && !ch.GetIsFallback() && ch.ContainsGroup(group) && ch.ContainsModel(name) {
			exact = append(exact, ch)
		}
	}
	return exact
}
