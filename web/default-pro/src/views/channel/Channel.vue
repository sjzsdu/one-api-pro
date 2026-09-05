<template>
  <div class="channel-page">
    <!-- 顶部欢迎条 -->
    <div class="welcome-bar">
      <div class="welcome-text">
        <h1 class="welcome-title">渠道</h1>
        <p class="welcome-desc">管理 API 渠道配置，启用后可在令牌中调用</p>
      </div>
      <div class="welcome-meta">
        <span class="meta-chip">共 {{ total }} 条</span>
      </div>
    </div>

    <!-- 独立搜索栏 -->
    <div class="search-card">
      <div class="search-left">
        <a-input-search
          v-model="searchKeyword"
          placeholder="搜索渠道名称..."
          allow-clear
          @search="handleSearch"
          @clear="handleSearchClear"
          :style="{ width: '320px' }"
        />
      </div>
      <div class="search-right">
        <a-button @click="handleTestAll" :loading="testingAll">测试全部</a-button>
        <a-button @click="handleUpdateBalance" :loading="updatingBalance">更新余额</a-button>
        <a-popconfirm content="确认删除所有已禁用的渠道？" @ok="handleDeleteDisabled">
          <a-button status="danger" :loading="deletingDisabled">删除已禁用</a-button>
        </a-popconfirm>
        <a-button type="primary" size="large" @click="openCreateModal">
          <template #icon><icon-plus :size="14" /></template>
          添加渠道
        </a-button>
      </div>
    </div>

    <!-- 列表 -->
    <div class="list-wrap">
      <div v-if="pageItems.length === 0 && !loading && !loadingMore" class="empty-state">
        <div class="empty-icon">
          <icon-apps :size="32" />
        </div>
        <p class="empty-title">还没有任何渠道</p>
        <p class="empty-desc">添加第一个 API 渠道即可开始接入</p>
        <a-button type="primary" @click="openCreateModal">
          <template #icon><icon-plus :size="14" /></template>
          立即添加
        </a-button>
      </div>

      <div v-else class="list-body">
        <div class="list-head">
          <div class="col">ID</div>
          <div class="col">类型</div>
          <div class="col">名称</div>
          <div class="col">Base URL</div>
          <div class="col">模型</div>
          <div class="col">分组</div>
          <div class="col">状态</div>
          <div class="col">响应时间</div>
          <div class="col">降级渠道</div>
          <div class="col col-action">操作</div>
        </div>

        <a-spin :loading="loading && !loadingMore" style="width: 100%">
          <div v-for="c in pageItems" :key="c.id" class="list-row">
            <div class="col"><span class="cell-mono">#{{ c.id }}</span></div>

            <div class="col">
              <a-tag :color="typeColorMap[c.type] || 'gray'" size="small">
                {{ typeNameMap[c.type] || `Type ${c.type}` }}
              </a-tag>
            </div>

            <div class="col">
              <span class="cell-strong ellipsis" :title="c.name">{{ c.name }}</span>
            </div>

            <div class="col">
              <code class="cell-mono ellipsis" :title="c.base_url">{{ c.base_url || '-' }}</code>
            </div>

            <div class="col">
              <span class="cell-muted ellipsis" :title="c.models">{{ c.models || '-' }}</span>
            </div>

            <div class="col">
              <span class="cell-muted ellipsis" :title="c.group">{{ c.group || '-' }}</span>
            </div>

            <div class="col">
              <span class="status-chip" :class="statusClass(c.status)">
                <span class="status-dot"></span>
                {{ statusText(c.status) }}
              </span>
            </div>

            <div class="col">
              <span class="cell-mono">{{ c.response_time ? `${c.response_time}ms` : '-' }}</span>
            </div>

            <div class="col">
              <div v-if="c.is_fallback" class="fallback-cell">
                <a-tooltip :content="$t('channel.fallbackHint')">
                  <a-tag color="orangered" size="small" class="fallback-tag">
                    <template #icon><icon-swap :size="12" /></template>
                    {{ $t('channel.fallbackTag') }}
                  </a-tag>
                </a-tooltip>
                <span v-if="c.fallback_priority" class="fallback-priority" :title="$t('channel.fallbackPriorityHint')">
                  P{{ c.fallback_priority }}
                </span>
              </div>
              <span v-else class="cell-muted">-</span>
            </div>

            <div class="col col-action">
              <a-button type="text" size="small" @click="openEditModal(c)">编辑</a-button>
              <a-popconfirm
                :content="c.status === 1 ? '确定禁用该渠道？' : '确定启用该渠道？'"
                @ok="handleToggleStatus(c)"
              >
                <a-button type="text" size="small">
                  {{ c.status === 1 ? '禁用' : '启用' }}
                </a-button>
              </a-popconfirm>
              <a-button type="text" size="small" :loading="testingIds.includes(c.id)" @click="handleTest(c)">测试</a-button>
              <a-popconfirm content="确定删除该渠道？" @ok="handleDelete(c.id)">
                <a-button type="text" size="small" class="danger-btn">删除</a-button>
              </a-popconfirm>
            </div>
          </div>
        </a-spin>

        <div v-if="loadingMore" class="load-more-row">
          <a-spin :loading="true" :size="14" />
          <span class="load-more-text">正在加载更多…</span>
        </div>
        <div
          v-else-if="isReachedEnd && channels.length > pageSize && !loading"
          class="load-end-row"
        >
          已显示全部 {{ channels.length }} 条数据
        </div>
      </div>

      <div v-if="channels.length > 0" class="list-footer">
        <a-pagination
          :current="activePage"
          :total="totalCountForPager"
          :page-size="pageSize"
          show-total
          show-page-size
          :page-size-options="[10, 20, 50]"
          size="small"
          @change="onPaginationChange"
          @page-size-change="onPageSizeChange"
        />
      </div>
    </div>

    <!-- 编辑 / 新建弹窗 -->
    <a-modal
      v-model:visible="modalVisible"
      :title="modalTitle"
      :width="560"
      @ok="handleSubmit"
      @cancel="closeModal"
      :ok-loading="submitting"
      ok-text="保存"
      cancel-text="取消"
    >
      <a-form ref="formRef" :model="form" layout="vertical" class="channel-form">
        <a-form-item field="type" label="类型" required>
          <a-select v-model="form.type" placeholder="选择渠道类型">
            <a-option v-for="(name, key) in typeNameMap" :key="key" :value="Number(key)" :label="name" />
          </a-select>
        </a-form-item>
        <a-form-item field="name" label="名称" required>
          <a-input v-model="form.name" placeholder="渠道名称" allow-clear />
        </a-form-item>
        <a-form-item field="base_url" label="Base URL">
          <a-input v-model="form.base_url" placeholder="https://api.openai.com" allow-clear />
        </a-form-item>
        <a-form-item field="models" label="模型">
          <div class="model-picker">
            <div class="model-picker-header">
              <span class="model-picker-title">
                {{ visibleModelOptions.length ? `选择模型（已选 ${selectedModels.length} 个）` : '暂未加载模型列表' }}
              </span>
              <a-space>
                <a-button v-if="form.type === 20 || form.type === 52" size="small" :loading="refreshingModels" @click="refreshChannelModels">
                  刷新模型
                </a-button>
                <a-button size="small" :loading="validatingModels" @click="validateChannelModels">
                  校验模型
                </a-button>
              </a-space>
            </div>
            <div v-if="visibleModelOptions.length" class="model-options">
              <a-checkbox
                v-for="model in visibleModelOptions"
                :key="model"
                :model-value="selectedModels.includes(model)"
                @change="toggleModel(model, $event)"
              >
                {{ model }}
              </a-checkbox>
            </div>
            <div v-else class="model-empty">点击“刷新模型”获取 Provider 模型，或手动添加模型。</div>
            <div class="model-manual">
              <a-input
                v-model="manualModels"
                placeholder="手动添加模型，多个用逗号分隔"
                allow-clear
                @press-enter="addManualModels"
              />
              <a-button @click="addManualModels">添加</a-button>
            </div>
            <div class="form-hint">可枚举的 Provider 会自动过滤不存在的模型；Codex OAuth 等特殊 Provider 会保留为待验证候选。</div>
          </div>
        </a-form-item>
        <a-form-item field="group" label="分组">
          <a-input v-model="form.group" placeholder="多个分组用逗号分隔" allow-clear />
        </a-form-item>
        <a-form-item field="key" label="密钥" :required="!isEdit">
          <a-input-password v-model="form.key" :placeholder="isEdit ? '留空表示不修改密钥' : 'API Key'" />
          <span v-if="isEdit" class="form-hint">留空表示不修改密钥</span>
        </a-form-item>
        <a-form-item v-if="form.type === 52" label="OpenAI 登录">
          <a-button :loading="oauthLoading" @click="startOpenAIOAuth">使用 OpenAI 设备码登录</a-button>
          <div v-if="oauthFlow" class="form-hint">
            请在新窗口完成授权；状态：{{ oauthFlow.status }}
            <a-link v-if="oauthFlow.verify_url" :href="oauthFlow.verify_url" target="_blank">打开授权页</a-link>
            <span v-if="oauthFlow.user_code">，设备码：{{ oauthFlow.user_code }}</span>
          </div>
        </a-form-item>
        <a-divider :margin="6" />
        <a-form-item field="is_fallback" :label="$t('channel.fallback')">
          <a-switch v-model="form.is_fallback" />
          <span class="form-hint">{{ $t('channel.fallbackHint') }}</span>
        </a-form-item>
        <a-form-item v-if="form.is_fallback" field="fallback_priority" :label="$t('channel.fallbackPriority')">
          <a-input-number v-model="form.fallback_priority" :min="0" :step="1" />
          <span class="form-hint">{{ $t('channel.fallbackPriorityHint') }}</span>
        </a-form-item>
      </a-form>
    </a-modal>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { Message } from '@arco-design/web-vue'
