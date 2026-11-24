// modelExamples.test.mjs 按操作生成调用示例的回归测试
// Ensures media examples never silently call the chat endpoint.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { buildModelExample, exampleSupported, orderExampleOperations } from './modelExamples.js'

test('speech and image examples use their own endpoint and payload', () => {
  const speech = buildModelExample({ model: 'voice-model', operation: 'audio.speech', language: 'curl', baseUrl: 'https://gateway.example', apiKey: 'sk-example' })
  assert.equal(speech.supported, true)
  assert.match(speech.code, /\/v1\/audio\/speech/)
  assert.match(speech.code, /response_format/)
  assert.doesNotMatch(speech.code, /chat\/completions/)
  const image = buildModelExample({ model: 'wan2.7-image', operation: 'image.generate', language: 'python', baseUrl: 'https://gateway.example' })
  assert.match(image.code, /images\.generate/)
  assert.doesNotMatch(image.code, /chat\.completions/)
})

test('embedding and rerank have task-specific inputs', () => {
  assert.match(buildModelExample({ model: 'bge-m3', operation: 'embedding.create', language: 'node' }).code, /embeddings\.create/)
  const rerank = buildModelExample({ model: 'gte-rerank-v2', operation: 'rerank.score' })
  assert.match(rerank.code, /documents/)
  assert.match(rerank.code, /\/v1\/rerank/)
})

test('native ASR, image edit and durable video examples match typed request fields', () => {
  const asr = buildModelExample({ model: 'qwen3-asr-flash', operation: 'audio.transcribe' })
  assert.match(asr.code, /"audio": "https:\/\/example.com\/audio.wav"/)
  assert.doesNotMatch(asr.code, /audio_url/)
  const edit = buildModelExample({ model: 'wan2.7-image', operation: 'image.edit' })
  assert.match(edit.code, /"images": \[/)
  const video = buildModelExample({ model: 'wan2.6-t2v', operation: 'video.generate' })
  assert.match(video.code, /"prompt":/)
  assert.match(video.code, /"duration": 5/)
  assert.doesNotMatch(video.code, /"parameters"|"input"/)
})

test('Python examples serialize booleans as valid Python literals', () => {
  const example = buildModelExample({ model: 'gte-rerank-v2', operation: 'rerank.score', language: 'python' })
  assert.match(example.code, /"return_documents": True/)
  assert.doesNotMatch(example.code, /: true\b|: null\b/)
})

test('unknown operations expose unavailable examples instead of chat fallback', () => {
  const example = buildModelExample({ model: 'custom', operation: 'unknown' })
  assert.equal(example.supported, false)
  assert.equal(example.code, '')
})

test('operation ordering picks the primary task for each model type', () => {
  const image = orderExampleOperations('wan2.7-image', ['image.edit', 'image.generate'])
  assert.equal(image[0], 'image.generate')
  assert.ok(image.includes('image.edit'))
  const speech = orderExampleOperations('cosyvoice-v3-flash', ['audio.speech'])
  assert.equal(speech[0], 'audio.speech')
  const chat = orderExampleOperations('gpt-4o', ['chat.generate'])
  assert.equal(chat[0], 'chat.generate')
  const noExample = orderExampleOperations('custom', ['unknown.op'])
  assert.equal(noExample.length, 0)
})

test('example support reflects available templates for media and transcription models', () => {
  assert.equal(exampleSupported('cosyvoice-v3-flash', 'audio.speech'), true)
  assert.equal(exampleSupported('wan2.6-t2i', 'image.generate'), true)
  assert.equal(exampleSupported('wan2.7-image', 'image.edit'), true)
  assert.equal(exampleSupported('custom', 'unknown.op'), false)
})
