package openaioauth

import (
	"net/url"
	"strings"
	"testing"
)

func TestBuildAuthorizeURLUsesCodexLoopbackRedirect(t *testing.T) {
	cfg := DefaultConfig()
	rawURL := BuildAuthorizeURL(cfg, PKCECodes{
		CodeVerifier:  "test-verifier",
		CodeChallenge: "test-challenge",
	}, "test-state", LoopbackRedirectURI(cfg.CallbackPort))
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	if query.Get("redirect_uri") != "http://localhost:1455/auth/callback" {
		t.Fatalf("unexpected redirect_uri: %q", query.Get("redirect_uri"))
	}
	if query.Get("code_challenge_method") != "S256" || query.Get("originator") != "codex_cli_rs" {
		t.Fatalf("unexpected OAuth parameters: %v", query)
	}
	for _, scope := range []string{"openid", "profile", "email", "offline_access"} {
		if !strings.Contains(query.Get("scope"), scope) {
			t.Fatalf("scope is missing %q", scope)
		}
	}
}
