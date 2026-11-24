<template>
  <div class="setting-layout">
    <a-card :bordered="false" class="setting-card">
      <div class="setting-sidebar">
        <div class="sidebar-title">{{ $t('settingPage.sidebarTitle') }}</div>
        <a-menu :selected-keys="[activeKey]" @menu-item-click="onMenuClick" class="setting-menu" role="tablist" :aria-label="$t('settingPage.sidebarTitle')">
          <a-menu-item key="system" v-if="authStore.isAdmin" role="tab" tabindex="0" :aria-selected="activeKey === 'system'"><template #icon><icon-settings /></template>{{ $t('settingPage.menu.system') }}</a-menu-item>
          <a-menu-item key="cluster" v-if="authStore.isRoot" role="tab" tabindex="0" :aria-selected="activeKey === 'cluster'"><template #icon><icon-storage /></template>{{ $t('settingPage.menu.cluster') }}</a-menu-item>
          <a-menu-item key="operation" v-if="authStore.isRoot" role="tab" tabindex="0" :aria-selected="activeKey === 'operation'"><template #icon><icon-tool /></template>{{ $t('settingPage.menu.operation') }}</a-menu-item>
          <a-menu-item key="payment" v-if="authStore.isRoot" role="tab" tabindex="0" :aria-selected="activeKey === 'payment'"><template #icon><icon-alipay-circle /></template>{{ $t('settingPage.menu.payment') }}</a-menu-item>
          <a-menu-item key="pricing" v-if="authStore.isRoot" role="tab" tabindex="0" :aria-selected="activeKey === 'pricing'"><template #icon><icon-tags /></template>{{ $t('settingPage.menu.pricing') }}</a-menu-item>
          <a-menu-item key="plan" v-if="authStore.isRoot" role="tab" tabindex="0" :aria-selected="activeKey === 'plan'"><template #icon><icon-calendar /></template>{{ $t('settingPage.menu.plan') }}</a-menu-item>
          <a-menu-item key="topup" v-if="authStore.isRoot" role="tab" tabindex="0" :aria-selected="activeKey === 'topup'"><template #icon><icon-subscribe-add /></template>{{ $t('settingPage.menu.topup') }}</a-menu-item>
          <a-menu-item key="personal" role="tab" tabindex="0" :aria-selected="activeKey === 'personal'"><template #icon><icon-user /></template>{{ $t('settingPage.menu.personal') }}</a-menu-item>
        </a-menu>
      </div>
      <div class="setting-content">
        <router-view />
      </div>
    </a-card>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { IconSettings, IconStorage, IconTool, IconAlipayCircle, IconTags, IconCalendar, IconUser, IconSubscribeAdd } from '@arco-design/web-vue/es/icon'

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()

const activeKey = computed(() => route.path.split('/').pop() || 'system')

function onMenuClick(key) {
  router.push(`/setting/${key}`)
}
</script>

<style scoped>
.setting-layout { height: 100%; }
.setting-card { height: 100%; display: flex; padding: 0; }
.setting-card :deep(.arco-card-body) { display: flex; padding: 0; width: 100%; min-width: 0; }
.setting-sidebar { width: 160px; flex-shrink: 0; border-right: 1px solid var(--color-border-2); padding: 20px 0; }
.sidebar-title { padding: 0 20px 16px; font-size: 16px; font-weight: 700; color: var(--color-text-1); }
.setting-menu { border: none; }
/* min-width:0 lets the flex item shrink below its content's intrinsic
 * min-width so that inner overflow-x:auto containers (e.g. the pricing
 * tables) can actually scroll instead of being pushed past the viewport
 * and clipped by an outer arco-layout overflow-x:hidden rule. */
.setting-content { flex: 1; padding: 15px; overflow-y: auto; overflow-x: hidden; min-width: 0; }
@media (max-width: 768px) {
  .setting-card :deep(.arco-card-body) { flex-direction: column; }
  .setting-sidebar { width: 100%; min-width: 0; border-right: none; border-bottom: 1px solid var(--color-border-2); padding: 12px 0; }
  .sidebar-title { padding: 0 12px 8px; }
  .setting-menu { overflow-x: auto; }
  .setting-menu :deep(.arco-menu-inner) { display: flex; width: max-content; padding: 0 8px; }
  .setting-menu :deep(.arco-menu-item) { flex-shrink: 0; width: auto; margin: 0 4px; }
  .setting-content { padding: 12px; }
}
</style>
