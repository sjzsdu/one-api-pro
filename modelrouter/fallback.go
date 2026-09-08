package modelrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/modelbus/one-api-pro/common/logger"
)

type FailureKind string

const (
	FailureUnknown               FailureKind = "unknown"
	FailureModelNotFound         FailureKind = "model_not_found"
	FailureRateLimited           FailureKind = "rate_limited"
	FailureUpstream              FailureKind = "upstream_error"
	FailureAuthentication        FailureKind = "authentication"
	FailureContextTooLarge       FailureKind = "context_too_large"
	FailureUnsupportedCapability FailureKind = "unsupported_capability"
)

type FallbackFailure struct {
	StatusCode int
	Code       string
	Message    string
}

// FallbackDecision is intentionally data-only so both HTTP and non-HTTP
// callers can apply the same bounded retry policy.
type FallbackDecision struct {
	Kind                 FailureKind `json:"kind"`
	RetryProvider        bool        `json:"retry_provider"`
	SwitchModel          bool        `json:"switch_model"`
	ExcludeCurrentModel  bool        `json:"exclude_current_model"`
	RequireLargerContext bool        `json:"require_larger_context"`
	RequireVision        bool        `json:"require_vision"`
	RequireTools         bool        `json:"require_tools"`
	MaxProviderRetries   int         `json:"max_provider_retries"`
	Reason               string      `json:"reason"`
}

type FallbackModelRouter interface {
	SelectFallbackModel(ctx context.Context, group, failedModel string, request *ModelSelectRequest, failure FallbackFailure) (string, FallbackDecision, error)
}

func ClassifyFailure(failure FallbackFailure, features *RequestFeatures) FallbackDecision {
	message := strings.ToLower(failure.Code + " " + failure.Message)
	if containsAny(message, "context_length_exceeded", "maximum context length", "context window", "too many tokens", "上下文过长", "超出上下文") {
		return FallbackDecision{Kind: FailureContextTooLarge, SwitchModel: true, ExcludeCurrentModel: true, RequireLargerContext: true, Reason: "request exceeds the model context window"}
	}
	if containsAny(message, "vision", "image input", "image_url", "multimodal", "tool use", "tool_use", "function calling", "tools are not supported", "不支持图片", "不支持工具") {
		decision := FallbackDecision{Kind: FailureUnsupportedCapability, SwitchModel: true, ExcludeCurrentModel: true, Reason: "model does not support a required request capability"}
		if features != nil {
			decision.RequireVision = features.HasImages
			decision.RequireTools = features.HasTools || features.HasToolResult
		}
		return decision
	}

	switch {
	case failure.StatusCode == http.StatusNotFound:
		return FallbackDecision{Kind: FailureModelNotFound, RetryProvider: true, SwitchModel: true, MaxProviderRetries: 1, Reason: "provider does not expose the requested model"}
	case failure.StatusCode == http.StatusTooManyRequests || failure.StatusCode == 529:
		return FallbackDecision{Kind: FailureRateLimited, RetryProvider: true, SwitchModel: true, MaxProviderRetries: 1, Reason: "provider is rate limited or overloaded"}
	case failure.StatusCode == http.StatusUnauthorized || failure.StatusCode == http.StatusForbidden:
		return FallbackDecision{Kind: FailureAuthentication, RetryProvider: true, SwitchModel: true, MaxProviderRetries: 1, Reason: "provider credentials or permissions are invalid"}
	case failure.StatusCode >= 500 && failure.StatusCode <= 599:
		return FallbackDecision{Kind: FailureUpstream, RetryProvider: true, SwitchModel: true, MaxProviderRetries: 1, Reason: "provider returned a transient upstream error"}
	default:
		return FallbackDecision{Kind: FailureUnknown, Reason: "failure is not eligible for automatic fallback"}
	}
}

func (r *ScoringModelRouter) SelectFallbackModel(ctx context.Context, group, failedModel string, request *ModelSelectRequest, failure FallbackFailure) (string, FallbackDecision, error) {
	features := requestFeatures(request)
	decision := ClassifyFailure(failure, features)
	if !decision.SwitchModel {
		return "", decision, fmt.Errorf("failure %s does not permit model fallback", decision.Kind)
	}
	candidates, err := ResolveCandidates(ctx, group, features)
	if err != nil {
		return "", decision, err
	}
	remaining := make([]string, 0, len(candidates.Models))
	for _, candidate := range candidates.Models {
		if !strings.EqualFold(candidate, failedModel) {
			remaining = append(remaining, candidate)
		}
	}
	if len(remaining) == 0 {
		return "", decision, fmt.Errorf("no compatible fallback model for %s", failedModel)
	}
	policy := "balanced"
	if DetectTurnType(features) != TurnTypeNormal {
		policy = "economy"
	}
	availability := make(map[string]float64, len(remaining))
	for _, name := range remaining {
		availability[name] = candidates.Availability[name]
	}
	result := ScoreModelProfilesWithFeatures(features, remaining, candidates.Profiles, availability, policy)
	return result.Selected, decision, nil
}

type FallbackEvent struct {
	FailedModel   string           `json:"failed_model"`
	SelectedModel string           `json:"selected_model,omitempty"`
	StatusCode    int              `json:"status_code"`
	Decision      FallbackDecision `json:"decision"`
	Outcome       string           `json:"outcome"`
}

func LogFallbackEvent(ctx context.Context, event FallbackEvent) {
	payload, err := json.Marshal(event)
	if err != nil {
		logger.Warnf(ctx, "model_router_fallback marshal_error=%q", err.Error())
		return
	}
	logger.Infof(ctx, "model_router_fallback %s", payload)
}
