<!--
  ProviderDetail 供应商（渠道商）详情
  Provider detail: channels, per-model usage, free models, trend and curated profile

  模型统一按「供应商:模型」展示，避免同名模型跨供应商混淆
  （deepseek-v4-pro 同时挂在 deepseek / alibailian / doubao 三个渠道下）。

  版本: v0.0.28
  日期: 2026-11-25
  作者: MCode
-->
<template>
  <div class="provider-page">
    <div class="welcome-bar">
      <div class="welcome-text">
        <a-button type="text" size="mini" @click="goBack">
          <template #icon><icon-left :size="14" /></template>
          {{ t('providerPage.backToList') }}
        </a-button>
        <h1 class="welcome-title">{{ detail.provider_name || providerId }}</h1>
        <p class="welcome-desc">
          <span class="model-id">{{ providerId }}</span>
          <span v-if="detail.default_base_url" class="model-id">· {{ detail.default_base_url }}</span>
        </p>
      </div>
      <div class="welcome-meta">
        <a-radio-group v-model="range" type="button" size="small" @change="loadData">
          <a-radio value="today">{{ t('admin.rangeToday') }}</a-radio>
          <a-radio value="7d">{{ t('admin.range7d') }}</a-radio>
          <a-radio value="30d">{{ t('admin.range30d') }}</a-radio>
          <a-radio value="all">{{ t('admin.rangeAll') }}</a-radio>
        </a-radio-group>
        <a-button size="mini" @click="openProfile">
          <template #icon><icon-edit :size="14" /></template>
          {{ t('providerPage.editProfile') }}
        </a-button>
        <a-button size="mini" :loading="loading" @click="loadData">
          <template #icon><icon-refresh :size="14" /></template>
          {{ t('admin.refresh') }}
        </a-button>
      </div>
    </div>

    <a-spin :loading="loading" style="width: 100%">
      <!-- 策展资料 -->
      <div class="panel">
        <div class="panel-head">
          <h2 class="panel-title">{{ t('providerPage.sectionProfile') }}</h2>
          <a-tag v-if="detail.category" size="small">{{ detail.category }}</a-tag>
        </div>
        <div v-if="detail.has_profile" class="profile-grid">
          <div class="profile-item">
            <span class="profile-label">{{ t('providerPage.profileSummary') }}</span>
            <span class="profile-value">{{ detail.summary || '—' }}</span>
          </div>
          <div class="profile-item">
            <span class="profile-label">{{ t('providerPage.profileSignup') }}</span>
            <span class="profile-value">
              <a-link v-if="detail.signup_url" :href="detail.signup_url" target="_blank">{{ detail.signup_url }}</a-link>
              <template v-else>—</template>
            </span>
          </div>
          <div class="profile-item">
            <span class="profile-label">{{ t('providerPage.profileConsole') }}</span>
            <span class="profile-value">
              <a-link v-if="detail.console_url" :href="detail.console_url" target="_blank">{{ detail.console_url }}</a-link>
              <template v-else>—</template>
            </span>
          </div>
          <div class="profile-item">
            <span class="profile-label">{{ t('providerPage.profileDoc') }}</span>
            <span class="profile-value">
              <a-link v-if="detail.doc_url" :href="detail.doc_url" target="_blank">{{ detail.doc_url }}</a-link>
              <template v-else>—</template>
            </span>
          </div>
          <div class="profile-item">
            <span class="profile-label">{{ t('providerPage.profileFreePolicy') }}</span>
            <span class="profile-value">{{ detail.free_policy || '—' }}</span>
          </div>
          <div class="profile-item">
            <span class="profile-label">{{ t('providerPage.profileNotes') }}</span>
            <span class="profile-value">{{ detail.notes || '—' }}</span>
          </div>
        </div>
        <a-empty v-else :description="t('providerPage.profileEmpty')" />
      </div>

      <!-- KPI -->
      <div class="stat-grid">
        <div v-for="item in statItems" :key="item.label" class="stat-item">
          <div class="stat-head">
            <span class="stat-label">{{ item.label }}</span>
          </div>
          <div class="stat-value">{{ item.value }}</div>
        </div>
      </div>

      <!-- 趋势 -->
      <div class="panel">
        <div class="panel-head">
          <h2 class="panel-title">{{ t('providerPage.sectionTrend') }}</h2>
        </div>
        <VChart v-if="trendOption" class="chart" :option="trendOption" autoresize />
        <a-empty v-else description="—" />
      </div>

      <!-- 模型用量 -->
      <div class="panel">
        <div class="panel-head">
          <h2 class="panel-title">{{ t('providerPage.sectionModels') }}</h2>
          <span class="model-id">{{ detail.models?.length || 0 }}</span>
        </div>
        <a-table
          row-key="model_name"
          :columns="modelColumns"
          :data="detail.models || []"
          :pagination="false"
          :scroll="{ x: 1200 }"
          size="small"
        >
          <template #model="{ record }">
            <div class="model-name">{{ record.display_name || record.model_name }}</div>
            <div v-if="record.model_name !== record.display_name" class="model-id">{{ record.model_name }}</div>
          </template>
          <template #requests="{ record }">
            <span class="cell-num">{{ formatInt(record.request_count) }}</span>
          </template>
          <template #tokens="{ record }">
            <span class="cell-num">{{ formatInt(record.total_tokens) }}</span>
          </template>
          <template #amount="{ record }">
            <span class="cell-num">{{ formatYuan(record.amount) }}</span>
          </template>
          <template #price="{ record }">
            <template v-if="record.has_price">
              <div>{{ t('providerPage.inputPrice') }} {{ record.input_price }} {{ t('providerPage.unitPerMillion') }}</div>
              <div>{{ t('providerPage.outputPrice') }} {{ record.output_price }} {{ t('providerPage.unitPerMillion') }}</div>
            </template>
            <a-tag v-else color="orange" size="small">{{ t('providerPage.noPrice') }}</a-tag>
          </template>
          <template #free="{ record }">
            <a-tag v-if="record.is_free" color="green" size="small">
              {{ freeTypeLabel(record.free_type) }}
            </a-tag>
            <span v-else class="muted">—</span>
          </template>
          <template #channels="{ record }">
            <span class="model-id">#{{ (record.channel_ids || []).join(', #') }}</span>
          </template>
        </a-table>
        <a-empty v-if="!detail.models?.length" :description="t('providerPage.emptyModels')" />
      </div>

      <!-- 免费模型 -->
      <div class="panel">
        <div class="panel-head">
          <h2 class="panel-title">{{ t('providerPage.sectionFree') }}</h2>
          <a-space size="mini">
            <a-button size="small" @click="detectFree">
              <template #icon><icon-search :size="13" /></template>
              {{ t('providerPage.detectFree') }}
            </a-button>
            <a-button size="small" type="primary" @click="openFreeModel()">
              <template #icon><icon-plus :size="13" /></template>
              {{ t('providerPage.addFreeModel') }}
            </a-button>
          </a-space>
        </div>
        <a-table
          row-key="model_name"
          :columns="freeColumns"
          :data="detail.free_models || []"
          :pagination="false"
          size="small"
        >
          <template #type="{ record }">
            <a-tag size="small">{{ freeTypeLabel(record.free_type) }}</a-tag>
          </template>
          <template #zeroPrice="{ record }">
            <a-tag v-if="record.zero_price" color="green" size="small">{{ t('providerPage.freeModelZeroPrice') }}</a-tag>
            <span v-else class="muted">—</span>
          </template>
          <template #validUntil="{ record }">
            <span v-if="record.valid_until">{{ formatDate(record.valid_until) }}</span>
            <span v-else class="muted">—</span>
          </template>
          <template #actions="{ record }">
            <a-button size="small" type="text" status="danger" @click="removeFreeModel(record)">
              <template #icon><icon-delete :size="13" /></template>
            </a-button>
          </template>
        </a-table>
        <a-empty v-if="!detail.free_models?.length" :description="t('providerPage.emptyFree')" />
      </div>

      <!-- 渠道 -->
      <div class="panel">
        <div class="panel-head">
          <h2 class="panel-title">{{ t('providerPage.sectionChannels') }}</h2>
          <span class="model-id">{{ detail.channels?.length || 0 }}</span>
        </div>
        <a-table
          row-key="id"
          :columns="channelColumns"
          :data="detail.channels || []"
          :pagination="false"
          :scroll="{ x: 1200 }"
          size="small"
        >
          <template #name="{ record }">
            <div class="model-name">{{ record.name }}</div>
            <div class="model-id">#{{ record.id }} · {{ record.group }}</div>
          </template>
          <template #status="{ record }">
            <a-tag :color="record.status === 1 ? 'green' : 'red'" size="small">
              {{ record.status === 1 ? t('providerPage.channelEnabled') : t('providerPage.channelDisabled') }}
            </a-tag>
          </template>
          <template #models="{ record }">
            <a-space wrap size="mini">
              <a-tag v-for="name in record.models" :key="name" size="small">{{ name }}</a-tag>
            </a-space>
          </template>
          <template #used="{ record }">
            <span class="cell-num">{{ formatYuan(record.used_amount) }}</span>
          </template>
          <template #lastError="{ record }">
            <span v-if="record.last_error" class="error-text" :title="record.last_error">{{ record.last_error }}</span>
            <span v-else class="muted">—</span>
          </template>
        </a-table>
        <a-empty v-if="!detail.channels?.length" :description="t('providerPage.emptyChannels')" />
      </div>
    </a-spin>

    <!-- 策展资料编辑 -->
    <a-modal
      :visible="profileVisible"
      :title="t('providerPage.editProfile')"
      :footer="false"
      :width="640"
      @cancel="profileVisible = false"
    >
      <a-form :model="profileForm" layout="vertical">
        <a-form-item :label="t('providerPage.profileTitle')">
          <a-input v-model="profileForm.title" />
        </a-form-item>
        <a-form-item :label="t('providerPage.profileCategory')">
          <a-input v-model="profileForm.category" placeholder="国产 / 海外 / 中转 / 本地" />
        </a-form-item>
        <a-form-item :label="t('providerPage.profileSummary')">
          <a-textarea v-model="profileForm.summary" />
        </a-form-item>
        <a-form-item :label="t('providerPage.profileSignup')">
          <a-input v-model="profileForm.signup_url" placeholder="https://…" />
        </a-form-item>
        <a-form-item :label="t('providerPage.profileConsole')">
          <a-input v-model="profileForm.console_url" placeholder="https://…" />
        </a-form-item>
        <a-form-item :label="t('providerPage.profileDoc')">
          <a-input v-model="profileForm.doc_url" placeholder="https://…" />
        </a-form-item>
        <a-form-item :label="t('providerPage.profileFreePolicy')">
          <a-textarea v-model="profileForm.free_policy" />
        </a-form-item>
        <a-form-item :label="t('providerPage.profileNotes')">
          <a-textarea v-model="profileForm.notes" />
        </a-form-item>
      </a-form>
      <template #footer>
        <a-space>
          <a-button @click="profileVisible = false">{{ t('admin.cancel') }}</a-button>
          <a-button type="primary" :loading="savingProfile" @click="saveProfile">{{ t('providerPage.saveProfile') }}</a-button>
        </a-space>
      </template>
    </a-modal>

    <!-- 免费模型编辑 -->
    <a-modal
      :visible="freeModelVisible"
      :title="t('providerPage.addFreeModel')"
      :footer="false"
      :width="560"
      @cancel="freeModelVisible = false"
    >
      <a-form :model="freeModelForm" layout="vertical">
        <a-form-item :label="t('providerPage.freeModelProvider')">
          <a-input v-model="freeModelForm.provider_id" />
        </a-form-item>
        <a-form-item :label="t('providerPage.freeModelName')">
          <a-input v-model="freeModelForm.model_name" />
        </a-form-item>
        <a-form-item :label="t('providerPage.freeModelType')">
          <a-select v-model="freeModelForm.free_type">
            <a-option value="permanent">{{ t('providerPage.freeTypePermanent') }}</a-option>
            <a-option value="quota">{{ t('providerPage.freeTypeQuota') }}</a-option>
            <a-option value="plan">{{ t('providerPage.freeTypePlan') }}</a-option>
            <a-option value="suffix">{{ t('providerPage.freeTypeSuffix') }}</a-option>
          </a-select>
        </a-form-item>
        <a-form-item :label="t('providerPage.freeModelNote')">
          <a-textarea v-model="freeModelForm.note" />
        </a-form-item>
        <a-form-item :label="t('providerPage.freeModelValidUntil')">
          <a-input-number v-model="freeModelForm.valid_until" :min="0" style="width: 100%" />
        </a-form-item>
        <a-form-item>
          <a-switch v-model="freeModelForm.zero_price">
            {{ t('providerPage.freeModelZeroPrice') }}
          </a-switch>
        </a-form-item>
      </a-form>
      <template #footer>
        <a-space>
          <a-button @click="freeModelVisible = false">{{ t('admin.cancel') }}</a-button>
          <a-button type="primary" :loading="savingFreeModel" @click="saveFreeModel">{{ t('providerPage.saveProfile') }}</a-button>
        </a-space>
      </template>
    </a-modal>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { Message } from '@arco-design/web-vue'
