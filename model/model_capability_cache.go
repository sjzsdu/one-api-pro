// model_capability_cache.go 数据库隔离的短期能力缓存与配置失效
// Bounds metadata staleness and invalidates local model/channel configuration changes.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package model

import (
	"gorm.io/gorm"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type capabilityCacheEntry struct {
	value    ModelCapabilities
	revision uint64
	expires  time.Time
}
type capabilityCacheState struct {
	revision  atomic.Uint64
	mu        sync.Mutex
	entries   map[string]capabilityCacheEntry
	hooksOnce sync.Once
}

var capabilityCaches sync.Map

func modelCapabilityCache(db *gorm.DB) *capabilityCacheState {
	if value, ok := capabilityCaches.Load(db); ok {
		return value.(*capabilityCacheState)
	}
	state := &capabilityCacheState{entries: map[string]capabilityCacheEntry{}}
	value, _ := capabilityCaches.LoadOrStore(db, state)
	return value.(*capabilityCacheState)
}

// InitModelCapabilityCache installs invalidation hooks before a database serves
// concurrent requests. Hot-path cache creation never mutates GORM callbacks.
// 版本: v0.0.28
// 日期: 2026-11-23
func InitModelCapabilityCache(db *gorm.DB) {
	state := modelCapabilityCache(db)
	state.hooksOnce.Do(func() {
		invalidate := func(update bool) func(*gorm.DB) {
			return func(tx *gorm.DB) {
				if tx.Error != nil {
					return
				}
				switch tx.Statement.Table {
				case "model_catalogs", "abilities":
					state.revision.Add(1)
				case "channels":
					if !update || channelConfigurationUpdate(tx.Statement.Dest) {
						state.revision.Add(1)
					}
				}
			}
		}
		db.Callback().Create().After("gorm:commit_or_rollback_transaction").Register("gateway:capability_invalidation", invalidate(false))
		db.Callback().Update().After("gorm:commit_or_rollback_transaction").Register("gateway:capability_invalidation", invalidate(true))
		db.Callback().Delete().After("gorm:commit_or_rollback_transaction").Register("gateway:capability_invalidation", invalidate(false))
	})
}

func channelConfigurationUpdate(dest any) bool {
	updates, ok := dest.(map[string]interface{})
	if !ok {
		return true
	}
	for field := range updates {
		switch strings.ReplaceAll(strings.ToLower(field), "_", "") {
		case "usedquota", "usedtokens", "testtime", "responsetime", "balance", "balanceupdatedtime", "lasterror", "lasterrortime", "updatedat":
		default:
			return true
		}
	}
	return false
}

// ModelDirectoryRevision changes after local catalog/channel writes. Cross-process writes
// are observed by the bounded 30-second cache lifetime rather than a process-local signal.
// 版本: v0.0.28
// 日期: 2026-11-23
func ModelDirectoryRevision() uint64 { return modelCapabilityCache(DB).revision.Load() }

func cloneModelCapabilities(value ModelCapabilities) ModelCapabilities {
	result := value
	result.InputModalities = slices.Clone(value.InputModalities)
	result.OutputModalities = slices.Clone(value.OutputModalities)
	result.Operations = make(map[string]OperationCapability, len(value.Operations))
	for operation, cap := range value.Operations {
		cap.Modes = slices.Clone(cap.Modes)
		cap.InputModalities = slices.Clone(cap.InputModalities)
		cap.OutputModalities = slices.Clone(cap.OutputModalities)
		cap.Features = slices.Clone(cap.Features)
		cap.Limits.Formats = slices.Clone(cap.Limits.Formats)
		cap.Limits.Voices = slices.Clone(cap.Limits.Voices)
		cap.Limits.Sizes = slices.Clone(cap.Limits.Sizes)
		cap.Limits.Resolutions = slices.Clone(cap.Limits.Resolutions)
		cap.Limits.AspectRatios = slices.Clone(cap.Limits.AspectRatios)
		cap.Limits.Dimensions = slices.Clone(cap.Limits.Dimensions)
		result.Operations[operation] = cap
	}
	return result
}
