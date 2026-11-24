<template>
  <a-spin :loading="loading" class="setting-container">
    <a-tabs v-model:active-key="tab" size="large">
      <a-tab-pane key="model" :title="$t('settingPage.pricing.modelTab')">
        <div class="section-header">
          <h3>{{ $t('settingPage.pricing.modelTab') }}</h3>
          <a-button type="primary" @click="openModelPrice()"><template #icon><icon-plus /></template>{{ $t('settingPage.pricing.add') }}</a-button>
        </div>
        <a-select v-model="categoryFilter" :aria-label="locale === 'en' ? 'Model category' : '模型类型'" class="category-filter">
          <a-option value="all">{{ locale === 'en' ? 'All categories' : '全部类型' }}</a-option>
          <a-option v-for="category in MODEL_CATEGORY_ORDER" :key="category" :value="category">{{ modelCategoryLabel(category, locale) }}</a-option>
        </a-select>
        <a-table
          row-key="id"
          :columns="mpColumns"
          :data="filteredModelPrices"
          :pagination="false"
          size="medium"
          :scroll="mpScroll"
        >
          <template #inputPrice="{ record }"><div>{{ record.input_price }}</div><small>{{ billingUnits(record.model_name).input }}</small></template>
          <template #outputPrice="{ record }"><div>{{ record.output_price }}</div><small>{{ billingUnits(record.model_name).output }}</small></template>
          <template #requestPrice="{ record }"><div>{{ record.per_request_price }}</div><small>{{ billingUnits(record.model_name).perRequest }}</small></template>
          <template #billing="{ record }">
            <a-tag :color="record.billing_type==='token'?'blue':'green'" size="small">{{ record.billing_type==='token' ? $t('settingPage.pricing.billingToken') : $t('settingPage.pricing.billingRequest') }}</a-tag>
          </template>
          <template #actions="{ record }">
            <a-space>
              <a-button type="text" size="small" @click="openModelPrice(record)">{{ $t('settingPage.pricing.edit') }}</a-button>
              <a-popconfirm :content="$t('settingPage.pricing.confirmDeleteModel')" @ok="handleDelMP(record.id)">
                <a-button type="text" size="small" status="danger">{{ $t('settingPage.pricing.delete') }}</a-button>
              </a-popconfirm>
            </a-space>
          </template>
        </a-table>
      </a-tab-pane>

      <a-tab-pane key="fallback" :title="$t('settingPage.pricing.fallbackTab')">
        <div class="section-header">
          <h3>{{ $t('settingPage.pricing.fallbackTab') }}</h3>
        </div>
        <a-alert class="fallback-tip" type="info" size="small">
          {{ $t('settingPage.pricing.fallbackTip') }}
        </a-alert>
        <a-form :model="fbForm" layout="vertical" class="fallback-form">
          <a-form-item :label="$t('settingPage.pricing.fallbackEnabled')">
            <a-switch v-model="fbForm.enabled" />
            <span class="fallback-hint">{{ fbForm.enabled ? $t('settingPage.pricing.fallbackOnHint') : $t('settingPage.pricing.fallbackOffHint') }}</span>
          </a-form-item>
          <a-row :gutter="16">
            <a-col :span="12">
              <a-form-item :label="$t('settingPage.pricing.inputPrice')"><a-input-number v-model="fbForm.input_price" :style="{ width: '100%' }" :precision="6" :min="0" /></a-form-item>
            </a-col>
            <a-col :span="12">
              <a-form-item :label="$t('settingPage.pricing.outputPrice')"><a-input-number v-model="fbForm.output_price" :style="{ width: '100%' }" :precision="6" :min="0" /></a-form-item>
            </a-col>
          </a-row>
          <a-row :gutter="16">
            <a-col :span="12">
              <a-form-item :label="$t('settingPage.pricing.cachedPrice')"><a-input-number v-model="fbForm.cached_price" :style="{ width: '100%' }" :precision="6" :min="0" /></a-form-item>
            </a-col>
            <a-col :span="12">
              <a-form-item :label="$t('settingPage.pricing.perRequestPrice')"><a-input-number v-model="fbForm.per_request_price" :style="{ width: '100%' }" :precision="6" :min="0" /></a-form-item>
            </a-col>
          </a-row>
          <a-form-item :label="$t('settingPage.pricing.billingType')">
            <a-select v-model="fbForm.billing_type">
              <a-option value="token">{{ $t('settingPage.pricing.billingByToken') }}</a-option>
              <a-option value="per_request">{{ $t('settingPage.pricing.billingByRequest') }}</a-option>
            </a-select>
          </a-form-item>
          <a-form-item>
            <a-button type="primary" :loading="fbSaving" @click="handleSaveFallback">{{ $t('settingPage.pricing.saveFallback') }}</a-button>
          </a-form-item>
        </a-form>
      </a-tab-pane>

      <a-tab-pane key="group" :title="$t('settingPage.pricing.groupTab')">
        <div class="section-header">
          <h3>{{ $t('settingPage.pricing.groupTab') }}</h3>
          <a-button type="primary" @click="openGroupPrice()"><template #icon><icon-plus /></template>{{ $t('settingPage.pricing.add') }}</a-button>
        </div>
        <a-table
          row-key="id"
          :columns="gpColumns"
          :data="groupPrices"
          :pagination="false"
          size="medium"
          :scroll="gpScroll"
        >
          <template #actions="{ record }">
            <a-space>
              <a-button type="text" size="small" @click="openGroupPrice(record)">{{ $t('settingPage.pricing.edit') }}</a-button>
              <a-popconfirm :content="$t('settingPage.pricing.confirmDeleteGroup')" @ok="handleDelGP(record.id)">
                <a-button type="text" size="small" status="danger">{{ $t('settingPage.pricing.delete') }}</a-button>
              </a-popconfirm>
            </a-space>
          </template>
        </a-table>
      </a-tab-pane>
    </a-tabs>

    <a-modal v-model:visible="mpVisible" :title="mpEditing ? $t('settingPage.pricing.editModelTitle') : $t('settingPage.pricing.addModelTitle')" @ok="handleSaveMP" :ok-loading="mpSaving" width="640">
      <a-form :model="mpForm" layout="vertical">
        <a-alert :type="billingUnits(mpForm.model_name).ambiguous ? 'warning' : 'info'" class="price-unit-note">
          {{ locale === 'en' ? 'All operations of a model currently share these prices. Check each operation’s units; separate prices per operation are not supported yet.' : '当前同一模型的所有操作共用这组单价。请核对各操作的实际单位，暂不支持按操作分别定价。' }}
        </a-alert>
        <a-form-item :label="$t('settingPage.pricing.modelName')" required>
          <GroupedModelSelect v-model="mpForm.model_name" :options="pricingModelOptions" :metadata="modelMetadata" allow-search allow-create placeholder="gpt-4o" />
        </a-form-item>
        <a-row :gutter="16">
          <a-col :span="12">
            <a-form-item :label="`${$t('settingPage.pricing.inputPrice')} · ${billingUnits(mpForm.model_name).input}`"><a-input-number v-model="mpForm.input_price" :style="{ width: '100%' }" :precision="6" /></a-form-item>
          </a-col>
          <a-col :span="12">
            <a-form-item :label="`${$t('settingPage.pricing.outputPrice')} · ${billingUnits(mpForm.model_name).output}`"><a-input-number v-model="mpForm.output_price" :style="{ width: '100%' }" :precision="6" /></a-form-item>
          </a-col>
        </a-row>
        <a-row :gutter="16">
          <a-col :span="12">
            <a-form-item :label="$t('settingPage.pricing.cachedPrice')"><a-input-number v-model="mpForm.cached_price" :style="{ width: '100%' }" :precision="6" /></a-form-item>
          </a-col>
          <a-col :span="12">
            <a-form-item :label="`${$t('settingPage.pricing.perRequestPrice')} · ${billingUnits(mpForm.model_name).perRequest}`"><a-input-number v-model="mpForm.per_request_price" :style="{ width: '100%' }" :precision="6" /></a-form-item>
          </a-col>
        </a-row>
        <a-form-item :label="$t('settingPage.pricing.billingType')">
          <a-select v-model="mpForm.billing_type">
            <a-option value="token">{{ $t('settingPage.pricing.billingByToken') }}</a-option>
            <a-option value="per_request">{{ $t('settingPage.pricing.billingByRequest') }}</a-option>
          </a-select>
        </a-form-item>
        <a-form-item :label="$t('settingPage.pricing.enabled')">
          <a-switch v-model="mpForm.enabled" />
        </a-form-item>
      </a-form>
    </a-modal>

    <a-modal v-model:visible="gpVisible" :title="gpEditing ? $t('settingPage.pricing.editGroupTitle') : $t('settingPage.pricing.addGroupTitle')" @ok="handleSaveGP" :ok-loading="gpSaving" width="520">
      <a-form :model="gpForm" layout="vertical">
        <a-row :gutter="16">
          <a-col :span="12">
            <a-form-item :label="$t('settingPage.pricing.groupName')" required><a-input v-model="gpForm.group_name" placeholder="vip" /></a-form-item>
          </a-col>
          <a-col :span="12">
            <a-form-item :label="$t('settingPage.pricing.modelName')" required><GroupedModelSelect v-model="gpForm.model_name" :options="pricingModelOptions" :metadata="modelMetadata" allow-search allow-create placeholder="gpt-4o" /></a-form-item>
          </a-col>
        </a-row>
        <a-form-item :label="$t('settingPage.pricing.discount')">
          <a-input-number v-model="gpForm.discount" :style="{ width: '100%' }" :precision="4" :min="0" :placeholder="$t('settingPage.pricing.discountPlaceholder')" />
        </a-form-item>
      </a-form>
    </a-modal>
  </a-spin>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { Message } from '@arco-design/web-vue'
