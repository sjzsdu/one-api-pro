package relaymode

import "strings"

func GetByPath(path string) int {
	relayMode := Unknown
	// Check the native multimodal path before the generic "*embeddings" branches
	// below; otherwise it would be misclassified as the OpenAI-compatible relay.
	if strings.HasPrefix(path, "/v1/multimodal/embeddings") {
		relayMode = MultimodalEmbeddings
	} else if strings.HasPrefix(path, "/v1/chat/completions") {
		relayMode = ChatCompletions
	} else if strings.HasPrefix(path, "/v1/messages") {
		relayMode = ChatCompletions
	} else if strings.HasPrefix(path, "/v1/completions") {
		relayMode = Completions
	} else if strings.HasPrefix(path, "/v1/embeddings") {
		relayMode = Embeddings
	} else if strings.HasSuffix(path, "embeddings") {
		relayMode = Embeddings
	} else if strings.HasPrefix(path, "/v1/moderations") {
		relayMode = Moderations
	} else if strings.HasPrefix(path, "/v1/images/generations") {
		relayMode = ImagesGenerations
	} else if strings.HasPrefix(path, "/v1/edits") {
		relayMode = Edits
	} else if strings.HasPrefix(path, "/v1/audio/speech") {
		relayMode = AudioSpeech
	} else if strings.HasPrefix(path, "/v1/audio/transcriptions") {
		relayMode = AudioTranscription
	} else if strings.HasPrefix(path, "/v1/audio/translations") {
		relayMode = AudioTranslation
	}
	return relayMode
}