import {
  IconRefresh,
  IconLeft,
  IconEdit,
  IconPlus,
  IconDelete,
  IconSearch,
} from '@arco-design/web-vue/es/icon'
import VChart from 'vue-echarts'
import 'echarts'
import providerApi from '@/api/provider'
import { formatYuan } from '@/utils/quota'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const providerId = computed(() => String(route.params.id || ''))
const loading = ref(false)
const range = ref('7d')
const detail = ref({})

const profileVisible = ref(false)
const savingProfile = ref(false)
const profileForm = ref({
  title: '',
  category: '',
  summary: '',
  signup_url: '',
  console_url: '',
  doc_url: '',
  free_policy: '',
  notes: '',
})

const freeModelVisible = ref(false)
const savingFreeModel = ref(false)
const freeModelForm = ref({
  provider_id: '',
  model_name: '',
  free_type: 'permanent',
  note: '',
  valid_until: 0,
  zero_price: false,
})

// formatInt 千分位整数。
// Thousands-separated integer.
function formatInt(value) {
  return Number(value || 0).toLocaleString('en-US')
}

// formatDate Unix 秒 → 本地日期。
// Unix seconds to a local date string.
function formatDate(value) {
  if (!value) return ''
  const d = new Date(Number(value) * 1000)
  if (Number.isNaN(d.getTime())) return ''
  return d.toLocaleDateString()
}

