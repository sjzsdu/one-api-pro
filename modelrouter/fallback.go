package modelrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/modelbus/one-api-pro/common/logger"
	"github.com/modelbus/one-api-pro/model"
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
	models, err := model.CacheGetGroupModels(ctx, group)
	if err != nil || len(models) == 0 {
		return "", FallbackDecision{}, fmt.Errorf("no available models for group %s", group)
	}
	features := requestFeatures(request)
	decision := ClassifyFailure(failure, features)
	if !decision.SwitchModel {
		return "", decision, fmt.Errorf("failure %s does not permit model fallback", decision.Kind)
	}

	candidates := filterFallbackModels(models, failedModel, features, decision)
	if len(candidates) == 0 {
		return "", decision, fmt.Errorf("no compatible fallback model for %s", failedModel)
	}
	if turnType := DetectTurnType(features); turnType != TurnTypeNormal {
		return selectSpecialModel(candidates, turnType), decision, nil
	}
	prompt := features.Prompt
	scores := scoreModels(prompt, candidates)
	best := candidates[0]
	bestScore := scores[0]
	for i := 1; i < len(candidates); i++ {
		if scores[i] > bestScore {
			best, bestScore = candidates[i], scores[i]
		}
	}
	return best, decision, nil
}

func filterFallbackModels(models []string, failedModel string, features *RequestFeatures, decision FallbackDecision) []string {
	result := make([]string, 0, len(models))
	neededContext := 0
	if features != nil {
		neededContext = features.EstimatedTokens
	}
	for _, candidate := range models {
		if strings.EqualFold(candidate, failedModel) {
			continue
		}
		profile := inferModelProfile(candidate)
		if decision.RequireLargerContext && profile.contextWindow > 0 && profile.contextWindow < neededContext {
			continue
		}
		if decision.RequireVision && !profile.vision {
			continue
		}
		if decision.RequireTools && !profile.tools {
			continue
		}
		result = append(result, candidate)
	}
	sort.Strings(result)
	return result
}

type modelProfile struct {
	costTier      int
	lightweight   bool
	contextWindow int
	vision        bool
	tools         bool
}

func inferModelProfile(name string) modelProfile {
	lower := strings.ToLower(name)
	p := modelProfile{costTier: 2, contextWindow: 128000, vision: true, tools: true}
	if containsAny(lower, "mini", "nano", "flash", "haiku", "turbo", "3.5", "small", "lite") {
		p.costTier, p.lightweight = 1, true
	}
	if !p.lightweight && containsAny(lower, "gpt-4", "opus", "sonnet", "o1", "o3", "reasoner", "max") {
		p.costTier = 3
	}
	if containsAny(lower, "embedding", "rerank", "moderation") {
		p.vision, p.tools = false, false
	}
	if containsAny(lower, "coder", "deepseek-reasoner", "o1-mini") {
		p.vision = false
	}
	if containsAny(lower, "gpt-3.5", "claude-2") {
		p.contextWindow = 16000
	}
	if containsAny(lower, "32k") {
		p.contextWindow = 32000
	}
	if containsAny(lower, "200k", "claude-3") {
		p.contextWindow = 200000
	}
	if containsAny(lower, "1m", "gemini-1.5", "gemini-2") {
		p.contextWindow = 1000000
	}
	return p
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
