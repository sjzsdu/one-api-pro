<!--
  ProviderList 供应商（渠道商）总览
  Provider overview: usage, cost share and model distribution per channel vendor

  供应商 = 我们采购的对象（渠道商），不是模型品牌厂牌。
  同一个模型可能同时挂在多个供应商下（如 deepseek-v4-pro 同时在
  deepseek / alibailian / doubao），费用归属完全不同，因此必须按供应商分层。

  版本: v0.0.28
  日期: 2026-11-25
  作者: MCode
-->
<template>
  <div class="provider-page">
    <div class="welcome-bar">
      <div class="welcome-text">
        <h1 class="welcome-title">{{ t('providerPage.title') }}</h1>
        <p class="welcome-desc">{{ t('providerPage.subtitle') }}</p>
      </div>
      <div class="welcome-meta">
        <a-radio-group v-model="range" type="button" size="small" @change="loadData">
          <a-radio value="today">{{ t('admin.rangeToday') }}</a-radio>
          <a-radio value="7d">{{ t('admin.range7d') }}</a-radio>
          <a-radio value="30d">{{ t('admin.range30d') }}</a-radio>
          <a-radio value="all">{{ t('admin.rangeAll') }}</a-radio>
        </a-radio-group>
        <a-button size="mini" :loading="loading" @click="loadData">
          <template #icon><icon-refresh :size="14" /></template>
          {{ t('admin.refresh') }}
        </a-button>
      </div>
    </div>

    <a-spin :loading="loading" style="width: 100%">
      <div class="stat-grid">
        <div v-for="item in statItems" :key="item.label" class="stat-item">
          <div class="stat-head">
            <span class="stat-label">{{ item.label }}</span>
            <span class="stat-icon"><component :is="item.icon" :size="15" /></span>
          </div>
          <div class="stat-value">{{ item.value }}</div>
          <div v-if="item.hint" class="stat-hint">{{ item.hint }}</div>
        </div>
      </div>

      <a-row :gutter="16">
        <a-col :xs="24" :lg="10">
          <div class="panel">
            <div class="panel-head">
              <h2 class="panel-title">{{ t('providerPage.sectionShare') }}</h2>
            </div>
            <VChart v-if="shareOption" class="chart" :option="shareOption" autoresize />
            <a-empty v-else description="—" />
          </div>
        </a-col>
        <a-col :xs="24" :lg="14">
          <div class="panel">
            <div class="panel-head">
              <h2 class="panel-title">{{ t('providerPage.sectionTokens') }}</h2>
            </div>
            <VChart v-if="tokenOption" class="chart" :option="tokenOption" autoresize />
            <a-empty v-else description="—" />
          </div>
        </a-col>
      </a-row>

      <div class="panel">
        <div class="panel-head">
          <h2 class="panel-title">{{ t('providerPage.sectionOverview') }}</h2>
        </div>
        <a-table
          row-key="provider_id"
          :columns="columns"
          :data="providers"
          :pagination="{ pageSize: 20, showTotal: true, showJumper: true }"
          :scroll="{ x: 1500 }"
          size="small"
        >
          <template #provider="{ record }">
            <div class="provider-name">
              <span>{{ record.provider_name || record.provider_id }}</span>
              <a-tag v-if="record.category" size="small">{{ record.category }}</a-tag>
            </div>
            <div class="provider-id">{{ record.provider_id }}</div>
          </template>
          <template #channels="{ record }">
            <span>{{ record.enabled_channels }} / {{ record.channel_count }}</span>
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
          <template #share="{ record }">
            <span class="cell-num">{{ (record.share * 100).toFixed(1) }}%</span>
          </template>
          <template #models="{ record }">
            <span class="cell-num">{{ record.model_count }}</span>
          </template>
          <template #free="{ record }">
            <a-space wrap size="mini">
              <a-tag v-for="name in record.free_models" :key="name" color="green" size="small">{{ name }}</a-tag>
              <span v-if="!record.free_models?.length" class="muted">—</span>
            </a-space>
          </template>
          <template #unpriced="{ record }">
            <a-tooltip v-if="record.unpriced_models?.length" :content="t('providerPage.unpricedHint')">
              <a-tag color="orange" size="small">{{ record.unpriced_models.length }}</a-tag>
            </a-tooltip>
            <span v-else class="muted">—</span>
          </template>
          <template #baseUrl="{ record }">
            <span class="model-id">{{ record.default_base_url || '—' }}</span>
          </template>
          <template #actions="{ record }">
            <a-button size="small" type="text" @click="goDetail(record)">
              {{ t('providerPage.sectionModels') }}
              <template #icon><icon-launch :size="13" /></template>
            </a-button>
          </template>
        </a-table>
      </div>
    </a-spin>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { Message } from '@arco-design/web-vue'
import {
  IconRefresh,
  IconLaunch,
  IconApps,
  IconBarChart,
  IconList,
  IconGift,
  IconStorage,
} from '@arco-design/web-vue/es/icon'
import VChart from 'vue-echarts'
import 'echarts'
import providerApi from '@/api/provider'
import { formatYuan } from '@/utils/quota'

const { t } = useI18n()
const router = useRouter()

const loading = ref(false)
const range = ref('7d')
const providers = ref([])

const providerColors = [
  '#165DFF', '#00B42A', '#FF7D00', '#F53F3F', '#722ED1',
  '#14C9C9', '#3491FA', '#D91AD9', '#F7BA1E', '#86909C',
]

// formatInt 千分位整数。
// Thousands-separated integer.
function formatInt(value) {
  const num = Number(value || 0)
  return num.toLocaleString('en-US')
}