import { IconPlus, IconApps, IconSwap } from '@arco-design/web-vue/es/icon'
import api from '@/api'

const defaultOpenAICodexModels = [
  'gpt-5.6',
  'gpt-5.6-sol',
  'gpt-5.6-terra',
  'gpt-5.6-luna',
  'gpt-5.5',
  'gpt-5.4',
  'gpt-5.4-mini',
  'gpt-5.4-nano',
  'gpt-5.3-codex',
  'gpt-5.2-codex',
  'gpt-5.1-codex-max',
  'gpt-5.1-codex-mini',
  'gpt-5-codex',
  'codex-mini-latest',
]

// 渠道类型映射（对应后端 relay/registry 的 LegacyType）
const typeNameMap = {
  1: 'OpenAI', 50: 'OpenAI 兼容', 14: 'Anthropic', 33: 'AWS',
  3: 'Azure', 11: 'PaLM2', 24: 'Gemini', 51: 'Gemini (OpenAI)',
  28: 'Mistral AI', 41: 'Novita', 40: '字节火山引擎', 15: '百度文心千帆',
  47: '百度文心千帆 V2', 17: '阿里通义千问', 49: '阿里云百炼',
  18: '讯飞星火认知', 48: '讯飞星火认知 V2', 16: '智谱 ChatGLM',
  19: '360 智脑', 25: 'Moonshot AI', 23: '腾讯混元', 26: '百川大模型',
  27: 'MiniMax', 29: 'Groq', 30: 'Ollama', 31: '零一万物',
  32: '阶跃星辰', 34: 'Coze', 35: 'Cohere', 36: 'DeepSeek',
  37: 'Cloudflare', 38: 'DeepL', 39: 'together.ai', 42: 'VertexAI',
  43: 'Proxy', 44: 'SiliconFlow', 45: 'xAI', 46: 'Replicate',
  52: 'OpenAI OAuth (Codex)',
  8: '自定义渠道', 22: '知识库：FastGPT', 21: '知识库：AI Proxy',
  20: 'OpenRouter', 2: '代理：API2D', 5: '代理：OpenAI-SB',
  7: '代理：OhMyGPT', 10: '代理：AI Proxy', 4: '代理：CloseAI',
  6: '代理：OpenAI Max', 9: '代理：AI.LS', 12: '代理：API2GPT',
  13: '代理：AIGC2D',
}
const typeColorMap = {
  1: 'arcoblue', 50: 'green', 14: 'gray', 33: 'gray',
  3: 'cyan', 11: 'orange', 24: 'orange', 51: 'orange',
  28: 'orange', 41: 'purple', 40: 'arcoblue', 15: 'arcoblue',
  47: 'arcoblue', 17: 'orange', 49: 'orange',
  18: 'arcoblue', 48: 'arcoblue', 16: 'purple',
  19: 'arcoblue', 25: 'gray', 23: 'green', 26: 'orange',
  27: 'red', 29: 'orange', 30: 'gray', 31: 'green',
  32: 'arcoblue', 34: 'arcoblue', 35: 'arcoblue', 36: 'gray',
  37: 'orange', 38: 'gray', 39: 'arcoblue', 42: 'arcoblue',
  43: 'arcoblue', 44: 'arcoblue', 45: 'arcoblue', 46: 'arcoblue',
  8: 'pinkpurple', 22: 'arcoblue', 21: 'purple',
  52: 'green',
  20: 'gray', 2: 'arcoblue', 5: 'gold', 7: 'purple',
  10: 'purple', 4: 'cyan', 6: 'purple', 9: 'gold', 12: 'arcoblue',
  13: 'purple',
}

