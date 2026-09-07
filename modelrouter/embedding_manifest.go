package modelrouter

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type EmbeddingModelManifest struct {
	Dimension int         `json:"dimension"`
	Files     []ModelFile `json:"files"`
}

type EmbeddingManifest struct {
	Models map[string]EmbeddingModelManifest `json:"models"`
}

//go:embed artifacts/embedding_manifest.json
var bundledEmbeddingManifest []byte

func LoadEmbeddingManifest(path string) (*EmbeddingManifest, error) {
	data := bundledEmbeddingManifest
	if strings.TrimSpace(path) != "" {
		var err error
		data, err = os.ReadFile(expandHomeDir(path))
		if err != nil {
			return nil, fmt.Errorf("read embedding manifest: %w", err)
		}
	}
	var manifest EmbeddingManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("decode embedding manifest: %w", err)
	}
	if len(manifest.Models) == 0 {
		return nil, fmt.Errorf("embedding manifest contains no models")
	}
	return &manifest, nil
}
