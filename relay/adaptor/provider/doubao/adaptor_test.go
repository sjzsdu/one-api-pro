// adaptor_test.go 火山方舟渠道 URL 归一化与模型清单单测
// Doubao channel URL normalisation and model list unit tests.
// 版本: v0.0.28
// 日期: 2026-11-21
// 作者: opencode
package doubao

import (
	"strings"
	"testing"

	"github.com/modelbus/one-api-pro/relay/meta"
	"github.com/modelbus/one-api-pro/relay/relaymode"
)

// TestGetRequestURL_ChatCompletionsBaseURLVariants 覆盖运维可能写下的各种
// BaseURL 拼法，全部必须落到同一个上游地址族，且不得出现重复版本段。
func TestGetRequestURL_ChatCompletionsBaseURLVariants(t *testing.T) {
	cases := []struct {
		name string
		base string
		want string
	}{
		{
			name: "host root",
			base: "https://ark.cn-beijing.volces.com",
			want: "https://ark.cn-beijing.volces.com/api/v3/chat/completions",
		},
		{
			name: "host root with trailing slash",
			base: "https://ark.cn-beijing.volces.com/",
			want: "https://ark.cn-beijing.volces.com/api/v3/chat/completions",
		},
		{
			name: "api prefix",
			base: "https://ark.cn-beijing.volces.com/api",
			want: "https://ark.cn-beijing.volces.com/api/v3/chat/completions",
		},
		{
			name: "api version already present",
			base: "https://ark.cn-beijing.volces.com/api/v3",
			want: "https://ark.cn-beijing.volces.com/api/v3/chat/completions",
		},
		{
			name: "agent plan",
			base: "https://ark.cn-beijing.volces.com/api/plan",
			want: "https://ark.cn-beijing.volces.com/api/plan/v3/chat/completions",
		},
		{
			name: "coding plan",
			base: "https://ark.cn-beijing.volces.com/api/coding",
			want: "https://ark.cn-beijing.volces.com/api/coding/v3/chat/completions",
		},
	}
	a := &Adaptor{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := a.GetRequestURL(&meta.Meta{BaseURL: tc.base, Mode: relaymode.ChatCompletions})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("base=%q got %q, want %q", tc.base, got, tc.want)
			}
			// 重复版本段会让上游直接 404（2026-11-21 线上验证）。
			if strings.Contains(got, "/v3/v3/") || strings.Contains(got, "/api/v3/api/") {
				t.Fatalf("base=%q produced a duplicated version segment: %q", tc.base, got)
			}
		})
	}
}

func TestGetRequestURL_Embeddings(t *testing.T) {
	a := &Adaptor{}
	got, err := a.GetRequestURL(&meta.Meta{
		BaseURL: "https://ark.cn-beijing.volces.com/api",
		Mode:    relaymode.Embeddings,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "https://ark.cn-beijing.volces.com/api/v3/embeddings"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestGetRequestURL_UnsupportedMode(t *testing.T) {
	a := &Adaptor{}
	if _, err := a.GetRequestURL(&meta.Meta{
		BaseURL: "https://ark.cn-beijing.volces.com/api/plan",
		Mode:    relaymode.ImagesGenerations,
	}); err == nil {
		t.Fatal("expected error for unsupported relay mode")
	}
}

// TestGetModelList_KeepsPlanAndLegacyModels 保证能力清单同时包含 Agent Plan
// 主力模型与按量计费端点的历史模型：清单是路由依据，不能因为换默认端点而删。
func TestGetModelList_KeepsPlanAndLegacyModels(t *testing.T) {
	a := &Adaptor{}
	models := a.GetModelList()
	for _, want := range []string{
		"doubao-seed-2.1-pro",
		"doubao-seed-2.1-lite",
		"deepseek-v4-pro",
		"glm-5.3",
		"kimi-k3.1-pro",
		"doubao-embedding-vision",
		"doubao-seed-1-6",
		"Doubao-pro-32k",
		"doubao-embedding",
	} {
		found := false
		for _, m := range models {
			if m == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("model %q missing from capability list", want)
		}
	}
}
