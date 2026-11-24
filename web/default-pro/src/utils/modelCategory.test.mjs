// modelCategory.test.mjs 模型分组工具单测
// Unit tests for modality-first model categorization and ordered grouping.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Doubao
import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  MODEL_CATEGORY_ORDER,
  classifyModel,
  groupModelsByCategory,
  modelCategoryLabel,
} from './modelCategory.js'

test('output modalities win over name heuristics', () => {
  assert.equal(classifyModel({ model_name: 'vendor-text-pro', output_modalities: ['image'] }), 'image')
  assert.equal(classifyModel({ model_name: 'vendor-chat', output_modalities: ['audio'] }), 'audio')
  assert.equal(classifyModel({ model_name: 'vendor-chat', output_modalities: ['video'] }), 'video')
  // Text chat models that accept images stay in the text group.
  assert.equal(classifyModel({ model_name: 'gpt-4o', input_modalities: ['text', 'image'], output_modalities: ['text'] }), 'text')
  // Audio/video understanding without explicit output stays in a media group.
  assert.equal(classifyModel({ model_name: 'qwen-audio-chat', input_modalities: ['audio'] }), 'audio')
  assert.equal(classifyModel({ model_name: 'video-chat', input_modalities: ['video'] }), 'video')
})

test('falls back to id markers when catalog metadata is missing', () => {
  const cases = [
    ['bge-m3', 'embedding'],
    ['text-embedding-3-small', 'embedding'],
    ['gte-large', 'embedding'],
    ['jina-reranker-v2', 'rerank'],
    ['llama-guard-3', 'moderation'],
    ['kling-v2', 'video'],
    ['flux-1-schnell', 'image'],
    ['seedream-4', 'image'],
    ['whisper-large-v3', 'audio'],
    ['openai/tts-1', 'audio'],
    ['gpt-4o', 'text'],
    ['deepseek-chat', 'text'],
  ]
  for (const [name, expected] of cases) {
    assert.equal(classifyModel({ model_name: name }), expected, name)
  }
})

test('category labels localize and unknown keys remain unclassified', () => {
  assert.equal(modelCategoryLabel('embedding'), '向量嵌入')
  assert.equal(modelCategoryLabel('embedding', 'en'), 'Embedding')
  assert.equal(modelCategoryLabel('unknown-key'), '未分类')
})

test('explicit capability classification wins over model name and preserves unknown', () => {
  assert.equal(classifyModel({ model_name: 'private-image-chat', capabilities: { primary_category: 'text' } }), 'text')
  assert.equal(classifyModel({ model_name: 'custom-new-model' }), 'unknown')
  assert.equal(classifyModel({ model_name: 'wanx-v1' }), 'image')
  assert.equal(classifyModel({ model_name: 'cogview-3' }), 'image')
  assert.equal(classifyModel({ model_name: 'private-image-chat', output_modalities: ['text'] }), 'text')
})

test('groups follow the fixed display order and omit empty categories', () => {
  const groups = groupModelsByCategory([
    'gpt-4o',
    'whisper-large-v3',
    'bge-m3',
    'flux-1-schnell',
    'claude-3',
  ])
  assert.deepEqual(groups.map(group => group.key), ['text', 'image', 'audio', 'embedding'])
  assert.equal(groups[0].items.length, 2)
  assert.equal(groups[0].label, '文本与对话')
  const keys = groups.map(group => group.key)
  assert.deepEqual(MODEL_CATEGORY_ORDER.filter(key => keys.includes(key)), keys)
})

test('object options keep original identity while grouped by value', () => {
  const options = [
    { value: 'whisper-large-v3', label: 'openai:whisper-large-v3' },
    { value: 'gpt-4o', label: 'openai:gpt-4o' },
  ]
  const groups = groupModelsByCategory(options, option => option.value)
  assert.deepEqual(groups.map(group => group.key), ['text', 'audio'])
  assert.equal(groups[1].items[0], options[0])
})
