package modelrouter

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	downloadTimeout     = 5 * time.Minute
	defaultCacheDir     = ".embedding_cache"
	modelFilePermission = 0644
	modelDirPermission  = 0755
)

// ModelFile represents a downloadable model file.
type ModelFile struct {
	Filename string
	URL      string
}

// ModelRegistry maps model names to their downloadable files.
// Users can extend this via EMBEDDING_MODEL_BASE_URL or by adding entries.
var ModelRegistry = map[string]struct {
	Dimension int
	Files     []ModelFile
}{
	"jina-v2-code": {
		Dimension: 768,
		Files: []ModelFile{
			{Filename: "model.onnx", URL: "https://github.com/nicepkg/model-router/releases/download/jina-v2-code/model.onnx"},
			{Filename: "tokenizer.json", URL: "https://github.com/nicepkg/model-router/releases/download/jina-v2-code/tokenizer.json"},
		},
	},
	"jina-embeddings-v2-base-code": {
		Dimension: 768,
		Files: []ModelFile{
			{Filename: "model.onnx", URL: "https://github.com/nicepkg/model-router/releases/download/jina-v2-code/model.onnx"},
			{Filename: "tokenizer.json", URL: "https://github.com/nicepkg/model-router/releases/download/jina-v2-code/tokenizer.json"},
		},
	},
	"qwen3-embedding": {
		Dimension: 1024,
		Files: []ModelFile{
			{Filename: "model.onnx", URL: "https://github.com/nicepkg/model-router/releases/download/qwen3-embedding/model.onnx"},
			{Filename: "tokenizer.json", URL: "https://github.com/nicepkg/model-router/releases/download/qwen3-embedding/tokenizer.json"},
		},
	},
	"qwen3-embedding-0.6b": {
		Dimension: 1024,
		Files: []ModelFile{
			{Filename: "model.onnx", URL: "https://github.com/nicepkg/model-router/releases/download/qwen3-embedding/model.onnx"},
			{Filename: "tokenizer.json", URL: "https://github.com/nicepkg/model-router/releases/download/qwen3-embedding/tokenizer.json"},
		},
	},
}

// ModelDownloader handles downloading and caching ONNX model files.
type ModelDownloader struct {
	cacheDir    string
	baseURL     string
	client      *http.Client
	mu          sync.Mutex
	downloading map[string]chan struct{}
}

// NewModelDownloader creates a downloader with the given cache directory.
// If cacheDir is empty, it defaults to $HOME/.embedding_cache or ./embedding_cache.
// If baseURL is non-empty, it overrides the registry URLs by prepending the base path.
func NewModelDownloader(cacheDir, baseURL string) *ModelDownloader {
	if cacheDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			cacheDir = filepath.Join(home, defaultCacheDir)
		} else {
			cacheDir = filepath.Join(".", defaultCacheDir)
		}
	}
	cacheDir = expandHomeDir(cacheDir)
	return &ModelDownloader{
		cacheDir:    cacheDir,
		baseURL:     strings.TrimRight(baseURL, "/"),
		client:      &http.Client{Timeout: downloadTimeout},
		downloading: make(map[string]chan struct{}),
	}
}

// ResolveModelFiles returns the local paths for a model's files, downloading
// them if necessary. If the model is not in the registry and baseURL is set,
// it constructs URLs from baseURL/<model>/<filename>.
func (d *ModelDownloader) ResolveModelFiles(modelName string) (modelPath, tokenizerPath string, err error) {
	return d.ResolveModelFilesTo(modelName, "", "")
}

