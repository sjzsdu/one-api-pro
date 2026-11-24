// model_capability_cache.go 模型选项目录快照缓存
// Reuses immutable directory snapshots while evaluating account and alias visibility per request.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package controller

import (
	"encoding/json"
	"github.com/modelbus/one-api-pro/model"
	"gorm.io/gorm"
	"sync"
	"time"
)

var configuredDirectoryCache struct {
	sync.Mutex
	db       *gorm.DB
	revision uint64
	expires  time.Time
	encoded  []byte
}

func cachedConfiguredModelItems() ([]configuredModelItem, error) {
	cache := &configuredDirectoryCache
	cache.Lock()
	defer cache.Unlock()
	db := model.DB
	revision := model.ModelDirectoryRevision()
	if cache.db == db && cache.revision == revision && time.Now().Before(cache.expires) {
		var rows []configuredModelItem
		if err := json.Unmarshal(cache.encoded, &rows); err != nil {
			return nil, err
		}
		return rows, nil
	}
	channels, err := model.GetAllChannels(0, 0, "all")
	if err != nil {
		return nil, err
	}
	rows, err := configuredModelItems(channels)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(rows)
	if err != nil {
		return nil, err
	}
	cache.db = db
	cache.revision = revision
	cache.expires = time.Now().Add(30 * time.Second)
	cache.encoded = encoded
	return rows, nil
}
