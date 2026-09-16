package controller

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildOpenRouterModelURLsKeepsCustomEndpointScoped(t *testing.T) {
	urls := buildOpenRouterModelURLs("https://gateway.example.com/api")
	require.Equal(t, []string{
		"https://gateway.example.com/api/v1/models",
		"https://gateway.example.com/api/models",
	}, urls)
	for _, url := range urls {
		require.False(t, strings.Contains(url, "openrouter.ai"))
	}
}

func TestBuildOpenRouterModelURLsUsesOfficialEndpointOnlyWhenDefaulted(t *testing.T) {
	urls := buildOpenRouterModelURLs("")
	require.Equal(t, []string{
		"https://openrouter.ai/api/v1/models",
		"https://openrouter.ai/api/models",
	}, urls)
}