// freeTypeLabel 免费类型文案。
// Free type label.
function freeTypeLabel(type) {
  const map = {
    permanent: t('providerPage.freeTypePermanent'),
    quota: t('providerPage.freeTypeQuota'),
    plan: t('providerPage.freeTypePlan'),
    suffix: t('providerPage.freeTypeSuffix'),
  }
  return map[type] || type || '—'
}

async function loadData() {
  loading.value = true
  try {
    const { data } = await providerApi.detail(providerId.value, range.value)
    if (data.success) {
      detail.value = data.data || {}
    } else {
      Message.error(data.message || t('providerPage.loadFailed'))
    }
  } catch (e) {
    Message.error(e.response?.data?.message || e.message || t('providerPage.loadFailed'))
  } finally {
    loading.value = false
  }
}

function goBack() {
  router.push({ name: 'ProviderList' })
}

function openProfile() {
  profileForm.value = {
    title: detail.value.title || '',
    category: detail.value.category || '',
    summary: detail.value.summary || '',
    signup_url: detail.value.signup_url || '',
    console_url: detail.value.console_url || '',
    doc_url: detail.value.doc_url || '',
    free_policy: detail.value.free_policy || '',
    notes: detail.value.notes || '',
  }
  profileVisible.value = true
}

async function saveProfile() {
  savingProfile.value = true
  try {
    const { data } = await providerApi.saveProfile(providerId.value, profileForm.value)
    if (data.success) {
      Message.success(t('providerPage.saveProfile'))
      profileVisible.value = false
      loadData()
    } else {
      Message.error(data.message || t('providerPage.saveFailed'))
    }
  } catch (e) {
    Message.error(e.response?.data?.message || e.message || t('providerPage.saveFailed'))
  } finally {
    savingProfile.value = false
  }
}

