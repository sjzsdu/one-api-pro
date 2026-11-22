<template>
  <!-- ModelAliases 用户模型别名管理与真实调用测试 / Account-local model aliases and test calls.
       版本: v0.0.28
       日期: 2026-11-21
       作者: Codex -->
  <section class="aliases-section" aria-labelledby="alias-title">
    <h3 id="alias-title">{{ t('title') }}</h3>
    <p>{{ t('description') }}</p>
    <a-alert>{{ t('notice') }}</a-alert>
    <a-spin :loading="loading" style="width: 100%">
      <a-alert v-if="loadError" type="error" class="alias-row">{{ loadError }} <a-button type="text" @click="load">{{ t('retry') }}</a-button></a-alert>
      <template v-else>
        <a-empty v-if="!rows.length" :description="t('empty')" />
        <div v-for="(row, index) in rows" :key="index" class="alias-row">
          <a-form layout="vertical">
            <div class="alias-fields">
              <a-form-item :label="t('alias')"><a-input v-model="row.alias" :aria-label="`${t('alias')} ${index + 1}`" placeholder="chat-tool" :max-length="64" :disabled="saving || testing" /></a-form-item>
              <a-form-item :label="t('target')"><a-select v-model="row.target_model" :aria-label="`${t('target')} ${index + 1}`" allow-search :disabled="saving || testing" :placeholder="t('select')"><a-option v-for="name in targets" :key="name" :value="name">{{ name }}</a-option></a-select></a-form-item>
            </div>
            <p class="capabilities">{{ capability(row.target_model) }}</p>
            <a-space wrap>
              <a-button :disabled="dirty || saving || testing || !row.alias" @click="openTest(row.alias)">{{ t('test') }}</a-button>
              <a-button status="danger" :disabled="saving || testing" @click="rows.splice(index, 1)">{{ t('remove') }}</a-button>
            </a-space>
          </a-form>
        </div>
        <a-space wrap class="alias-actions">
          <a-button :disabled="rows.length >= 50 || saving || testing" @click="rows.push({ alias: '', target_model: '' })">{{ t('add') }}</a-button>
          <a-button type="primary" :loading="saving" :disabled="!dirty || testing" @click="save">{{ t('save') }}</a-button>
          <span v-if="dirty" class="capabilities">{{ t('unsaved') }}</span>
        </a-space>
      </template>
    </a-spin>
    <a-modal width="min(520px, calc(100vw - 32px))" :visible="!!testAlias" :title="`${t('test')}: ${testAlias}`" :footer="false" :mask-closable="!testing" :closable="!testing" @cancel="closeTest">
      <a-alert>{{ t('testNotice') }}</a-alert>
      <a-form layout="vertical" class="alias-row">
        <a-form-item :label="t('token')"><a-input-password v-model="testToken" :disabled="testing" placeholder="sk-…" autocomplete="off" /></a-form-item>
        <a-button type="primary" :loading="testing" @click="runTest">{{ t('send') }}</a-button>
      </a-form>
      <pre v-if="testResult" role="status" class="test-result">{{ testResult }}</pre>
    </a-modal>
  </section>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { Message } from '@arco-design/web-vue'
import api from '@/api'

const { t } = useI18n({ useScope: 'local', messages: {
  zh: {
    title: '我的模型别名', description: '业务始终调用同一个名称，在这里切换实际使用的模型。别名仅对当前账号生效。',
    notice: '切换在保存后对新请求生效，在途请求不变。权限、套餐与价格按目标模型执行。不同模型的工具调用、结构化输出和上下文能力可能不同。第一版支持 JSON 请求，不支持 multipart 音频上传。',
    empty: '还没有模型别名，添加一个 chat-tool 开始使用。', alias: '业务模型名称', target: '目标模型', select: '选择有权使用的模型',
    add: '添加别名', remove: '删除', save: '保存别名', test: '测试调用', unsaved: '有未保存修改，请先保存再测试。',
    saved: '模型别名已保存', failed: '操作失败，请重试', retry: '重试', invalid: '请填写完整的别名和目标模型',
    unknown: '能力信息未知，请测试目标模型是否满足业务要求。', context: '上下文长度', params: '已知支持参数',
    testNotice: '将使用你输入的 API 令牌，通过标准聊天接口发送一次短请求，按正常规则计费。令牌须属于当前账号且有目标模型权限。',
    token: '当前账号的 API 令牌', send: '发送测试请求', tokenRequired: '请输入当前账号的 API 令牌', success: '调用成功',
  },
  en: {
    title: 'My model aliases', description: 'Keep the same model name in your application and switch its target here. Aliases belong to your account.',
    notice: 'Saved changes apply to new requests. Running requests keep their target. Target permissions, subscriptions and pricing apply. Tool calling, structured output and context capabilities vary. Version one supports JSON requests; multipart audio uploads are unsupported.',
    empty: 'No model aliases yet. Add chat-tool to get started.', alias: 'Application model name', target: 'Target model', select: 'Select an available model',
    add: 'Add alias', remove: 'Delete', save: 'Save aliases', test: 'Test call', unsaved: 'Unsaved changes. Save before testing.',
    saved: 'Model aliases saved', failed: 'Operation failed. Please retry.', retry: 'Retry', invalid: 'Enter an alias and target model',
    unknown: 'Capabilities unknown. Test whether the target meets your requirements.', context: 'Context length', params: 'Known supported parameters',
    testNotice: 'Sends one short request through the standard chat API using your API token. Normal billing applies. Use a token from this account with access to the target model.',
    token: 'API token from this account', send: 'Send test request', tokenRequired: 'Enter an API token from this account', success: 'Call succeeded',
  },
} })
const loading = ref(true), saving = ref(false), loadError = ref('')
const rows = ref([]), targets = ref([]), metadata = ref([]), saved = ref('[]')
const dirty = computed(() => JSON.stringify(rows.value) !== saved.value)
const testAlias = ref(''), testToken = ref(''), testResult = ref(''), testing = ref(false)

