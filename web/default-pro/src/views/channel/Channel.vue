<template>
  <div class="channel-page">
    <!-- 顶部欢迎条 -->
    <div class="welcome-bar">
      <div class="welcome-text">
        <h1 class="welcome-title">{{ $t('channelPage.title') }}</h1>
        <p class="welcome-desc">{{ $t('channelPage.subtitle') }}</p>
      </div>
      <div class="welcome-meta">
        <span class="meta-chip">{{ $t('channelPage.total', { n: channels.length }) }}</span>
      </div>
    </div>

    <!-- 独立搜索栏 -->
    <div class="search-card">
      <div class="search-left">
        <a-input-search
          v-model="searchKeyword"
          :placeholder="$t('channelPage.searchPlaceholder')"
          allow-clear
          @search="handleSearch"
          @clear="handleSearchClear"
          :style="{ width: '320px' }"
        />
      </div>
      <div class="search-right">
        <a-button @click="handleTestAll" :loading="testingAll">{{ $t('channelPage.testAll') }}</a-button>
        <a-button @click="handleUpdateBalance" :loading="updatingBalance">{{ $t('channelPage.updateBalance') }}</a-button>
        <a-popconfirm :content="$t('channelPage.confirmDeleteDisabled')" @ok="handleDeleteDisabled">
          <a-button status="danger" :loading="deletingDisabled">{{ $t('channelPage.deleteDisabled') }}</a-button>
        </a-popconfirm>
        <a-button type="primary" size="large" @click="openCreateModal">
          <template #icon><icon-plus :size="14" /></template>
          {{ $t('channelPage.addChannel') }}
        </a-button>
      </div>
    </div>

    <!-- 列表 -->
    <div class="list-wrap">
      <div v-if="pageItems.length === 0 && !loading && !loadingMore" class="empty-state">
        <div class="empty-icon">
          <icon-apps :size="32" />
        </div>
        <p class="empty-title">{{ $t('channelPage.emptyTitle') }}</p>
        <p class="empty-desc">{{ $t('channelPage.emptyDesc') }}</p>
        <a-button type="primary" @click="openCreateModal">
          <template #icon><icon-plus :size="14" /></template>
          {{ $t('channelPage.addNow') }}
        </a-button>
      </div>

      <div v-else class="list-body">
        <div class="list-head">
          <div class="col">ID</div>
          <div class="col">{{ $t('channelPage.colType') }}</div>
          <div class="col">{{ $t('channelPage.colName') }}</div>
          <div class="col">Base URL</div>
          <div class="col">{{ $t('channelPage.colModels') }}</div>
          <div class="col">{{ $t('channelPage.colGroups') }}</div>
          <div class="col">{{ $t('channelPage.colStatus') }}</div>
          <div class="col">{{ $t('channelPage.colResponseTime') }}</div>
          <div class="col">{{ $t('channelPage.colFallback') }}</div>
          <div class="col col-action">{{ $t('channelPage.colAction') }}</div>
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
                <a-tooltip :content="$t('channelPage.fallbackHint')">
                  <a-tag color="orangered" size="small" class="fallback-tag">
                    <template #icon><icon-swap :size="12" /></template>
                    {{ $t('channelPage.fallbackTag') }}
                  </a-tag>
                </a-tooltip>
                <span v-if="c.fallback_priority" class="fallback-priority" :title="$t('channelPage.fallbackPriorityHint')">
                  P{{ c.fallback_priority }}
                </span>
              </div>
              <span v-else class="cell-muted">-</span>
            </div>

            <div class="col col-action">
              <a-button type="text" size="small" @click="openEditModal(c)">{{ $t('channelPage.edit') }}</a-button>
              <a-popconfirm
                :content="c.status === 1 ? $t('channelPage.confirmDisable') : $t('channelPage.confirmEnable')"
                @ok="handleToggleStatus(c)"
              >
                <a-button type="text" size="small">
                  {{ c.status === 1 ? $t('channelPage.disable') : $t('channelPage.enable') }}
                </a-button>
              </a-popconfirm>
              <a-button type="text" size="small" :loading="testingIds.includes(c.id)" @click="handleTest(c)">{{ $t('channelPage.test') }}</a-button>
              <a-popconfirm :content="$t('channelPage.confirmDelete')" @ok="handleDelete(c.id)">
                <a-button type="text" size="small" class="danger-btn">{{ $t('channelPage.delete') }}</a-button>
              </a-popconfirm>
            </div>
          </div>
        </a-spin>

        <div v-if="loadingMore" class="load-more-row">
          <a-spin :loading="true" :size="14" />
          <span class="load-more-text">{{ $t('channelPage.loadingMore') }}</span>
        </div>
        <div
          v-else-if="isReachedEnd && channels.length > pageSize && !loading"
          class="load-end-row"
        >
          {{ $t('channelPage.allLoaded', { n: channels.length }) }}
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
      :on-before-ok="handleSubmit"
      @cancel="closeModal"
      :ok-loading="submitting"
      :ok-text="$t('channelPage.save')"
      :cancel-text="$t('channelPage.cancel')"
    >
      <a-form ref="formRef" :model="form" layout="vertical" class="channel-form">
        <a-form-item field="type" :label="$t('channelPage.labelType')" required>
          <a-select
            v-model="form.type"
            :placeholder="$t('channelPage.selectType')"
            allow-search
            :filter-option="filterTypeOption"
            @change="onTypeChange"
          >
            <a-option v-for="(name, key) in typeNameMap" :key="key" :value="Number(key)" :label="name" />
          </a-select>
          <span v-if="!isEdit && activePreset" class="form-hint form-hint-block auto-fill-hint">
            {{ $t('channelPage.autoFilled') }}
          </span>
        </a-form-item>
        <a-form-item field="name" :label="$t('channelPage.labelName')" required>
          <a-input v-model="form.name" :placeholder="$t('channelPage.channelName')" allow-clear />
        </a-form-item>
        <a-form-item field="groups" :label="$t('channelPage.labelGroups')">
          <a-select
            v-model="form.groups"
            :placeholder="$t('channelPage.selectGroups')"
            multiple
            allow-clear
            allow-search
            allow-create
          >
            <a-option v-for="g in availableGroups" :key="g" :value="g" :label="g" />
          </a-select>
        </a-form-item>
        <a-form-item field="base_url" label="Base URL">
          <a-input v-model="form.base_url" placeholder="https://api.openai.com" allow-clear />
          <a-button
            v-if="activePreset && activePreset.base_url"
            type="text"
            size="mini"
            class="reset-default-btn"
            @click="resetBaseURLToDefault"
          >
            {{ $t('channelPage.resetDefault') }}
          </a-button>
        </a-form-item>
        <a-form-item field="models" :label="$t('channelPage.labelModels')">
          <div class="model-picker">
            <div class="model-picker-header">
              <span class="model-picker-title">
                {{ visibleModelOptions.length ? t('channelPage.pickModelsTitle', { count: selectedModels.length }) : t('channelPage.noModelsLoaded') }}
                <span v-if="defaultsAsOf" class="catalog-asof"> · {{ t('channelPage.mainstreamAsOf', { date: defaultsAsOf }) }}</span>
              </span>
              <a-space>
                <a-button v-if="form.type === 20 || form.type === 52" size="small" :loading="refreshingModels" @click="refreshChannelModels">
                  {{ $t('channelPage.refreshModels') }}
                </a-button>
                <a-button v-if="visibleModelOptions.length" size="small" @click="selectAllModels">
                  {{ $t('channelPage.selectAllModels') }}
                </a-button>
                <a-button v-if="selectedModels.length" size="small" @click="clearSelectedModels">
                  {{ $t('channelPage.clearAllModels') }}
                </a-button>
                <a-tooltip :content="$t('channelPage.checkModelNamesHint')">
                  <a-button size="small" :loading="validatingModels" @click="validateChannelModels">
                    {{ $t('channelPage.checkModelNames') }}
                  </a-button>
                </a-tooltip>
              </a-space>
            </div>
            <a-input-search
              v-if="visibleModelOptions.length"
              v-model="modelSearch"
              class="model-search"
              :placeholder="$t('channelPage.searchModelsPlaceholder')"
              allow-clear
            />
            <div v-if="filteredModelOptions.length" class="model-options">
              <div v-for="group in groupedModelOptions" :key="group.key" class="model-group">
                <div class="model-group-title">{{ group.label }} <span class="model-group-count">{{ group.items.length }}</span></div>
                <a-checkbox
                  v-for="model in group.items"
                  :key="model"
                  :model-value="selectedModels.includes(model)"
                  @change="toggleModel(model, $event)"
                >
                <div class="model-option-content">
                  <div class="model-option-name">{{ modelCatalog[model]?.name || model }}</div>
                  <div v-if="modelCatalog[model]?.name" class="model-option-id">{{ model }}</div>
                  <div v-if="modelCatalog[model]" class="model-option-meta">
                    <span v-if="modelCatalog[model].context_length">{{ t('channelPage.contextLabel', { n: formatContext(modelCatalog[model].context_length) }) }}</span>
                    <span v-if="modelCatalog[model].input_price_usd_per_million != null">{{ t('channelPage.inputPriceLabel', { price: formatPrice(modelCatalog[model].input_price_usd_per_million) }) }}</span>
                    <span v-if="modelCatalog[model].output_price_usd_per_million != null">{{ t('channelPage.outputPriceLabel', { price: formatPrice(modelCatalog[model].output_price_usd_per_million) }) }}</span>
                    <span v-if="modelCatalog[model].input_modalities?.length">{{ t('channelPage.inputModalitiesLabel') }}{{ modelCatalog[model].input_modalities.join('、') }}</span>
                    <span v-if="modelCatalog[model].output_modalities?.length">{{ t('channelPage.outputModalitiesLabel') }}{{ modelCatalog[model].output_modalities.join('、') }}</span>
                    <span v-if="modelCatalog[model].supported_parameters?.includes('tools')">{{ $t('channelPage.toolsSupported') }}</span>
                  </div>
                  <div v-if="modelCatalog[model]?.description" class="model-option-description" :title="modelCatalog[model].description">
                    {{ modelCatalog[model].description }}
                  </div>
                </div>
              </a-checkbox>
              </div>
            </div>
            <div v-else-if="visibleModelOptions.length" class="model-empty">{{ $t('channelPage.noMatchingModels') }}</div>
            <div v-else class="model-empty">{{ $t('channelPage.noModelsLoadedTip') }}</div>
            <div class="model-manual">
              <a-input
                v-model="manualModels"
                :placeholder="$t('channelPage.manualModelsPlaceholder')"
                allow-clear
                @press-enter="addManualModels"
              />
              <a-button @click="addManualModels">{{ $t('channelPage.add') }}</a-button>
            </div>
            <div class="form-hint">{{ $t('channelPage.modelPickerHint') }}</div>
          </div>

        </a-form-item>
        <a-form-item field="model_mapping" :label="$t('channelPage.labelModelMapping')">
          <div class="form-stack">
            <a-textarea
              v-model="form.model_mapping"
              :placeholder="$t('channelPage.modelMappingPlaceholder')"
              :auto-size="{ minRows: 4, maxRows: 8 }"
              allow-clear
            />
            <span class="form-hint form-hint-block">{{ $t('channelPage.modelMappingExample') }}&#123;"gpt-3.5-turbo-0301":"gpt-3.5-turbo","gpt-4-0314":"gpt-4"&#125;</span>
          </div>
        </a-form-item>
        <a-form-item field="key" :label="$t('channelPage.labelKey')" :required="!isEdit">
          <a-input-password v-model="form.key" :placeholder="isEdit ? $t('channelPage.keyKeepHint') : 'API Key'" />
          <span v-if="isEdit" class="form-hint">{{ $t('channelPage.keyKeepHint') }}</span>
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
        <a-form-item field="is_fallback" :label="$t('channelPage.fallback')">
          <a-switch v-model="form.is_fallback" />
          <span class="form-hint">{{ $t('channelPage.fallbackHint') }}</span>
        </a-form-item>
        <a-form-item v-if="form.is_fallback" field="fallback_priority" :label="$t('channelPage.fallbackPriority')">
          <a-input-number v-model="form.fallback_priority" :min="0" :step="1" />
          <span class="form-hint">{{ $t('channelPage.fallbackPriorityHint') }}</span>
        </a-form-item>
      </a-form>
    </a-modal>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { Message } from '@arco-design/web-vue'