import { IconPlus } from '@arco-design/web-vue/es/icon'
import api from '@/api'
import GroupedModelSelect from '@/components/GroupedModelSelect.vue'
import { MODEL_CATEGORY_ORDER, classifyModel, modelCategoryLabel } from '@/utils/modelCategory'
import { modelBillingUnits } from '@/utils/modelBilling'

const { t, locale } = useI18n()

const tab = ref('model')
const loading = ref(false)
const modelPrices = ref([])
const groupPrices = ref([])
const modelMetadata = ref([])
const categoryFilter = ref('all')
function billingUnits(name) { return modelBillingUnits(modelMetadata.value.find(item => item.model_name === name) || { model_name: name }, locale.value) }
const pricingModelOptions = computed(() => [...new Set([...modelMetadata.value.map(item => item.model_name), ...modelPrices.value.map(item => item.model_name)])])
const filteredModelPrices = computed(() => modelPrices.value.filter(item => categoryFilter.value === 'all' || classifyModel(modelMetadata.value.find(model => model.model_name === item.model_name) || item) === categoryFilter.value))

// Columns + scroll per arco-design table#scroll convention:
//  - the first column (模型 / 分组) has NO width so it absorbs leftover
//    horizontal space and the table element stretches to fill the container
//  - the other columns keep fixed widths for a predictable layout
//  - scroll.x is a numeric minimum table width (no scroll.minWidth, which
//    would override the component's CSS min-width:100% and pin the table
//    to a fixed pixel width, producing a blank trailing column)
//  - fixed: 'right' keeps the 操作 column pinned while scrolling
const mpColumns = computed(() => [
  { title: t('settingPage.pricing.colModel'), dataIndex: 'model_name' },
  { title: t('settingPage.pricing.colInputPrice'), slotName: 'inputPrice', width: 110 },
  { title: t('settingPage.pricing.colOutputPrice'), slotName: 'outputPrice', width: 110 },
  { title: t('settingPage.pricing.colCachedPrice'), dataIndex: 'cached_price', width: 110 },
  { title: t('settingPage.pricing.colRequestPrice'), slotName: 'requestPrice', width: 110 },
  { title: t('settingPage.pricing.colBilling'), slotName: 'billing', width: 90 },
  { title: t('settingPage.pricing.colAction'), slotName: 'actions', width: 160, fixed: 'right' },
])
const mpScroll = { x: 870 }

