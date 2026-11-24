// modelCategory.js 模型类型分组：文本 / 图像 / 视频 / 音频 / 向量嵌入 / 重排序 / 内容审核
// Group models by modality and functional type for catalog tables and model pickers.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Doubao

// MODEL_CATEGORY_ORDER 分组展示顺序，文本排在最前，低频功能排在最后。
// 版本: v0.0.28
// 日期: 2026-11-23
export const MODEL_CATEGORY_ORDER = ['text', 'image', 'video', 'audio', 'embedding', 'rerank', 'moderation', 'unknown']

// MODEL_CATEGORIES 分组元数据；color 取 Arco 标签色，保证同一类型在各页面颜色一致。
// 版本: v0.0.28
// 日期: 2026-11-23
export const MODEL_CATEGORIES = {
  text: { zh: '文本与对话', en: 'Text & chat', color: 'arcoblue' },
  unknown: { zh: '未分类', en: 'Unclassified', color: 'gray' },
  image: { zh: '图像', en: 'Image', color: 'green' },
  video: { zh: '视频', en: 'Video', color: 'purple' },
  audio: { zh: '音频', en: 'Audio', color: 'orange' },
  embedding: { zh: '向量嵌入', en: 'Embedding', color: 'cyan' },
  rerank: { zh: '重排序', en: 'Rerank', color: 'gold' },
  moderation: { zh: '内容审核', en: 'Moderation', color: 'red' },
}

// modelCategoryLabel returns the localized label for a category key.
// 版本: v0.0.28
// 日期: 2026-11-23
export function modelCategoryLabel(key, lang = 'zh') {
  const category = MODEL_CATEGORIES[key] || MODEL_CATEGORIES.unknown
  return lang === 'en' ? category.en : category.zh
}

// modelCategoryColor returns the Arco tag color for a category key.
// 版本: v0.0.28
// 日期: 2026-11-23
export function modelCategoryColor(key) {
  return (MODEL_CATEGORIES[key] || MODEL_CATEGORIES.unknown).color
}

// modelCategoryRank returns the display order index, unknown keys fall back to text.
// 版本: v0.0.28
// 日期: 2026-11-23
export function modelCategoryRank(key) {
  const index = MODEL_CATEGORY_ORDER.indexOf(key)
  return index === -1 ? MODEL_CATEGORY_ORDER.length - 1 : index
}

const asLowerList = values => {
  if (typeof values === 'string') { try { values = JSON.parse(values) } catch { values = [] } }
  return Array.isArray(values) ? values.map(value => String(value).toLowerCase()) : []
}

// Name markers mirror free-router's classifyID heuristics so upstream metadata gaps
// (currently only OpenRouter rows carry modalities) still land in a sensible group.
const NAME_MARKERS = [
  { category: 'rerank', pattern: /rerank|reranker/ },
  { category: 'embedding', pattern: /embed|bge-|gte-|e5-|voyage|jina-embed/ },
  { category: 'moderation', pattern: /moderation|\bguard\b|safety/ },
  { category: 'video', pattern: /video|t2v|i2v|veo-|veo\d|kling|seedance|hunyuanvideo|sora/ },
  { category: 'image', pattern: /image|imagine|dall-?e|sdxl|stable-diffusion|flux|kolors|cogview|wanx|seedream|midjourney|recraft|ideogram/ },
  { category: 'audio', pattern: /audio|speech|tts|voice|\basr\b|whisper|transcri|music|suno/ },
]

