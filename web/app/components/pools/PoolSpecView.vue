<template>
  <section :aria-labelledby="headingId" class="rounded-md bg-surface-container-low p-4 shadow-elevation-1">
    <h2 :id="headingId" class="mb-4 text-title-large text-on-surface">{{ $t('pools.spec.heading') }}</h2>
    <div class="grid gap-6 md:grid-cols-2">
      <div v-for="section in sections" :key="section.key" data-test="spec-section">
        <h3 class="mb-2 text-title-small text-on-surface-variant">{{ $t(`pools.spec.sections.${section.key}`) }}</h3>
        <dl class="grid grid-cols-[minmax(0,auto)_minmax(0,1fr)] gap-x-4 gap-y-1 text-body-medium">
          <template v-for="row in section.rows" :key="row.key">
            <dt class="text-on-surface-variant">{{ $t(`pools.spec.${row.key}`) }}</dt>
            <dd class="min-w-0 break-words" :class="{ 'italic text-on-surface-variant': row.value === null }">
              {{ row.value ?? $t('pools.spec.none') }}
            </dd>
          </template>
        </dl>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { useId } from 'vue'
import type { PoolSpec } from '~/lib/grus'
import { specSections } from '~/lib/pools/view'

/** Read-only view of the whole spec. Secret values are never part of a spec; only their references are shown. */
const props = defineProps<{ spec: PoolSpec }>()
const headingId = `spec-${useId()}`
const sections = computed(() => specSections(props.spec))
</script>