// 渠道状态（对齐后端 model/channel.go）：1=启用 2=手动禁用 3=自动禁用
function statusText(status) {
  if (status === 1) return '已启用'
  if (status === 2) return '已禁用'
  if (status === 3) return '自动禁用'
  return '未知'
}
function statusClass(status) {
  if (status === 1) return 'status-on'
  if (status === 3) return 'status-warn'
  return 'status-off'
}

const loading = ref(false)
const loadingMore = ref(false)
const isReachedEnd = ref(false)
const submitting = ref(false)
const channels = ref([])
const total = computed(() => channels.value.length)
const searchKeyword = ref('')
const activePage = ref(1)
const pageSize = ref(10)
const modalVisible = ref(false)
const isEdit = ref(false)
const editingId = ref(null)
const testingAll = ref(false)
const updatingBalance = ref(false)
const deletingDisabled = ref(false)
const testingIds = ref([])
const oauthLoading = ref(false)
const oauthFlow = ref(null)
const refreshingModels = ref(false)
const validatingModels = ref(false)
const modelOptions = ref([])
const manualModels = ref('')

const formRef = ref(null)
const form = reactive({
  type: 1,
  name: '',
  base_url: '',
  models: '',
  group: '',
  key: '',
  is_fallback: false,
  fallback_priority: 0,
})