function openFreeModel(record) {
  freeModelForm.value = {
    provider_id: providerId.value,
    model_name: record?.model_name || '',
    free_type: record?.free_type || 'permanent',
    note: record?.note || '',
    valid_until: record?.valid_until || 0,
    zero_price: !!record?.zero_price,
  }
  freeModelVisible.value = true
}

async function saveFreeModel() {
  savingFreeModel.value = true
  try {
    const { data } = await providerApi.saveFreeModel(freeModelForm.value)
    if (data.success) {
      Message.success(t('providerPage.saveProfile'))
      freeModelVisible.value = false
      loadData()
    } else {
      Message.error(data.message || t('providerPage.saveFailed'))
    }
  } catch (e) {
    Message.error(e.response?.data?.message || e.message || t('providerPage.saveFailed'))
  } finally {
    savingFreeModel.value = false
  }
}

async function removeFreeModel(record) {
  try {
    const { data } = await providerApi.deleteFreeModel(providerId.value, record.model_name)
    if (data.success) {
      Message.success(t('providerPage.deleteSuccess'))
      loadData()
    } else {
      Message.error(data.message || t('providerPage.deleteFailed'))
    }
  } catch (e) {
    Message.error(e.response?.data?.message || e.message || t('providerPage.deleteFailed'))
  }
}

