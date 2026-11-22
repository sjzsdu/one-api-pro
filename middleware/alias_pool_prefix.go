// alias_pool_prefix.go 根据完整结构化消息历史生成自动会话前缀身份
// Hashes canonical message boundaries while preserving tools and multimodal content.
// 版本: v0.0.28
// 日期: 2026-11-22
// 作者: Codex
package middleware

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"unicode"
)

const aliasPrefixMinUserCharacters = 32
const aliasPrefixBoundaryLimit = 128

type aliasPrefixIdentity struct {
	Protocol string
	Current  string
	Hashes   []string // Longest boundary first.
}

func buildAliasPrefixIdentity(path string, body []byte) aliasPrefixIdentity {
	result := aliasPrefixIdentity{Protocol: path}
	if path != "/v1/chat/completions" && path != "/v1/messages" && path != "/v1/responses" {
		return result
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var fields map[string]any
	if decoder.Decode(&fields) != nil {
		return result
	}
	var messages []any
	if path == "/v1/responses" {
		switch input := fields["input"].(type) {
		case string:
			messages = []any{map[string]any{"type": "message", "role": "user", "content": input}}
		case []any:
			messages = input
			for _, raw := range messages {
				if item, ok := raw.(map[string]any); ok && item["role"] != nil && item["type"] == nil {
					item["type"] = "message"
				}
			}
		}
	} else {
		messages, _ = fields["messages"].([]any)
	}
	if len(messages) == 0 {
		return result
	}
	root := map[string]any{}
	for _, key := range []string{"system", "instructions", "tools", "functions", "tool_choice", "function_call", "response_format"} {
		if value, ok := fields[key]; ok {
			root[key] = value
		}
	}
	canonical, err := json.Marshal(root)
	if err != nil {
		return result
	}
	rolling := sha256.New()
	rolling.Write([]byte("alias-prefix-v1"))
	rolling.Write(canonical)
	userCharacters := 0
	for _, raw := range messages {
		item, ok := raw.(map[string]any)
		if !ok {
			return aliasPrefixIdentity{Protocol: path}
		}
		if item["role"] == "user" {
			userCharacters += aliasUserContentCharacters(item["content"])
		}
		canonical, err = json.Marshal(item)
		if err != nil {
			return aliasPrefixIdentity{Protocol: path}
		}
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(canonical)))
		rolling.Write(size[:])
		rolling.Write(canonical)
		if userCharacters >= aliasPrefixMinUserCharacters {
			digest := hex.EncodeToString(rolling.Sum(nil))
			result.Hashes = append(result.Hashes, digest)
			if len(result.Hashes) > aliasPrefixBoundaryLimit {
				result.Hashes = result.Hashes[1:]
			}
		}
	}
	if len(result.Hashes) > 0 {
		result.Current = result.Hashes[len(result.Hashes)-1]
	}
	for left, right := 0, len(result.Hashes)-1; left < right; left, right = left+1, right-1 {
		result.Hashes[left], result.Hashes[right] = result.Hashes[right], result.Hashes[left]
	}
	return result
}

func aliasUserContentCharacters(content any) int {
	count := func(text string) int {
		n := 0
		for _, r := range text {
			if !unicode.IsSpace(r) {
				n++
			}
		}
		return n
	}
	switch value := content.(type) {
	case string:
		return count(value)
	case []any:
		total := 0
		for _, raw := range value {
			if block, ok := raw.(map[string]any); ok {
				if block["type"] == "text" || block["type"] == "input_text" || block["type"] == "output_text" {
					if text, ok := block["text"].(string); ok {
						total += count(text)
					}
				}
			}
		}
		return total
	}
	return 0
}
