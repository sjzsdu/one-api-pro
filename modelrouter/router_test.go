package modelrouter

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRoundRobinModelRouter(t *testing.T) {
	router := NewRoundRobinModelRouter()
	request := &ModelSelectRequest{AvailableModels: []string{"a", "b", "c"}}

	for _, expected := range []string{"a", "b", "c", "a"} {
		selected, err := router.SelectModel(context.Background(), "default", 1, request)
		require.NoError(t, err)
		assert.Equal(t, expected, selected)
	}
}

func TestScoringModelRouterUsesKeywordScores(t *testing.T) {
	router := NewScoringModelRouter(NewRandomModelRouter())
	request := &ModelSelectRequest{
		Messages:        []Message{{Role: "user", Content: "Please debug this code"}},
		AvailableModels: []string{"gpt-3.5-turbo", "gpt-4o", "deepseek-chat"},
	}

	selected, err := router.SelectModel(context.Background(), "default", 1, request)
	require.NoError(t, err)
	assert.Equal(t, "deepseek-chat", selected)
}

func TestScoringModelRouterFallsBack(t *testing.T) {
	fallback := NewRoundRobinModelRouter()
	router := NewScoringModelRouter(fallback)
	request := &ModelSelectRequest{
		Messages:        []Message{{Role: "user", Content: "an unmatched request"}},
		AvailableModels: []string{"first", "second"},
	}

	selected, err := router.SelectModel(context.Background(), "default", 1, request)
	require.NoError(t, err)
	assert.Equal(t, "first", selected)
}

func TestRouterRejectsEmptyCandidates(t *testing.T) {
	_, err := NewRandomModelRouter().SelectModel(context.Background(), "default", 1, &ModelSelectRequest{})
	require.Error(t, err)
}

func TestRegistry(t *testing.T) {
	InitRouter("round_robin")
	assert.Equal(t, "round_robin", DefaultRouter.Name())

	InitRouter("missing")
	assert.Equal(t, "random", DefaultRouter.Name())

	InitRouter(defaultStrategy)
}
