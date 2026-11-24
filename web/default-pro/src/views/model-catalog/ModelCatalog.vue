<template>
  <div class="model-catalog-page">
    <div class="page-header">
      <div>
        <h1>{{ label('模型', 'Models') }}</h1>
        <p>{{ label('统一能力目录。按操作测试会调用指定渠道并可能产生费用；批量测试排除生成、实时和需手动输入媒体的操作。', 'Unified capability catalog. Operation probes call the selected channel and may incur charges. Bulk probes exclude generation, realtime and manual media input.') }}</p>
      </div>
      <a-space wrap class="header-actions">
        <a-button :loading="loading" @click="loadModels">{{ label('刷新列表', 'Refresh') }}</a-button>
        <a-button type="primary" :loading="testingAll" @click="testingAll ? stopTestAll() : confirmTestAll()">
          {{ testingAll ? `停止测试 (${testedCount}/${testAllQueue.length})` : label('批量测试（非生成）', 'Probe non-generation') }}
        </a-button>
      </a-space>
    </div>

    <div class="toolbar">
      <a-input-search v-model="keyword" :placeholder="label('搜索模型、渠道、类型', 'Search model, channel or category')" allow-clear class="catalog-search" />
      <a-radio-group v-model="activeCategory" type="button" size="small" class="category-filter">
        <a-radio value="all">{{ label('全部', 'All') }} {{ models.length }}</a-radio>
        <a-radio v-for="group in categoryGroups" :key="group.key" :value="group.key">{{ group.label }} {{ group.count }}</a-radio>
      </a-radio-group>
      <span class="count">{{ filteredModels.length }} {{ label('个渠道模型', 'channel models') }}</span>
      <span v-if="testSummary" class="test-summary">{{ testSummary }}</span>
    </div>

    <a-table row-key="rowKey" :columns="columns" :data="filteredModels" :loading="loading"
      :pagination="{ pageSize: 20, showTotal: true, showJumper: true }" :scroll="{ x: 1600 }" size="small">
      <template #model="{ record }">
        <div class="model-name">{{ record.display_name || record.model_name }}</div>
        <div v-if="record.display_name" class="model-id">{{ record.model_name }}</div>
        <div v-if="record.description" class="description" :title="record.description">{{ record.description }}</div>
      </template>
      <template #category="{ record }">
        <a-tag :color="record.categoryColor" size="small">{{ record.categoryLabel }}</a-tag>
      </template>
      <template #capabilities="{ record }">
        <a-space wrap size="mini">
          <a-tag v-for="modality in record.input_modalities || []" :key="`in-${modality}`" color="blue">输入 {{ modality }}</a-tag>
          <a-tag v-for="modality in record.output_modalities || []" :key="`out-${modality}`" color="green">输出 {{ modality }}</a-tag>
          <a-tag v-for="operation in modelOperations(record)" :key="operation" :color="operationRunnable(record, operation) ? 'arcoblue' : 'gray'">{{ modelOperationLabel(operation, locale) }} · {{ operationRunnable(record, operation) ? label('已实现', 'Implemented') : label('待接入', 'Not implemented') }}</a-tag>
          <a-tag v-if="record.supported_parameters?.includes('tools')" color="purple">{{ label('工具', 'Tools') }}</a-tag>
          <span class="model-id">{{ capabilitySourceLabel(record.capabilities?.source) }}</span>
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
        <a-select :model-value="probeOperation(record)" :aria-label="label('测试操作', 'Probe operation')" size="small" @update:model-value="selectedProbeOperations[record.rowKey] = $event">
          <a-option v-for="operation in modelOperations(record)" :key="operation" :value="operation" :disabled="!probeRunnable(record, operation)">{{ modelOperationLabel(operation, locale) }}</a-option>
        </a-select>
        <a-space size="mini">
          <a-button size="small" :loading="testingKey === record.rowKey || testResults[record.rowKey]?.pending" :disabled="testingAll || !probeRunnable(record, probeOperation(record)) || record.channel_status !== 1" @click="testModel(record)">{{ label('测试', 'Probe') }}</a-button>
          <a-button size="small" type="text" @click="capabilityRecord = record">{{ label('编辑能力', 'Edit capabilities') }}</a-button>
        </a-space>
        <div v-if="!probeRunnable(record, probeOperation(record))" class="model-id">{{ label('使用调用示例验证', 'Verify using an API example') }}</div>
        <div v-if="testResults[record.rowKey]" class="test-result" :class="testResults[record.rowKey].skipped ? 'skipped' : (testResults[record.rowKey].pending ? 'pending' : (testResults[record.rowKey].success ? 'success' : 'failure'))">
          {{ testResults[record.rowKey].message }}
        </div>
      </template>
    </a-table>
    <ModelCapabilityModal :visible="!!capabilityRecord" :record="capabilityRecord" :saving="capabilitySaving" @cancel="capabilityRecord = null" @save="saveCapability" />
    <a-modal :visible="!!probeRecord" :title="probeModalTitle" :ok-loading="!!testingKey" :ok-text="probeProducesMedia(probeInputOperation) && probeResultMedia.length ? label('重新生成', 'Regenerate') : label('开始测试', 'Run probe')" :width="probeProducesMedia(probeInputOperation) ? 640 : 520" @cancel="probeRecord = null" @before-ok="submitInputProbe">
      <a-alert type="warning" style="margin-bottom: 16px">{{ probeWarning }}</a-alert>
      <a-form :model="probeDraft" layout="vertical">
        <a-form-item v-if="probeInputOperation === 'audio.transcribe'" :label="label('音频 URL（必填）', 'Audio URL (required)')"><a-input v-model="probeDraft.audio" placeholder="https://…/audio.wav" /></a-form-item>
        <template v-if="probeInputOperation === 'image.edit'">
          <a-form-item :label="label('图片 URL（必填）', 'Image URL (required)')"><a-input v-model="probeDraft.image" placeholder="https://…/image.png" /></a-form-item>
          <a-form-item :label="label('编辑提示词（必填）', 'Editing prompt (required)')"><a-textarea v-model="probeDraft.prompt" /></a-form-item>
        </template>
        <template v-if="probeInputOperation === 'audio.speech'">
          <a-form-item :label="label('合成文本（必填）', 'Text to synthesize (required)')">
            <a-textarea v-model="probeDraft.input" :auto-size="{ minRows: 3, maxRows: 6 }" :placeholder="label('输入要让模型朗读的文字，也可从 txt 文件导入', 'Enter text for the model to speak, or import from a .txt file')" />
            <a-upload :show-file-list="false" :before-upload="importTextFile" accept=".txt,.md,.text,text/plain" class="probe-file-import">
              <a-button size="small">{{ label('从文件导入文本', 'Import text from file') }}</a-button>
            </a-upload>
          </a-form-item>
          <a-row :gutter="12">
            <a-col :span="12">
              <a-form-item :label="label('音色（必填）', 'Voice (required)')">
                <a-input v-model="probeDraft.voice" :placeholder="label('如 longwan / longyu，或模型支持的音色 ID', 'e.g. longwan / longyu, or a supported voice ID')" />
              </a-form-item>
            </a-col>
            <a-col :span="12">
              <a-form-item :label="label('语速（0.5–2）', 'Speed (0.5–2)')">
                <a-input-number v-model="probeDraft.speed" :min="0.5" :max="2" :step="0.1" style="width: 100%" :placeholder="label('默认 1.0', 'default 1.0')" />
              </a-form-item>
            </a-col>
          </a-row>
          <a-form-item :label="label('输出格式', 'Output format')">
            <a-select v-model="probeDraft.response_format" style="width: 200px">
              <a-option value="wav">wav</a-option>
              <a-option value="mp3">mp3</a-option>
              <a-option value="opus">opus</a-option>
              <a-option value="pcm">pcm</a-option>
            </a-select>
          </a-form-item>
          <a-alert v-if="voiceHint" type="info" show-icon style="margin-bottom: 8px">{{ voiceHint }}</a-alert>
        </template>
        <template v-if="probeInputOperation === 'image.generate'">
          <a-form-item :label="label('生图提示词（必填）', 'Image prompt (required)')">
            <a-textarea v-model="probeDraft.prompt" :auto-size="{ minRows: 3, maxRows: 6 }" :placeholder="label('描述要生成的画面，也可从 txt 文件导入', 'Describe the image to generate, or import from a .txt file')" />
            <a-upload :show-file-list="false" :before-upload="importTextFile" accept=".txt,.md,.text,text/plain" class="probe-file-import">
              <a-button size="small">{{ label('从文件导入提示词', 'Import prompt from file') }}</a-button>
            </a-upload>
          </a-form-item>
          <a-row :gutter="12">
            <a-col :span="12">
              <a-form-item :label="label('尺寸', 'Size')">
                <a-select v-model="probeDraft.size" style="width: 100%" allow-create>
                  <a-option value="1024x1024">1024x1024</a-option>
                  <a-option value="1024x1536">1024x1536</a-option>
                  <a-option value="1536x1024">1536x1024</a-option>
                  <a-option value="1280x720">1280x720</a-option>
                  <a-option value="720x1280">720x1280</a-option>
                </a-select>
              </a-form-item>
            </a-col>
            <a-col :span="12">
              <a-form-item :label="label('生成数量', 'Count')">
                <a-input-number v-model="probeDraft.n" :min="1" :max="4" style="width: 100%" />
              </a-form-item>
            </a-col>
          </a-row>
        </template>
      </a-form>
      <div v-if="probeResultMedia.length" class="probe-media-preview">
        <div class="preview-title">{{ label('生成结果', 'Generated result') }}</div>
        <template v-for="(item, index) in probeResultMedia" :key="index">
          <audio v-if="item.kind === 'audio'" :src="mediaSrc(item)" controls class="probe-audio" />
          <div v-else-if="item.kind === 'image' && item.base64" class="probe-image-wrap">
            <img :src="mediaSrc(item)" :alt="item.description || 'probe image'" class="probe-image" />
          </div>
          <a-link v-else-if="item.kind === 'image' && item.description" :href="item.description" target="_blank">{{ label('查看生成图片', 'View generated image') }}</a-link>
        </template>
      </div>
    </a-modal>
  </div>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { Message, Modal } from '@arco-design/web-vue'
