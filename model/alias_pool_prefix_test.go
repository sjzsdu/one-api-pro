// alias_pool_prefix_test.go 自动前缀索引的容量、过期与最长历史匹配测试
// Tests bounded routing references without storing conversation content.
// 版本: v0.0.28
// 日期: 2026-11-22
// 作者: Codex
package model

import (
	"fmt"
	"github.com/modelbus/one-api-pro/common"
	"testing"
	"time"
)

// TestAliasPrefixIndex_LongestExpiryAndIsolation verifies automatic prefix routing behavior.
// 版本: v0.0.28
// 日期: 2026-11-22
func TestAliasPrefixIndex_LongestExpiryAndIsolation(t *testing.T) {
	old := common.RedisEnabled
	common.RedisEnabled = false
	defer func() { common.RedisEnabled = old }()
	alias := UserModelAlias{UserId: 12, Alias: "prefix-test"}
	scope := AliasPrefixScope(12, alias, "/v1/chat/completions")
	for _, other := range []string{AliasPrefixScope(13, alias, "/v1/chat/completions"), AliasPrefixScope(12, alias, "/v1/responses"), AliasPrefixScope(12, UserModelAlias{UserId: 0, Alias: alias.Alias}, "/v1/chat/completions")} {
		if other == scope {
			t.Fatal("prefix scope not isolated")
		}
	}
	one, two := AliasPrefixSessionKey(scope, "first"), AliasPrefixSessionKey(scope, "second")
	ClaimAliasSession(one, AliasSessionBinding{Model: "a"}, time.Minute)
	ClaimAliasSession(two, AliasSessionBinding{Model: "b"}, time.Minute)
	if e := RememberAliasPrefix(scope, "short", one, time.Minute); e != nil {
		t.Fatal(e)
	}
	if e := RememberAliasPrefix(scope, "long", two, time.Minute); e != nil {
		t.Fatal(e)
	}
	got, e := FindAliasPrefixSession(scope, []string{"long", "short"})
	if e != nil || got != two {
		t.Fatalf("longest %s %v", got, e)
	}
	UpdateAliasSession(two, "b", AliasSessionBinding{Model: "replacement"}, time.Minute)
	got, e = FindAliasPrefixSession(scope, []string{"long"})
	binding, _, _ := ReadAliasSession(got)
	if e != nil || binding.Model != "replacement" {
		t.Fatal("index copied a stale model instead of stable binding")
	}
	aliasRuntime.Lock()
	delete(aliasRuntime.Sessions, two)
	aliasRuntime.Unlock()
	got, e = FindAliasPrefixSession(scope, []string{"long", "short"})
	if e != nil || got != one {
		t.Fatal("expired binding was reused")
	}
	RememberAliasPrefix(scope, "expired", one, -time.Second)
	got, e = FindAliasPrefixSession(scope, []string{"expired"})
	if e != nil || got != "" {
		t.Fatal("expired prefix retained")
	}
}

// TestAliasPrefixIndex_Capacity verifies automatic prefix routing behavior.
// 版本: v0.0.28
// 日期: 2026-11-22
func TestAliasPrefixIndex_Capacity(t *testing.T) {
	old := common.RedisEnabled
	common.RedisEnabled = false
	defer func() { common.RedisEnabled = old }()
	scope := AliasPrefixScope(591, UserModelAlias{UserId: 591, Alias: "capacity"}, "chat")
	key := AliasPrefixSessionKey(scope, "root")
	ClaimAliasSession(key, AliasSessionBinding{Model: "a"}, time.Hour)
	for i := 0; i < 1025; i++ {
		if e := RememberAliasPrefix(scope, fmt.Sprint(i), key, time.Hour); e != nil {
			t.Fatal(e)
		}
	}
	if got, _ := FindAliasPrefixSession(scope, []string{"0"}); got != "" {
		t.Fatal("scope capacity unbounded")
	}
	if got, _ := FindAliasPrefixSession(scope, []string{"1024"}); got != key {
		t.Fatal("newest prefix evicted")
	}
	for i := 0; i < 300; i++ {
		RememberAliasPrefix(fmt.Sprintf("scope-%d", i), "one", key, time.Hour)
	}
	aliasPrefixes.Lock()
	defer aliasPrefixes.Unlock()
	if len(aliasPrefixes.Scopes) > aliasPrefixScopeLimit || aliasPrefixes.Count > aliasPrefixGlobalLimit {
		t.Fatal("local scope/global capacity unbounded")
	}
}
