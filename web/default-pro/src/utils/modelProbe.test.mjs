// modelProbe.test.mjs 管理探测支持与输入回归
// Tests probe support independently from inference capabilities.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { probeRunnable, probeNeedsInput, probeBulkRunnable, buildProbeInput } from './modelProbe.js'

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