import { IconPlus, IconApps, IconSwap } from '@arco-design/web-vue/es/icon'
import api from '@/api'
import { groupModelsByCategory } from '@/utils/modelCategory'

const { t, locale } = useI18n()

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
const channelTypeIds = [
  1, 50, 14, 33, 3, 11, 24, 51, 28, 41, 40, 15,
  47, 17, 49, 18, 48, 16, 19, 25, 23, 26, 27, 29,
  30, 31, 32, 34, 35, 36, 37, 38, 39, 42, 44,
  45, 46, 52, 53, 8, 22, 21, 20, 2, 5, 7, 10, 4, 6, 9, 12, 13,
]
const typeNameMap = computed(() => {
  const map = {}
  channelTypeIds.forEach((id) => {
    map[id] = t(`channelPage.types.${id}`)
  })
  return map
})
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
  44: 'arcoblue', 45: 'arcoblue', 46: 'arcoblue',
  8: 'pinkpurple', 22: 'arcoblue', 21: 'purple',
  52: 'green', 53: 'gray',
  20: 'gray', 2: 'arcoblue', 5: 'gold', 7: 'purple',
  10: 'purple', 4: 'cyan', 6: 'purple', 9: 'gold', 12: 'arcoblue',
  13: 'purple',
}

// 渠道状态（对齐后端 model/channel.go）：1=启用 2=手动禁用 3=自动禁用
function statusText(status) {
  if (status === 1) return t('channelPage.statusEnabled')
  if (status === 2) return t('channelPage.statusDisabled')
  if (status === 3) return t('channelPage.statusAutoDisabled')
  return t('channelPage.statusUnknown')
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
const modelCatalog = ref({})
const configuredCapabilityRows = ref([])
const modelSearch = ref('')
const manualModels = ref('')
const availableModelOptions = ref([])
// Provider defaults (base url + curated mainstream models), keyed by legacy type.
const channelDefaults = ref({})
const defaultsAsOf = ref('')
const defaultsLoading = ref(false)
// 来自 /api/group/ 的分组目录；实际可选项还要合并「已有渠道的分组」
// 和「当前表单已选分组」，见下方 availableGroups computed。
const apiGroups = ref([])

const formRef = ref(null)
const form = reactive({
  type: 1,
  name: '',
  base_url: '',
  models: '',
  model_mapping: '',
  groups: [],
  key: '',
  is_fallback: false,
  fallback_priority: 0,
})

// 分组下拉的选项来源（三路合并，去重排序）：
// 1. /api/group/ 返回的分组目录；
// 2. 已加载渠道里实际使用的分组（历史分组即使没配价格也必须能选到）；
// 3. 当前表单已选的分组（避免回填值因不在选项里而显示异常）。
const availableGroups = computed(() => {
  const ids = new Set(apiGroups.value || [])
  channels.value.forEach((c) => {
    parseModelNames(c.group).forEach((g) => {
      if (g) ids.add(g)
    })
  })
  if (Array.isArray(form.groups)) {
    form.groups.forEach((g) => {
      if (g) ids.add(g)
    })
  }
  return Array.from(ids).sort()
})

const modalTitle = computed(() =>
  isEdit.value ? t('channelPage.editChannel') : t('channelPage.addChannel'),
)

// 当前选中类型的默认值预设（仅新建时用于自动填充提示与恢复默认按钮）。
const activePreset = computed(() => channelDefaults.value?.[String(form.type)] || null)

const selectedModels = computed({
  get: () => parseModelNames(form.models),
  set: (models) => {
    form.models = mergeModelNames(models).join(',')
  },
})

const visibleModelOptions = computed(() => mergeModelNames(modelOptions.value, selectedModels.value))
const filteredModelOptions = computed(() => {
  const keyword = modelSearch.value.trim().toLowerCase()
  if (!keyword) return visibleModelOptions.value
  return visibleModelOptions.value.filter((model) => {
    const item = modelCatalog.value[model]
    const haystack = [model, item?.name, item?.description, ...(item?.input_modalities || []), ...(item?.output_modalities || []), ...(item?.supported_parameters || [])]
      .filter(Boolean).join(' ').toLowerCase()
    return haystack.includes(keyword)
  })
})

// Group the picker by modality; classification prefers catalog modalities and
// falls back to model-name markers, same rules as the model catalog page.
const groupedModelOptions = computed(() => {
  const lang = locale.value === 'en' ? 'en' : 'zh'
  const descriptors = new Map(configuredCapabilityRows.value.filter(row => row.channel_type === form.type && (!isEdit.value || row.channel_id === editingId.value)).map(row => [row.model_name, row]))
  const items = filteredModelOptions.value.map(model => ({ ...(modelCatalog.value[model] || {}), ...(descriptors.get(model) || {}), model_name: model, model }))
  return groupModelsByCategory(items, item => item.model_name, lang)
    .map(group => ({ ...group, items: group.items.map(item => item.model) }))
})

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

function selectAllModels() {
  selectedModels.value = visibleModelOptions.value
}

function clearSelectedModels() {
  selectedModels.value = []
}

function formatContext(value) {
  return value >= 1000 ? `${(value / 1000).toFixed(value % 1000 === 0 ? 0 : 1)}K` : String(value)
}

function formatPrice(value) {
  const number = Number(value)
  if (number === 0) return '0'
  if (number < 0.01) return number.toFixed(4)
  return number.toFixed(number < 1 ? 3 : 2).replace(/\.0+$/, '')
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
      Message.error(data.message || t('channelPage.loadFailed'))
    }
  } catch (e) {
    Message.error(e.response?.data?.message || e.message || t('channelPage.loadFailed'))
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
    .catch((e) => Message.error(e.response?.data?.message || e.message || t('channelPage.searchFailed')))
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

let defaultsPromise = null
function fetchChannelDefaults() {
  // 复用同一个请求，避免 onMounted 预加载与打开弹窗时的竞态。
  if (defaultsPromise) return defaultsPromise
  defaultsLoading.value = true
  defaultsPromise = api.get('/api/channel/defaults')
    .then(({ data }) => {
      if (data?.success) {
        channelDefaults.value = data.data?.presets || {}
        defaultsAsOf.value = data.data?.as_of || ''
      }
    })
    .catch(() => {
      // 默认值是锦上添花，加载失败时表单仍可手动填写。
      channelDefaults.value = {}
      defaultsAsOf.value = ''
    })
    .finally(() => { defaultsLoading.value = false })
  return defaultsPromise
}

// 把某个类型的默认值套用到表单：Base URL、渠道名、主流模型列表与元信息。
// 只用于新建；编辑时保留用户已有配置，避免覆盖。
function applyTypeDefaults(type, { overwriteName = true, overwriteModels = true } = {}) {
  const preset = channelDefaults.value?.[String(type)]
  if (!preset) return false
  if (preset.base_url) form.base_url = preset.base_url
  if (overwriteName && preset.name) form.name = preset.name

  const models = Array.isArray(preset.models) ? preset.models : []
  // 后端已按时效过滤掉过期模型，这里全部作为候选并默认选中。
  const ids = models.map((m) => (typeof m === 'string' ? m : m?.id)).filter(Boolean)
  if (overwriteModels && ids.length) {
    modelOptions.value = [...ids]
    selectedModels.value = [...ids]
    const catalog = {}
    models.forEach((m) => {
      if (m && typeof m !== 'string' && m.id) catalog[m.id] = m
    })
    modelCatalog.value = catalog
  }
  return true
}

function onTypeChange(type) {
  if (isEdit.value) return
  const ok = applyTypeDefaults(type)
  if (!ok) {
    // 未收录的类型：清空上一次的默认值，避免遗留错误 Base URL。
    form.base_url = ''
    form.name = ''
    modelOptions.value = []
    selectedModels.value = []
    modelCatalog.value = {}
  }
}

function resetBaseURLToDefault() {
  if (activePreset.value?.base_url) form.base_url = activePreset.value.base_url
}

function openCreateModal() {
  isEdit.value = false
  editingId.value = null
  form.type = 1
  form.name = ''
  form.base_url = ''
  form.models = ''
  form.model_mapping = ''
  // 新建时默认选中 default 分组，避免用户漏填导致提交失败。
  // 后端 GetGroups 保证至少有 default；availableGroups 也会合并表单已选值。
  form.groups = ['default']
  form.key = ''
  form.is_fallback = false
  form.fallback_priority = 0
  modelOptions.value = []
  modelCatalog.value = {}
  manualModels.value = ''
  modalVisible.value = true
  // 打开弹窗即套用默认类型（OpenAI）的预设，并在后台拉取最新默认值。
  if (!Object.keys(channelDefaults.value).length) fetchChannelDefaults().then(() => applyTypeDefaults(form.type))
  else applyTypeDefaults(form.type)
}

function openEditModal(record) {
  isEdit.value = true
  editingId.value = record.id
  form.type = record.type || 1
  form.name = record.name || ''
  form.base_url = record.base_url || ''
  form.models = record.models || ''
  form.model_mapping = record.model_mapping || ''
  // 后端 Channel 的 JSON 字段是 group（单数，逗号分隔），不存在 groups。
  // 之前读 record.groups 恒为 undefined，导致编辑时分组永远是空的：
  // 用户不改分组直接保存就会把 group 写成空串，渠道从此不属于任何分组。
  form.groups = parseModelNames(record.group || record.groups)
  form.key = record.key || ''
  form.is_fallback = !!record.is_fallback
  form.fallback_priority = record.fallback_priority || 0
  modelOptions.value = parseModelNames(form.models)
  if (form.type === 52) {
    modelOptions.value = mergeModelNames(defaultOpenAICodexModels, modelOptions.value)
  }
  manualModels.value = ''
  modalVisible.value = true
}

function closeModal() {
  modalVisible.value = false
  formRef.value?.clearValidate()
}

// 类型下拉支持输入筛选：按显示名或类型数字 ID 匹配。
function filterTypeOption(inputValue, option) {
  const keyword = String(inputValue ?? '').trim().toLowerCase()
  if (!keyword) return true
  const label = String(option?.label ?? '').toLowerCase()
  const value = String(option?.value ?? '')
  return label.includes(keyword) || value.includes(keyword)
}

// 作为 a-modal 的 on-before-ok 使用：返回 false 阻止弹窗关闭。
// 这样校验失败或提交失败时弹窗保持打开，用户可直接修正后重试。
async function handleSubmit() {
  const errors = await formRef.value?.validate()
  if (errors) return false
  // 分组为空会让渠道不属于任何组：abilities 不会生成，用户侧永远看不到该渠道的模型。
  // 过去编辑时回填失效 + 允许清空，正是「改了分组不生效」的直接原因。
  if (!Array.isArray(form.groups) || form.groups.filter((g) => String(g || '').trim()).length === 0) {
    Message.error(t('channelPage.groupRequired'))
    return false
  }
  if (form.model_mapping && form.model_mapping.trim()) {
    try {
      const parsed = JSON.parse(form.model_mapping)
      if (typeof parsed !== 'object' || Array.isArray(parsed) || parsed === null) {
        Message.error(t('channelPage.modelMappingMustBeObject'))
        return false
      }
    } catch {
      Message.error(t('channelPage.modelMappingInvalidJson'))
      return false
    }
  }
  submitting.value = true
  try {
    const payload = {
      type: form.type,
      name: form.name,
      base_url: form.base_url,
      models: form.models,
      model_mapping: form.model_mapping && form.model_mapping.trim() ? form.model_mapping : '',
      group: Array.isArray(form.groups) ? form.groups.join(',') : form.groups,
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
      Message.success(isEdit.value ? t('channelPage.channelUpdated') : t('channelPage.channelAdded'))
      formRef.value?.clearValidate()
      fetchChannels()
      return true // 允许 on-before-ok 关闭弹窗
    }
    Message.error(res.data.message || t('channelPage.opFailed'))
    return false
  } catch (e) {
    Message.error(e.response?.data?.message || e.message || t('channelPage.opFailed'))
    return false
  } finally {
    submitting.value = false
  }
}

async function fetchAvailableModels() {
  try {
    const { data } = await api.get('/api/model_price/options')
    const list = Array.isArray(data?.data) ? data.data : []
    const ids = new Set()
    list.forEach((m) => {
      if (typeof m === 'string' && m) ids.add(m)
    })
    if (Array.isArray(form.models)) {
      form.models.forEach((m) => {
        if (m) ids.add(m)
      })
    }
    availableModelOptions.value = Array.from(ids).sort()
  } catch (e) {
    availableModelOptions.value = []
  }
}

async function fetchGroups() {
  try {
    const { data } = await api.get('/api/group/')
    apiGroups.value = (data?.data || []).filter(Boolean)
  } catch (e) {
    apiGroups.value = []
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
    modelCatalog.value = Object.fromEntries((data.data?.catalog || [])
      .filter((item) => item?.id)
      .map((item) => [item.id, item]))
    modelSearch.value = ''
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
      model_mapping: form.model_mapping,
      models: models.join(','),
    })
    if (!data.success) throw new Error(data.message || '模型名核对失败')

    // 只读核对：上游 /models 清单天然不完整（模型重定向别名、原生端点模型、
    // 账号/区域可见性都可能让模型缺席），因此绝不据此改写已填模型。
    const result = data.data || {}
    if (!result.verification_enabled) {
      Message.warning({
        content: '上游未提供模型清单，无法核对；已填模型全部保留。实际可用性请保存后点击「测试」。',
        duration: 6000,
      })
      return
    }

    const validModels = result.valid_models || []
    const invalidModels = result.invalid_models || []
    if (!invalidModels.length) {
      Message.success(`已核对 ${validModels.length} 个模型名：均在上游清单中（仅代表清单存在，不代表可调用）。`)
      return
    }

    const preview = invalidModels.slice(0, 5).join(', ')
    const more = invalidModels.length > 5 ? ` 等 ${invalidModels.length} 个` : ''
    Message.warning({
      content: `${invalidModels.length} 个模型名未出现在上游清单中：${preview}${more}。常见原因是模型重定向别名、原生端点模型或上游清单不完整；已全部保留，未做修改。`,
      duration: 8000,
    })
  } catch (e) {
    Message.error(e.response?.data?.message || e.message || '模型名核对失败')
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
      Message.success(t('channelPage.channelDeleted'))
      fetchChannels()
    } else {
      Message.error(data.message || t('channelPage.deleteFailed'))
    }
  } catch (e) {
    Message.error(e.response?.data?.message || e.message || t('channelPage.deleteFailed'))
  }
}

