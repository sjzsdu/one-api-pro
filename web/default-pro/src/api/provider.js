// provider.js 供应商统计与目录接口聚合模块
// Provider statistics and catalogue API module.
// 版本: v0.0.28
// 日期: 2026-11-25
import api from './index'

// 仅追加有意义的过滤参数，避免 ?range= 这种空值污染 query。
// Only forward truthy params to keep query strings clean.
function buildParams(extras = {}) {
  const out = {}
  for (const [k, v] of Object.entries(extras)) {
    if (v === 0 || (v !== '' && v !== null && v !== undefined)) {
      out[k] = v
    }
  }
  return out
}

export const providerApi = {
  // 供应商列表：用量、渠道概况、免费模型与未定价模型。
  // Provider list: usage, channel overview, free and unpriced models.
  // range: today | 7d | 30d | all（默认 7d）
  list: (range) => api.get('/api/admin/providers/', { params: buildParams({ range }) }),

  // 单个供应商详情：渠道、模型用量、免费模型与趋势。
  // Provider detail: channels, model usage, free models and trend.
  detail: (id, range) => api.get(`/api/admin/providers/${id}`, { params: buildParams({ range }) }),

  // 保存供应商策展资料（简介 / 注册地址 / 免费政策等）。
  // Save the curated provider profile.
  saveProfile: (id, payload) => api.put(`/api/admin/providers/${id}/profile`, payload),

  // 免费模型清单；providerId 为空时返回全部。
  // Free model catalogue; an empty providerId returns every entry.
  listFreeModels: (providerId) => api.get('/api/admin/free-models/', { params: buildParams({ provider_id: providerId }) }),

  // 新增或更新一条免费模型记录。
  // Create or update one free model record.
  saveFreeModel: (payload) => api.put('/api/admin/free-models/', payload),

  // 删除一条免费模型记录。
  // Delete one free model record.
  deleteFreeModel: (providerId, modelName) =>
    api.delete('/api/admin/free-models/', { params: { provider_id: providerId, model_name: modelName } }),

  // 按名称后缀识别疑似免费模型。
  // Detect candidate free models by name suffix.
  detectFreeModels: (modelNames) => api.post('/api/admin/free-models/detect', { model_names: modelNames }),

  // 当前用户按供应商拆分的用量。
  // The current user's usage grouped by provider.
  myUsage: (start, end) => api.get('/api/user/provider-usage', { params: buildParams({ start, end }) }),
}

export default providerApi
