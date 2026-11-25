<template>
  <div>
    <PageHeader :title="$t('templates.title')" :subtitle="$t('templates.subtitle')">
      <template #actions>
        <span class="text-label-medium text-on-surface-variant" data-test="live-status">
          {{ $t(`common.live.${status}`) }}
        </span>
      </template>
    </PageHeader>

    <AppProblem :error="error" />

    <section v-if="views.length" class="grid gap-4 lg:grid-cols-2" :aria-label="$t('templates.listLabel')">
      <TemplateCard v-for="view in views" :key="view.name" :view="view" />
    </section>

    <M3EmptyState v-else-if="!pending && !error" :title="$t('templates.empty')" icon="mdi-tag-multiple-outline" />
  </div>
</template>

<script setup lang="ts">
import { templatesApi } from '~/lib/api/templates'
import { buildTemplateViews } from '~/lib/templates/model'

// Templates with their tags, the pools that reference them. Re-tagging elsewhere shows up through the live stream.
const { status, onResync, onEvent } = useGrusEvents()
const { data, error, pending, refresh } = await useAsyncData('templates', async () => {
  const [tags, pools] = await Promise.all([templatesApi.listTemplates(), templatesApi.listPools()])
  return buildTemplateViews(tags, pools)
})

const views = computed(() => data.value ?? [])

onResync(() => void refresh())
onEvent(['template.tagged', 'pool.*'], () => void refresh())
</script>
