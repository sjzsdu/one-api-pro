package modelrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"unicode"

	hftokenizer "github.com/sugarme/tokenizer"
	"github.com/sugarme/tokenizer/pretrained"
	ort "github.com/yalue/onnxruntime_go"
)

// Tokenizer supplies model-specific token IDs. This keeps the runtime usable
// with both WordPiece-based Jina exports and BPE-based Qwen exports.
type Tokenizer interface {
	Encode(text string, maxTokens int) ([]int64, error)
}

type TokenizerFunc func(text string, maxTokens int) ([]int64, error)

func (f TokenizerFunc) Encode(text string, maxTokens int) ([]int64, error) {
	return f(text, maxTokens)
}

// HuggingFaceTokenizer supports the full tokenizer.json pipeline used by
// Qwen3 (BPE) and Jina (WordPiece) models.
type HuggingFaceTokenizer struct {
	tokenizer *hftokenizer.Tokenizer
}

func NewHuggingFaceTokenizer(path string) (*HuggingFaceTokenizer, error) {
	t, err := pretrained.FromFile(path)
	if err != nil {
		return nil, fmt.Errorf("load HuggingFace tokenizer: %w", err)
	}
	return &HuggingFaceTokenizer{tokenizer: t}, nil
}

func (t *HuggingFaceTokenizer) Encode(text string, maxTokens int) ([]int64, error) {
	input := hftokenizer.NewSingleEncodeInput(hftokenizer.NewInputSequence(text))
	encoding, err := t.tokenizer.Encode(input, true)
	if err != nil {
		return nil, err
	}
	ids := encoding.GetIds()
	if len(ids) > maxTokens {
		ids = ids[:maxTokens]
	}
	result := make([]int64, len(ids))
	for i, id := range ids {
		result[i] = int64(id)
	}
	return result, nil
}

// WordPieceTokenizer loads the vocabulary found in vocab.json or the vocab
// section of a HuggingFace tokenizer.json. It is suitable for Jina v2 Code.
type WordPieceTokenizer struct {
	vocab map[string]int64
	unk   int64
	cls   int64
	sep   int64
}

func NewWordPieceTokenizer(path string) (*WordPieceTokenizer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read tokenizer: %w", err)
	}
	var direct map[string]int64
	if err := json.Unmarshal(data, &direct); err != nil || len(direct) == 0 {
		var tokenizer struct {
			Model struct {
				Vocab map[string]int64 `json:"vocab"`
			} `json:"model"`
		}
		if err := json.Unmarshal(data, &tokenizer); err != nil || len(tokenizer.Model.Vocab) == 0 {
			return nil, fmt.Errorf("tokenizer has no vocabulary")
		}
		direct = tokenizer.Model.Vocab
	}
	t := &WordPieceTokenizer{vocab: direct}
	t.unk = firstTokenID(direct, "[UNK]", "<unk>")
	t.cls = firstTokenID(direct, "[CLS]", "<s>")
	t.sep = firstTokenID(direct, "[SEP]", "</s>")
	return t, nil
}

func firstTokenID(vocab map[string]int64, names ...string) int64 {
	for _, name := range names {
		if id, ok := vocab[name]; ok {
			return id
		}
	}
	return 0
}

func (t *WordPieceTokenizer) Encode(text string, maxTokens int) ([]int64, error) {
	if maxTokens < 2 {
		return nil, fmt.Errorf("max tokens must be at least 2")
	}
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsPunct(r)
	})
	ids := make([]int64, 0, min(maxTokens, len(words)+2))
	ids = append(ids, t.cls)
	for _, word := range words {
		firstPiece := true
		for len(word) > 0 && len(ids) < maxTokens-1 {
			matched := false
			for end := len(word); end > 0; end-- {
				piece := word[:end]
				if !firstPiece {
					piece = "##" + piece
				}
				if id, ok := t.vocab[piece]; ok {
					ids = append(ids, id)
					word = word[end:]
					firstPiece = false
					matched = true
					break
				}
			}
			if !matched {
				ids = append(ids, t.unk)
				break
			}
		}
	}
	return append(ids, t.sep), nil
}

type ONNXEmbedderConfig struct {
	ModelPath      string
	RuntimeLibrary string
	Model          string
	Dimension      int
	MaxTokens      int
	InputNames     []string
	OutputName     string
	Tokenizer      Tokenizer
}

// ONNXEmbedder runs a HuggingFace-style embedding model in-process. The
// session is reused; a mutex serializes native session calls safely.
type ONNXEmbedder struct {
	dimension  int
	maxTokens  int
	inputNames []string
	tokenizer  Tokenizer
	session    *ort.DynamicAdvancedSession
	mu         sync.Mutex
}

var ortInitMu sync.Mutex