async function handleTest(record) {
  testingIds.value.push(record.id)
  try {
    // 单个测试：GET /api/channel/test/:id?model=，返回 time（秒）
    const model = record.models ? record.models.split(',')[0] : ''
    const { data } = await api.get(`/api/channel/test/${record.id}`, { params: { model } })
    if (data.success) {
      Message.success(t('channelPage.testPassed', { time: data.time }))
      fetchChannels()
    } else {
      Message.error(data.message || t('channelPage.testFailed'))
    }
  } catch (e) {
    Message.error(e.response?.data?.message || e.message || t('channelPage.testFailed'))
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
      Message.info(t('channelPage.testAllStarted'))
    } else {
      Message.error(data.message || t('channelPage.testAllFailed'))
    }
  } catch (e) {
    Message.error(e.response?.data?.message || e.message || t('channelPage.testAllFailed'))
  } finally {
    testingAll.value = false
  }
}

async function handleToggleStatus(record) {
  const newStatus = record.status === 1 ? 2 : 1
  try {
    const { data } = await api.put('/api/channel/', { id: record.id, status: newStatus })
    if (data.success) {
      Message.success(newStatus === 1 ? t('channelPage.channelEnabled') : t('channelPage.channelDisabled'))
      record.status = newStatus
    } else {
      Message.error(data.message || t('channelPage.opFailed'))
    }
  } catch (e) {
    Message.error(e.response?.data?.message || e.message || t('channelPage.opFailed'))
  }
}

