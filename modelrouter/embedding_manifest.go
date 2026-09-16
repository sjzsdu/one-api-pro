package modelrouter

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// EmbeddingModelManifest describes a single downloadable embedding model.
type EmbeddingModelManifest struct {
	Dimension int         `json:"dimension"`
	Revision  string      `json:"revision,omitempty"`
	Files     []ModelFile `json:"files"`
}

// ModelFile represents a downloadable model file with optional integrity checks.
type ModelFile struct {
	Filename string `json:"filename"`
	URL      string `json:"url"`
	Size     int64  `json:"size,omitempty"`
	SHA256   string `json:"sha256,omitempty"`
}

// EmbeddingManifest is a collection of known embedding models.
type EmbeddingManifest struct {
	Revision string                                `json:"revision,omitempty"`
	Models   map[string]EmbeddingModelManifest     `json:"models"`
}

//go:embed artifacts/embedding_manifest.json
var bundledEmbeddingManifest []byte

const (
	remoteManifestTimeout   = 30 * time.Second
	remoteManifestCacheTTL  = 1 * time.Hour
	maxManifestResponseSize = 4 * 1024 * 1024 // 4 MB
)

// remoteManifestCache holds a fetched remote manifest with its fetch time.
type remoteManifestCache struct {
	manifest  *EmbeddingManifest
	fetchedAt time.Time
}

var (
	globalRemoteManifest   remoteManifestCache
	globalRemoteManifestMu sync.RWMutex
)

// LoadEmbeddingManifest loads the embedding manifest from the given path.
// If path is empty, it uses the bundled embedded manifest.
// If path starts with "http://" or "https://", it fetches from the remote URL
// with local caching (TTL = 1 hour).
func LoadEmbeddingManifest(path string) (*EmbeddingManifest, error) {
	trimmed := strings.TrimSpace(path)

	// Remote URL: fetch with caching
	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		return loadRemoteManifest(trimmed)
	}

	// Local file or bundled
	data := bundledEmbeddingManifest
	if trimmed != "" {
		var err error
		data, err = os.ReadFile(expandHomeDir(trimmed))
		if err != nil {
			return nil, fmt.Errorf("read embedding manifest: %w", err)
		}
	}
	return parseManifest(data)
}

// loadRemoteManifest fetches a manifest from a URL with local in-memory caching.
func loadRemoteManifest(url string) (*EmbeddingManifest, error) {
	globalRemoteManifestMu.RLock()
	if globalRemoteManifest.manifest != nil && time.Since(globalRemoteManifest.fetchedAt) < remoteManifestCacheTTL {
		manifest := globalRemoteManifest.manifest
		globalRemoteManifestMu.RUnlock()
		return manifest, nil
	}
	globalRemoteManifestMu.RUnlock()

	globalRemoteManifestMu.Lock()
	defer globalRemoteManifestMu.Unlock()

	// Double-check after acquiring write lock
	if globalRemoteManifest.manifest != nil && time.Since(globalRemoteManifest.fetchedAt) < remoteManifestCacheTTL {
		return globalRemoteManifest.manifest, nil
	}

	data, err := fetchURL(url)
	if err != nil {
		// If we have a stale cache, use it rather than failing
		if globalRemoteManifest.manifest != nil {
			return globalRemoteManifest.manifest, nil
		}
		return nil, fmt.Errorf("fetch remote manifest: %w", err)
	}

	manifest, err := parseManifest(data)
	if err != nil {
		return nil, err
	}

	globalRemoteManifest = remoteManifestCache{manifest: manifest, fetchedAt: time.Now()}
	return manifest, nil
}

// fetchURL fetches a URL with safety limits (HTTPS enforcement, redirect cap, size cap).
func fetchURL(url string) ([]byte, error) {
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") {
		return nil, fmt.Errorf("only http/https URLs are supported: %s", url)
	}

	client := &http.Client{
		Timeout: remoteManifestTimeout,
		// Limit redirects to prevent infinite loops and SSRF
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects (%d)", len(via))
			}
			// Enforce HTTPS after the first request
			if len(via) > 0 && !strings.HasPrefix(req.URL.String(), "https://") {
				return fmt.Errorf("redirect to non-HTTPS URL rejected: %s", req.URL.String())
			}
			return nil
		},
	}

	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("HTTP GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	// Limit response body size
	limited := io.LimitReader(resp.Body, maxManifestResponseSize)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read response from %s: %w", url, err)
	}
	if int64(len(data)) >= maxManifestResponseSize {
		return nil, fmt.Errorf("manifest response from %s exceeds %d bytes limit", url, maxManifestResponseSize)
	}

	return data, nil
}

func parseManifest(data []byte) (*EmbeddingManifest, error) {
	var manifest EmbeddingManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("decode embedding manifest: %w", err)
	}
	if len(manifest.Models) == 0 {
		return nil, fmt.Errorf("embedding manifest contains no models")
	}
	return &manifest, nil
}

// InvalidateRemoteManifestCache forces the next remote manifest fetch to
// re-download instead of using the cached version.
func InvalidateRemoteManifestCache() {
	globalRemoteManifestMu.Lock()
	globalRemoteManifest = remoteManifestCache{}
	globalRemoteManifestMu.Unlock()
}
