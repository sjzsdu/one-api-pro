<template>
  <div class="model-catalog-page">
    <div class="page-header">
      <div>
        <h1>模型</h1>
        <p>已配置渠道的模型目录。测试会向指定渠道发送最小请求，不会走 auto 路由。</p>
      </div>
      <a-button type="primary" :loading="loading" @click="loadModels">刷新列表</a-button>
    </div>

    <div class="toolbar">
      <a-input-search v-model="keyword" placeholder="搜索模型、渠道、能力" allow-clear :style="{ width: '320px' }" />
      <span class="count">{{ filteredModels.length }} 个渠道模型</span>
    </div>

    <a-table row-key="rowKey" :columns="columns" :data="filteredModels" :loading="loading"
      :pagination="{ pageSize: 20, showTotal: true, showJumper: true }" :scroll="{ x: 1450 }" size="small">
      <template #model="{ record }">
        <div class="model-name">{{ record.display_name || record.model_name }}</div>
        <div v-if="record.display_name" class="model-id">{{ record.model_name }}</div>
        <div v-if="record.description" class="description" :title="record.description">{{ record.description }}</div>
      </template>
      <template #capabilities="{ record }">
        <a-space wrap size="mini">
          <a-tag v-for="modality in record.input_modalities || []" :key="`in-${modality}`" color="blue">输入 {{ modality }}</a-tag>
          <a-tag v-for="modality in record.output_modalities || []" :key="`out-${modality}`" color="green">输出 {{ modality }}</a-tag>
          <a-tag v-if="record.supported_parameters?.includes('tools')" color="purple">工具</a-tag>
        </a-space>
      </template>
      <template #pricing="{ record }">
        <template v-if="record.input_price_usd_per_million != null || record.output_price_usd_per_million != null">
          <div>输入 ${{ formatPrice(record.input_price_usd_per_million) }}/M</div>
          <div>输出 ${{ formatPrice(record.output_price_usd_per_million) }}/M</div>
        </template>
        <span v-else>—</span>
      </template>
      <template #channel="{ record }">
        <div>{{ record.channel_name }}</div>
        <div class="model-id">#{{ record.channel_id }} · {{ record.group }}</div>
      </template>
      <template #status="{ record }">
        <a-tag :color="record.channel_status === 1 ? 'green' : 'red'">{{ record.channel_status === 1 ? '启用' : '停用' }}</a-tag>
      </template>
      <template #actions="{ record }">
        <a-button size="small" :loading="testingKey === record.rowKey" @click="testModel(record)">测试</a-button>
        <div v-if="testResults[record.rowKey]" class="test-result" :class="testResults[record.rowKey].success ? 'success' : 'failure'">
          {{ testResults[record.rowKey].message }}
        </div>
      </template>
    </a-table>
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { Message } from '@arco-design/web-vue'
import api from '@/api'

const loading = ref(false)
const models = ref([])
const keyword = ref('')
const testingKey = ref('')
const testResults = ref({})

const columns = [
  { title: '模型', slotName: 'model', width: 300 },
  { title: '能力', slotName: 'capabilities', width: 250 },
  { title: '上下文', dataIndex: 'contextLabel', width: 100 },
  { title: 'OpenRouter 参考价格', slotName: 'pricing', width: 165 },
  { title: '渠道', slotName: 'channel', width: 170 },
  { title: '状态', slotName: 'status', width: 80 },
  { title: '验证', slotName: 'actions', fixed: 'right', width: 170 },
]

const filteredModels = computed(() => {
  const term = keyword.value.trim().toLowerCase()
  const rows = models.value.map(item => ({ ...item, rowKey: `${item.channel_id}:${item.model_name}`, contextLabel: formatContext(item.context_length) }))
  if (!term) return rows
  return rows.filter((item) => [item.model_name, item.display_name, item.channel_name, item.description,
    ...(item.input_modalities || []), ...(item.output_modalities || []), ...(item.supported_parameters || [])]
    .filter(Boolean).join(' ').toLowerCase().includes(term))
})

async function loadModels() {
  loading.value = true
  try {
    const { data } = await api.get('/api/channel/model_catalog')
    if (!data.success) throw new Error(data.message || '加载失败')
    models.value = data.data || []
  } catch (error) {
    Message.error(error.response?.data?.message || error.message || '加载模型失败')
  } finally {
    loading.value = false
  }
}

async function testModel(record) {
  testingKey.value = record.rowKey
  try {
    const { data } = await api.get(`/api/channel/test/${record.channel_id}`, { params: { model: record.model_name } })
    testResults.value[record.rowKey] = data.success
      ? { success: true, message: `可用 · ${Number(data.time || 0).toFixed(2)} 秒` }
      : { success: false, message: data.message || '不可用' }
  } catch (error) {
    testResults.value[record.rowKey] = { success: false, message: error.response?.data?.message || error.message || '测试失败' }
  } finally {
    testingKey.value = ''
  }
}

function formatContext(value) {
  if (!value) return '—'
  return value >= 1000 ? `${(value / 1000).toFixed(value % 1000 === 0 ? 0 : 1)}K` : String(value)
}

function formatPrice(value) {
  const number = Number(value)
  if (!Number.isFinite(number)) return '—'
  if (number === 0) return '0'
  return number < 0.01 ? number.toFixed(4) : number.toFixed(number < 1 ? 3 : 2).replace(/\.0+$/, '')
}

onMounted(loadModels)
</script>

<style scoped>
.model-catalog-page { padding: 24px; }
.page-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 24px; margin-bottom: 20px; }
.page-header h1 { margin: 0 0 6px; font-size: 26px; }
.page-header p { margin: 0; color: var(--color-text-3); }
.toolbar { display: flex; align-items: center; gap: 12px; margin-bottom: 16px; }
.count, .model-id { color: var(--color-text-3); font-size: 12px; }
.model-name { font-weight: 600; }
.description { display: -webkit-box; overflow: hidden; margin-top: 3px; color: var(--color-text-2); font-size: 12px; line-height: 1.4; -webkit-box-orient: vertical; -webkit-line-clamp: 2; }
.test-result { margin-top: 5px; font-size: 12px; line-height: 1.3; }
.success { color: rgb(var(--green-6)); }
.failure { color: rgb(var(--red-6)); }
@media (max-width: 640px) { .model-catalog-page { padding: 16px; } .page-header { flex-direction: column; } }
</style>
