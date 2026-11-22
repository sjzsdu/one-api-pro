<template>
  <!-- ModelAliasPool 候选模型、权重与会话策略编辑 / Weighted candidates and session policy editor.
       版本: v0.0.28
       日期: 2026-11-22
       作者: Codex -->
  <article class="pool-row">
    <a-form layout="vertical">
      <a-form-item :label="t('alias')">
        <a-input v-if="!readonly" v-model="row.alias" :aria-label="`${t('alias')} ${index + 1}`" placeholder="chat-tool" :max-length="64" :disabled="disabled" />
        <strong v-else class="alias-name">{{ row.alias }}</strong>
      </a-form-item>
      <div class="candidate-heading">{{ t('candidates') }}</div>
      <div v-for="(target, targetIndex) in row.targets" :key="targetIndex" class="candidate-row">
        <div class="candidate-fields">
          <a-form-item :label="t('target')" class="model-field">
            <a-select v-if="!readonly" v-model="target.model" :aria-label="`${t('target')} ${index + 1}-${targetIndex + 1}`" allow-search :disabled="disabled" :placeholder="t('select')">
              <a-option v-for="name in targets" :key="name" :value="name">{{ name }}</a-option>
            </a-select>
            <span v-else class="model-name">{{ target.model }}</span>
          </a-form-item>
          <a-form-item :label="t('weight')" class="weight-field">
            <a-input-number v-if="!readonly" v-model="target.weight" :aria-label="`${t('weight')} ${index + 1}-${targetIndex + 1}`" :min="1" :max="1000" :precision="0" :disabled="disabled" />
            <span v-else>{{ target.weight }}</span>
          </a-form-item>
          <a-button v-if="!readonly" class="remove-target" status="danger" type="text" :disabled="disabled || row.targets.length <= 1" @click="row.targets.splice(targetIndex, 1)">{{ t('removeTarget') }}</a-button>
        </div>
        <p class="candidate-detail">{{ t('share', { percent: share(target.weight) }) }} · {{ capability(target.model) }}</p>
      </div>
      <a-button v-if="!readonly" type="text" :disabled="disabled || row.targets.length >= 8" @click="row.targets.push({ model: '', weight: 1 })">{{ t('addTarget') }}</a-button>
      <div class="policy-fields">
        <a-form-item :label="t('failover')" :extra="t('failoverHint')">
          <a-switch v-model="row.failover_enabled" :aria-label="`${t('failover')} ${index + 1}`" :disabled="readonly || disabled" />
        </a-form-item>
        <a-form-item :label="t('sessionMinutes')" :extra="t('sessionHint')">
          <a-input-number :model-value="row.session_ttl_seconds / 60" :aria-label="`${t('sessionMinutes')} ${index + 1}`" :min="1" :max="1440" :precision="2" :disabled="readonly || disabled" @update:model-value="setSessionMinutes" />
        </a-form-item>
      </div>
      <a-space wrap>
        <a-button :disabled="disabled || !canTest || !row.alias" @click="emit('test', row.alias)">{{ t('test') }}</a-button>
        <a-button v-if="!readonly" status="danger" :disabled="disabled" @click="emit('remove')">{{ t('remove') }}</a-button>
      </a-space>
    </a-form>
  </article>
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import messages from '@/i18n/pages/modelAliases'

const props = defineProps({
  row: { type: Object, required: true }, index: { type: Number, required: true },
  targets: { type: Array, default: () => [] }, metadata: { type: Array, default: () => [] },
  readonly: Boolean, disabled: Boolean, canTest: Boolean,
})
const emit = defineEmits(['test', 'remove'])
const { t } = useI18n({ useScope: 'local', messages })
const totalWeight = computed(() => props.row.targets.reduce((sum, target) => sum + (Number(target.weight) || 0), 0))
function share(weight) { return totalWeight.value ? Math.round((Number(weight) || 0) / totalWeight.value * 100) : 0 }
function setSessionMinutes(value) { props.row.session_ttl_seconds = Math.round(Number(value) * 60) }
function capability(name) {
  const item = props.metadata.find(item => item.model_name === name)
  if (!item) return t('unknown')
  let parameters = item.supported_parameters
  if (typeof parameters === 'string') {
    try { parameters = JSON.parse(parameters) } catch { parameters = parameters ? [parameters] : [] }
  }
  const supported = Array.isArray(parameters) ? parameters.join(', ') : ''
  const parts = [item.context_length ? `${t('context')}: ${item.context_length}` : '', supported ? `${t('params')}: ${supported}` : ''].filter(Boolean)
  return parts.length ? parts.join(' · ') : t('unknown')
}
</script>

<style scoped>
.pool-row { margin-top: 16px; padding: 16px; border: 1px solid var(--color-border-2); border-radius: var(--border-radius-medium); }
.alias-name, .model-name { color: var(--color-text-1); overflow-wrap: anywhere; }
.candidate-heading { margin-bottom: 12px; color: var(--color-text-2); font-weight: 500; }
.candidate-row { padding: 12px; margin-bottom: 8px; background: var(--color-fill-1); border-radius: var(--border-radius-small); }
.candidate-fields { display: flex; align-items: flex-start; gap: 12px; }
.candidate-fields :deep(.arco-form-item) { margin-bottom: 0; min-width: 0; }
.model-field { flex: 1; }
.weight-field { width: 120px; flex-shrink: 0; }
.remove-target { margin-top: 28px; flex-shrink: 0; }
.candidate-detail { margin: 8px 0 0; color: var(--color-text-3); line-height: 1.6; font-size: 12px; overflow-wrap: anywhere; }
.policy-fields { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; margin-top: 16px; }
.policy-fields :deep(.arco-form-item) { min-width: 0; }
@media (max-width: 900px) { .candidate-fields { flex-wrap: wrap; } .model-field { flex-basis: calc(100% - 132px); } .remove-target { margin-top: 0; } }
@media (max-width: 600px) { .pool-row { padding: 12px; } .candidate-fields { flex-direction: column; } .model-field, .weight-field { width: 100%; flex: initial; } .policy-fields { grid-template-columns: 1fr; gap: 0; } }
</style>
