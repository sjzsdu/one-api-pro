package modelrouter

import (
	"crypto/sha256"
	"encoding/hex"
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

func TestLoadEmbeddingManifestSupportsNewModelWithoutCodeChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	data := `{"models":{"brand-new":{"dimension":384,"files":[{"filename":"model.onnx","url":"https://example.com/model.onnx"},{"filename":"tokenizer.json","url":"https://example.com/tokenizer.json"}]}}}`
	require.NoError(t, os.WriteFile(path, []byte(data), 0o600))
	manifest, err := LoadEmbeddingManifest(path)
	require.NoError(t, err)
	require.Equal(t, 384, manifest.Models["brand-new"].Dimension)
	require.Len(t, manifest.Models["brand-new"].Files, 2)
}

func TestSHA256VerificationPassesForValidFile(t *testing.T) {
	content := []byte("hello world model data")
	expectedHash := sha256.Sum256(content)
	expectedSHA256 := hex.EncodeToString(expectedHash[:])

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(content)
	}))
	t.Cleanup(server.Close)

	root := t.TempDir()
	cacheDir := filepath.Join(root, "cache")

	// Create a manifest with SHA256
	manifestPath := filepath.Join(root, "manifest.json")
	manifestData := fmt.Sprintf(`{
		"models": {
			"test-model": {
				"dimension": 128,
				"files": [
					{"filename": "model.onnx", "url": "%s/model.onnx", "sha256": "%s", "size": %d},
					{"filename": "tokenizer.json", "url": "%s/tokenizer.json"}
				]
			}
		}
	}`, server.URL, expectedSHA256, len(content), server.URL)
	require.NoError(t, os.WriteFile(manifestPath, []byte(manifestData), 0o600))

	// Create a downloader that uses this manifest
	dl := NewModelDownloader(cacheDir, "")
	dl.manifest, dl.manifestErr = LoadEmbeddingManifest(manifestPath)

	modelPath, _, err := dl.ResolveModelFiles("test-model")
	require.NoError(t, err)
	require.FileExists(t, modelPath)

	// Verify file contents
	data, err := os.ReadFile(modelPath)
	require.NoError(t, err)
	require.Equal(t, content, data)
}

func TestSHA256VerificationRejectsCorruptedFile(t *testing.T) {
	correctContent := []byte("correct model data")
	correctHash := sha256.Sum256(correctContent)

	// Server always returns the correct content
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(correctContent)
	}))
	t.Cleanup(server.Close)

	root := t.TempDir()
	cacheDir := filepath.Join(root, "cache")
	modelDir := filepath.Join(cacheDir, "test-model")
	require.NoError(t, os.MkdirAll(modelDir, 0o755))

	// Pre-create a file with wrong content (simulates corrupted cache)
	wrongFile := filepath.Join(modelDir, "model.onnx")
	require.NoError(t, os.WriteFile(wrongFile, []byte("corrupted data"), 0o644))

	// Manifest with SHA256 that matches the correct content
	manifestPath := filepath.Join(root, "manifest.json")
	manifestData := fmt.Sprintf(`{
		"models": {
			"test-model": {
				"dimension": 128,
				"files": [
					{"filename": "model.onnx", "url": "%s/model.onnx", "sha256": "%s"},
					{"filename": "tokenizer.json", "url": "%s/tokenizer.json"}
				]
			}
		}
	}`, server.URL, hex.EncodeToString(correctHash[:]), server.URL)
	require.NoError(t, os.WriteFile(manifestPath, []byte(manifestData), 0o600))

	dl := NewModelDownloader(cacheDir, "")
	dl.manifest, dl.manifestErr = LoadEmbeddingManifest(manifestPath)

	modelPath, _, err := dl.ResolveModelFiles("test-model")
	require.NoError(t, err)
	require.FileExists(t, modelPath)

	// File should now have the correct (re-downloaded) content, not the corrupted content
	data, err := os.ReadFile(modelPath)
	require.NoError(t, err)
	require.Equal(t, correctContent, data)
}

func TestFileSHA256(t *testing.T) {
	content := []byte("test content for sha256")
	path := filepath.Join(t.TempDir(), "test_file")
	require.NoError(t, os.WriteFile(path, content, 0o644))

	hash, err := fileSHA256(path)
	require.NoError(t, err)

	expected := sha256.Sum256(content)
	require.Equal(t, hex.EncodeToString(expected[:]), hash)
}

func TestIsFileValidChecksSize(t *testing.T) {
	dl := NewModelDownloader(t.TempDir(), "")
	
	// Empty file
	emptyFile := filepath.Join(t.TempDir(), "empty")
	require.NoError(t, os.WriteFile(emptyFile, []byte{}, 0o644))
	require.False(t, dl.isFileValid(emptyFile, ""))
	
	// Non-existent file
	require.False(t, dl.isFileValid(filepath.Join(t.TempDir(), "nonexistent"), ""))
}

func TestClearCache(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "cache")
	dl := NewModelDownloader(cacheDir, "")
	
	modelDir := filepath.Join(cacheDir, "test-model")
	require.NoError(t, os.MkdirAll(modelDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(modelDir, "model.onnx"), []byte("data"), 0o644))
	
	require.NoError(t, dl.ClearCache("test-model"))
	_, err := os.Stat(modelDir)
	require.True(t, os.IsNotExist(err))
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
