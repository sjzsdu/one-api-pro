// model_family.go 已实现原生协议的受限日期版本归一化
// Recognizes validated date suffixes only for explicitly implemented native families.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package model

import (
	"strings"
	"time"
)

// AliOperationModelFamily returns a known protocol family without rewriting its upstream wire name.
// Arbitrary aliases and malformed date suffixes remain unchanged and gain no native capability.
// 版本: v0.0.28
// 日期: 2026-11-23
func AliOperationModelFamily(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, family := range []string{"gte-rerank-v2", "qwen3-asr-flash", "wan2.6-t2v", "wan2.7-image-pro", "wan2.7-image", "qwen3.5-omni-flash-realtime", "qwen3.5-omni-plus-realtime"} {
		if name == family {
			return family
		}
		if suffix, ok := strings.CutPrefix(name, family+"-"); ok {
			for _, layout := range []string{"2006-01-02", "20060102"} {
				if parsed, err := time.Parse(layout, suffix); err == nil && parsed.Year() >= 2020 {
					return family
				}
			}
		}
	}
	return name
}
