package modelrouter

import (
	"context"
	"testing"
)

type staticProfileProvider map[string]ModelProfile

func (p staticProfileProvider) Profiles(_ context.Context, names []string) (map[string]ModelProfile, error) {
	result := make(map[string]ModelProfile, len(names))
	for _, name := range names {
		if profile, ok := p[name]; ok {
			result[name] = profile
		}
	}
	return result, nil
}

func TestClassifyFailure(t *testing.T) {
	tests := []struct {
		name        string
		failure     FallbackFailure
		want        FailureKind
		retry       bool
		switchModel bool
	}{
		{name: "not found", failure: FallbackFailure{StatusCode: 404}, want: FailureModelNotFound, retry: true, switchModel: true},
		{name: "rate limit", failure: FallbackFailure{StatusCode: 429}, want: FailureRateLimited, retry: true, switchModel: true},
		{name: "auth", failure: FallbackFailure{StatusCode: 401}, want: FailureAuthentication, retry: true, switchModel: true},
		{name: "server", failure: FallbackFailure{StatusCode: 502}, want: FailureUpstream, retry: true, switchModel: true},
		{name: "context", failure: FallbackFailure{StatusCode: 400, Code: "context_length_exceeded"}, want: FailureContextTooLarge, switchModel: true},
		{name: "other client error", failure: FallbackFailure{StatusCode: 400, Message: "bad input"}, want: FailureUnknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ClassifyFailure(test.failure, nil)
			if got.Kind != test.want || got.RetryProvider != test.retry || got.SwitchModel != test.switchModel {
				t.Fatalf("ClassifyFailure() = %+v", got)
			}
			if got.MaxProviderRetries > 1 {
				t.Fatalf("unbounded provider retry: %+v", got)
			}
		})
	}
}

func TestFallbackRequiresAdvertisedCapabilityAfterFailure(t *testing.T) {
	features := &RequestFeatures{HasImages: true}
	decision := ClassifyFailure(FallbackFailure{StatusCode: 400, Message: "vision is not supported"}, features)
	if supportsFallbackConstraints(ModelProfile{Vision: CapabilityUnknown}, features, decision) {
		t.Fatal("unknown vision capability must not be selected after a vision failure")
	}
	if !supportsFallbackConstraints(ModelProfile{Vision: CapabilitySupported}, features, decision) {
		t.Fatal("supported vision capability was rejected")
	}
}

func TestCapabilityFallbackFiltersIncompatibleModels(t *testing.T) {
	features := &RequestFeatures{HasImages: true, HasTools: true}
	provider := staticProfileProvider{
		"text-only": {Model: "text-only", Vision: CapabilityUnsupported, Tools: CapabilitySupported},
		"no-tools":  {Model: "no-tools", Vision: CapabilitySupported, Tools: CapabilityUnsupported},
		"unknown":   {Model: "unknown", Vision: CapabilityUnknown, Tools: CapabilityUnknown},
	}
	got, err := resolveCandidateNames(context.Background(), []string{"text-only", "no-tools", "unknown"}, features, provider)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Models) != 1 || got.Models[0] != "unknown" {
		t.Fatalf("resolveCandidateNames() = %v, want [unknown]", got.Models)
	}
}

func TestContextFallbackRequiresLargeEnoughWindow(t *testing.T) {
	features := &RequestFeatures{EstimatedTokens: 100000}
	provider := staticProfileProvider{
		"small":   {Model: "small", ContextWindow: 16000},
		"large":   {Model: "large", ContextWindow: 200000},
		"unknown": {Model: "unknown"},
	}
	got, err := resolveCandidateNames(context.Background(), []string{"small", "large", "unknown"}, features, provider)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Models) != 2 || got.Models[0] != "large" || got.Models[1] != "unknown" {
		t.Fatalf("resolveCandidateNames() = %v", got.Models)
	}
}
