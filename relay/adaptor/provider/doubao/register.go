// register.go 火山方舟（豆包）渠道类型注册与默认 BaseURL
// Doubao (Volcano Ark) channel type registration and default BaseURL.
// 版本: v0.0.28
// 日期: 2026-11-21
// 作者: opencode
package doubao

import (
	"github.com/modelbus/one-api-pro/relay/adaptor"
	"github.com/modelbus/one-api-pro/relay/adaptor/openai"
	"github.com/modelbus/one-api-pro/relay/registry"
)

func init() {
	registry.Register(registry.ChannelMeta{
		ID:             "doubao",
		Name:           "Doubao",
		// 默认走方舟 Agent Plan 套餐端点，最终请求地址为
		// https://ark.cn-beijing.volces.com/api/plan/v3/chat/completions。
		// 使用按量计费 key 的账号改填 https://ark.cn-beijing.volces.com 即可
		// 切到 /api/v3/chat/completions（两种拼法由 adaptor 统一归一化）。
		DefaultBaseURL: "https://ark.cn-beijing.volces.com/api/plan",
		LegacyType:     40,
	}, func() adaptor.Adaptor {
		return &Adaptor{Adaptor: &openai.Adaptor{}}
	})
}
