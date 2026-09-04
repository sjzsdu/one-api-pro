package modelrouter

import (
	"strings"

	schema "github.com/modelbus/one-api-pro/relay/schema"
)

type RequestFeatures struct {
	Prompt          string
	HasImages       bool
	HasTools        bool
	HasSystemPrompt bool
	MessageCount    int
	EstimatedTokens int
	MaxTokens       int
	LastMessageType string
	IsToolResult    bool
	IsSubAgent      bool
	IsCompression   bool
}

const maxPromptLength = 4096

func ExtractFeatures(messages []schema.Message, req *ModelSelectRequest) *RequestFeatures {
	f := &RequestFeatures{
		MessageCount: len(messages),
	}

	if len(messages) == 0 {
		return f
	}

	var sb strings.Builder
	var systemPrompt strings.Builder

	for i, msg := range messages {
		switch msg.Role {
		case "system":
			f.HasSystemPrompt = true
			systemPrompt.WriteString(msg.StringContent())
			systemPrompt.WriteByte(' ')
		case "user":
			sb.WriteString(msg.StringContent())
			sb.WriteByte(' ')
		}

		if len(msg.ToolCalls) > 0 {
			f.HasTools = true
		}

		if msg.Role == "tool" || msg.ToolCallId != "" {
			f.IsToolResult = true
		}

		if i == len(messages)-1 {
			f.LastMessageType = msg.Role
		}

		f.detectImageInContent(msg)
	}

	prompt := sb.String()
	if len(prompt) > maxPromptLength {
		prompt = prompt[:maxPromptLength]
	}
	f.Prompt = strings.TrimSpace(prompt)

	f.EstimateTokens(f.Prompt + " " + systemPrompt.String())

	f.detectSpecialRequests(messages)

	return f
}

func (f *RequestFeatures) detectImageInContent(msg schema.Message) {
	if f.HasImages {
		return
	}
	contents := msg.ParseContent()
	for _, c := range contents {
		if c.Type == schema.ContentTypeImageURL {
			f.HasImages = true
			return
		}
	}
}

func (f *RequestFeatures) detectSpecialRequests(messages []schema.Message) {
	if len(messages) == 0 {
		return
	}

	firstMsg := messages[0]
	firstContent := strings.ToLower(firstMsg.StringContent())

	compressionKeywords := []string{"compress", "summarize context", "context compression", "压缩上下文"}
	for _, kw := range compressionKeywords {
		if strings.Contains(firstContent, kw) {
			f.IsCompression = true
			return
		}
	}

	if firstMsg.Role == "system" {
		systemContent := strings.ToLower(firstMsg.StringContent())
		if strings.Contains(systemContent, "sub-agent") || strings.Contains(systemContent, "subagent") ||
			strings.Contains(systemContent, "子agent") || strings.Contains(systemContent, "子代理") {
			f.IsSubAgent = true
		}
	}
}

func (f *RequestFeatures) EstimateTokens(text string) {
	runes := []rune(text)
	f.EstimatedTokens = (len(runes) + 3) / 4
}
