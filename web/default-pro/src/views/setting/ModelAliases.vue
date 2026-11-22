<template>
  <!-- ModelAliases 个人与全局模型池管理 / Personal and global weighted model pools.
       版本: v0.0.28
       日期: 2026-11-22
       作者: Codex -->
  <section class="aliases-section" aria-labelledby="alias-title">
    <h3 id="alias-title">{{ t('title') }}</h3>
    <p class="description">{{ t('description') }}</p>
    <a-radio-group v-model="scope" type="button" :aria-label="t('scope')" :disabled="saving || !!testAlias" class="scope-picker">
      <a-radio value="personal">{{ t('personal') }}</a-radio>
      <a-radio value="global">{{ t('global') }}</a-radio>
    </a-radio-group>
    <a-alert :type="scope === 'global' && authStore.isRoot ? 'warning' : 'info'">{{ t(scope === 'personal' ? 'personalNotice' : authStore.isRoot ? 'globalNotice' : 'globalReadOnly') }}</a-alert>
    <p class="routing-notice">{{ t('routingNotice') }}</p>
    <a-spin :loading="current.loading" style="width: 100%">
      <a-alert v-if="current.error" type="error" class="load-error">{{ current.error }} <a-button type="text" @click="load(true)">{{ t('retry') }}</a-button></a-alert>
      <template v-else>
        <a-empty v-if="!current.rows.length" :description="t(scope === 'personal' ? 'emptyPersonal' : editable ? 'emptyGlobal' : 'emptyAvailable')" />
        <ModelAliasPool v-for="(row, index) in current.rows" :key="index" :row="row" :index="index" :targets="current.targets" :metadata="current.metadata" :readonly="!editable" :disabled="saving || !!testAlias" :can-test="!dirty" @test="testAlias = $event" @remove="current.rows.splice(index, 1)" />
        <a-space wrap class="alias-actions">
          <template v-if="editable">
            <a-button :disabled="current.rows.length >= 50 || saving || !!testAlias" @click="current.rows.push(normalizeModelAlias())">{{ t('add') }}</a-button>
            <a-button type="primary" :loading="saving" :disabled="!dirty || !!testAlias" @click="save">{{ t(scope === 'global' ? 'publish' : 'save') }}</a-button>
          </template>
          <a-button type="text" :disabled="dirty || saving || !!testAlias" @click="load(true)">{{ t('refresh') }}</a-button>
          <span v-if="dirty" class="unsaved">{{ t('unsaved') }}</span>
        </a-space>
      </template>
    </a-spin>
    <ModelAliasTestModal :alias="testAlias" @close="testAlias = ''" />
  </section>
</template>

<script setup>
import { ref, reactive, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Message } from '@arco-design/web-vue'
import { useAuthStore } from '@/stores/auth'
import { modelAliasApi } from '@/api/modelAliases'
import { normalizeModelAlias, serializeModelAliases, validateModelAliases } from '@/utils/modelAliases'
import messages from '@/i18n/pages/modelAliases'
import ModelAliasPool from './ModelAliasPool.vue'
import ModelAliasTestModal from './ModelAliasTestModal.vue'

const { t } = useI18n({ useScope: 'local', messages })
const authStore = useAuthStore()
const scope = ref('personal'), saving = ref(false), testAlias = ref('')
const emptyState = () => ({ rows: [], targets: [], metadata: [], saved: '[]', error: '', loading: false, loaded: false })
const states = reactive({ personal: emptyState(), global: emptyState() })
const current = computed(() => states[scope.value])
const editable = computed(() => scope.value === 'personal' || authStore.isRoot)
const dirty = computed(() => editable.value && JSON.stringify(current.value.rows) !== current.value.saved)
async function load(force = false) {
  const selectedScope = scope.value, state = states[selectedScope]
  if (state.loaded && !force) return
  state.loading = true; state.error = ''
  try {
    const readOnlyGlobal = selectedScope === 'global' && !authStore.isRoot
    const [aliases, targets] = await Promise.all([
      readOnlyGlobal ? modelAliasApi.effective() : modelAliasApi.list(selectedScope),
      modelAliasApi.targets(readOnlyGlobal ? 'personal' : selectedScope),
    ])
    if (!aliases.data.success || !targets.data.success) throw new Error(aliases.data.message || targets.data.message || t('failed'))
    const items = Array.isArray(aliases.data.data) ? aliases.data.data : []
    state.rows = (readOnlyGlobal ? items.filter(item => item.source === 'global') : items).map(normalizeModelAlias)
    state.targets = targets.data.data.models || []; state.metadata = targets.data.data.metadata || []
    state.saved = JSON.stringify(state.rows); state.loaded = true
  } catch (error) { state.error = error.message || t('failed') }
  finally { state.loading = false }
}
async function save() {
  if (!editable.value) return
  const state = current.value, selectedScope = scope.value
  const error = validateModelAliases(state.rows, state.targets)
  if (error) { Message.error(t(error.code, { index: error.index })); return }
  saving.value = true
  try {
    const { data } = await modelAliasApi.save(selectedScope, serializeModelAliases(state.rows))
    if (!data.success) throw new Error(data.message || t('failed'))
    state.rows = (data.data || []).map(normalizeModelAlias); state.saved = JSON.stringify(state.rows)
    Message.success(t(selectedScope === 'global' ? 'published' : 'saved'))
    // A personal override can change the ordinary user's effective global list.
    if (selectedScope === 'personal' && !authStore.isRoot) states.global.loaded = false
  } catch (error) { Message.error(error.message || t('failed')) }
  finally { saving.value = false }
}
watch(scope, () => load(), { immediate: true })
</script>

<style scoped>
.aliases-section { width: 100%; min-width: 0; }
h3 { color: var(--color-text-1); font-size: 16px; margin: 0 0 16px; }
.description, .routing-notice, .unsaved { color: var(--color-text-3); line-height: 1.6; }
.scope-picker { margin: 16px 0; }
.routing-notice { font-size: 12px; margin: 12px 0; }
.load-error, .alias-actions { margin-top: 16px; }
.unsaved { font-size: 12px; overflow-wrap: anywhere; }
</style>
