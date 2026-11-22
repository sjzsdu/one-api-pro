// modelAliases.js 用户及全局模型别名接口
// Provides scoped alias settings, candidate metadata, and effective model pools.
// 版本: v0.0.28
// 日期: 2026-11-22
// 作者: Codex
import api from './index'

// modelAliasApi keeps personal and root-only global settings on separate endpoints.
// 版本: v0.0.28
// 日期: 2026-11-22
export const modelAliasApi = {
  list: scope => api.get(scope === 'global' ? '/api/model_alias/global' : '/api/user/model_aliases'),
  save: (scope, aliases) => api.put(scope === 'global' ? '/api/model_alias/global' : '/api/user/model_aliases', { aliases }),
  targets: scope => api.get(scope === 'global' ? '/api/model_alias/global/targets' : '/api/user/model_alias_targets'),
  effective: () => api.get('/api/user/model_aliases/effective'),
}
