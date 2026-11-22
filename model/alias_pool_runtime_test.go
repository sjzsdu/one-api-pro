// alias_pool_runtime_test.go 权重分配与并发会话绑定测试
// Verifies weighted selection, concurrent claiming and stale-success protection.
// 版本: v0.0.28
// 日期: 2026-11-22
// 作者: Codex
package model

import (
	"github.com/modelbus/one-api-pro/common"
	"sync"
	"testing"
	"time"
)

// TestAliasPoolRuntime_AffinityAndWeights verifies the model-pool behavior described by this test.
// 版本: v0.0.28
// 日期: 2026-11-22
func TestAliasPoolRuntime_AffinityAndWeights(t *testing.T) {
	old := common.RedisEnabled
	common.RedisEnabled = false
	defer func() { common.RedisEnabled = old }()
	alias := UserModelAlias{UserId: 777, Alias: "test"}
	key := AliasPoolKey(777, alias, "concurrent")
	aliasRuntime.Lock()
	delete(aliasRuntime.Sessions, key)
	aliasRuntime.Unlock()
	var wg sync.WaitGroup
	results := make(chan string, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := "a"
			if i%2 == 0 {
				name = "b"
			}
			v, e := ClaimAliasSession(key, AliasSessionBinding{Model: name}, time.Minute)
			if e != nil {
				results <- "ERROR"
			} else {
				results <- v.Model
			}
		}(i)
	}
	wg.Wait()
	close(results)
	winner := ""
	for name := range results {
		if winner == "" {
			winner = name
		}
		if name != winner {
			t.Fatal("concurrent session split")
		}
	}
	if e := UpdateAliasSession(key, winner, AliasSessionBinding{Model: "replacement", ChannelID: 2}, time.Minute); e != nil {
		t.Fatal(e)
	}
	UpdateAliasSession(key, winner, AliasSessionBinding{Model: winner, ChannelID: 1}, time.Minute)
	got, ok, _ := ReadAliasSession(key)
	if !ok || got.Model != "replacement" || got.ChannelID != 2 {
		t.Fatal("late success overwrote failover")
	}
	if AliasPoolKey(778, alias, "concurrent") == key {
		t.Fatal("user isolation")
	}
	count := 0
	for i := 0; i < 4000; i++ {
		order := WeightedAliasTargets([]UserModelAliasTarget{{Model: "a", Weight: 3}, {Model: "b", Weight: 1}})
		if order[0].Model == "a" {
			count++
		}
	}
	if count < 2700 || count > 3300 {
		t.Fatalf("unexpected weighted share %d", count)
	}
	expiry := AliasPoolKey(777, alias, "expiry")
	ClaimAliasSession(expiry, AliasSessionBinding{Model: "a"}, -time.Second)
	if _, ok, _ := ReadAliasSession(expiry); ok {
		t.Fatal("expired binding retained")
	}
}