const modalTitle = ref('添加渠道')

const selectedModels = computed({
  get: () => parseModelNames(form.models),
  set: (models) => {
    form.models = mergeModelNames(models).join(',')
  },
})

const visibleModelOptions = computed(() => mergeModelNames(modelOptions.value, selectedModels.value))

function parseModelNames(value) {
  const values = Array.isArray(value) ? value : String(value || '').split(',')
  return mergeModelNames(values)
}

function mergeModelNames(...groups) {
  const result = []
  const seen = new Set()
  groups.flat().forEach((value) => {
    const model = String(value || '').trim()
    if (model && !seen.has(model)) {
      seen.add(model)
      result.push(model)
    }
  })
  return result
}

function toggleModel(model, checked) {
  const selected = new Set(selectedModels.value)
  if (checked) selected.add(model)
  else selected.delete(model)
  selectedModels.value = Array.from(selected)
}

function addManualModels() {
  const additions = parseModelNames(manualModels.value)
  if (!additions.length) {
    Message.warning('请输入至少一个模型名')
    return
  }
  modelOptions.value = mergeModelNames(modelOptions.value, additions)
  selectedModels.value = mergeModelNames(selectedModels.value, additions)
  manualModels.value = ''
}

const pageItems = computed(() => {
  const start = (activePage.value - 1) * pageSize.value
  return channels.value.slice(start, start + pageSize.value)
})

const totalCountForPager = computed(() => {
  if (isReachedEnd.value) return channels.value.length
  return channels.value.length + pageSize.value
})

async function fetchChannels({ append = false, pageIdx = 0 } = {}) {
  if (append) loadingMore.value = true
  else loading.value = true
  try {
    const params = { p: pageIdx, size: pageSize.value }
    const { data } = await api.get('/api/channel/', { params })
    if (data.success) {
      const list = data.data || []
      if (append) {
        if (list.length === 0) {
          isReachedEnd.value = true
        } else {
          channels.value = [...channels.value, ...list]
          if (list.length < pageSize.value) isReachedEnd.value = true
        }
      } else {
        channels.value = list
        activePage.value = 1
        isReachedEnd.value = list.length < pageSize.value
      }
    } else {
      Message.error(data.message || '加载失败')
    }
  } catch (e) {
    Message.error(e.response?.data?.message || e.message || '加载失败')
  } finally {
    loading.value = false
    loadingMore.value = false
  }
}

