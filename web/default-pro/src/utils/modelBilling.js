// modelBilling.js 当前模型共用单价的实际计量单位说明
// Labels the implemented gateway's token, character, duration and image price units.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
import { modelOperations } from './modelCategory.js'

// modelBillingUnits describes known meters and exposes uncertainty instead of inventing token prices.
// 版本: v0.0.28
// 日期: 2026-11-23
export function modelBillingUnits(model = {}, lang = 'zh') {
  const name = String(model.model_name || model.name || '').toLowerCase()
  const operations = modelOperations(model)
  const input = new Set(), output = new Set()
  for (const operation of operations) {
    if (operation === 'audio.speech') {
      if (name === 'qwen-audio-3.1-tts-flash') { input.add('tokens'); output.add('tokens') }
      else if (/cosyvoice|qwen-audio.*tts|^tts-1|gpt-4o-mini-tts/.test(name)) input.add('characters')
      else input.add('unknown')
    } else if (operation === 'audio.transcribe' || operation === 'audio.translate') {
      input.add(name.startsWith('qwen3-asr') ? 'seconds' : name.startsWith('whisper-') ? 'transcript_tokens' : 'unknown')
    } else if (operation.startsWith('video.')) {
      input.add(name === 'wan2.6-t2v' ? 'seconds' : 'unknown')
    } else if (operation.startsWith('image.')) input.add('images')
    else if (operation === 'chat.generate') { input.add('tokens'); output.add('tokens') }
    else if (['embedding.create', 'rerank.score', 'moderation.check'].includes(operation)) input.add('tokens')
    else input.add('unknown')
  }
  if (!input.size) input.add('unknown')
  const en = lang === 'en'
  const labels = {
    tokens: en ? 'CNY/million tokens' : '元/百万tokens',
    characters: en ? 'CNY/million characters' : '元/百万字符',
    seconds: en ? 'CNY/million seconds' : '元/百万秒',
    images: en ? 'CNY/million images' : '元/百万张',
    transcript_tokens: en ? 'CNY/million transcript tokens' : '元/百万转写tokens',
  }
  const uncertain = en ? 'Confirm units per operation' : '按操作确认单位'
  const format = units => units.size === 1 && !units.has('unknown') ? labels[[...units][0]] : uncertain
  const allImages = operations.length > 0 && operations.every(operation => operation.startsWith('image.'))
  const mixedImages = operations.some(operation => operation.startsWith('image.')) && !allImages
  return {
    input: format(input),
    output: output.size ? format(output) : input.has('unknown') ? uncertain : en ? 'Not used by these operations' : '该操作不使用输出价',
    perRequest: mixedImages ? uncertain : allImages ? en ? 'CNY/image' : '元/张' : en ? 'CNY/request' : '元/次',
    ambiguous: input.size > 1 || input.has('unknown') || mixedImages,
  }
}