// detectFree 按模型名后缀识别疑似免费模型，命中后直接加入清单。
// Detect candidate free models by suffix and add them to the catalogue.
async function detectFree() {
  const names = (detail.value.models || []).map((item) => item.model_name).filter(Boolean)
  if (!names.length) {
    Message.info(t('providerPage.emptyModels'))
    return
  }
  try {
    const { data } = await providerApi.detectFreeModels(names)
    if (!data.success) {
      Message.error(data.message || t('providerPage.loadFailed'))
      return
    }
    const detected = data.data || []
    if (!detected.length) {
      Message.info(t('providerPage.emptyFree'))
      return
    }
    Message.success(`${t('providerPage.detectedFree')}: ${detected.map((item) => item.model_name).join(', ')}`)
    for (const item of detected) {
      await providerApi.saveFreeModel({
        provider_id: providerId.value,
        model_name: item.model_name,
        free_type: 'suffix',
        note: item.suffix,
        valid_until: 0,
        zero_price: false,
      })
    }
    loadData()
  } catch (e) {
    Message.error(e.response?.data?.message || e.message || t('providerPage.loadFailed'))
  }
}

const statItems = computed(() => {
  const usage = detail.value.usage || {}
  return [
    { label: t('providerPage.statRequests'), value: formatInt(usage.request_count) },
    { label: t('providerPage.statTokens'), value: formatInt(usage.total_tokens) },
    { label: t('providerPage.statAmount'), value: formatYuan(usage.amount) },
    { label: t('providerPage.statModels'), value: formatInt(usage.model_count) },
    { label: t('providerPage.statChannels'), value: formatInt(usage.channel_count) },
  ]
})

const modelColumns = computed(() => [
  { title: t('providerPage.colModel'), slotName: 'model', width: 260 },
  { title: t('providerPage.colRequests'), slotName: 'requests', width: 100 },
  { title: t('providerPage.colTokens'), slotName: 'tokens', width: 120 },
  { title: t('providerPage.colAmount'), slotName: 'amount', width: 110 },
  { title: t('providerPage.colPrice'), slotName: 'price', width: 230 },
  { title: t('providerPage.colFreeModels'), slotName: 'free', width: 120 },
  { title: t('providerPage.colChannel'), slotName: 'channels', width: 140 },
])

const freeColumns = computed(() => [
  { title: t('providerPage.colModel'), dataIndex: 'model_name', width: 260 },
  { title: t('providerPage.colType'), slotName: 'type', width: 120 },
  { title: t('providerPage.colZeroPrice'), slotName: 'zeroPrice', width: 150 },
  { title: t('providerPage.colValidUntil'), slotName: 'validUntil', width: 130 },
  { title: t('providerPage.colNote'), dataIndex: 'note' },
  { title: ' ', slotName: 'actions', width: 60 },
])

