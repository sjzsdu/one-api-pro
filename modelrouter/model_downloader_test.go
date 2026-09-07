package modelrouter

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveModelFilesCreatesDefaultCacheDirectories(t *testing.T) {
	server := newModelFileServer(t)
	t.Cleanup(server.Close)

	cacheDir := filepath.Join(t.TempDir(), "missing", "cache")
	downloader := NewModelDownloader(cacheDir, server.URL)
	modelPath, tokenizerPath, err := downloader.ResolveModelFiles("jina-v2-code")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(cacheDir, "jina-v2-code", "model.onnx"), modelPath)
	require.Equal(t, filepath.Join(cacheDir, "jina-v2-code", "tokenizer.json"), tokenizerPath)
	require.FileExists(t, modelPath)
	require.FileExists(t, tokenizerPath)
}

func TestResolveModelFilesCreatesExplicitPathDirectories(t *testing.T) {
	server := newModelFileServer(t)
	t.Cleanup(server.Close)

	root := t.TempDir()
	modelTarget := filepath.Join(root, "models", "nested", "embedding.onnx")
	tokenizerTarget := filepath.Join(root, "tokenizers", "nested", "tokenizer.json")
	downloader := NewModelDownloader("", server.URL)
	modelPath, tokenizerPath, err := downloader.ResolveModelFilesTo(
		"jina-v2-code", modelTarget, tokenizerTarget,
	)
	require.NoError(t, err)
	require.Equal(t, modelTarget, modelPath)
	require.Equal(t, tokenizerTarget, tokenizerPath)
	require.FileExists(t, modelPath)
	require.FileExists(t, tokenizerPath)
}

func TestResolveModelFilesKeepsExistingFile(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = fmt.Fprint(w, "downloaded")
	}))
	t.Cleanup(server.Close)

	root := t.TempDir()
	modelTarget := filepath.Join(root, "model.onnx")
	require.NoError(t, os.WriteFile(modelTarget, []byte("existing"), 0644))
	tokenizerTarget := filepath.Join(root, "new", "tokenizer.json")
	downloader := NewModelDownloader("", server.URL)
	_, _, err := downloader.ResolveModelFilesTo("jina-v2-code", modelTarget, tokenizerTarget)
	require.NoError(t, err)
	require.Equal(t, 1, requests)
	contents, err := os.ReadFile(modelTarget)
	require.NoError(t, err)
	require.Equal(t, "existing", string(contents))
}

func newModelFileServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch filepath.Base(r.URL.Path) {
		case "model.onnx":
			_, _ = fmt.Fprint(w, "model")
		case "tokenizer.json":
			_, _ = fmt.Fprint(w, `{"model":{"vocab":{"[UNK]":0}}}`)
		default:
			http.NotFound(w, r)
		}
	}))
}