import { useI18n } from 'vue-i18n'
import ModelCapabilityModal from '@/components/ModelCapabilityModal.vue'
import api from '@/api'
import { probeRunnable, probeNeedsInput, probeBulkRunnable, buildProbeInput, probeProducesMedia } from '@/utils/modelProbe'
import { classifyModel, groupModelsByCategory, modelCategoryColor, modelCategoryLabel, modelCategoryRank, modelOperations, modelOperationLabel } from '@/utils/modelCategory'

const { locale } = useI18n()
const label = (zh, en) => locale.value === 'en' ? en : zh
const selectedProbeOperations = ref({})
const probeRecord = ref(null)
const probeInputOperation = ref('')
const probeDraft = ref({ audio: '', image: '', prompt: '', input: '', voice: '', speed: '', response_format: 'wav', size: '1024x1024', n: 1 })
const probeResultMedia = ref([])
const capabilityRecord = ref(null)
const capabilitySaving = ref(false)
const loading = ref(false)
const models = ref([])
const keyword = ref('')
const activeCategory = ref('all')
const testingKey = ref('')
const testResults = ref({})
const testingAll = ref(false)
const testAllQueue = ref([])
const testedCount = ref(0)
const testSummary = ref('')
const testAllAbort = ref(false)
const TEST_CONCURRENCY = 5
const narrowViewport = ref(typeof window !== 'undefined' && window.innerWidth <= 640)
const updateViewport = () => { narrowViewport.value = window.innerWidth <= 640 }
onMounted(() => window.addEventListener('resize', updateViewport))
onUnmounted(() => window.removeEventListener('resize', updateViewport))