// classifyModel infers one display category from catalog modalities first, then the model name.
// Priority: functional name markers → output modality → audio/video input → text.
// Text-chat models that merely accept images stay in the text group, matching user intent.
// 版本: v0.0.28
// 日期: 2026-11-23
export function classifyModel(model = {}) {
  const capabilities = modelCapabilities(model)
  if (MODEL_CATEGORIES[capabilities.primary_category]) return capabilities.primary_category
  if (MODEL_CATEGORIES[model.primary_category]) return model.primary_category
  const output = asLowerList(model.output_modalities)
  if (output.includes('vector')) return 'embedding'
  if (output.includes('score')) return 'rerank'
  if (output.includes('video')) return 'video'
  if (output.includes('image')) return 'image'
  if (output.includes('audio')) return 'audio'
  if (output.includes('text')) return 'text'
  const id = String(model.model_name || model.name || model.id || '').toLowerCase()
  for (const marker of NAME_MARKERS) {
    if (marker.pattern.test(id)) return marker.category
  }
  const input = asLowerList(model.input_modalities)
  if (input.includes('audio')) return 'audio'
  if (input.includes('video')) return 'video'
  return /gpt-|claude|qwen|deepseek|gemini|glm-|llama|mistral|doubao|kimi|command-r/.test(id) ? 'text' : 'unknown'
}

// modelCapabilities accepts normalized API descriptors and legacy JSON catalog fields.
// 版本: v0.0.28
// 日期: 2026-11-23
export function modelCapabilities(model = {}) {
  const raw = model.capability_override || model.capabilities || {}
  if (typeof raw === 'string') { try { return JSON.parse(raw) || {} } catch { return {} } }
  return raw && typeof raw === 'object' ? raw : {}
}

// modelOperations returns declared operations without mistaking execution modes for capabilities.
// 版本: v0.0.28
// 日期: 2026-11-23
export function modelOperations(model = {}) {
  const declared = Object.keys(modelCapabilities(model).operations || {})
  if (declared.length) return declared
  const category = classifyModel(model), name = String(model.model_name || model.id || '').toLowerCase()
  if (category === 'audio') return [/asr|whisper|transcri/.test(name) ? 'audio.transcribe' : /tts|speech|voice/.test(name) ? 'audio.speech' : 'chat.generate']
  return ({ text: ['chat.generate'], image: ['image.generate'], video: ['video.generate'], embedding: ['embedding.create'], rerank: ['rerank.score'], moderation: ['moderation.check'] })[category] || []
}

// modelOperationLabel labels each task consistently in selectors, examples and probes.
// 版本: v0.0.28
// 日期: 2026-11-23
export function modelOperationLabel(operation, lang = 'zh') {
  const labels = {
    'chat.generate': ['对话', 'Chat'], 'image.generate': ['图像生成', 'Image generation'],
    'image.edit': ['图像编辑', 'Image editing'], 'image.variation': ['图像变体', 'Image variations'],
    'audio.speech': ['语音合成', 'Speech synthesis'], 'audio.transcribe': ['语音转写', 'Transcription'],
    'audio.translate': ['语音翻译', 'Audio translation'], 'video.generate': ['视频生成', 'Video generation'],
    'video.edit': ['视频编辑', 'Video editing'], 'embedding.create': ['向量嵌入', 'Embedding'],
    'rerank.score': ['重排序', 'Rerank'], 'moderation.check': ['内容审核', 'Moderation'],
  }
  return labels[operation]?.[lang === 'en' ? 1 : 0] || operation
}

// groupModelsByCategory splits items into ordered groups; empty categories are omitted.
// getName lets callers pass plain model-name strings or catalog rows.
// 版本: v0.0.28
// 日期: 2026-11-23
export function groupModelsByCategory(items = [], getName, lang = 'zh') {
  const buckets = new Map()
  for (const item of items) {
    const model = typeof item === 'string'
      ? { model_name: item }
      : getName ? { ...item, model_name: getName(item) } : item
    const key = classifyModel(model)
    if (!buckets.has(key)) buckets.set(key, [])
    buckets.get(key).push(item)
  }
  return MODEL_CATEGORY_ORDER
    .filter(key => buckets.has(key))
    .map(key => ({
      key,
      label: modelCategoryLabel(key, lang),
      color: modelCategoryColor(key),
      items: buckets.get(key),
    }))
}
