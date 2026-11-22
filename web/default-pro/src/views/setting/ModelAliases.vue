<template>
  <!-- ModelAliases 个人与全局模型池管理 / Personal and global weighted model pools.
       版本: v0.0.28
       日期: 2026-11-22
       作者: Codex -->
  <section class="aliases-section" aria-labelledby="alias-title">
    <h3 id="alias-title">{{ t('title') }}</h3>
    <p class="description">{{ t('description') }}</p>
    <a-radio-group v-model="scope" type="button" :aria-label="t('scope')" :disabled="busy || editorOpen || current.loading" class="scope-picker">
      <a-radio value="personal">{{ t('personal') }}</a-radio>
      <a-radio value="global">{{ t('global') }}</a-radio>
    </a-radio-group>
    <a-alert :type="scope === 'global' && authStore.isRoot ? 'warning' : 'info'">{{ t(scope === 'personal' ? 'personalNotice' : authStore.isRoot ? 'globalNotice' : 'globalReadOnly') }}</a-alert>
    <p class="routing-notice">{{ t('routingNotice') }}</p>
    <div class="alias-actions">
      <a-button v-if="editable" type="primary" :disabled="current.rows.length >= 50 || busy || editorOpen || current.loading || !!current.error" @click="openEditor(-1)">{{ t('add') }}</a-button>
      <a-button type="text" :disabled="busy || editorOpen || current.loading" @click="load(true)">{{ t('refresh') }}</a-button>
    </div>
    <a-alert v-if="operationError && !editorOpen" type="error" class="load-error" role="alert">{{ operationError }}</a-alert>
    <a-spin :loading="current.loading || (saving && !editorOpen)" style="width: 100%">
      <a-alert v-if="current.error" type="error" class="load-error">{{ current.error }} <a-button type="text" :disabled="current.loading" @click="load(true)">{{ t('retry') }}</a-button></a-alert>
      <template v-else>
        <a-empty v-if="!current.rows.length" :description="t(scope === 'personal' ? 'emptyPersonal' : editable ? 'emptyGlobal' : 'emptyAvailable')" />
        <ModelAliasSummary v-for="(row, index) in current.rows" :key="row.alias" :row="row" :editable="editable" :global="scope === 'global'" :disabled="busy || editorOpen || current.loading" @test="openTest(row.alias)" @edit="openEditor(index)" @remove="removeAlias(index)" />
      </template>
    </a-spin>
    <ModelAliasEditModal :visible="editorOpen" :row="editorRow" :index="editorIndex ?? -1" :targets="current.targets" :metadata="current.metadata" :global="scope === 'global'" :saving="saving" :error="editorError" @save="saveDraft" @cancel="closeEditor" />
    <ModelAliasTestModal :alias="testAlias" @close="testAlias = ''" />
  </section>
</template>

<script setup>
import { ref, reactive, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Message } from '@arco-design/web-vue'
import { useAuthStore } from '@/stores/auth'
import { modelAliasApi } from '@/api/modelAliases'
import { normalizeModelAlias, mergeModelAliasDraft, serializeModelAliases, validateModelAliases } from '@/utils/modelAliases'
import messages from '@/i18n/pages/modelAliases'
import ModelAliasSummary from './ModelAliasSummary.vue'
import ModelAliasEditModal from './ModelAliasEditModal.vue'
import ModelAliasTestModal from './ModelAliasTestModal.vue'

const { t } = useI18n({ useScope: 'local', messages })
const authStore = useAuthStore()
const scope = ref('personal'), saving = ref(false), testAlias = ref('')
const editorIndex = ref(null), editorRow = ref(null), editorError = ref(''), operationError = ref('')
const editorOpen = computed(() => editorIndex.value !== null)
const busy = computed(() => saving.value || !!testAlias.value)
const emptyState = () => ({ rows: [], targets: [], metadata: [], error: '', loading: false, loaded: false })
const states = reactive({ personal: emptyState(), global: emptyState() })
const current = computed(() => states[scope.value])
const editable = computed(() => scope.value === 'personal' || authStore.isRoot)
async function load(force = false) {
  const selectedScope = scope.value, state = states[selectedScope]
  if (state.loading || (state.loaded && !force)) return
  operationError.value = ''
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
    state.loaded = true
  } catch (error) { state.error = error.message || t('failed') }
  finally { state.loading = false }
}
function openEditor(index) {
  if (!editable.value || busy.value || editorOpen.value || current.value.loading || current.value.error) return
  if (index < 0 && current.value.rows.length >= 50) return
  editorRow.value = index < 0 ? normalizeModelAlias() : normalizeModelAlias(current.value.rows[index])
  editorError.value = ''; operationError.value = ''; editorIndex.value = index
}
function closeEditor() {
  if (saving.value) return
  editorIndex.value = null; editorRow.value = null; editorError.value = ''
}
function openTest(alias) {
  if (!busy.value && !editorOpen.value && !current.value.loading) testAlias.value = alias
}
async function persist(rows) {
  const state = current.value, selectedScope = scope.value
  saving.value = true; operationError.value = ''
  try {
    const { data } = await modelAliasApi.save(selectedScope, serializeModelAliases(rows))
    if (!data.success) throw new Error(data.message || t('failed'))
    state.rows = (data.data || []).map(normalizeModelAlias)
    Message.success(t(selectedScope === 'global' ? 'published' : 'saved'))
    // A personal override can change the ordinary user's effective global list.
    if (selectedScope === 'personal' && !authStore.isRoot) states.global.loaded = false
    return true
  } catch (error) {
    operationError.value = error.message || t('failed')
    return false
  } finally { saving.value = false }
}
async function saveDraft(draft) {
  if (!editable.value || saving.value || !editorOpen.value) return
  const rows = mergeModelAliasDraft(current.value.rows, draft, editorIndex.value)
  const error = validateModelAliases(rows, current.value.targets)
  if (error) { editorError.value = t(error.code, { index: error.index }); Message.error(editorError.value); return }
  editorError.value = ''
  if (await persist(rows)) closeEditor()
  else editorError.value = operationError.value
}
async function removeAlias(index) {
  if (!editable.value || busy.value || editorOpen.value || current.value.loading) return
  await persist(current.value.rows.filter((_, rowIndex) => rowIndex !== index))
}
watch(scope, () => load(), { immediate: true })
</script>

<style scoped>
.aliases-section { width: 100%; min-width: 0; }
h3 { color: var(--color-text-1); font-size: 16px; margin: 0 0 16px; }
.description, .routing-notice { color: var(--color-text-3); line-height: 1.6; }
.scope-picker { margin: 16px 0; }
.routing-notice { font-size: 12px; margin: 12px 0; }
.load-error { margin: 16px 0; overflow-wrap: anywhere; }
.alias-actions { display: flex; flex-wrap: wrap; gap: 8px; margin: 16px 0; }
</style>
