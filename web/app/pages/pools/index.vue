<template>
  <div>
    <M3PageHeader :title="$t('pools.title')" :subtitle="$t('pools.subtitle')">
      <template #actions>
        <span class="text-label-medium text-on-surface-variant" data-test="live-status">
          {{ $t(`common.live.${status}`) }}
        </span>
      </template>
    </M3PageHeader>

    <M3Problem :error="error" />

    <PoolTable v-if="pools.length" :pools="pools" :instances="instances" />
    <M3EmptyState v-else-if="!pending && !error" :title="$t('pools.empty')" icon="mdi-layers-outline" />
  </div>
</template>

<script setup lang="ts">
import { grus } from '~/lib/grus'
import { poolsApi } from '~/lib/api/pools'
import { createCoalescer, LIVE_REFRESH_DELAY_MS } from '~/lib/pools/coalesce'

// Pools with their instance counts. Pool and instance events re-run the load, at most once per second.
const { status, onResync, onEvent } = useGrusEvents()
const { data, error, pending, refresh } = await useAsyncData('pools.list', async () => {
  const [poolList, instanceList] = await Promise.all([poolsApi.listPools(), grus.listInstances()])
  return { pools: poolList ?? [], instances: instanceList ?? [] }
})

const pools = computed(() => data.value?.pools ?? [])
const instances = computed(() => data.value?.instances ?? [])

const reload = createCoalescer(() => void refresh(), { delayMs: LIVE_REFRESH_DELAY_MS })
onEvent(['pool.*', 'instance.*'], () => reload.request())
onResync(() => reload.request())
onBeforeUnmount(() => reload.cancel())
</script>
