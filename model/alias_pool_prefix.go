// alias_pool_prefix.go 有界消息前缀到模型池会话的路由索引
// Stores only hashed histories and stable session references with bounded TTL indexes.
// 版本: v0.0.28
// 日期: 2026-11-22
// 作者: Codex
package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/modelbus/one-api-pro/common"
)

const aliasPrefixPerScopeLimit = 1024
const aliasPrefixScopeLimit = 256
const aliasPrefixGlobalLimit = 8192

type aliasPrefixEntry struct {
	SessionKey string
	Expires    time.Time
}

var aliasPrefixes = struct {
	sync.Mutex
	Scopes map[string]map[string]aliasPrefixEntry
	Count  int
}{Scopes: make(map[string]map[string]aliasPrefixEntry)}

// AliasPrefixScope isolates account, alias ownership and wire protocol without exposing their content.
// 版本: v0.0.28
// 日期: 2026-11-22
func AliasPrefixScope(userID int, alias UserModelAlias, protocol string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%s:%s", userID, alias.UserId, alias.Alias, protocol)))
	return hex.EncodeToString(sum[:])
}

// AliasPrefixSessionKey separates inferred bindings from every explicit client session ID.
// 版本: v0.0.28
// 日期: 2026-11-22
func AliasPrefixSessionKey(scope, digest string) string {
	sum := sha256.Sum256([]byte(scope + ":" + digest))
	return "alias_auto_session:" + hex.EncodeToString(sum[:])
}

func aliasPrefixRedisKeys(scope string) []string {
	prefix := "alias_prefix:{" + scope + "}:"
	return []string{prefix + "records", prefix + "expires"}
}

// FindAliasPrefixSession returns the longest matching history whose binding still exists.
// 版本: v0.0.28
// 日期: 2026-11-22
func FindAliasPrefixSession(scope string, hashes []string) (string, error) {
	if len(hashes) == 0 {
		return "", nil
	}
	if len(hashes) > 128 {
		hashes = hashes[:128]
	}
	refs := []string{}
	if common.RedisEnabled && common.RDB != nil {
		args := []any{time.Now().UnixMilli()}
		for _, digest := range hashes {
			args = append(args, digest)
		}
		values, err := common.RDB.Eval(context.Background(), `local result={}; local seen={}; for i=2,#ARGV do local expiry=redis.call('ZSCORE',KEYS[2],ARGV[i]); if expiry and tonumber(expiry)>tonumber(ARGV[1]) then local ref=redis.call('HGET',KEYS[1],ARGV[i]); if ref and not seen[ref] then table.insert(result,ref);seen[ref]=true end else redis.call('HDEL',KEYS[1],ARGV[i]);redis.call('ZREM',KEYS[2],ARGV[i]) end end; return result`, aliasPrefixRedisKeys(scope), args...).StringSlice()
		if err != nil {
			return "", err
		}
		refs = values
	} else {
		aliasPrefixes.Lock()
		entries := aliasPrefixes.Scopes[scope]
		now := time.Now()
		for _, digest := range hashes {
			if entry, ok := entries[digest]; ok {
				if now.Before(entry.Expires) {
					refs = append(refs, entry.SessionKey)
				} else {
					delete(entries, digest)
					aliasPrefixes.Count--
				}
			}
		}
		if len(entries) == 0 {
			delete(aliasPrefixes.Scopes, scope)
		}
		aliasPrefixes.Unlock()
	}
	seen := map[string]bool{}
	for _, key := range refs {
		if seen[key] {
			continue
		}
		seen[key] = true
		_, found, err := ReadAliasSession(key)
		if err != nil {
			return "", err
		}
		if found {
			return key, nil
		}
	}
	return "", nil
}

// RememberAliasPrefix publishes one successful complete history, never conversation text.
// 版本: v0.0.28
// 日期: 2026-11-22
func RememberAliasPrefix(scope, digest, key string, ttl time.Duration) error {
	if scope == "" || digest == "" || key == "" {
		return nil
	}
	now := time.Now()
	if common.RedisEnabled && common.RDB != nil {
		return common.RDB.Eval(context.Background(), `local old=redis.call('ZRANGEBYSCORE',KEYS[2],'-inf',ARGV[4]); for _,hash in ipairs(old) do redis.call('HDEL',KEYS[1],hash);redis.call('ZREM',KEYS[2],hash) end; redis.call('HSET',KEYS[1],ARGV[1],ARGV[2]);redis.call('ZADD',KEYS[2],ARGV[3],ARGV[1]);local overflow=redis.call('ZCARD',KEYS[2])-tonumber(ARGV[5]);if overflow>0 then local evict=redis.call('ZRANGE',KEYS[2],0,overflow-1);for _,hash in ipairs(evict) do redis.call('HDEL',KEYS[1],hash);redis.call('ZREM',KEYS[2],hash) end end;redis.call('PEXPIRE',KEYS[1],ARGV[6]);redis.call('PEXPIRE',KEYS[2],ARGV[6]);return 1`, aliasPrefixRedisKeys(scope), digest, key, now.Add(ttl).UnixMilli(), now.UnixMilli(), aliasPrefixPerScopeLimit, ttl.Milliseconds()).Err()
	}
	aliasPrefixes.Lock()
	defer aliasPrefixes.Unlock()
	entries := aliasPrefixes.Scopes[scope]
	for digest, entry := range entries {
		if !now.Before(entry.Expires) {
			delete(entries, digest)
			aliasPrefixes.Count--
		}
	}
	if entries == nil {
		if len(aliasPrefixes.Scopes) >= aliasPrefixScopeLimit {
			evictAliasPrefixScope()
		}
		entries = map[string]aliasPrefixEntry{}
		aliasPrefixes.Scopes[scope] = entries
	}
	if _, ok := entries[digest]; !ok {
		if len(entries) >= aliasPrefixPerScopeLimit {
			evictAliasPrefixEntry(scope)
		}
		if aliasPrefixes.Count >= aliasPrefixGlobalLimit {
			evictAliasPrefixEntry("")
		}
		// Global eviction may have removed this namespace's last record.
		aliasPrefixes.Scopes[scope] = entries
		aliasPrefixes.Count++
	}
	entries[digest] = aliasPrefixEntry{key, now.Add(ttl)}
	return nil
}

func evictAliasPrefixEntry(onlyScope string) {
	var chosenScope, chosenHash string
	var expiry time.Time
	for scope, entries := range aliasPrefixes.Scopes {
		if onlyScope != "" && scope != onlyScope {
			continue
		}
		for digest, entry := range entries {
			if expiry.IsZero() || entry.Expires.Before(expiry) {
				chosenScope, chosenHash, expiry = scope, digest, entry.Expires
			}
		}
	}
	if !expiry.IsZero() {
		delete(aliasPrefixes.Scopes[chosenScope], chosenHash)
		aliasPrefixes.Count--
		if len(aliasPrefixes.Scopes[chosenScope]) == 0 {
			delete(aliasPrefixes.Scopes, chosenScope)
		}
	}
}
func evictAliasPrefixScope() {
	var chosen string
	var expiry time.Time
	for scope, entries := range aliasPrefixes.Scopes {
		if len(entries) == 0 {
			chosen = scope
			break
		}
		for _, entry := range entries {
			if expiry.IsZero() || entry.Expires.Before(expiry) {
				chosen, expiry = scope, entry.Expires
			}
		}
	}
	aliasPrefixes.Count -= len(aliasPrefixes.Scopes[chosen])
	delete(aliasPrefixes.Scopes, chosen)
}
