// modelBilling.test.mjs 模型价格实际计量单位展示回归测试
// Verifies that multimodal prices are not mislabeled as token-only rates.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { modelBillingUnits } from './modelBilling.js'

test('native duration and character prices show explicit per-million units', () => {
  assert.equal(modelBillingUnits({ model_name: 'qwen3-asr-flash' }).input, '元/百万秒')
  assert.equal(modelBillingUnits({ model_name: 'wan2.6-t2v' }).input, '元/百万秒')
  assert.equal(modelBillingUnits({ model_name: 'cosyvoice-v3-flash' }).input, '元/百万字符')
  assert.equal(modelBillingUnits({ model_name: 'qwen-audio-3.1-tts-flash' }).input, '元/百万tokens')
})

test('image per-request prices are per-image and ambiguous models remain explicit', () => {
  assert.equal(modelBillingUnits({ model_name: 'wan2.7-image' }).perRequest, '元/张')
  assert.equal(modelBillingUnits({ model_name: 'gpt-4o' }).perRequest, '元/次')
  const mixed = modelBillingUnits({ model_name: 'custom', capabilities: { operations: { 'chat.generate': { modes: ['sync'] }, 'video.generate': { modes: ['async'] } } } })
  assert.equal(mixed.input, '按操作确认单位')
  assert.equal(mixed.ambiguous, true)
})

test('legacy ASR and English labels describe current gateway measurement', () => {
  assert.equal(modelBillingUnits({ model_name: 'whisper-1' }).input, '元/百万转写tokens')
  assert.equal(modelBillingUnits({ model_name: 'qwen3-asr-flash' }, 'en').input, 'CNY/million seconds')
})