function handleSearch() {
  if (!searchKeyword.value) { fetchChannels({ append: false }); return }
  loading.value = true
  api.get('/api/channel/search', { params: { keyword: searchKeyword.value } })
    .then(({ data }) => {
      if (data.success) {
        channels.value = data.data || []
        activePage.value = 1
        isReachedEnd.value = true
      }
    })
    .catch((e) => Message.error(e.response?.data?.message || e.message || '搜索失败'))
    .finally(() => { loading.value = false })
}

function handleSearchClear() {
  searchKeyword.value = ''
  isReachedEnd.value = false
  fetchChannels({ append: false })
}

function onPaginationChange(page) {
  activePage.value = page
  const totalPages = Math.ceil(channels.value.length / pageSize.value)
  if (page > totalPages && !isReachedEnd.value && !loadingMore.value && !searchKeyword.value) {
    const nextPageIdx = totalPages
    fetchChannels({ append: true, pageIdx: nextPageIdx })
  }
}

function onPageSizeChange(s) {
  pageSize.value = s
  activePage.value = 1
}

function openCreateModal() {
  isEdit.value = false
  editingId.value = null
  form.type = 1
  form.name = ''
  form.base_url = ''
  form.models = ''
  form.group = ''
  form.key = ''
  form.is_fallback = false
  form.fallback_priority = 0
  modelOptions.value = []
  manualModels.value = ''
  modalTitle.value = '添加渠道'
  modalVisible.value = true
}

function openEditModal(record) {
  isEdit.value = true
  editingId.value = record.id
  form.type = record.type || 1
  form.name = record.name || ''
  form.base_url = record.base_url || ''
  form.models = record.models || ''
  form.group = record.group || ''
  form.key = record.key || ''
  form.is_fallback = !!record.is_fallback
  form.fallback_priority = record.fallback_priority || 0
  modelOptions.value = parseModelNames(form.models)
  if (form.type === 52) {
    modelOptions.value = mergeModelNames(defaultOpenAICodexModels, modelOptions.value)
  }
  manualModels.value = ''
  modalTitle.value = '编辑渠道'
  modalVisible.value = true
}

function closeModal() {
  modalVisible.value = false
  formRef.value?.clearValidate()
}

