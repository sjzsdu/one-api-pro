package modelrouter

import (
	"crypto/sha256"
	"encoding/hex"
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
	downloadTimeout         = 5 * time.Minute
	defaultCacheDir         = ".embedding_cache"
	modelFilePermission     = 0644
	modelDirPermission      = 0755
	maxDownloadFileSize     = 2 * 1024 * 1024 * 1024 // 2 GB
	maxDownloadRedirects    = 5
	downloadVerifyExtension = ".verify"
)

// ModelDownloader handles downloading and caching ONNX model files.
type ModelDownloader struct {
	cacheDir    string
	baseURL     string
	manifest    *EmbeddingManifest
	manifestErr error
	client      *http.Client
	mu          sync.Mutex
	downloading map[string]chan struct{}
}

// NewModelDownloader creates a downloader with the given cache directory.
// If cacheDir is empty, it defaults to $HOME/.embedding_cache or ./embedding_cache.
// If baseURL is non-empty, it overrides manifest URLs by prepending the base path.
func NewModelDownloader(cacheDir, baseURL string) *ModelDownloader {
	if cacheDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			cacheDir = filepath.Join(home, defaultCacheDir)
		} else {
			cacheDir = filepath.Join(".", defaultCacheDir)
		}
	}
	cacheDir = expandHomeDir(cacheDir)
	manifest, manifestErr := LoadEmbeddingManifest(os.Getenv("EMBEDDING_MANIFEST_PATH"))
	return &ModelDownloader{
		cacheDir:    cacheDir,
		baseURL:     strings.TrimRight(baseURL, "/"),
		manifest:    manifest,
		manifestErr: manifestErr,
		client: &http.Client{
			Timeout: downloadTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= maxDownloadRedirects {
					return fmt.Errorf("too many redirects (%d)", len(via))
				}
				if len(via) > 0 && !strings.HasPrefix(req.URL.String(), "https://") {
					return fmt.Errorf("redirect to non-HTTPS URL rejected: %s", req.URL.String())
				}
				return nil
			},
		},
		downloading: make(map[string]chan struct{}),
	}
}

