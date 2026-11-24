// modelProbe.test.mjs 管理探测支持与输入回归
// Tests probe support independently from inference capabilities.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { probeRunnable, probeNeedsInput, probeBulkRunnable, buildProbeInput, probeProducesMedia } from './modelProbe.js'

test('sync inference alone never enables a probe', () => {
  assert.equal(probeRunnable({ gateway_support: { 'audio.speech': ['sync'] } }, 'audio.speech'), false)
  assert.equal(probeRunnable({ probe_operations: { 'chat.generate': {} } }, 'chat.generate'), true)
})
test('manual media probes are excluded from bulk', () => {
  const row = { probe_operations: { 'audio.transcribe': { requires_input: true } } }
  assert.equal(probeNeedsInput(row, 'audio.transcribe'), true)
  assert.equal(probeBulkRunnable(row, 'audio.transcribe'), false)
})
test('media input is required and preserved in API shape', () => {
  assert.throws(() => buildProbeInput('audio.transcribe', { audio: '' }))
  assert.throws(() => buildProbeInput('image.edit', { image: 'https://media.example/a.png', prompt: '' }))
  assert.throws(() => buildProbeInput('image.edit', { image: 'invalid', prompt: 'blue' }))
  assert.deepEqual(buildProbeInput('audio.transcribe', { audio: 'https://media.example/a.wav' }), { audio: 'https://media.example/a.wav', response_format: 'json' })
  assert.deepEqual(buildProbeInput('image.edit', { image: 'https://media.example/a.png', prompt: 'blue' }), { images: ['https://media.example/a.png'], prompt: 'blue', n: 1 })
})

test('speech and image generation probes require text input and build valid payloads', () => {
  assert.throws(() => buildProbeInput('audio.speech', { input: '', voice: 'cosyvoice-v3-flash' }))
  assert.throws(() => buildProbeInput('audio.speech', { input: 'hello' }))
  assert.deepEqual(buildProbeInput('audio.speech', { input: 'hello', voice: 'longwan', speed: '1.2' }),
    { input: 'hello', voice: 'longwan', response_format: 'wav', speed: 1.2 })
  assert.throws(() => buildProbeInput('image.generate', { prompt: '' }))
  assert.deepEqual(buildProbeInput('image.generate', { prompt: 'a cat', size: '1024x1024', n: '2' }),
    { prompt: 'a cat', size: '1024x1024', n: 2 })
})

test('generation media probes are recognized for inline preview', () => {
  assert.equal(probeProducesMedia('audio.speech'), true)
  assert.equal(probeProducesMedia('image.generate'), true)
  assert.equal(probeProducesMedia('chat.generate'), false)
  assert.equal(probeProducesMedia('image.edit'), false)
})

test('generation probes are not bulk-runnable because they require input', () => {
  const row = { probe_operations: { 'audio.speech': { requires_input: true }, 'image.generate': { requires_input: true } } }
  assert.equal(probeNeedsInput(row, 'audio.speech'), true)
  assert.equal(probeBulkRunnable(row, 'audio.speech'), false)
  assert.equal(probeBulkRunnable(row, 'image.generate'), false)
})