const columns = computed(() => [
  { title: label('模型', 'Model'), slotName: 'model', width: 300 },
  { title: label('渠道', 'Channel'), slotName: 'channel', width: 170 },
  { title: label('类型', 'Category'), slotName: 'category', width: 100 },
  { title: label('能力', 'Capabilities'), slotName: 'capabilities', width: 250 },
  { title: label('上下文', 'Context'), dataIndex: 'contextLabel', width: 100 },
  { title: label('上游参考价格', 'Reference price'), slotName: 'pricing', width: 165 },
  { title: label('状态', 'Status'), slotName: 'status', width: 80 },
  { title: label('验证', 'Probe'), slotName: 'actions', fixed: narrowViewport.value ? undefined : 'right', width: 200 },
])

const probeModalTitle = computed(() => {
  if (probeInputOperation.value === 'audio.speech') return label('语音合成调试', 'Speech synthesis probe')
  if (probeInputOperation.value === 'image.generate') return label('文生图调试', 'Image generation probe')
  return label('填写探测输入并确认费用', 'Provide probe input and confirm cost')
})
const probeWarning = computed(() => {
  if (probeInputOperation.value === 'audio.speech') return label('将用这里的文本和音色向该渠道发起真实语音合成，可能产生费用；生成结果会直接显示在下方。', 'This sends a real speech request with the text and voice above and may incur charges; the audio result is shown below.')
  if (probeInputOperation.value === 'image.generate') return label('将用这里的提示词向该渠道发起真实生图请求，可能产生费用；生成结果会直接显示在下方。', 'This sends a real image request with the prompt above and may incur charges; the image result is shown below.')
  return label('将向该渠道发送真实媒体请求并可能产生费用。仅使用有权处理的公网媒体 URL。', 'This sends a real media request and may incur charges. Use a public media URL you are authorized to process.')
})
const voiceHint = computed(() => {
  const name = String(probeRecord.value?.model_name || '')
  if (probeInputOperation.value === 'audio.speech' && name.includes('cosyvoice')) {
    return label('百炼 CosyVoice 常用音色：longwan（龙婉）、longyu（龙宇）、longfei（龙飞）等，以渠道实际支持为准。', 'CosyVoice common voices: longwan, longyu, longfei, etc. — subject to your channel.')
  }
  return ''
})