const channelColumns = computed(() => [
  { title: t('providerPage.colChannel'), slotName: 'name', width: 180 },
  { title: t('providerPage.colStatus'), slotName: 'status', width: 90 },
  { title: t('providerPage.colBaseUrl'), dataIndex: 'base_url', width: 240 },
  { title: t('providerPage.colModels'), slotName: 'models' },
  { title: t('providerPage.colPriority'), dataIndex: 'priority', width: 90 },
  { title: t('providerPage.colUsed'), slotName: 'used', width: 110 },
  { title: t('providerPage.colLastError'), slotName: 'lastError' },
])

// trendOption 用量趋势折线图。
// Usage trend line chart.
const trendOption = computed(() => {
  const points = detail.value.trend || []
  if (!points.length) return null
  return {
    grid: { left: 56, right: 56, top: 24, bottom: 36 },
    tooltip: {
      trigger: 'axis',
      backgroundColor: '#1d2129',
      borderWidth: 0,
      textStyle: { color: '#fff', fontSize: 12 },
    },
    legend: {
      bottom: 0,
      left: 'center',
      icon: 'roundRect',
      itemWidth: 10,
      itemHeight: 10,
      textStyle: { fontSize: 11, color: '#86909c' },
    },
    xAxis: {
      type: 'category',
      data: points.map((item) => item.day),
      axisLabel: { fontSize: 11, color: '#86909c' },
      axisLine: { lineStyle: { color: '#e5e6eb' } },
    },
    yAxis: [
      {
        type: 'value',
        name: t('providerPage.trendTokens'),
        axisLabel: { fontSize: 11, color: '#86909c' },
        splitLine: { lineStyle: { color: '#f2f3f5' } },
      },
      {
        type: 'value',
        name: t('providerPage.trendAmount'),
        axisLabel: { fontSize: 11, color: '#86909c' },
        splitLine: { show: false },
      },
    ],
    series: [
      {
        name: t('providerPage.trendRequests'),
        type: 'bar',
        barMaxWidth: 26,
        itemStyle: { color: '#165DFF', borderRadius: [3, 3, 0, 0] },
        data: points.map((item) => item.request_count),
      },
      {
        name: t('providerPage.trendTokens'),
        type: 'line',
        smooth: true,
        yAxisIndex: 0,
        itemStyle: { color: '#00B42A' },
        data: points.map((item) => item.total_tokens),
      },
      {
        name: t('providerPage.trendAmount'),
        type: 'line',
        smooth: true,
        yAxisIndex: 1,
        itemStyle: { color: '#FF7D00' },
        data: points.map((item) => item.amount),
      },
    ],
  }
})

onMounted(loadData)
</script>

<style scoped>
.provider-page {
  padding: 20px;
}

.welcome-bar {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 16px;
  flex-wrap: wrap;
}

.welcome-title {
  margin: 4px 0 0;
  font-size: 20px;
  font-weight: 600;
}

.welcome-desc {
  margin: 4px 0 0;
  color: var(--color-text-3);
  font-size: 13px;
}

.welcome-meta {
  display: flex;
  align-items: center;
  gap: 10px;
}

.profile-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
  gap: 12px;
}

.profile-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.profile-label {
  font-size: 12px;
  color: var(--color-text-3);
}

.profile-value {
  font-size: 13px;
  word-break: break-all;
}

.stat-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
  gap: 12px;
  margin-bottom: 16px;
}

.stat-item {
  background: var(--color-bg-2);
  border: 1px solid var(--color-border-2);
  border-radius: 8px;
  padding: 14px 16px;
}

.stat-label {
  font-size: 12px;
  color: var(--color-text-3);
}

.stat-value {
  margin-top: 8px;
  font-size: 22px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}

.panel {
  background: var(--color-bg-2);
  border: 1px solid var(--color-border-2);
  border-radius: 8px;
  padding: 14px 16px;
  margin-bottom: 16px;
}

.panel-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 10px;
}

.panel-title {
  margin: 0;
  font-size: 14px;
  font-weight: 600;
}

.chart {
  height: 300px;
}

.model-name {
  font-weight: 500;
}

.model-id,
.muted {
  color: var(--color-text-3);
  font-size: 12px;
}

.cell-num {
  font-variant-numeric: tabular-nums;
}

.error-text {
  color: rgb(var(--danger-6));
  font-size: 12px;
}
</style>
