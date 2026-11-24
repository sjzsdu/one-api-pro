<template>
  <!-- ModelCapabilityModal 模型能力人工覆盖编辑 / Explicit model capability editor.
       版本: v0.0.28
       日期: 2026-11-23
       作者: Codex -->
  <a-modal :visible="visible" :title="label('模型能力', 'Model capabilities')" :width="680" :ok-loading="saving" @ok="save" @cancel="$emit('cancel')">
    <p>{{ record?.model_name }}</p>
    <a-alert type="info">{{ label('能力声明用于分组与路由约束；实际可调用范围仍由网关适配器决定。', 'Declarations control grouping and routing constraints. Adapter implementation still determines executable operations.') }}</a-alert>
    <a-form :model="draft" layout="vertical" class="capability-form">
      <a-form-item :label="label('主要分类', 'Primary category')">
        <a-select v-model="draft.primary_category">
          <a-option v-for="category in MODEL_CATEGORY_ORDER" :key="category" :value="category">{{ modelCategoryLabel(category, locale) }}</a-option>
        </a-select>
      </a-form-item>
      <a-form-item :label="label('支持的操作', 'Operations')">
        <a-select v-model="operations" multiple allow-clear>
          <a-option v-for="operation in operationNames" :key="operation" :value="operation">{{ modelOperationLabel(operation, locale) }}</a-option>
        </a-select>
      </a-form-item>
      <div v-for="operation in operations" :key="operation" class="operation-editor">
        <strong>{{ modelOperationLabel(operation, locale) }}</strong>
        <a-form-item :label="label('执行模式', 'Execution modes')">
          <a-select v-model="draft.operations[operation].modes" multiple>
            <a-option v-for="mode in modes" :key="mode.value" :value="mode.value">{{ label(mode.zh, mode.en) }}</a-option>
          </a-select>
        </a-form-item>
        <a-form-item :label="label('输入类型', 'Input modalities')">
          <a-select v-model="draft.operations[operation].input_modalities" multiple><a-option v-for="modality in modalities" :key="modality" :value="modality">{{ modality }}</a-option></a-select>
        </a-form-item>
        <a-form-item :label="label('输出类型', 'Output modalities')">
          <a-select v-model="draft.operations[operation].output_modalities" multiple><a-option v-for="modality in modalities" :key="modality" :value="modality">{{ modality }}</a-option></a-select>
        </a-form-item>
        <a-form-item :label="label('支持格式', 'Supported formats')"><a-input-tag v-model="draft.operations[operation].limits.formats" allow-clear /></a-form-item>
        <a-form-item :label="label('最大输入字符数（0 表示未声明）', 'Maximum input characters (0 = unspecified)')"><a-input-number v-model="draft.operations[operation].limits.max_input_characters" :min="0" :precision="0" /></a-form-item>
        <a-form-item :label="label('最大输出数量（0 表示未声明）', 'Maximum items (0 = unspecified)')"><a-input-number v-model="draft.operations[operation].limits.max_items" :min="0" :precision="0" /></a-form-item>
      </div>
      <a-button type="text" status="warning" :disabled="saving" @click="$emit('save', null)">{{ label('恢复自动能力信息', 'Restore discovered capabilities') }}</a-button>
    </a-form>
  </a-modal>
</template>

<script setup>
import { reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Message } from '@arco-design/web-vue'
import { MODEL_CATEGORY_ORDER, modelCategoryLabel, modelCapabilities, modelOperationLabel } from '@/utils/modelCategory'

const props = defineProps({ visible: Boolean, saving: Boolean, record: { type: Object, default: null } })
const emit = defineEmits(['save', 'cancel'])
const { locale } = useI18n()
const label = (zh, en) => locale.value === 'en' ? en : zh
const draft = reactive({ primary_category: 'unknown', operations: {} })
const operations = ref([])
const operationNames = ['chat.generate', 'image.generate', 'image.edit', 'image.variation', 'audio.speech', 'audio.transcribe', 'audio.translate', 'video.generate', 'video.edit', 'embedding.create', 'rerank.score', 'moderation.check']
const modes = [{ value: 'sync', zh: '同步', en: 'Synchronous' }, { value: 'stream', zh: '单向流', en: 'Streaming' }, { value: 'async', zh: '异步任务', en: 'Asynchronous' }, { value: 'realtime', zh: '实时会话', en: 'Realtime' }]
const modalities = ['text', 'image', 'audio', 'video', 'vector', 'score']
watch(() => [props.visible, props.record], () => {
  if (!props.visible) return
  const capability = JSON.parse(JSON.stringify(modelCapabilities(props.record || {})))
  draft.primary_category = capability.primary_category || 'unknown'
  draft.operations = capability.operations || {}
  operations.value = Object.keys(draft.operations)
  for (const op of operations.value) prepare(op)
}, { immediate: true })
watch(operations, value => { for (const op of value) prepare(op) }, { flush: 'sync' })
function prepare(op) {
  if (!draft.operations[op]) draft.operations[op] = { modes: ['sync'], input_modalities: [], output_modalities: [] }
  const descriptor = draft.operations[op]
  descriptor.limits ||= {}
  descriptor.limits.formats ||= []
}
function save() {
  if (operations.value.some(op => !draft.operations[op]?.modes?.length)) { Message.error(label('每个操作至少选择一个执行模式', 'Select at least one mode per operation')); return }
  emit('save', { primary_category: draft.primary_category, operations: Object.fromEntries(operations.value.map(op => [op, draft.operations[op]])) })
}
</script>

<style scoped>
.capability-form { margin-top: 16px; }
.operation-editor { margin-bottom: 16px; padding: 12px; border: 1px solid var(--color-border-2); border-radius: var(--border-radius-medium); }
.operation-editor strong { display: block; margin-bottom: 12px; }
</style>
