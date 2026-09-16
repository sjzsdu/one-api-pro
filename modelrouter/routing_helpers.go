package modelrouter

import (
	"os"
	"strings"

	"github.com/modelbus/one-api-pro/common/logger"
	schema "github.com/modelbus/one-api-pro/relay/schema"
)

// CandidateScore is the score explanation emitted by the cluster router.
type CandidateScore struct {
	Total      float64            `json:"total"`
	Components map[string]float64 `json:"components"`
	Confidence float64            `json:"confidence"`
}

func detectTaskCategory(prompt string) string {
	lower := strings.ToLower(prompt)
	categories := map[string][]string{
		"code":      {"代码", "code", "编程", "函数", "bug", "debug", "实现", "implement", "算法", "algorithm", "refactor", "重构", "syntax"},
		"translate": {"翻译", "translate", "translation", "英译中", "中译英", "localize"},
		"math":      {"数学", "计算", "方程", "证明", "math", "calculate", "equation", "proof", "微积分", "线性代数", "统计"},
		"reason":    {"推理", "分析", "逻辑", "reason", "analyze", "logic", "为什么", "why", "对比", "compare", "评估", "evaluate"},
		"creative":  {"写", "创作", "故事", "诗", "write", "create", "story", "poem", "文案", "copywriting", "小说", "novel"},
		"chat":      {"你好", "hello", "hi", "聊天", "chat", "闲聊", "你是谁", "who are you"},
	}
	best, maxHits := "default", 0
	for category, keywords := range categories {
		hits := 0
		for _, keyword := range keywords {
			if strings.Contains(lower, keyword) {
				hits++
			}
		}
		if hits > maxHits || hits == maxHits && hits > 0 && category < best {
			best, maxHits = category, hits
		}
	}
	return best
}

func requestFeatures(req *ModelSelectRequest) *RequestFeatures {
	if req == nil {
		return &RequestFeatures{}
	}
	if req.Features == nil {
		req.Features = ExtractRequestFeatures(req.Messages, req.Tools, req.MaxTokens)
	}
	return req.Features
}

func extractPrompt(messages []schema.Message) string {
	var builder strings.Builder
	for i, message := range messages {
		content := message.StringContent()
		logger.Debugf(nil, "extractPrompt message[%d]: role=%s, content=%q, isString=%v", i, message.Role, content, message.IsStringContent())
		if message.Role == "user" {
			builder.WriteString(content)
			builder.WriteByte(' ')
		}
	}
	result := strings.TrimSpace(builder.String())
	logger.Debugf(nil, "extractPrompt result: %q (len=%d)", result, len(result))
	return result
}

func isNonEmptyFile(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(expandHomeDir(path))
	return err == nil && !info.IsDir() && info.Size() > 0
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
