package modelrouter

import "testing"

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

func TestCapabilityFallbackFiltersIncompatibleModels(t *testing.T) {
	features := &RequestFeatures{HasImages: true, HasTools: true}
	decision := ClassifyFailure(FallbackFailure{StatusCode: 400, Message: "tools are not supported"}, features)
	got := filterFallbackModels([]string{"failed", "text-embedding-3-small", "deepseek-coder", "gpt-4o-mini"}, "failed", features, decision)
	if len(got) != 1 || got[0] != "gpt-4o-mini" {
		t.Fatalf("filterFallbackModels() = %v, want [gpt-4o-mini]", got)
	}
}

func TestContextFallbackRequiresLargeEnoughWindow(t *testing.T) {
	features := &RequestFeatures{EstimatedTokens: 100000}
	decision := ClassifyFailure(FallbackFailure{StatusCode: 400, Message: "maximum context length exceeded"}, features)
	got := filterFallbackModels([]string{"failed", "gpt-3.5-turbo", "gpt-4o", "gemini-2-flash"}, "failed", features, decision)
	if len(got) != 2 || got[0] != "gemini-2-flash" || got[1] != "gpt-4o" {
		t.Fatalf("filterFallbackModels() = %v", got)
	}
}