async function handleSubmit() {
  const errors = await formRef.value?.validate()
  if (errors) return
  submitting.value = true
  try {
    const payload = {
      type: form.type,
      name: form.name,
      base_url: form.base_url,
      models: form.models,
      group: form.group,
      key: form.key,
      is_fallback: form.is_fallback,
      fallback_priority: form.fallback_priority,
    }
    let res
    if (isEdit.value) {
      payload.id = editingId.value
      res = await api.put('/api/channel/', payload)
    } else {
      res = await api.post('/api/channel/', payload)
    }
    if (res.data.success) {
      Message.success(isEdit.value ? '渠道已更新' : '渠道已添加')
      closeModal()
      fetchChannels()
    } else {
      Message.error(res.data.message || '操作失败')
    }
  } catch (e) {
    Message.error(e.response?.data?.message || e.message || '操作失败')
  } finally {
    submitting.value = false
  }
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

async function refreshChannelModels() {
  refreshingModels.value = true
  try {
    const { data } = await api.post('/api/channel/models/refresh', {
      id: editingId.value || 0,
      type: form.type,
      key: form.key,
      base_url: form.base_url,
    })
    if (!data.success) throw new Error(data.message || '模型刷新失败')
    let models = data.data?.models || []
    if (!models.length && form.type === 52) {
      models = defaultOpenAICodexModels
    }
    modelOptions.value = mergeModelNames(models)
    form.models = modelOptions.value.join(',')
    const count = modelOptions.value.length
    if (data.data?.source_url === 'builtin://openaicodex') {
      Message.success(`已加载内置 Codex 模型目录（${count} 个候选模型）`)
    } else {
      Message.success(`已刷新 ${count} 个模型`)
    }
  } catch (e) {
    Message.error(e.response?.data?.message || e.message || '模型刷新失败')
  } finally {
    refreshingModels.value = false
  }
}

async function validateChannelModels() {
  const models = form.models
    .split(',')
    .map((model) => model.trim())
    .filter(Boolean)
  if (!models.length) {
    Message.warning('请先填写至少一个模型名')
    return
  }

  validatingModels.value = true
  try {
    const { data } = await api.post('/api/channel/models/validate', {
      id: editingId.value || 0,
      type: form.type,
      key: form.key,
      base_url: form.base_url,
      models: models.join(','),
    })
    if (!data.success) throw new Error(data.message || '模型校验失败')

    const result = data.data || {}
    if (result.verification_enabled) {
      const validModels = result.valid_models || []
      const invalidModels = result.invalid_models || []
      modelOptions.value = mergeModelNames(validModels)
      form.models = validModels.join(',')
      if (invalidModels.length) {
        Message.warning(`已移除 ${invalidModels.length} 个不可用模型：${invalidModels.join(', ')}`)
      } else {
        Message.success(`模型校验通过，共 ${validModels.length} 个`)
      }
    } else {
      Message.warning(data.message || '上游未提供模型目录，当前模型仅作为候选项保留')
    }
  } catch (e) {
    Message.error(e.response?.data?.message || e.message || '模型校验失败')
  } finally {
    validatingModels.value = false
  }
}

async function startOpenAIOAuth() {
  oauthLoading.value = true
  oauthFlow.value = null
  const popup = window.open('about:blank', '_blank')
  try {
    const { data } = await api.post('/api/oauth/openai/login', { method: 'device_code' })
    if (!data.success) throw new Error(data.message || 'OpenAI 登录启动失败')
    oauthFlow.value = data.data
    if (popup && data.data?.verify_url) popup.location.href = data.data.verify_url
    const interval = Math.max(Number(data.data?.interval || 5), 1) * 1000
    for (let attempt = 0; attempt < 180; attempt += 1) {
      await sleep(interval)
      const result = await api.post(`/api/oauth/openai/flows/${data.data.flow_id}/poll`)
      if (!result.data.success) throw new Error(result.data.message || 'OpenAI 登录轮询失败')
      const flow = result.data.data
      oauthFlow.value = flow
      if (flow.status === 'success') {
        form.key = flow.credential || ''
        if (!form.models) form.models = defaultOpenAICodexModels.join(',')
        modelOptions.value = mergeModelNames(defaultOpenAICodexModels, modelOptions.value, selectedModels.value)
        Message.success('OpenAI OAuth 登录成功，请保存渠道')
        return
      }
      if (flow.status === 'error' || flow.status === 'expired') {
        throw new Error(flow.error || 'OpenAI 登录未完成')
      }
    }
    throw new Error('OpenAI 登录超时，请重试')
  } catch (e) {
    if (popup && !popup.closed) popup.close()
    Message.error(e.response?.data?.message || e.message || 'OpenAI 登录失败')
  } finally {
    oauthLoading.value = false
  }
}

async function handleDelete(id) {
  try {
    const { data } = await api.delete(`/api/channel/${id}/`)
    if (data.success) {
      Message.success('渠道已删除')
      fetchChannels()
    } else {
      Message.error(data.message || '删除失败')
    }
  } catch (e) {
    Message.error(e.response?.data?.message || e.message || '删除失败')
  }
}

async function handleTest(record) {
  testingIds.value.push(record.id)
  try {
    // 单个测试：GET /api/channel/test/:id?model=，返回 time（秒）
    const model = record.models ? record.models.split(',')[0] : ''
    const { data } = await api.get(`/api/channel/test/${record.id}`, { params: { model } })
    if (data.success) {
      Message.success(`测试通过 · 耗时 ${data.time}s`)
      fetchChannels()
    } else {
      Message.error(data.message || '测试失败')
    }
  } catch (e) {
    Message.error(e.response?.data?.message || e.message || '测试失败')
  } finally {
    testingIds.value = testingIds.value.filter((id) => id !== record.id)
  }
}

async function handleTestAll() {
  testingAll.value = true
  try {
    // 全部测试：GET /api/channel/test?scope=all（异步启动）
    const { data } = await api.get('/api/channel/test', { params: { scope: 'all' } })
    if (data.success) {
      Message.info('全部渠道测试已开始，完成后将自动通知')
    } else {
      Message.error(data.message || '批量测试失败')
    }
  } catch (e) {
    Message.error(e.response?.data?.message || e.message || '批量测试失败')
  } finally {
    testingAll.value = false
  }
}

async function handleToggleStatus(record) {
  const newStatus = record.status === 1 ? 2 : 1
  try {
    const { data } = await api.put('/api/channel/', { id: record.id, status: newStatus })
    if (data.success) {
      Message.success(newStatus === 1 ? '渠道已启用' : '渠道已禁用')
      record.status = newStatus
    } else {
      Message.error(data.message || '操作失败')
    }
  } catch (e) {
    Message.error(e.response?.data?.message || e.message || '操作失败')
  }
}

async function handleUpdateBalance() {
  updatingBalance.value = true
  try {
    // 全部更新余额：GET /api/channel/update_balance
    const { data } = await api.get('/api/channel/update_balance')
    if (data.success) {
      Message.success('余额更新已开始')
      fetchChannels()
    } else {
      Message.error(data.message || '更新余额失败')
    }
  } catch (e) {
    Message.error(e.response?.data?.message || e.message || '更新余额失败')
  } finally {
    updatingBalance.value = false
  }
}

async function handleDeleteDisabled() {
  deletingDisabled.value = true
  try {
    const { data } = await api.delete('/api/channel/disabled')
    if (data.success) {
      Message.success('已删除所有禁用的渠道')
      fetchChannels()
    } else {
      Message.error(data.message || '删除失败')
    }
  } catch (e) {
    Message.error(e.response?.data?.message || e.message || '删除失败')
  } finally {
    deletingDisabled.value = false
  }
}

onMounted(() => {
  fetchChannels()
})
</script>

<style scoped>
.channel-page {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

/* ============ 顶部欢迎条 ============ */
.welcome-bar {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  padding: 4px 4px 0;
}
.welcome-title {
  font-size: 24px;
  font-weight: 600;
  color: var(--color-text-1);
  margin: 0 0 4px;
  letter-spacing: -0.2px;
}
.welcome-desc {
  font-size: 13px;
  color: var(--color-text-3);
  margin: 0;
}
.welcome-meta {
  display: flex;
  gap: 6px;
}
.meta-chip {
  font-size: 12px;
  color: var(--color-text-3);
  background: var(--color-fill-2);
  padding: 3px 10px;
  border-radius: 4px;
}

/* ============ 搜索栏 ============ */
.search-card {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 14px 20px;
  background: var(--color-bg-2);
  border: 1px solid var(--color-border-2);
  border-radius: 8px;
}
.search-left,
.search-right {
  display: flex;
  align-items: center;
  gap: 8px;
}

/* ============ 列表（与 Token 一致） ============ */
.list-wrap {
  background: var(--color-bg-2);
  border: 1px solid var(--color-border-2);
  border-radius: 8px;
  overflow: hidden;
}
.list-body {
  padding: 0;
  overflow-x: auto;
}
.list-head,
.list-row {
  display: grid;
  grid-template-columns: 80px 140px 160px 220px 180px 110px 110px 100px 130px 260px;
  align-items: center;
  padding: 0 20px;
  min-width: max-content;
}
.list-head {
  height: 40px;
  background: var(--color-fill-1);
  border-bottom: 1px solid var(--color-fill-3);
  font-size: 12px;
  font-weight: 500;
  color: var(--color-text-3);
}
.list-row {
  min-height: 52px;
  border-bottom: 1px solid var(--color-fill-3);
  transition: background 0.15s;
}
.list-row:last-child {
  border-bottom: none;
}
.list-row:hover {
  background: var(--color-fill-1);
}

/* ============ 单元格 ============ */
.col {
  font-size: 13px;
  color: var(--color-text-2);
  min-width: 0;
  padding-right: 16px;
}
.col:last-child {
  padding-right: 0;
}
.col-action {
  display: flex;
  justify-content: flex-end;
  gap: 0;
}
.col-action :deep(.arco-btn) {
  padding: 0 6px;
}

.cell-mono {
  font-family: 'SF Mono', Menlo, Consolas, monospace;
  font-size: 12px;
  color: var(--color-text-2);
  font-variant-numeric: tabular-nums;
}
.cell-strong {
  color: var(--color-text-1);
  font-weight: 500;
}
.cell-muted {
  color: var(--color-text-3);
}
.ellipsis {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 100%;
}

/* ============ 状态 chip ============ */
.status-chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 2px 10px;
  border-radius: 10px;
  font-size: 12px;
  font-weight: 500;
  width: max-content;
}
.status-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
}
.status-on {
  background: rgba(0, 180, 42, 0.08);
  color: #00b42a;
}
.status-on .status-dot {
  background: #00b42a;
}
.status-off {
  background: var(--color-fill-2);
  color: var(--color-text-3);
}
.status-off .status-dot {
  background: var(--color-text-4);
}
.status-warn {
  background: rgba(245, 63, 63, 0.08);
  color: #f53f3f;
}
.status-warn .status-dot {
  background: #f53f3f;
}

