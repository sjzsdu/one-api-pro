// adaptor.go 火山方舟（豆包）渠道 URL 构造与模型能力列表
// Doubao (Volcano Ark) channel URL building and model capability list.
// 版本: v0.0.28
// 日期: 2026-11-21
// 作者: opencode
package doubao

import (
	"fmt"
	"strings"

	"github.com/modelbus/one-api-pro/relay/meta"
	"github.com/modelbus/one-api-pro/relay/relaymode"
	oa "github.com/modelbus/one-api-pro/relay/adaptor/openai"
)

type Adaptor struct {
	*oa.Adaptor
}

// ModelList 是能力清单：历史模型名必须长期保留，已有渠道仍按这些名字路由。
// 方舟 Agent Plan（/api/plan/v3）当前主力模型排在前面，仅在套餐端点可用；
// 按量计费端点（/api/v3）的豆包模型继续保留，供未开通套餐的账号使用。
var ModelList = []string{
	// 方舟 Agent Plan 主力模型（/api/plan/v3）
	"doubao-seed-2.1-pro",
	"doubao-seed-2.1-lite",
	"doubao-seed-2.0-mini",
	"doubao-seed-evolving",
	"deepseek-v4-pro",
	"deepseek-v4-flash",
	"deepseek-v4.1-flash",
	"deepseek-v4.1-flash-economy",
	"glm-5.3",
	"glm-5.3-flash",
	"glm-latest",
	"kimi-k2.8-preview",
	"kimi-k3",
	"kimi-k3.1-pro",
	"doubao-embedding-vision",
	// 按量计费端点模型（/api/v3）
	"doubao-seed-1-6",
	"doubao-seed-1-6-thinking",
	"doubao-seed-1-6-flash",
	"doubao-seed-1-6-vision",
	"doubao-1-5-pro-32k",
	"doubao-1-5-pro-256k",
	"doubao-1-5-lite-32k",
	"doubao-1-5-thinking-pro",
	"doubao-1-5-vision-pro",
	"Doubao-pro-128k",
	"Doubao-pro-32k",
	"Doubao-pro-4k",
	"Doubao-lite-128k",
	"Doubao-lite-32k",
	"Doubao-lite-4k",
	"Doubao-embedding",
	"doubao-embedding",
	"doubao-embedding-large",
	"doubao-embedding-text-240715",
	"deepseek-r1",
	"deepseek-v3",
	"deepseek-r1-distill-qwen-32b",
}

// arkVersionedBase 把渠道 BaseURL 归一化成方舟的「带版本号 API 根」。
//
// 方舟有三套 OpenAI 兼容端点，都以 /v3 结尾：
//   - 按量计费：https://ark.cn-beijing.volces.com/api/v3
//   - Agent 套餐：https://ark.cn-beijing.volces.com/api/plan/v3
//   - Coding 套餐：https://ark.cn-beijing.volces.com/api/coding/v3
//
// 运维可能把 BaseURL 写成主机根、/api、/api/v3、/api/plan、/api/coding 中任意一种，
// 这里统一只保留一个版本段：BaseURL 已经带 /api/v3 时再拼 /api/v3 会得到
// /api/v3/api/v3/chat/completions，上游直接 404（2026-11-21 线上验证）。
func arkVersionedBase(baseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	switch {
	case strings.HasSuffix(base, "/api/v3"):
		// 已带版本段，直接使用。
		return base
	case strings.HasSuffix(base, "/api"),
		strings.HasSuffix(base, "/api/plan"),
		strings.HasSuffix(base, "/api/coding"):
		// 这三类路径自带版本号所在层级，只需补 /v3。
		return base + "/v3"
	default:
		// 主机根（以及任何未知后缀）：按官方按量计费端点补全。
		return base + "/api/v3"
	}
}

func (a *Adaptor) GetRequestURL(meta *meta.Meta) (string, error) {
	switch meta.Mode {
	case relaymode.ChatCompletions:
		return fmt.Sprintf("%s/chat/completions", arkVersionedBase(meta.BaseURL)), nil
	case relaymode.Embeddings:
		return fmt.Sprintf("%s/embeddings", arkVersionedBase(meta.BaseURL)), nil
	default:
	}
	return "", fmt.Errorf("unsupported relay mode %d for doubao", meta.Mode)
}

func (a *Adaptor) GetModelList() []string  { return ModelList }