const gpColumns = computed(() => [
  { title: t('settingPage.pricing.colGroup'), dataIndex: 'group_name' },
  { title: t('settingPage.pricing.colModel'), dataIndex: 'model_name', width: 220 },
  { title: t('settingPage.pricing.colDiscount'), dataIndex: 'discount', width: 110 },
  { title: t('settingPage.pricing.colAction'), slotName: 'actions', width: 160, fixed: 'right' },
])
const gpScroll = { x: 650 }

const mpVisible = ref(false), mpEditing = ref(false), mpSaving = ref(false)
const mpForm = reactive({ model_name: '', input_price: 0, output_price: 0, cached_price: 0, per_request_price: 0, billing_type: 'token', enabled: true })

const gpVisible = ref(false), gpEditing = ref(false), gpSaving = ref(false)
const gpForm = reactive({ group_name: '', model_name: '', discount: 1 })

const fbSaving = ref(false)
const fbForm = reactive({ input_price: 0, output_price: 0, cached_price: 0, per_request_price: 0, billing_type: 'token', enabled: true })

async function loadData() {
  loading.value = true
  try {
    const [mp, gp, fb, catalog] = await Promise.all([
      api.get('/api/model_price/'),
      api.get('/api/group_price/'),
      api.get('/api/model_price/fallback'),
      api.get('/api/model/options', { params: { scope: 'all' } }),
    ])
    if (catalog.data.success) modelMetadata.value = catalog.data.data || []
    const mpList = Array.isArray(mp.data.data) ? mp.data.data : mp.data.data?.items || []
    // 兜底价（*）在独立的「兜底价格」tab 里配置，不出现在普通模型定价列表。
    modelPrices.value = mpList.filter((r) => r.model_name !== '*')
    groupPrices.value = Array.isArray(gp.data.data) ? gp.data.data : gp.data.data?.items || []
    if (fb.data.success && fb.data.data) Object.assign(fbForm, fb.data.data)
  } catch (e) { /* ignore */ } finally { loading.value = false }
}