/* ============ 操作列 ============ */
.danger-btn {
  color: var(--color-text-2);
}
.danger-btn:hover {
  color: #f53f3f !important;
  background: rgba(245, 63, 63, 0.06) !important;
}

/* ============ 分页 ============ */
.list-footer {
  display: flex;
  justify-content: flex-end;
  padding: 14px 20px;
  border-top: 1px solid var(--color-fill-3);
}

/* ============ 追加加载 / 末尾提示 ============ */
.load-more-row,
.load-end-row {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 14px 20px;
  font-size: 12px;
  color: var(--color-text-3);
  border-top: 1px dashed var(--color-fill-3);
}
.load-more-text {
  color: var(--color-text-3);
}
.load-end-row {
  color: var(--color-text-4);
  background: var(--color-fill-1);
  border-top: 1px solid var(--color-fill-3);
}

/* ============ 空状态 ============ */
.empty-state {
  padding: 80px 20px;
  text-align: center;
}
.empty-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 56px;
  height: 56px;
  border-radius: 14px;
  background: var(--color-fill-2);
  color: var(--color-text-3);
  margin-bottom: 12px;
}
.empty-title {
  font-size: 14px;
  font-weight: 500;
  color: var(--color-text-1);
  margin: 0 0 4px;
}
.empty-desc {
  font-size: 13px;
  color: var(--color-text-3);
  margin: 0 0 16px;
}