function mediaSrc(item) {
  if (item.base64) return `data:${item.mime_type || 'application/octet-stream'};base64,${item.base64}`
  return item.description || ''
}

// 从本地 txt/md 文件读取文本填入探测输入，避免手动粘贴长文本。
function importTextFile(file) {
  const reader = new FileReader()
  reader.onload = () => {
    const text = String(reader.result || '')
    if (probeInputOperation.value === 'image.generate') probeDraft.value.prompt = text
    else if (probeInputOperation.value === 'audio.speech') probeDraft.value.input = text
  }
  reader.readAsText(file, 'utf-8')
  return false
}

// categoryGroups powers the filter tabs; counts are computed over the full catalog.
const categoryGroups = computed(() => groupModelsByCategory(models.value, item => item.model_name, locale.value)
  .map(group => ({ key: group.key, label: group.label, count: group.items.length })))

const filteredModels = computed(() => {
  const term = keyword.value.trim().toLowerCase()
  const rows = models.value.map(item => {
    const category = classifyModel(item)
    return {
      ...item,
      rowKey: `${item.channel_id}:${item.model_name}`,
      contextLabel: formatContext(item.context_length),
      categoryKey: category,
      categoryLabel: modelCategoryLabel(category, locale.value),
      categoryColor: modelCategoryColor(category),
      categoryRank: modelCategoryRank(category),
    }
  })
  const matched = !term ? rows : rows.filter(item => [item.model_name, item.display_name, item.channel_name, item.description,
    item.categoryLabel, ...(item.input_modalities || []), ...(item.output_modalities || []), ...(item.supported_parameters || [])]
    .filter(Boolean).join(' ').toLowerCase().includes(term))
  return matched
    .filter(item => activeCategory.value === 'all' || item.categoryKey === activeCategory.value)
    .sort((a, b) => a.categoryRank - b.categoryRank
      || String(a.display_name || a.model_name).localeCompare(String(b.display_name || b.model_name)))
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

// 单个模型的测试请求；批量与单测共用，保证结果口径一致。
async function runOneTest(record, input, operation = probeOperation(record)) {
  try {
    const { data } = await api.post(`/api/channel/test_operation/${record.channel_id}`, { model: record.model_name, operation, confirm_cost: true, ...(input ? { input } : {}) })
    if (data.status === 'unsupported_probe') return { skipped: true, message: label('使用调用示例验证', 'Verify using an API example') }
    const media = Array.isArray(data.data?.media) ? data.data.media : []
    const seconds = Number(data.data?.time || data.time || 0)
    return data.success
      ? { success: true, message: `可用 · ${seconds.toFixed(2)} 秒`, media }
      : { success: false, message: data.message || '不可用', media }
  } catch (error) {
    return { success: false, message: error.response?.data?.message || error.message || '测试失败' }
  }
}

function operationRunnable(record, operation) {
  const declared = record.capabilities?.operations?.[operation]?.modes || []
  return !!record.gateway_support?.[operation]?.some(mode => !declared.length || declared.includes(mode))
}
function probeOperation(record) {
  return selectedProbeOperations.value[record.rowKey] || modelOperations(record).find(operation => probeRunnable(record, operation)) || ''
}
function capabilitySourceLabel(source) {
  return ({ override: label('人工配置', 'Manual override'), catalog: label('目录声明', 'Catalog'), catalog_modalities: label('目录模态', 'Catalog modalities'), builtin: label('内置声明', 'Built-in'), name_inference: label('名称推断 · 待确认', 'Name inference · unverified'), invalid_catalog: label('能力配置无效', 'Invalid capabilities') })[source] || label('待补充能力', 'Capabilities unknown')
}
async function saveCapability(capabilities) {
  capabilitySaving.value = true
  try {
    const row = capabilityRecord.value
    const { data } = await api.put('/api/model/capabilities', { provider: row.provider, model_name: row.catalog_model_name || row.model_name, capabilities })
    if (!data.success) throw new Error(data.message || label('保存失败', 'Save failed'))
    capabilityRecord.value = null
    Message.success(label('能力配置已保存', 'Capabilities saved'))
    await loadModels()
  } catch (error) { Message.error(error.message) }
  finally { capabilitySaving.value = false }
}
function testModel(record) {
  if (testingAll.value || !probeRunnable(record, probeOperation(record))) return
  if (probeNeedsInput(record, probeOperation(record))) {
    probeRecord.value = record
    probeInputOperation.value = probeOperation(record)
    probeDraft.value = { audio: '', image: '', prompt: '', input: '', voice: '', speed: '', response_format: 'wav', size: '1024x1024', n: 1 }
    probeResultMedia.value = []
    return
  }
  Modal.confirm({ title: label('确认模型调用测试', 'Confirm model probe'), content: label('该测试会向上游发送真实请求并可能产生费用。是否继续？', 'This probe sends a real upstream request and may incur charges. Continue?'), onOk: async () => {
    testingKey.value = record.rowKey
    testResults.value[record.rowKey] = await runOneTest(record)
    testingKey.value = ''
  } })
}

async function submitInputProbe() {
  let input
  try { input = buildProbeInput(probeInputOperation.value, probeDraft.value) }
  catch (error) { Message.error(error.message); return false }
  const record = probeRecord.value
  const isMedia = probeProducesMedia(probeInputOperation.value)
  testingKey.value = record.rowKey
  try {
    const result = await runOneTest(record, input, probeInputOperation.value)
    testResults.value[record.rowKey] = result
    if (isMedia) {
      probeResultMedia.value = result.media || []
      // 生成类探测保持弹窗打开以展示预览；返回 false 阻止 a-modal 自动关闭。
      return false
    }
    probeRecord.value = null
    return false
  } catch (error) {
    probeRecord.value = null
    return false
  } finally { testingKey.value = '' }
}

// 一键测试当前筛选出的全部模型。用有界并发避免打爆上游；停止时已发出的请求
// 仍会回填结果，只是不再派发新请求。
function confirmTestAll() {
  Modal.confirm({ title: label('确认批量调用测试', 'Confirm bulk probes'), content: label('将测试支持自动输入的操作，排除生成、实时和需手动输入的媒体。真实调用可能产生费用。', 'Calls probes with automatic inputs; generation, realtime and manual media input are excluded. Real requests may incur charges.'), onOk: testAll })
}
async function testAll() {
  const queue = filteredModels.value.filter(record => record.channel_status === 1 && probeBulkRunnable(record, probeOperation(record)))
  if (!queue.length) {
    Message.info('当前没有可测试的模型')
    return
  }
  testingAll.value = true
  testAllAbort.value = false
  testAllQueue.value = queue
  testedCount.value = 0
  testSummary.value = `测试中 0/${queue.length}`
  // 全部置为进行中，清除上一轮结果
  queue.forEach((record) => {
    testResults.value[record.rowKey] = { success: true, message: '测试中…', pending: true }
  })

  let cursor = 0
  const worker = async () => {
    while (!testAllAbort.value) {
      const index = cursor++
      if (index >= queue.length) return
      const record = queue[index]
      const result = await runOneTest(record)
      testResults.value[record.rowKey] = result
      testedCount.value++
      testSummary.value = `测试中 ${testedCount.value}/${queue.length}`
    }
  }
  await Promise.all(Array.from({ length: Math.min(TEST_CONCURRENCY, queue.length) }, worker))

  testingAll.value = false
  const results = queue.map((r) => testResults.value[r.rowKey])
  const ok = results.filter((r) => r && r.success).length
  const fail = results.length - ok
  testSummary.value = testAllAbort.value
    ? `已停止 · 完成 ${testedCount.value}/${queue.length} · 成功 ${ok} / 失败 ${fail}`
    : `完成 · 成功 ${ok} / 失败 ${fail}`
  if (!testAllAbort.value) {
    fail > 0 ? Message.warning(`测试完成：${ok} 个可用，${fail} 个失败`) : Message.success(`全部 ${ok} 个模型可用`)
  }
}

function stopTestAll() {
  testAllAbort.value = true
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
.header-actions { max-width: 100%; }
.catalog-search { width: 300px; max-width: 100%; }
.toolbar { display: flex; align-items: center; flex-wrap: wrap; gap: 12px; margin-bottom: 16px; }
.category-filter { max-width: 100%; flex-shrink: 0; overflow-x: auto; white-space: nowrap; }
.category-filter :deep(.arco-radio-button), .category-filter :deep(.arco-radio-button-content) { flex-shrink: 0; white-space: nowrap; }
.count, .model-id { color: var(--color-text-3); font-size: 12px; }
.test-summary { color: var(--color-text-2); font-size: 12px; }
.model-name { font-weight: 600; }
.description { display: -webkit-box; overflow: hidden; margin-top: 3px; color: var(--color-text-2); font-size: 12px; line-height: 1.4; -webkit-box-orient: vertical; -webkit-line-clamp: 2; }
.test-result { margin-top: 5px; font-size: 12px; line-height: 1.3; }
.pending { color: var(--color-text-3); }
.success { color: rgb(var(--green-6)); }
.failure { color: rgb(var(--red-6)); }
.probe-file-import { display: inline-block; margin-top: 8px; }
.probe-media-preview { margin-top: 20px; padding-top: 16px; border-top: 1px solid var(--color-border-2); }
.preview-title { margin-bottom: 10px; font-weight: 600; }
.probe-audio { width: 100%; }
.probe-image-wrap { margin-bottom: 12px; text-align: center; }
.probe-image { max-width: 100%; max-height: 420px; border-radius: 4px; box-shadow: 0 2px 8px rgba(0, 0, 0, 0.12); }
@media (max-width: 640px) { .model-catalog-page { padding: 16px; } .page-header { flex-direction: column; } .category-filter { flex-basis: 100%; width: 100%; } }
</style>
