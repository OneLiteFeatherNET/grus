<template>
  <M3Card variant="elevated" :title="view.name" data-test="template-card">
    <section v-if="view.versions.length" :aria-label="$t('templates.versions')">
      <h3 class="text-title-small text-on-surface-variant">{{ $t('templates.versions') }}</h3>
      <ul class="mt-2 divide-y divide-outline-variant">
        <li
          v-for="version in view.versions"
          :key="version.digest"
          class="flex flex-wrap items-center gap-2 py-2"
          data-test="template-version"
        >
          <code class="font-mono text-body-medium" :title="version.digest">{{ shortDigest(version.digest) }}</code>
          <M3IconButton
            icon="mdi-content-copy"
            :label="$t('templates.copyDigest')"
            :size="32"
            @click="copyDigest(version.digest)"
          />
          <span class="flex flex-wrap gap-1">
            <M3Chip v-for="tag in version.tags" :key="tag" small tone="primary">{{ tag }}</M3Chip>
            <span v-if="!version.tags.length" class="text-body-small text-on-surface-variant">{{ $t('templates.untagged') }}</span>
          </span>
          <a
            class="ml-auto inline-flex items-center gap-1 text-label-large text-primary"
            :href="templatesApi.archiveUrl(view.name, version.digest)"
            :download="archiveFileName(view.name, version.digest)"
            data-test="template-download"
          >
            <M3Icon name="mdi-download-outline" />
            {{ $t('templates.download') }}
          </a>
        </li>
      </ul>
    </section>
    <p v-else class="text-body-medium text-on-surface-variant">{{ $t('templates.noVersions') }}</p>

    <section class="mt-4" :aria-label="$t('templates.usedBy')">
      <h3 class="text-title-small text-on-surface-variant">{{ $t('templates.usedBy') }}</h3>
      <p v-if="!view.pools.length" class="mt-1 text-body-medium text-on-surface-variant">{{ $t('templates.unused') }}</p>
      <ul v-else class="mt-2 flex flex-wrap gap-2" data-test="template-usage">
        <li v-for="usage in view.pools" :key="`${usage.pool}-${usage.ref}`">
          <M3Chip :tone="usage.resolved ? 'neutral' : 'warning'" :icon="usage.resolved ? undefined : 'mdi-alert-outline'" small>
            {{ usage.pool }} · {{ usage.ref }}
          </M3Chip>
        </li>
      </ul>
    </section>
  </M3Card>
</template>

<script setup lang="ts">
import type { TemplateView } from '~/lib/templates/model'
import { archiveFileName, shortDigest } from '~/lib/templates/model'
import { templatesApi } from '~/lib/api/templates'

/** One template: its versions with tags, a download per version and the pools that use it. */
defineProps<{ view: TemplateView }>()

const { t } = useI18n()
const snackbar = useSnackbar()

async function copyDigest(digest: string) {
  try {
    await navigator.clipboard.writeText(digest)
    snackbar.show(t('templates.copied'))
  }
  catch {
    snackbar.show(t('templates.copyFailed'))
  }
}
</script>