/* ============ 表单 ============ */
.channel-form :deep(.arco-form-item) {
  margin-bottom: 16px;
}
.channel-form :deep(.arco-form-item-label) {
  font-weight: 500;
  font-size: 13px;
  color: var(--color-text-2);
}
.model-picker {
  width: 100%;
  padding: 12px;
  border: 1px solid var(--color-border-2);
  border-radius: 6px;
  background: var(--color-fill-1);
  box-sizing: border-box;
}
.model-picker-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 10px;
}
.model-picker-title {
  font-size: 12px;
  color: var(--color-text-2);
}
.model-options {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px 12px;
  max-height: 220px;
  overflow-y: auto;
  padding: 10px;
  margin-bottom: 10px;
  background: var(--color-bg-2);
  border: 1px solid var(--color-border-2);
  border-radius: 4px;
}
.model-options :deep(.arco-checkbox) {
  min-width: 0;
  margin-right: 0;
}
.model-options :deep(.arco-checkbox-label) {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.model-empty {
  padding: 16px 10px;
  color: var(--color-text-3);
  font-size: 12px;
  text-align: center;
}
.model-manual {
  display: flex;
  gap: 8px;
}
.model-manual :deep(.arco-input-wrapper) {
  flex: 1;
}
.form-hint {
  margin-left: 12px;
  font-size: 12px;
  color: var(--color-text-3);
}

/* ============ 降级渠道标识 ============ */
.fallback-cell {
  display: flex;
  align-items: center;
  gap: 6px;
}
.fallback-tag {
  font-weight: 500;
}
.fallback-priority {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 28px;
  padding: 1px 6px;
  font-size: 11px;
  font-weight: 600;
  color: #d25c1f;
  background: #fff7e8;
  border: 1px solid #ffcca8;
  border-radius: 10px;
  line-height: 1.4;
  cursor: help;
}
</style>
