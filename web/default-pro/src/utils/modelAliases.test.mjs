// modelAliases.test.mjs 模型池表单兼容、校验与请求测试
// Tests legacy aliases, pool validation, and session-aware test requests.
// 版本: v0.0.28
// 日期: 2026-11-22
// 作者: Codex
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { normalizeModelAlias, mergeModelAliasDraft, serializeModelAliases, validateModelAliases, buildAliasTestRequest } from './modelAliases.js'

test('modal drafts and merged candidates never mutate the saved list', () => {
  const saved = [normalizeModelAlias({ alias: 'chat-tool', targets: [{ model: 'GLM', weight: 3 }] })]
  const draft = normalizeModelAlias(saved[0])
  draft.targets[0].weight = 9
  draft.alias = 'chat-renamed'
  assert.equal(saved[0].targets[0].weight, 3)
  assert.equal(saved[0].alias, 'chat-tool')
  const merged = mergeModelAliasDraft(saved, draft, 0)
  assert.equal(merged[0].alias, 'chat-renamed')
  draft.targets[0].model = 'DeepSeek'
  assert.equal(merged[0].targets[0].model, 'GLM')
  assert.equal(saved[0].alias, 'chat-tool')
})

test('modal creation appends and editing preserves neighboring saved aliases', () => {
  const saved = ['first', 'second'].map(alias => normalizeModelAlias({ alias, target_model: 'GLM' }))
  const draft = normalizeModelAlias({ alias: 'third', target_model: 'DeepSeek' })
  const created = mergeModelAliasDraft(saved, draft)
  assert.deepEqual(created.map(row => row.alias), ['first', 'second', 'third'])
  const edited = mergeModelAliasDraft(saved, draft, 1)
  assert.deepEqual(edited.map(row => row.alias), ['first', 'third'])
  assert.deepEqual(saved.map(row => row.alias), ['first', 'second'])
})

test('modal create and rename validate duplicates against the entire saved scope', () => {
  const saved = ['chat-tool', 'chat-fast'].map(alias => normalizeModelAlias({ alias, target_model: 'GLM' }))
  const draft = normalizeModelAlias({ alias: 'CHAT-TOOL', target_model: 'GLM' })
  assert.equal(validateModelAliases(mergeModelAliasDraft(saved, draft))?.code, 'duplicateAlias')
  assert.equal(validateModelAliases(mergeModelAliasDraft(saved, draft, 1))?.code, 'duplicateAlias')
  assert.equal(validateModelAliases(mergeModelAliasDraft(saved, draft, 0)), null)
  assert.deepEqual(saved.map(row => row.alias), ['chat-tool', 'chat-fast'])
})

test('legacy single target becomes an independent one-model pool with defaults', () => {
  const source = { alias: 'chat-tool', target_model: 'DeepSeek' }
  const row = normalizeModelAlias(source)
  assert.deepEqual(row.targets, [{ model: 'DeepSeek', weight: 1 }])
  assert.equal(row.session_ttl_seconds, 3600)
  assert.equal(row.failover_enabled, true)
  row.targets[0].model = 'GLM'
  assert.equal(source.target_model, 'DeepSeek')
})

test('pool targets win over the legacy field and serialize without response metadata', () => {
  const rows = [normalizeModelAlias({ alias: ' Chat-Tool ', target_model: 'old', targets: [{ model: ' GLM ', weight: 3 }, { model: 'DeepSeek', weight: 2 }], source: 'global', session_ttl_seconds: 120, failover_enabled: false })]
  assert.deepEqual(serializeModelAliases(rows), [{ alias: 'chat-tool', target_model: 'GLM', targets: [{ model: 'GLM', weight: 3 }, { model: 'DeepSeek', weight: 2 }], session_ttl_seconds: 120, failover_enabled: false }])
})

test('case-insensitive aliases cannot repeat while concrete models retain their case', () => {
  const row = name => normalizeModelAlias({ alias: name, targets: [{ model: 'GLM', weight: 1 }] })
  assert.equal(validateModelAliases([row('chat-tool'), row('CHAT-TOOL')], ['GLM'])?.code, 'duplicateAlias')
  assert.equal(validateModelAliases([row('chat-tool')], ['glm'])?.code, 'unavailableTarget')
})

test('pool validation rejects duplicate targets, invalid weights and unsafe session durations', () => {
  const row = normalizeModelAlias({ alias: 'chat-tool', targets: [{ model: 'GLM', weight: 1 }, { model: 'GLM', weight: 2 }] })
  assert.equal(validateModelAliases([row])?.code, 'duplicateTarget')
  row.targets = [{ model: 'GLM', weight: 0 }]
  assert.equal(validateModelAliases([row])?.code, 'invalidWeight')
  row.targets[0].weight = 1.5
  assert.equal(validateModelAliases([row])?.code, 'invalidWeight')
  row.targets[0].weight = 1
  row.session_ttl_seconds = 59
  assert.equal(validateModelAliases([row])?.code, 'invalidSessionTtl')
  row.session_ttl_seconds = 86400
  assert.equal(validateModelAliases([row]), null)
})

test('pool bounds and alias syntax are validated before saving', () => {
  const row = normalizeModelAlias({ alias: 'auto', target_model: 'GLM' })
  assert.equal(validateModelAliases([row])?.code, 'invalidAlias')
  row.alias = 'chat-tool'
  row.targets = Array.from({ length: 9 }, (_, index) => ({ model: `model-${index}`, weight: 1 }))
  assert.equal(validateModelAliases([row])?.code, 'targetCount')
  assert.equal(validateModelAliases(Array.from({ length: 51 }, () => row))?.code, 'aliasCount')
  assert.equal(validateModelAliases([]), null)
})

test('test requests retain a supplied session ID and omit the header for independent requests', () => {
  const request = buildAliasTestRequest('chat-tool', ' sk-test ', ' session-123 ')
  assert.equal(request.headers.Authorization, 'Bearer sk-test')
  assert.equal(request.headers['X-Session-ID'], 'session-123')
  assert.equal(JSON.parse(request.body).model, 'chat-tool')
  assert.equal(JSON.parse(request.body).stream, false)
  assert.equal(buildAliasTestRequest('chat-tool', 'sk-test', '').headers['X-Session-ID'], undefined)
})
