package modelrouter

import "strings"

// CanonicalModelName maps a provider-qualified channel model name to the
// catalog name used by routing profiles and embedding artifacts. The original
// name is always retained for channel selection and the upstream request.
//
// Examples: openai/gpt-4o -> gpt-4o; deepseek/deepseek-chat -> deepseek-chat.
func CanonicalModelName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.TrimPrefix(name, "~")
	if slash := strings.IndexByte(name, '/'); slash >= 0 && slash+1 < len(name) {
		return name[slash+1:]
	}
	return strings.TrimSpace(name)
}
