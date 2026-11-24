// operations.go 多模态强类型操作与计量契约
// Defines typed synchronous operations, durable video requests and usage meters.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package model

import (
	"encoding/json"
	"errors"
)

// UpstreamRejectedError proves the provider has not accepted an operation for execution.
// 版本: v0.0.28
// 日期: 2026-11-23
type UpstreamRejectedError struct{ Err error }

func (e *UpstreamRejectedError) Error() string { return e.Err.Error() }
func (e *UpstreamRejectedError) Unwrap() error { return e.Err }

// ExecutionWasRejected requires affirmative evidence before releasing a reservation.
// 版本: v0.0.28
// 日期: 2026-11-23
func ExecutionWasRejected(err error) bool {
	var rejected *UpstreamRejectedError
	if errors.As(err, &rejected) {
		return true
	}
	var classified interface{ ExecutionRejected() bool }
	return errors.As(err, &classified) && classified.ExecutionRejected()
}

// RejectedHTTPStatus distinguishes explicit rejection from timeout or server uncertainty.
// 版本: v0.0.28
// 日期: 2026-11-23
func RejectedHTTPStatus(status int) bool {
	switch status {
	case 400, 401, 402, 403, 404, 405, 413, 414, 415, 422, 429:
		return true
	}
	return false
}

// SubmissionUnknownError prevents retrying a generation whose external outcome cannot be proven.
// 版本: v0.0.28
// 日期: 2026-11-23
type SubmissionUnknownError struct{}

func (*SubmissionUnknownError) Error() string { return "submission outcome is unknown" }

// ExecutionMayHaveSucceeded forbids provider fallback after an external effect or unresolved local accounting.
// 版本: v0.0.28
// 日期: 2026-11-23
func ExecutionMayHaveSucceeded(err *ErrorWithStatusCode) bool {
	if err == nil {
		return false
	}
	code, ok := err.Code.(string)
	if !ok {
		return false
	}
	switch code {
	case "settlement_pending", "usage_pending", "usage_missing", "outcome_unknown", "submission_outcome_unknown", "execution_outcome_unknown", "quota_rollback_failed":
		return true
	}
	return false
}

// UsageMetric stores a quantity in explicit fixed-point units; Scale defaults to one.
// 版本: v0.0.28
// 日期: 2026-11-23
type UsageMetric struct {
	Metric   string `json:"metric"`
	Quantity int64  `json:"quantity"`
	Scale    int64  `json:"scale,omitempty"`
	Unit     string `json:"unit"`
}

// OperationResult contains validated wire output and independent accounting quantities.
// 版本: v0.0.28
// 日期: 2026-11-23
type OperationResult struct {
	Body  json.RawMessage
	Usage []UsageMetric
}

// RerankRequest ranks documents for a query without using chat messages.
// 版本: v0.0.28
// 日期: 2026-11-23
type RerankRequest struct {
	Model           string   `json:"model"`
	Query           string   `json:"query"`
	Documents       []string `json:"documents"`
	TopN            int      `json:"top_n,omitempty"`
	ReturnDocuments bool     `json:"return_documents,omitempty"`
}

// TranscriptionRequest provides a public audio URL or a bounded uploaded data URI.
// 版本: v0.0.28
// 日期: 2026-11-23
type TranscriptionRequest struct {
	Model          string `json:"model" form:"model"`
	Audio          string `json:"audio"`
	Language       string `json:"language,omitempty" form:"language"`
	ResponseFormat string `json:"response_format,omitempty" form:"response_format"`
}

// ImageEditRequest combines the prompt with reference images and explicit optional mask.
// 版本: v0.0.28
// 日期: 2026-11-23
type ImageEditRequest struct {
	Model          string   `json:"model" form:"model"`
	Prompt         string   `json:"prompt" form:"prompt"`
	Images         []string `json:"images"`
	Mask           string   `json:"mask,omitempty"`
	Size           string   `json:"size,omitempty" form:"size"`
	N              int      `json:"n,omitempty" form:"n"`
	ResponseFormat string   `json:"response_format,omitempty" form:"response_format"`
}

// VideoRequest describes the first asynchronous text-to-video operation.
// 版本: v0.0.28
// 日期: 2026-11-23
type VideoRequest struct {
	Model          string `json:"model"`
	Operation      string `json:"operation,omitempty"`
	Prompt         string `json:"prompt"`
	NegativePrompt string `json:"negative_prompt,omitempty"`
	Size           string `json:"size,omitempty"`
	Duration       int    `json:"duration,omitempty"`
	Seed           *int64 `json:"seed,omitempty"`
}

// VideoTask is a provider task snapshot, with stable identity and terminal usage.
// 版本: v0.0.28
// 日期: 2026-11-23
type VideoTask struct {
	ID        string
	State     string
	URL       string
	Usage     []UsageMetric
	ErrorCode string
}
