// operation_candidate_cache.go 操作候选回退查询与短期负缓存
// Bounds fallback miss traffic while invalidating local channel configuration changes.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/modelbus/one-api-pro/model"
	"gorm.io/gorm"
	"sync"
	"time"
)

type operationCandidateKey struct {
	db          *gorm.DB
	group, name string
}
type operationNegativeEntry struct {
	revision uint64
	expires  time.Time
}

var operationNegativeCache = struct {
	sync.Mutex
	entries map[operationCandidateKey]operationNegativeEntry
}{entries: map[operationCandidateKey]operationNegativeEntry{}}

func fallbackOperationCandidates(c *gin.Context, group, name string) []*model.Channel {
	const requestKey = "gateway.operation_fallback_candidates"
	key := operationCandidateKey{model.DB, group, name}
	requestCache := map[operationCandidateKey][]*model.Channel{}
	if value, ok := c.Get(requestKey); ok {
		requestCache = value.(map[operationCandidateKey][]*model.Channel)
	} else {
		c.Set(requestKey, requestCache)
	}
	if rows, ok := requestCache[key]; ok {
		return rows
	}
	revision := model.ModelDirectoryRevision()
	cache := &operationNegativeCache
	cache.Lock()
	entry, ok := cache.entries[key]
	cache.Unlock()
	if ok && entry.revision == revision && time.Now().Before(entry.expires) {
		requestCache[key] = nil
		return nil
	}
	var rows []*model.Channel
	if err := key.db.Where("status = ? AND (models LIKE ? OR models = ?)", model.ChannelStatusEnabled, "%"+name+"%", "*").Find(&rows).Error; err != nil {
		return nil
	}
	result := make([]*model.Channel, 0, len(rows))
	for _, ch := range rows {
		if ch.ContainsGroup(group) && ch.ContainsModel(name) {
			result = append(result, ch)
		}
	}
	requestCache[key] = result
	if len(result) == 0 && model.ModelDirectoryRevision() == revision {
		cache.Lock()
		if len(cache.entries) >= 4096 {
			cache.entries = map[operationCandidateKey]operationNegativeEntry{}
		}
		cache.entries[key] = operationNegativeEntry{revision: revision, expires: time.Now().Add(2 * time.Second)}
		cache.Unlock()
	}
	return result
}
