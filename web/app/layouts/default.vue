<template>
  <div class="min-h-screen">
    <a
      class="absolute -top-16 left-2 z-[70] rounded-full bg-primary px-4 py-2 text-on-primary focus:top-2"
      href="#main"
      @click.prevent="focusMain"
    >{{ $t('common.skipToContent') }}</a>

    <M3TopAppBar>
      <template #leading>
        <M3IconButton v-if="!isDesktop" icon="mdi-menu" :label="$t('common.menu')" :aria-expanded="String(drawer)" @click="drawer = true" />
      </template>
      <template #title>
        <NuxtLink to="/" class="inline-flex items-center gap-3 rounded-full py-1 pr-3 pl-1 text-on-surface no-underline" :aria-label="$t('common.appName')">
          <img src="/logo.png" alt="" width="40" height="40" class="size-10 object-contain">
          <span class="text-title-large">Grus</span>
        </NuxtLink>
      </template>
      <template #actions>
        <M3IconButton icon="mdi-translate" :label="$t('common.language')" @click="toggleLocale" />
        <M3IconButton
          :icon="isDark ? 'mdi-weather-sunny' : 'mdi-weather-night'"
          :label="isDark ? $t('common.lightTheme') : $t('common.darkTheme')"
          @click="toggleTheme"
        />
      </template>
    </M3TopAppBar>

    <div class="flex">
      <M3NavDrawer
        v-if="isDesktop"
        class="sticky top-16 h-[calc(100dvh-4rem)] overflow-y-auto"
        :items="entries"
        :active-path="route.path"
        :label="$t('common.navigation')"
      />
      <M3NavDrawer
        v-else
        v-model:open="drawer"
        modal
        :items="entries"
        :active-path="route.path"
        :label="$t('common.navigation')"
        :close-label="$t('common.close')"
      />
      <main id="main" tabindex="-1" class="min-w-0 flex-1 px-4 py-6 outline-none md:px-6">
        <div class="max-w-[88rem]">
          <slot />
        </div>
      </main>
    </div>

    <M3Snackbar :message="snackbar.message.value" :close-label="$t('common.close')" @dismiss="snackbar.dismiss" />
  </div>
</template>

<script setup lang="ts">
import { NAV_ITEMS } from '~/lib/nav'

const { t, locale, setLocale } = useI18n()
const { isDark, toggle: toggleTheme, restore: restoreTheme } = useColorTheme()
const snackbar = useSnackbar()
const route = useRoute()

const isDesktop = useMediaQuery('(min-width: 1024px)')
const drawer = ref(false)
const entries = computed(() => NAV_ITEMS.map(item => ({ to: item.to, icon: item.icon, label: t(`common.nav.${item.key}`) })))

// The stored theme choice is only known in the browser; restoring it after mount avoids a hydration mismatch.
onMounted(() => restoreTheme())

function toggleLocale() {
  void setLocale(locale.value === 'de' ? 'en' : 'de')
}

// After a navigation, focus moves to the new page so keyboard and screen reader users start at the content.
watch(() => route.path, () => nextTick(() => document.getElementById('main')?.focus({ preventScroll: true })))

function focusMain() {
  document.getElementById('main')?.focus()
}
</script>