// ResolveModelFiles returns the local paths for a model's files, downloading
// them if necessary. If the model is not in the manifest and baseURL is set,
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
	if d.manifestErr != nil && d.baseURL == "" {
		return "", "", d.manifestErr
	}

	var info EmbeddingModelManifest
	var ok bool
	if d.manifest != nil {
		info, ok = d.manifest.Models[modelName]
	}
	if !ok && d.baseURL == "" {
		return "", "", fmt.Errorf("model %q not found in embedding manifest and no EMBEDDING_MODEL_BASE_URL set", modelName)
	}

	// Build the file list from the manifest or the base URL convention.
	var files []ModelFile
	if ok && d.baseURL == "" {
		files = info.Files
	} else {
		// Convention: baseURL/<model>/model.onnx and baseURL/<model>/tokenizer.json.
		// A configured base URL intentionally overrides manifest URLs.
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
		if err := d.ensureFile(localPath, f); err != nil {
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

// ensureFile checks if the file exists locally and is valid, downloading it if not.
// Validates SHA256 if the manifest provides a checksum. Re-downloads corrupted files.
func (d *ModelDownloader) ensureFile(localPath string, file ModelFile) error {
	// Fast path: file exists and passes integrity check
	if d.isFileValid(localPath, file.SHA256) {
		return nil
	}

	// File exists but checksum mismatch → remove and re-download
	if info, err := os.Stat(localPath); err == nil && info.Size() > 0 {
		os.Remove(localPath)
		os.Remove(localPath + downloadVerifyExtension)
	}

	// Serialize downloads for the same path
	d.mu.Lock()
	if ch, exists := d.downloading[localPath]; exists {
		d.mu.Unlock()
		<-ch
		// Re-check after download completed
		if d.isFileValid(localPath, file.SHA256) {
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

	if err := d.downloadFile(localPath, file); err != nil {
		return err
	}

	// Write verification marker to avoid repeated checks
	if file.SHA256 != "" {
		verifyPath := localPath + downloadVerifyExtension
		os.WriteFile(verifyPath, []byte(file.SHA256), modelFilePermission)
	}

	return nil
}

// isFileValid checks if a local file exists, is non-empty, and optionally
// matches the expected SHA256 checksum. Also checks the verify marker file
// to avoid re-hashing large files on every startup.
func (d *ModelDownloader) isFileValid(localPath, expectedSHA256 string) bool {
	info, err := os.Stat(localPath)
	if err != nil || info.Size() == 0 {
		return false
	}

	// No checksum to verify
	if expectedSHA256 == "" {
		return true
	}

	// Check verify marker — if it matches, skip expensive hash
	verifyPath := localPath + downloadVerifyExtension
	if marker, err := os.ReadFile(verifyPath); err == nil {
		if strings.TrimSpace(string(marker)) == expectedSHA256 {
			return true
		}
	}

	// Compute SHA256 of the file
	f, err := os.Open(localPath)
	if err != nil {
		return false
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false
	}
	actual := hex.EncodeToString(h.Sum(nil))
	return actual == expectedSHA256
}

// downloadFile downloads a ModelFile to a local path using atomic write
// (write to tmp, then rename). Enforces file size limits via io.LimitReader.
func (d *ModelDownloader) downloadFile(localPath string, file ModelFile) error {
	url := file.URL
	if url == "" {
		return fmt.Errorf("no download URL for %s", file.Filename)
	}

	resp, err := d.client.Get(url)
	if err != nil {
		return fmt.Errorf("download %s: %w", file.Filename, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %d", file.Filename, resp.StatusCode)
	}

	// Enforce expected file size if provided
	reader := io.LimitReader(resp.Body, maxDownloadFileSize+1)
	if file.Size > 0 {
		reader = io.LimitReader(resp.Body, file.Size+1)
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

	written, err := io.Copy(f, reader)
	if err != nil {
		return fmt.Errorf("write model file: %w", err)
	}

	// Check size limits
	if file.Size > 0 && written > file.Size {
		return fmt.Errorf("downloaded file %s size %d exceeds expected %d bytes", file.Filename, written, file.Size)
	}
	if file.Size == 0 && written > maxDownloadFileSize {
		return fmt.Errorf("downloaded file %s exceeds maximum size limit (%d bytes)", file.Filename, maxDownloadFileSize)
	}

	if err := f.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	// Verify SHA256 before committing
	if file.SHA256 != "" {
		actual, err := fileSHA256(tmpPath)
		if err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("verify checksum for %s: %w", file.Filename, err)
		}
		if actual != file.SHA256 {
			os.Remove(tmpPath)
			return fmt.Errorf("checksum mismatch for %s: expected %s, got %s", file.Filename, file.SHA256, actual)
		}
	}

	// Atomic rename
	if err := os.Rename(tmpPath, localPath); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}

	return nil
}

// fileSHA256 computes the SHA256 hex digest of a file.
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// IsModelCached checks if all required files for a model are already cached
// and pass integrity checks.
func (d *ModelDownloader) IsModelCached(modelName string) bool {
	modelName = strings.ToLower(strings.TrimSpace(modelName))
	modelDir := filepath.Join(d.cacheDir, modelName)

	if d.manifest == nil {
		return false
	}
	info, ok := d.manifest.Models[modelName]
	if !ok {
		return false
	}

	for _, f := range info.Files {
		path := filepath.Join(modelDir, f.Filename)
		if !d.isFileValid(path, f.SHA256) {
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

// ClearAllCache removes the entire embedding cache directory.
func (d *ModelDownloader) ClearAllCache() error {
	return os.RemoveAll(d.cacheDir)
}

// CacheDir returns the root cache directory path.
func (d *ModelDownloader) CacheDir() string {
	return d.cacheDir
}
