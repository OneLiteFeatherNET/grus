<template>
  <div>
    <PageHeader :title="$t('overview.title')" :subtitle="$t('overview.subtitle')">
      <template #actions>
        <span class="text-label-medium text-on-surface-variant" data-test="live-status">
          {{ $t(`common.live.${status}`) }}
        </span>
      </template>
    </PageHeader>

    <AppProblem :error="error" />

    <section v-if="pools?.length" class="grid gap-4 sm:grid-cols-2 xl:grid-cols-3" :aria-label="$t('overview.poolsLabel')">
      <M3Card v-for="pool in pools" :key="pool.name" variant="elevated" :title="pool.name">
        <dl class="grid grid-cols-2 gap-x-4 gap-y-2 text-body-medium">
          <dt class="text-on-surface-variant">{{ $t('overview.role') }}</dt>
          <dd>{{ pool.spec.role }}</dd>
          <dt class="text-on-surface-variant">{{ $t('overview.mode') }}</dt>
          <dd>{{ pool.spec.mode }}</dd>
          <dt class="text-on-surface-variant">{{ $t('overview.instances') }}</dt>
          <dd data-test="pool-instances">{{ $t('overview.serving', countsOf(pool.name)) }}</dd>
        </dl>
        <div v-if="pool.maintenance" class="mt-3">
          <M3Chip tone="warning" icon="mdi-alert-outline" small>{{ $t('overview.maintenance') }}</M3Chip>
        </div>
      </M3Card>
    </section>

    <M3EmptyState v-else-if="!pending && !error" :title="$t('overview.empty')" />
  </div>
</template>

<script setup lang="ts">
import { grus } from '~/lib/grus'
import type { Instance } from '~/lib/grus'
import { isServing } from '~/lib/states'

// The first page: pools with their instance counts, straight from the BFF. Live updates re-run the load.
const { status, onResync, onEvent } = useGrusEvents()
const { data, error, pending, refresh } = await useAsyncData('overview', async () => {
  const [poolList, instanceList] = await Promise.all([grus.listPools(), grus.listInstances()])
  return { pools: poolList, instances: instanceList }
})

const pools = computed(() => data.value?.pools)

function countsOf(pool: string): { serving: number, total: number } {
  const list: Instance[] = (data.value?.instances ?? []).filter(i => i.pool === pool)
  return { serving: list.filter(i => isServing(i.state)).length, total: list.length }
}

onResync(() => void refresh())
onEvent(['pool.*', 'instance.*'], () => void refresh())
</script>
