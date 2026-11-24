// modelProbe.js 独立管理探测契约及媒体输入校验
// Separates supported administrative probes from inference runtime modes.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex

// probeRunnable requires the explicit backend probe contract and declared sync mode.
// 版本: v0.0.28
// 日期: 2026-11-23
export function probeRunnable(record, operation) {
  const modes = record.capabilities?.operations?.[operation]?.modes
  return !!record.probe_operations?.[operation] && (!modes || modes.includes('sync'))
}

// probeNeedsInput identifies probes that need caller-provided media.
// 版本: v0.0.28
// 日期: 2026-11-23
export function probeNeedsInput(record, operation) {
  return record.probe_operations?.[operation]?.requires_input === true
}

// probeBulkRunnable excludes all manually supplied media from bulk execution.
// 版本: v0.0.28
// 日期: 2026-11-23
export function probeBulkRunnable(record, operation) {
  return probeRunnable(record, operation) && !probeNeedsInput(record, operation)
}

function mediaURL(value) {
  const raw = String(value || '').trim()
  let url
  try { url = new URL(raw) } catch { throw new Error('请输入有效的 HTTP(S) 媒体 URL / Enter a valid HTTP(S) media URL') }
  if (!['https:', 'http:'].includes(url.protocol) || !url.hostname || url.username || url.password || url.hash) throw new Error('请输入有效的 HTTP(S) 媒体 URL / Enter a valid HTTP(S) media URL')
  return raw
}

// buildProbeInput returns the exact typed API payload after required-field validation.
// 版本: v0.0.28
// 日期: 2026-11-23
export function buildProbeInput(operation, draft) {
  if (operation === 'audio.transcribe') return { audio: mediaURL(draft.audio), response_format: 'json' }
  if (operation === 'image.edit') {
    const prompt = String(draft.prompt || '').trim()
    if (!prompt) throw new Error('请输入图片编辑提示词 / Enter an image editing prompt')
    return { images: [mediaURL(draft.image)], prompt, n: 1 }
  }
  if (operation === 'audio.speech') {
    const input = String(draft.input || '').trim()
    if (!input) throw new Error('请输入要合成的文本 / Enter the text to synthesize')
    const voice = String(draft.voice || '').trim()
    if (!voice) throw new Error('请输入音色 / Enter a voice')
    const payload = { input, voice, response_format: draft.response_format || 'wav' }
    const speed = Number(draft.speed)
    if (draft.speed !== '' && draft.speed != null && Number.isFinite(speed)) payload.speed = speed
    return payload
  }
  if (operation === 'image.generate') {
    const prompt = String(draft.prompt || '').trim()
    if (!prompt) throw new Error('请输入生图提示词 / Enter an image prompt')
    const payload = { prompt }
    const size = String(draft.size || '').trim()
    if (size) payload.size = size
    const n = Number(draft.n)
    if (Number.isInteger(n) && n >= 1 && n <= 4) payload.n = n
    return payload
  }
  throw new Error('此操作不支持媒体探测 / Media probe unsupported for this operation')
}

// probeProducesMedia identifies operations whose probe response carries inline
// audio/image artifacts that the admin UI should preview.
// 版本: v0.0.28
// 日期: 2026-11-24
export function probeProducesMedia(operation) {
  return operation === 'audio.speech' || operation === 'image.generate'
}
