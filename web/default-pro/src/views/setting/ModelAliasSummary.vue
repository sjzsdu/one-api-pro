<template>
  <!-- ModelAliasSummary 已保存别名与模型池策略摘要 / Saved alias and pool policy summary.
       版本: v0.0.28
       日期: 2026-11-22
       作者: Codex -->
  <article class="alias-summary">
    <div class="summary-content">
      <h4>{{ row.alias }}</h4>
      <ul class="candidate-list" :aria-label="t('candidates')">
        <li v-for="target in row.targets" :key="target.model"><span class="model-name">{{ target.model }}</span><span class="weight">{{ t('candidateWeight', { weight: target.weight }) }}</span></li>
      </ul>
      <p class="policy-summary">{{ t(row.failover_enabled ? 'failoverOn' : 'failoverOff') }} · {{ t('sessionSummary', { minutes: row.session_ttl_seconds / 60 }) }}</p>
    </div>
    <div class="summary-actions">
      <a-button size="small" :disabled="disabled" @click="emit('test')">{{ t('test') }}</a-button>
      <template v-if="editable">
        <a-button size="small" :disabled="disabled" @click="emit('edit')">{{ t('edit') }}</a-button>
        <a-popconfirm :content="t(global ? 'deleteGlobalConfirm' : 'deleteConfirm')" :ok-text="t('remove')" :cancel-text="t('cancel')" :disabled="disabled" @ok="emit('remove')">
          <a-button size="small" type="text" status="danger" :disabled="disabled">{{ t('remove') }}</a-button>
        </a-popconfirm>
      </template>
    </div>
  </article>
</template>

<script setup>
import { useI18n } from 'vue-i18n'
import messages from '@/i18n/pages/modelAliases'
defineProps({ row: { type: Object, required: true }, editable: Boolean, disabled: Boolean, global: Boolean })
const emit = defineEmits(['edit', 'test', 'remove'])
const { t } = useI18n({ useScope: 'local', messages })
</script>

<style scoped>
.alias-summary { display: flex; justify-content: space-between; gap: 16px; padding: 16px; border: 1px solid var(--color-border-2); border-radius: var(--border-radius-medium); margin-top: 12px; }
.summary-content { flex: 1; min-width: 0; }
h4 { color: var(--color-text-1); font-size: 14px; margin: 0 0 10px; overflow-wrap: anywhere; }
.candidate-list { display: flex; flex-wrap: wrap; gap: 8px; list-style: none; margin: 0; padding: 0; }
.candidate-list li { display: flex; flex-wrap: wrap; gap: 8px; max-width: 100%; padding: 4px 8px; background: var(--color-fill-2); border-radius: var(--border-radius-small); font-size: 12px; line-height: 1.6; }
.model-name { color: var(--color-text-1); overflow-wrap: anywhere; min-width: 0; }
.weight, .policy-summary { color: var(--color-text-3); }
.policy-summary { font-size: 12px; line-height: 1.6; margin: 10px 0 0; }
.summary-actions { display: flex; flex-wrap: wrap; align-content: flex-start; gap: 8px; flex-shrink: 0; }
@media (max-width: 900px) { .alias-summary { flex-direction: column; gap: 12px; } }
@media (max-width: 600px) { .alias-summary { padding: 12px; } .summary-actions { gap: 6px; } }
</style>