function capability(name) {
  const item = metadata.value.find(item => item.model_name === name)
  if (!item || (!item.context_length && !item.supported_parameters)) return t('unknown')
  return [item.context_length ? `${t('context')}: ${item.context_length}` : '', item.supported_parameters ? `${t('params')}: ${item.supported_parameters}` : ''].filter(Boolean).join(' · ')
}
async function load() {
  loading.value = true; loadError.value = ''
  try {
    const [aliases, models] = await Promise.all([api.get('/api/user/model_aliases'), api.get('/api/user/model_alias_targets')])
    if (!aliases.data.success || !models.data.success) throw new Error(aliases.data.message || models.data.message)
    rows.value = aliases.data.data; targets.value = models.data.data.models || []; metadata.value = models.data.data.metadata || []
    saved.value = JSON.stringify(rows.value)
  } catch (error) { loadError.value = error.message || t('failed') }
  finally { loading.value = false }
}
async function save() {
  if (rows.value.some(row => !row.alias.trim() || !row.target_model)) { Message.error(t('invalid')); return }
  saving.value = true
  try {
    const { data } = await api.put('/api/user/model_aliases', { aliases: rows.value })
    if (!data.success) throw new Error(data.message)
    rows.value = data.data || []; saved.value = JSON.stringify(rows.value); Message.success(t('saved'))
  } catch (error) { Message.error(error.message || t('failed')) }
  finally { saving.value = false }
}
function openTest(alias) { testAlias.value = alias; testResult.value = ''; testToken.value = '' }
function closeTest() { if (!testing.value) { testAlias.value = ''; testToken.value = ''; testResult.value = '' } }
async function runTest() {
  if (!testToken.value.trim()) { Message.error(t('tokenRequired')); return }
  testing.value = true; testResult.value = ''
  const abort = new AbortController(), timer = setTimeout(() => abort.abort(), 60000)
  try {
    const response = await fetch('/v1/chat/completions', {
      method: 'POST', headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${testToken.value.trim()}` },
      body: JSON.stringify({ model: testAlias.value, messages: [{ role: 'user', content: 'Reply with OK.' }], max_tokens: 32, stream: false }), signal: abort.signal,
    })
    const data = await response.json()
    if (!response.ok || data.error) throw new Error(data.error?.message || t('failed'))
    testResult.value = `${t('success')}\n${data.model || ''}\n${data.choices?.[0]?.message?.content || JSON.stringify(data)}`
  } catch (error) { testResult.value = error.message || t('failed') }
  finally { clearTimeout(timer); testing.value = false }
}
onMounted(load)
</script>

<style scoped>
.aliases-section { width: 100%; }
h3 { color: var(--color-text-1); font-size: 16px; margin-bottom: 16px; }
p, .capabilities { color: var(--color-text-3); line-height: 1.6; }
.alias-row { margin-top: 16px; padding: 16px; border: 1px solid var(--color-border-2); border-radius: var(--border-radius-medium); }
.alias-fields { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
.alias-fields :deep(.arco-form-item) { margin-bottom: 0; min-width: 0; }
.capabilities { overflow-wrap: anywhere; font-size: 12px; }
.alias-actions { margin-top: 16px; }
.test-result { white-space: pre-wrap; overflow-wrap: anywhere; color: var(--color-text-1); }
@media (max-width: 600px) { .alias-fields { grid-template-columns: 1fr; gap: 12px; } .alias-row { padding: 12px; } }
</style>
