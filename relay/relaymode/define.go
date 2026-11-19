package relaymode

const (
	Unknown = iota
	ChatCompletions
	Completions
	Embeddings
	Moderations
	ImagesGenerations
	Edits
	AudioSpeech
	AudioTranscription
	AudioTranslation
	// MultimodalEmbeddings is the native DashScope multimodal embedding relay.
	// It must stay at the end of the iota block to keep existing numeric values stable.
	MultimodalEmbeddings
)