async function loadData() {
  loading.value = true
  try {
    const { data } = await providerApi.list(range.value)
    if (data.success) {
      providers.value = data.data?.items || []
    } else {
      Message.error(data.message || t('providerPage.loadFailed'))
    }
  } catch (e) {
    Message.error(e.response?.data?.message || e.message || t('providerPage.loadFailed'))
  } finally {
    loading.value = false
  }
}

function goDetail(record) {
  router.push({ name: 'ProviderDetail', params: { id: record.provider_id } })
}

const totals = computed(() => {
  const acc = { providers: providers.value.length, channels: 0, requests: 0, tokens: 0, amount: 0, free: 0 }
  for (const item of providers.value) {
    acc.channels += item.channel_count || 0
    acc.requests += item.request_count || 0
    acc.tokens += item.total_tokens || 0
    acc.amount += item.amount || 0
    acc.free += item.free_models?.length || 0
  }
  return acc
})

const statItems = computed(() => [
  { label: t('providerPage.statProviders'), value: formatInt(totals.value.providers), icon: IconApps },
  { label: t('providerPage.statChannels'), value: formatInt(totals.value.channels), icon: IconStorage },
  { label: t('providerPage.statRequests'), value: formatInt(totals.value.requests), icon: IconList },
  { label: t('providerPage.statTokens'), value: formatInt(totals.value.tokens), icon: IconBarChart },
  { label: t('providerPage.statAmount'), value: formatYuan(totals.value.amount), icon: IconGift },
])

const columns = computed(() => [
  { title: t('providerPage.colProvider'), slotName: 'provider', width: 200 },
  { title: t('providerPage.colChannels'), slotName: 'channels', width: 90 },
  { title: t('providerPage.colRequests'), slotName: 'requests', width: 100 },
  { title: t('providerPage.colTokens'), slotName: 'tokens', width: 120 },
  { title: t('providerPage.colAmount'), slotName: 'amount', width: 110 },
  { title: t('providerPage.colShare'), slotName: 'share', width: 80 },
  { title: t('providerPage.colModels'), slotName: 'models', width: 80 },
  { title: t('providerPage.colFreeModels'), slotName: 'free', width: 220 },
  { title: t('providerPage.colUnpriced'), slotName: 'unpriced', width: 90 },
  { title: t('providerPage.colBaseUrl'), slotName: 'baseUrl', width: 240 },
  { title: t('providerPage.sectionModels'), slotName: 'actions', width: 130, fixed: 'right' },
])

// shareOption 费用占比饼图。
// Cost-share pie chart.
const shareOption = computed(() => {
  const items = providers.value.filter((item) => (item.amount || 0) > 0)
  if (!items.length) return null
  return {
    color: providerColors,
    tooltip: {
      trigger: 'item',
      backgroundColor: '#1d2129',
      borderWidth: 0,
      textStyle: { color: '#fff', fontSize: 12 },
      formatter: (p) => `${p.name}<br/>${formatYuan(p.value)} (${p.percent}%)`,
    },
    legend: {
      bottom: 0,
      left: 'center',
      type: 'scroll',
      icon: 'roundRect',
      itemWidth: 10,
      itemHeight: 10,
      textStyle: { fontSize: 11, color: '#86909c' },
    },
    series: [
      {
        type: 'pie',
        radius: ['42%', '66%'],
        center: ['50%', '44%'],
        itemStyle: { borderRadius: 4, borderColor: '#fff', borderWidth: 2 },
        label: { show: false },
        data: items.map((item) => ({ name: item.provider_name || item.provider_id, value: item.amount })),
      },
    ],
  }
})

// tokenOption 各供应商 Token 柱状图。
// Per-provider token bar chart.
const tokenOption = computed(() => {
  const items = providers.value.slice(0, 12)
  if (!items.length) return null
  return {
    color: providerColors,
    grid: { left: 56, right: 16, top: 16, bottom: 56 },
    tooltip: {
      trigger: 'axis',
      axisPointer: { type: 'shadow' },
      backgroundColor: '#1d2129',
      borderWidth: 0,
      textStyle: { color: '#fff', fontSize: 12 },
    },
    xAxis: {
      type: 'category',
      data: items.map((item) => item.provider_name || item.provider_id),
      axisLabel: { fontSize: 11, color: '#86909c', interval: 0, rotate: 30 },
      axisLine: { lineStyle: { color: '#e5e6eb' } },
    },
    yAxis: {
      type: 'value',
      axisLabel: { fontSize: 11, color: '#86909c' },
      splitLine: { lineStyle: { color: '#f2f3f5' } },
    },
    series: [
      {
        name: t('providerPage.statTokens'),
        type: 'bar',
        barMaxWidth: 34,
        itemStyle: { borderRadius: [3, 3, 0, 0] },
        data: items.map((item) => item.total_tokens || 0),
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
  margin: 0;
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

.stat-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 12px;
  margin-bottom: 16px;
}

.stat-item {
  background: var(--color-bg-2);
  border: 1px solid var(--color-border-2);
  border-radius: 8px;
  padding: 14px 16px;
}

.stat-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
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

.stat-hint {
  margin-top: 2px;
  font-size: 11px;
  color: var(--color-text-3);
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
  height: 320px;
}

.provider-name {
  display: flex;
  align-items: center;
  gap: 6px;
}

.provider-id,
.model-id,
.muted {
  color: var(--color-text-3);
  font-size: 12px;
}

.cell-num {
  font-variant-numeric: tabular-nums;
}
</style>
