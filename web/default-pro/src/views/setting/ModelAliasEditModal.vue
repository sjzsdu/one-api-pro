<template>
  <!-- ModelAliasEditModal 独立草稿编辑与即时保存弹窗 / Isolated alias draft editor.
       版本: v0.0.28
       日期: 2026-11-22
       作者: Codex -->
  <a-modal :visible="visible" :title="t(index < 0 ? 'createTitle' : 'editTitle')" width="min(760px, calc(100vw - 32px))" :footer="false" :mask-closable="!saving" :closable="!saving" :esc-to-close="!saving" @cancel="cancel" @open="focusEditor">
    <div class="editor-scroll" :aria-busy="saving">
      <a-alert v-if="error" type="error" class="editor-error" role="alert">{{ error }}</a-alert>
      <ModelAliasPool ref="editor" :row="draft" :index="Math.max(index, 0)" :targets="targets" :metadata="metadata" :disabled="saving" editor-only />
    </div>
    <div class="editor-actions">
      <a-button :disabled="saving" @click="cancel">{{ t('cancel') }}</a-button>
      <a-button type="primary" :loading="saving" :disabled="saving" @click="emit('save', draft)">{{ t(global ? 'savePublish' : 'saveChanges') }}</a-button>
    </div>
  </a-modal>
</template>

<script setup>
import { ref, watch, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { normalizeModelAlias } from '@/utils/modelAliases'
import messages from '@/i18n/pages/modelAliases'
import ModelAliasPool from './ModelAliasPool.vue'

const props = defineProps({
  visible: Boolean, row: { type: Object, default: null }, index: { type: Number, default: -1 },
  targets: { type: Array, default: () => [] }, metadata: { type: Array, default: () => [] },
  global: Boolean, saving: Boolean, error: { type: String, default: '' },
})
const emit = defineEmits(['save', 'cancel'])
const { t } = useI18n({ useScope: 'local', messages })
const draft = ref(normalizeModelAlias()), editor = ref(null)
watch(() => props.visible, visible => {
  if (visible) draft.value = normalizeModelAlias(props.row || {})
}, { immediate: true })
async function focusEditor() { await nextTick(); editor.value?.focusAlias() }
function cancel() { if (!props.saving) emit('cancel') }
</script>

<style scoped>
.editor-scroll { max-height: calc(100dvh - 220px); overflow-y: auto; padding: 2px 4px 0; }
.editor-error { margin-bottom: 16px; overflow-wrap: anywhere; }
.editor-actions { display: flex; justify-content: flex-end; gap: 12px; border-top: 1px solid var(--color-border-2); padding-top: 16px; margin-top: 8px; }
@media (max-width: 600px) { .editor-actions { gap: 8px; } .editor-actions :deep(.arco-btn) { flex: 1; min-width: 0; } }
</style>