async function handleSaveFallback() {
  fbSaving.value = true
  try {
    const { data } = await api.put('/api/model_price/fallback', { ...fbForm })
    if (data.success) Message.success(t('settingPage.pricing.fallbackSaved'))
    else Message.error(data.message)
  } catch (e) { Message.error(t('settingPage.pricing.opFailed')) } finally { fbSaving.value = false }
}

function openModelPrice(r) {
  mpEditing.value = !!r
  Object.assign(mpForm, r ? { ...r } : { id: 0, model_name: '', input_price: 0, output_price: 0, cached_price: 0, per_request_price: 0, billing_type: 'token', enabled: true })
  mpVisible.value = true
}
async function handleSaveMP() {
  mpSaving.value = true
  try {
    const b = { ...mpForm }; if (mpEditing.value) b.id = mpForm.id
    const { data } = mpEditing.value ? await api.put('/api/model_price/', b) : await api.post('/api/model_price/', b)
    if (data.success) { mpVisible.value = false; loadData() } else Message.error(data.message)
  } catch (e) { Message.error(t('settingPage.pricing.opFailed')) } finally { mpSaving.value = false }
}
async function handleDelMP(id) { try { await api.delete(`/api/model_price/${id}`); loadData() } catch (e) { Message.error(t('settingPage.pricing.deleteFailed')) } }

function openGroupPrice(r) {
  gpEditing.value = !!r
  Object.assign(gpForm, r ? { ...r } : { id: 0, group_name: '', model_name: '', discount: 1 })
  gpVisible.value = true
}
async function handleSaveGP() {
  gpSaving.value = true
  try {
    const b = { ...gpForm }; if (gpEditing.value) b.id = gpForm.id
    const { data } = gpEditing.value ? await api.put('/api/group_price/', b) : await api.post('/api/group_price/', b)
    if (data.success) { gpVisible.value = false; loadData() } else Message.error(data.message)
  } catch (e) { Message.error(t('settingPage.pricing.opFailed')) } finally { gpSaving.value = false }
}
async function handleDelGP(id) { try { await api.delete(`/api/group_price/${id}`); loadData() } catch (e) { Message.error(t('settingPage.pricing.deleteFailed')) } }

onMounted(() => { loadData() })
</script>

<style scoped>
.price-unit-note { margin-bottom: 16px; }
small { color: var(--color-text-3); }
.category-filter { width: 220px; max-width: 100%; margin-bottom: 16px; }
.setting-container { width: 100%; padding: 4px 0; }
.fallback-tip { margin-bottom: 16px; }
.fallback-form { max-width: 560px; }
.fallback-hint { margin-left: 12px; color: var(--color-text-3); font-size: 13px; }
.section-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 20px; }
.section-header h3 { font-size: 16px; font-weight: 600; color: var(--color-text-1); margin: 0; padding: 0; }
</style>
