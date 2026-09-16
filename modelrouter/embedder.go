package modelrouter

import "context"

// Embedder converts text into a fixed-size, normalized semantic vector.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float64, error)
	Dimension() int
}

// CloseableEmbedder is implemented by embedders that own native resources.
type CloseableEmbedder interface {
	Embedder
	Close() error
}