// ResolveModelFilesTo resolves a model's files into the requested paths. Empty
// paths use the normal cache location. Missing parent directories and files are
// created automatically, including for explicitly configured paths.
func (d *ModelDownloader) ResolveModelFilesTo(modelName, modelPath, tokenizerPath string) (string, string, error) {
	modelName = strings.ToLower(strings.TrimSpace(modelName))
	if modelName == "" {
		return "", "", fmt.Errorf("embedding model name is required")
	}

	info, ok := ModelRegistry[modelName]
	if !ok && d.baseURL == "" {
		return "", "", fmt.Errorf("model %q not found in registry and no EMBEDDING_MODEL_BASE_URL set", modelName)
	}

	// Build file list: either from registry or from baseURL convention
	var files []ModelFile
	if ok && d.baseURL == "" {
		files = info.Files
	} else {
		// Convention: baseURL/<model>/model.onnx and baseURL/<model>/tokenizer.json.
		// A configured base URL intentionally overrides built-in registry URLs.
		files = []ModelFile{
			{Filename: "model.onnx", URL: d.baseURL + "/" + modelName + "/model.onnx"},
			{Filename: "tokenizer.json", URL: d.baseURL + "/" + modelName + "/tokenizer.json"},
		}
	}

	modelPath = expandHomeDir(strings.TrimSpace(modelPath))
	tokenizerPath = expandHomeDir(strings.TrimSpace(tokenizerPath))
	modelDir := filepath.Join(d.cacheDir, modelName)

	for _, f := range files {
		localPath := ""
		switch f.Filename {
		case "model.onnx":
			localPath = modelPath
		case "tokenizer.json":
			localPath = tokenizerPath
		}
		if localPath == "" {
			localPath = filepath.Join(modelDir, f.Filename)
		}
		if err := os.MkdirAll(filepath.Dir(localPath), modelDirPermission); err != nil {
			return "", "", fmt.Errorf("create directory for %s: %w", localPath, err)
		}
		if err := d.ensureFile(localPath, f.URL); err != nil {
			return "", "", err
		}
		if f.Filename == "model.onnx" {
			modelPath = localPath
		} else if f.Filename == "tokenizer.json" {
			tokenizerPath = localPath
		}
	}

	if modelPath == "" {
		return "", "", fmt.Errorf("model.onnx not found for %q", modelName)
	}
	if tokenizerPath == "" {
		return "", "", fmt.Errorf("tokenizer.json not found for %q", modelName)
	}
	return modelPath, tokenizerPath, nil
}

func expandHomeDir(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// ensureFile checks if the file exists locally and downloads it if not.
// Uses a per-path mutex to avoid concurrent downloads of the same file.
func (d *ModelDownloader) ensureFile(localPath, url string) error {
	// Fast path: file already exists
	if info, err := os.Stat(localPath); err == nil && info.Size() > 0 {
		return nil
	}

	// Serialize downloads for the same path
	d.mu.Lock()
	if ch, exists := d.downloading[localPath]; exists {
		d.mu.Unlock()
		<-ch
		// Re-check after download completed
		if info, err := os.Stat(localPath); err == nil && info.Size() > 0 {
			return nil
		}
		return fmt.Errorf("download failed for %s", localPath)
	}
	ch := make(chan struct{})
	d.downloading[localPath] = ch
	d.mu.Unlock()

	defer func() {
		d.mu.Lock()
		delete(d.downloading, localPath)
		d.mu.Unlock()
		close(ch)
	}()

	return d.downloadFile(localPath, url)
}

// downloadFile downloads a URL to a local path using atomic write (write to tmp, then rename).
func (d *ModelDownloader) downloadFile(localPath, url string) error {
	resp, err := d.client.Get(url)
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}

	f, err := os.CreateTemp(filepath.Dir(localPath), ".embedding-download-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := f.Name()
	defer func() {
		f.Close()
		os.Remove(tmpPath) // cleanup on error path
	}()
	if err := f.Chmod(modelFilePermission); err != nil {
		return fmt.Errorf("set temp file permissions: %w", err)
	}

	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("write model file: %w", err)
	}

	if err := f.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, localPath); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}

	return nil
}

// IsModelCached checks if all required files for a model are already cached.
func (d *ModelDownloader) IsModelCached(modelName string) bool {
	modelName = strings.ToLower(strings.TrimSpace(modelName))
	modelDir := filepath.Join(d.cacheDir, modelName)

	info, ok := ModelRegistry[modelName]
	if !ok {
		return false
	}

	for _, f := range info.Files {
		path := filepath.Join(modelDir, f.Filename)
		if stat, err := os.Stat(path); err != nil || stat.Size() == 0 {
			return false
		}
	}
	return true
}

// ClearCache removes all cached files for a model.
func (d *ModelDownloader) ClearCache(modelName string) error {
	modelName = strings.ToLower(strings.TrimSpace(modelName))
	modelDir := filepath.Join(d.cacheDir, modelName)
	return os.RemoveAll(modelDir)
}