func NewONNXEmbedder(cfg ONNXEmbedderConfig) (*ONNXEmbedder, error) {
	applyEmbeddingPreset(&cfg)
	if cfg.ModelPath == "" || cfg.Tokenizer == nil {
		return nil, fmt.Errorf("ONNX model path and tokenizer are required")
	}
	if len(cfg.InputNames) < 2 || len(cfg.InputNames) > 3 {
		return nil, fmt.Errorf("ONNX embedding model must have two or three inputs")
	}
	ortInitMu.Lock()
	if !ort.IsInitialized() {
		if cfg.RuntimeLibrary != "" {
			ort.SetSharedLibraryPath(cfg.RuntimeLibrary)
		}
		if err := ort.InitializeEnvironment(ort.WithLogLevelWarning()); err != nil {
			ortInitMu.Unlock()
			return nil, fmt.Errorf("initialize ONNX Runtime: %w", err)
		}
	}
	ortInitMu.Unlock()
	session, err := ort.NewDynamicAdvancedSession(cfg.ModelPath, cfg.InputNames, []string{cfg.OutputName}, nil)
	if err != nil {
		return nil, fmt.Errorf("load ONNX embedding model: %w", err)
	}
	return &ONNXEmbedder{dimension: cfg.Dimension, maxTokens: cfg.MaxTokens, inputNames: cfg.InputNames, tokenizer: cfg.Tokenizer, session: session}, nil
}

func applyEmbeddingPreset(cfg *ONNXEmbedderConfig) {
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 512
	}
	if len(cfg.InputNames) == 0 {
		cfg.InputNames = []string{"input_ids", "attention_mask"}
	}
	if cfg.OutputName == "" {
		cfg.OutputName = "last_hidden_state"
	}
	if cfg.Dimension == 0 {
		switch strings.ToLower(cfg.Model) {
		case "jina-v2-code", "jina-embeddings-v2-base-code":
			cfg.Dimension = 768
		case "qwen3-embedding", "qwen3-embedding-0.6b":
			cfg.Dimension = 1024
		}
	}
}

func (e *ONNXEmbedder) Dimension() int { return e.dimension }

func (e *ONNXEmbedder) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.session == nil {
		return nil
	}
	err := e.session.Destroy()
	e.session = nil
	return err
}

func (e *ONNXEmbedder) Embed(ctx context.Context, text string) ([]float64, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ids, err := e.tokenizer.Encode(text, e.maxTokens)
	if err != nil {
		return nil, fmt.Errorf("tokenize input: %w", err)
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("tokenizer returned no tokens")
	}
	shape := ort.NewShape(1, int64(len(ids)))
	idTensor, err := ort.NewTensor(shape, ids)
	if err != nil {
		return nil, err
	}
	defer idTensor.Destroy()
	mask := make([]int64, len(ids))
	for i := range mask {
		mask[i] = 1
	}
	maskTensor, err := ort.NewTensor(shape, mask)
	if err != nil {
		return nil, err
	}
	defer maskTensor.Destroy()
	inputs := []ort.Value{idTensor, maskTensor}
	if len(e.inputNames) == 3 {
		types := make([]int64, len(ids))
		typeTensor, tensorErr := ort.NewTensor(shape, types)
		if tensorErr != nil {
			return nil, tensorErr
		}
		defer typeTensor.Destroy()
		inputs = append(inputs, typeTensor)
	}
	outputs := []ort.Value{nil}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.session == nil {
		return nil, fmt.Errorf("ONNX embedder is closed")
	}
	err = e.session.Run(inputs, outputs)
	if err != nil {
		return nil, fmt.Errorf("run ONNX embedding model: %w", err)
	}
	if outputs[0] == nil {
		return nil, fmt.Errorf("ONNX model returned no output")
	}
	defer outputs[0].Destroy()
	out, ok := outputs[0].(*ort.Tensor[float32])
	if !ok {
		return nil, fmt.Errorf("ONNX output must be a float32 tensor")
	}
	data, outShape := out.GetData(), out.GetShape()
	vector, err := poolEmbedding(data, outShape, len(ids), e.dimension)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return normalizeVector(vector), nil
}

func poolEmbedding(data []float32, shape ort.Shape, tokens, dimension int) ([]float64, error) {
	outputDimension := 0
	if len(shape) == 2 { // already pooled: [batch, dimension]
		outputDimension = int(shape[1])
		tokens = 1
	} else if len(shape) == 3 { // mean pool: [batch, sequence, dimension]
		outputDimension = int(shape[2])
		tokens = min(tokens, int(shape[1]))
	} else {
		return nil, fmt.Errorf("unsupported ONNX output shape %v", shape)
	}
	if dimension <= 0 || dimension > outputDimension {
		dimension = outputDimension
	}
	if dimension <= 0 || tokens <= 0 || len(data) < tokens*outputDimension {
		return nil, fmt.Errorf("invalid ONNX output shape %v", shape)
	}
	result := make([]float64, dimension)
	for token := 0; token < tokens; token++ {
		for d := 0; d < dimension; d++ {
			result[d] += float64(data[token*outputDimension+d])
		}
	}
	for d := range result {
		result[d] /= float64(tokens)
	}
	return result, nil
}
