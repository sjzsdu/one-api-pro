package modelcatalog

import (
	"testing"
	"time"

	"github.com/modelbus/one-api-pro/relay/channeltype"
)

func TestGetAtFiltersExpiredModels(t *testing.T) {
	// qwen-vl-plus is marked deprecated 2026-06-01 with qwen3-vl-plus as successor.
	before := time.Date(2026, 5, 31, 12, 0, 0, 0, time.Local)
	after := time.Date(2026, 6, 1, 0, 0, 0, 0, time.Local)

	presetBefore, ok := GetAt(channeltype.AliBailian, before)
	if !ok {
		t.Fatal("expected a curated preset for AliBailian")
	}
	if !hasModel(presetBefore, "qwen-vl-plus") {
		t.Fatal("qwen-vl-plus should still be recommended before its deprecation date")
	}

	presetAfter, ok := GetAt(channeltype.AliBailian, after)
	if !ok {
		t.Fatal("expected a curated preset for AliBailian")
	}
	if hasModel(presetAfter, "qwen-vl-plus") {
		t.Fatal("qwen-vl-plus must be filtered out on/after its deprecation date")
	}
	if !hasModel(presetAfter, "qwen3-vl-plus") {
		t.Fatal("successor qwen3-vl-plus should remain recommended")
	}
}

func TestGetAtUnknownTypeFallsBack(t *testing.T) {
	if _, ok := GetAt(99999, time.Now()); ok {
		t.Fatal("unknown channel type should not have a curated preset")
	}
}

func TestAllAtDoesNotMutateCatalog(t *testing.T) {
	// Call at two different dates; the first call's expiry filtering must not
	// permanently remove entries from the shared catalog for the second call.
	_ = AllAt(time.Date(2026, 12, 1, 0, 0, 0, 0, time.Local))
	early := AllAt(time.Date(2026, 5, 31, 0, 0, 0, 0, time.Local))
	if !hasModel(early[channeltype.AliBailian], "qwen-vl-plus") {
		t.Fatal("expiry filtering must not mutate the shared catalog")
	}
}

func hasModel(p Preset, id string) bool {
	for _, m := range p.Models {
		if m.ID == id {
			return true
		}
	}
	return false
}
