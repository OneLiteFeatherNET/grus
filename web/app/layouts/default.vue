<template>
  <M3AppFrame :items="entries" :active-path="route.path" show-language @toggle-language="toggleLocale">
    <template #title>
      <NuxtLink to="/" class="inline-flex items-center gap-3 rounded-full py-1 pr-3 pl-1 text-on-surface no-underline" :aria-label="$t('common.appName')">
        <img src="/logo.png" alt="" width="40" height="40" class="size-10 object-contain">
        <span class="text-title-large">Grus</span>
      </NuxtLink>
    </template>
    <slot />
  </M3AppFrame>
</template>

<script setup lang="ts">
import { NAV_ITEMS } from '~/lib/nav'

const { t, locale, setLocale } = useI18n()
const route = useRoute()

const entries = computed(() => NAV_ITEMS.map(item => ({ to: item.to, icon: item.icon, label: t(`common.nav.${item.key}`) })))

function toggleLocale() {
  void setLocale(locale.value === 'de' ? 'en' : 'de')
}
</script>