async function handleUpdateBalance() {
  updatingBalance.value = true
  try {
    // 全部更新余额：GET /api/channel/update_balance
    const { data } = await api.get('/api/channel/update_balance')
    if (data.success) {
      Message.success(t('channelPage.balanceUpdateStarted'))
      fetchChannels()
    } else {
      Message.error(data.message || t('channelPage.balanceUpdateFailed'))
    }
  } catch (e) {
    Message.error(e.response?.data?.message || e.message || t('channelPage.balanceUpdateFailed'))
  } finally {
    updatingBalance.value = false
  }
}

async function handleDeleteDisabled() {
  deletingDisabled.value = true
  try {
    const { data } = await api.delete('/api/channel/disabled')
    if (data.success) {
      Message.success(t('channelPage.disabledDeleted'))
      fetchChannels()
    } else {
      Message.error(data.message || t('channelPage.deleteFailed'))
    }
  } catch (e) {
    Message.error(e.response?.data?.message || e.message || t('channelPage.deleteFailed'))
  } finally {
    deletingDisabled.value = false
  }
}

async function fetchCapabilityCatalog() {
  try {
    const { data } = await api.get('/api/channel/model_catalog')
    if (data.success) configuredCapabilityRows.value = data.data || []
  } catch { /* Manual channel configuration remains available. */ }
}

