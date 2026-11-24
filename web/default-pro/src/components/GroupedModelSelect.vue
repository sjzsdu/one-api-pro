<template>
  <!-- GroupedModelSelect 模型能力分组选择器 / Shared capability-aware model picker.
       版本: v0.0.28
       日期: 2026-11-23
       作者: Codex -->
  <a-select v-bind="$attrs" :model-value="modelValue" @update:model-value="$emit('update:modelValue', $event)">
    <a-option v-for="item in prependOptions" :key="item.value" :value="item.value" :label="item.label" />
    <a-optgroup v-for="group in groups" :key="group.key" :label="`${group.label} (${group.items.length})`">
      <a-option v-for="item in group.items" :key="item.value" :value="item.value" :label="item.label" :disabled="item.disabled" />
    </a-optgroup>
  </a-select>
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { groupModelsByCategory, modelOperations } from '@/utils/modelCategory'

defineOptions({ inheritAttrs: false })
const props = defineProps({
  modelValue: { type: [String, Number, Array], default: undefined },
  options: { type: Array, default: () => [] },
  metadata: { type: Array, default: () => [] },
  prependOptions: { type: Array, default: () => [] },
  operation: { type: String, default: '' },
})
defineEmits(['update:modelValue'])
const { locale } = useI18n()
const groups = computed(() => {
  const metadata = new Map(props.metadata.map(item => [item.model_name || item.id, item]))
  const items = props.options.map(option => {
    const value = typeof option === 'string' ? option : option.value || option.model_name || option.id
    return { ...(metadata.get(value) || {}), ...(typeof option === 'object' ? option : {}), model_name: value, value, label: option?.label || value }
  }).filter(item => item.value && (!props.operation || modelOperations(item).includes(props.operation)))
  return groupModelsByCategory(items, item => item.model_name, locale.value === 'en' ? 'en' : 'zh')
})
</script>
