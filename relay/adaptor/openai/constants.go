package openai

var ModelList = []string{
	// GPT-5 / latest aliases
	"gpt-5", "gpt-5-mini", "gpt-5-nano", "gpt-5-chat-latest",
	"gpt-5.1", "gpt-5.1-mini", "gpt-5.1-nano", "gpt-5.1-chat-latest",
	"chatgpt-4o-latest",

	// GPT-4.1 / GPT-4o
	"gpt-4.1", "gpt-4.1-mini", "gpt-4.1-nano",
	"gpt-4o", "gpt-4o-2024-05-13", "gpt-4o-2024-08-06", "gpt-4o-2024-11-20",
	"gpt-4o-mini", "gpt-4o-mini-2024-07-18",
	"gpt-4-turbo", "gpt-4-turbo-2024-04-09", "gpt-4-turbo-preview",
	"gpt-4", "gpt-4-0613", "gpt-4-0314",
	"gpt-4-32k", "gpt-4-32k-0613", "gpt-4-32k-0314",
	"gpt-4-vision-preview",

	// Reasoning models
	"o1", "o1-2024-12-17", "o1-preview", "o1-preview-2024-09-12",
	"o1-mini", "o1-mini-2024-09-12",
	"o3", "o3-mini", "o3-mini-2025-01-31", "o3-pro",
	"o4-mini", "o4-mini-2025-04-16",

	// GPT-3.5 / legacy completions
	"gpt-3.5-turbo", "gpt-3.5-turbo-0125", "gpt-3.5-turbo-1106", "gpt-3.5-turbo-0613", "gpt-3.5-turbo-0301",
	"gpt-3.5-turbo-16k", "gpt-3.5-turbo-16k-0613", "gpt-3.5-turbo-instruct",
	"davinci-002", "babbage-002", "text-davinci-003", "text-davinci-002", "text-curie-001", "text-babbage-001", "text-ada-001",

	// Embeddings / moderation
	"text-embedding-3-small", "text-embedding-3-large", "text-embedding-ada-002",
	"text-moderation-latest", "text-moderation-stable", "omni-moderation-latest", "omni-moderation-2024-09-26",

	// Image / audio
	"gpt-image-1", "dall-e-3", "dall-e-2",
	"whisper-1", "gpt-4o-transcribe", "gpt-4o-mini-transcribe",
	"tts-1", "tts-1-1106", "tts-1-hd", "tts-1-hd-1106", "gpt-4o-mini-tts",

	// Edits legacy
	"text-davinci-edit-001",
}
