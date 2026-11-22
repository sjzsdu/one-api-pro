// modelAliases.js 模型池表单的兼容、校验和测试请求
// Normalizes legacy aliases and validates session-aware weighted model pools.
// 版本: v0.0.28
// 日期: 2026-11-22
// 作者: Codex

// normalizeModelAlias builds an editable pool without mutating the API response.
// 版本: v0.0.28
// 日期: 2026-11-22
export function normalizeModelAlias(item = {}) {
  const candidates = Array.isArray(item.targets) && item.targets.length
    ? item.targets
    : [{ model: item.target_model || '', weight: 1 }]
  return {
    alias: item.alias || '',
    targets: candidates.map(target => ({ model: target.model || '', weight: target.weight ?? 1 })),
    failover_enabled: item.failover_enabled ?? true,
    session_ttl_seconds: item.session_ttl_seconds || 3600,
  }
}

// serializeModelAliases emits the canonical pool and its legacy first-target field.
// 版本: v0.0.28
// 日期: 2026-11-22
export function serializeModelAliases(rows) {
  return rows.map(row => {
    const targets = row.targets.map(target => ({ model: target.model.trim(), weight: target.weight }))
    return {
      alias: row.alias.trim().toLowerCase(),
      target_model: targets[0]?.model || '',
      targets,
      failover_enabled: row.failover_enabled,
      session_ttl_seconds: row.session_ttl_seconds,
    }
  })
}

// validateModelAliases returns a localizable error for invalid or unavailable pool settings.
// 版本: v0.0.28
// 日期: 2026-11-22
export function validateModelAliases(rows, availableModels) {
  if (rows.length > 50) return { code: 'aliasCount' }
  const aliases = new Set()
  const available = availableModels ? new Set(availableModels) : null
  for (let index = 0; index < rows.length; index++) {
    const row = rows[index], alias = row.alias.trim().toLowerCase()
    const error = code => ({ code, index: index + 1 })
    if (!/^[a-z][a-z0-9._-]{0,63}$/.test(alias) || alias === 'auto') return error('invalidAlias')
    if (aliases.has(alias)) return error('duplicateAlias')
    aliases.add(alias)
    if (!Array.isArray(row.targets) || !row.targets.length || row.targets.length > 8) return error('targetCount')
    const seen = new Set()
    for (const target of row.targets) {
      const name = target.model.trim()
      if (!name) return error('missingTarget')
      if (seen.has(name)) return error('duplicateTarget')
      seen.add(name)
      if (available && !available.has(name)) return error('unavailableTarget')
      if (!Number.isInteger(target.weight) || target.weight < 1 || target.weight > 1000) return error('invalidWeight')
    }
    if (!Number.isInteger(row.session_ttl_seconds) || row.session_ttl_seconds < 60 || row.session_ttl_seconds > 86400) return error('invalidSessionTtl')
  }
  return null
}

// buildAliasTestRequest keeps session affinity optional while using normal API authentication.
// 版本: v0.0.28
// 日期: 2026-11-22
export function buildAliasTestRequest(alias, token, sessionId) {
  const headers = { 'Content-Type': 'application/json', Authorization: `Bearer ${token.trim()}` }
  if (sessionId.trim()) headers['X-Session-ID'] = sessionId.trim()
  return {
    method: 'POST',
    headers,
    body: JSON.stringify({ model: alias, messages: [{ role: 'user', content: 'Reply with OK.' }], max_tokens: 32, stream: false }),
  }
}