onMounted(() => {
  fetchCapabilityCatalog()
  fetchChannels()
  fetchAvailableModels()
  fetchGroups()
  fetchChannelDefaults()
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
.model-search {
  margin-bottom: 10px;
}
.model-options {
  display: flex;
  flex-direction: column;
  gap: 10px;
  max-height: 360px;
  overflow-y: auto;
  padding: 10px;
  margin-bottom: 10px;
  background: var(--color-bg-2);
  border: 1px solid var(--color-border-2);
  border-radius: 4px;
}
.model-group {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px 12px;
}
.model-group-title {
  grid-column: 1 / -1;
  display: flex;
  align-items: center;
  gap: 6px;
  padding-bottom: 4px;
  border-bottom: 1px solid var(--color-border-1);
  color: var(--color-text-3);
  font-size: 12px;
  font-weight: 600;
}
.model-group-count {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 18px;
  height: 16px;
  padding: 0 5px;
  border-radius: 8px;
  background: var(--color-fill-2);
  color: var(--color-text-2);
  font-weight: 400;
}
.model-options :deep(.arco-checkbox) {
  min-width: 0;
  margin-right: 0;
}
.model-options :deep(.arco-checkbox-label) {
  min-width: 0;
}
.model-option-content {
  min-width: 0;
}
.model-option-name {
  overflow: hidden;
  color: var(--color-text-1);
  font-size: 13px;
  font-weight: 500;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.model-option-id {
  overflow: hidden;
  color: var(--color-text-3);
  font-size: 11px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.model-option-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 2px 8px;
  margin-top: 2px;
  color: var(--color-text-2);
  font-size: 11px;
  line-height: 1.4;
}
.model-option-description {
  display: -webkit-box;
  overflow: hidden;
  margin-top: 2px;
  color: var(--color-text-3);
  font-size: 11px;
  line-height: 1.35;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
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
.form-hint-block {
  display: block;
  margin-left: 0;
  margin-top: 8px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-all;
}
.form-stack {
  display: flex;
  flex-direction: column;
  width: 100%;
}
.auto-fill-hint {
  margin-left: 0;
  margin-top: 6px;
  color: #00a870;
  line-height: 1.5;
}
.catalog-asof {
  color: var(--color-text-4);
  font-size: 11px;
}
.reset-default-btn {
  margin-left: 8px;
  font-size: 12px;
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
