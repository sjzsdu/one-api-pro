<template>
  <!-- ModelAliasTestModal 使用正常令牌验证模型池与会话 / Tests pool routing through the standard API.
       版本: v0.0.28
       日期: 2026-11-22
       作者: Codex -->
  <a-modal width="min(560px, calc(100vw - 32px))" :visible="!!alias" :title="`${t('test')}: ${alias}`" :footer="false" :mask-closable="!testing" :closable="!testing" :esc-to-close="!testing" @cancel="close">
    <a-alert>{{ t('testNotice') }}</a-alert>
    <a-form layout="vertical" class="test-form">
      <a-form-item :label="t('token')">
        <a-input-password v-model="token" :aria-label="t('token')" :disabled="testing" placeholder="sk-…" autocomplete="off" />
      </a-form-item>
      <a-form-item :label="t('session')" :extra="t('testSessionHint')">
        <div class="session-field">
          <a-input v-model="sessionId" :aria-label="t('session')" :disabled="testing" :max-length="128" :placeholder="t('sessionPlaceholder')" />
          <a-button :disabled="testing" @click="newSession">{{ t('newSession') }}</a-button>
        </div>
      </a-form-item>
      <a-button type="primary" :loading="testing" @click="runTest">{{ t('send') }}</a-button>
    </a-form>
    <pre v-if="result" role="status" aria-live="polite" class="test-result">{{ result }}</pre>
  </a-modal>
</template>

<script setup>
import { ref, watch, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { Message } from '@arco-design/web-vue'
import messages from '@/i18n/pages/modelAliases'
import { buildAliasTestRequest } from '@/utils/modelAliases'

const props = defineProps({ alias: { type: String, default: '' } })
const emit = defineEmits(['close'])
const { t } = useI18n({ useScope: 'local', messages })
const token = ref(''), sessionId = ref(''), result = ref(''), testing = ref(false)
let abort
function newSession() {
  sessionId.value = `test-${globalThis.crypto?.randomUUID?.() || `${Date.now()}-${Math.random().toString(36).slice(2)}`}`
  result.value = ''
}
function close() { if (!testing.value) emit('close') }
watch(() => props.alias, alias => {
  token.value = ''; result.value = ''
  if (alias) newSession()
  else sessionId.value = ''
})
async function runTest() {
  if (!token.value.trim()) { Message.error(t('tokenRequired')); return }
  testing.value = true; result.value = ''
  abort = new AbortController()
  const timer = setTimeout(() => abort.abort(), 60000)
  try {
    const response = await fetch('/v1/chat/completions', { ...buildAliasTestRequest(props.alias, token.value, sessionId.value), signal: abort.signal })
    const data = await response.json()
    if (!response.ok || data.error) throw new Error(data.error?.message || t('failed'))
    result.value = `${t('success')}\n${t('resultModel')}: ${data.model || '—'}\n${data.choices?.[0]?.message?.content || JSON.stringify(data)}`
  } catch (error) { result.value = error.name === 'AbortError' ? t('timeout') : error.message || t('failed') }
  finally { clearTimeout(timer); testing.value = false }
}
onBeforeUnmount(() => abort?.abort())
</script>

<style scoped>
.test-form { margin-top: 16px; }
.session-field { display: flex; gap: 8px; width: 100%; }
.session-field :deep(.arco-input-wrapper) { flex: 1; min-width: 0; }
.test-result { white-space: pre-wrap; overflow-wrap: anywhere; color: var(--color-text-1); line-height: 1.6; }
@media (max-width: 480px) { .session-field { flex-direction: column; align-items: stretch; } }
</style>
