package model

type ImageRequest struct {
	Model          string `json:"model"`
	Prompt         string `json:"prompt" binding:"required"`
	N              int    `json:"n,omitempty"`
	Size           string `json:"size,omitempty"`
	Quality        string `json:"quality,omitempty"`
	ResponseFormat string `json:"response_format,omitempty"`
	Style          string `json:"style,omitempty"`
	User           string `json:"user,omitempty"`
	// Seed 让同一 prompt 复现同一张图；上游不支持时应忽略而非报错。
	Seed *int64 `json:"seed,omitempty"`
	// NegativePrompt 让调用方声明不希望出现的元素。上游不支持时由适配器
	// 决定是否拼进正向 prompt，网关层不强制。
	NegativePrompt string `json:"negative_prompt,omitempty"`
}
