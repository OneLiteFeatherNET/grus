<template>
  <div>
    <M3PageHeader :title="$t('overview.title')" :subtitle="$t('overview.subtitle')">
      <template #actions>
        <span class="text-label-medium text-on-surface-variant" data-test="live-status">
          {{ $t(`common.live.${status}`) }}
        </span>
      </template>
    </M3PageHeader>

    <M3Problem :error="error" />

    <template v-if="pools?.length">
      <section class="mb-6 grid grid-cols-2 gap-3 md:grid-cols-4" :aria-label="$t('overview.totals.label')" data-test="totals">
        <div class="rounded-md bg-surface-container p-4">
          <p class="text-label-medium text-on-surface-variant">{{ $t('overview.totals.pools') }}</p>
          <p class="text-headline-small" data-test="total-pools">{{ totals.pools }}</p>
        </div>
        <div class="rounded-md bg-surface-container p-4">
          <p class="text-label-medium text-on-surface-variant">{{ $t('overview.totals.ready') }}</p>
          <p class="text-headline-small" data-test="total-ready">{{ totals.instancesReady }} / {{ totals.instancesTotal }}</p>
        </div>
        <div class="rounded-md bg-surface-container p-4">
          <p class="text-label-medium text-on-surface-variant">{{ $t('overview.totals.players') }}</p>
          <p class="text-headline-small" data-test="total-players">{{ totals.players }}</p>
        </div>
        <div class="rounded-md bg-surface-container p-4">
          <p class="text-label-medium text-on-surface-variant">{{ $t('overview.totals.maintenance') }}</p>
          <p class="text-headline-small" data-test="total-maintenance">{{ totals.maintenance }}</p>
        </div>
      </section>

      <div class="grid gap-6 xl:grid-cols-[minmax(0,1fr)_22rem]">
        <section class="grid content-start gap-4 sm:grid-cols-2 xl:grid-cols-2" :aria-label="$t('overview.poolsLabel')">
          <M3Card
            v-for="pool in pools"
            :key="pool.name"
            variant="elevated"
            :title="pool.name"
            :to="poolHref(pool.name)"
            :data-test="`pool-card-${pool.name}`"
          >
            <dl class="grid grid-cols-2 gap-x-4 gap-y-2 text-body-medium">
              <dt class="text-on-surface-variant">{{ $t('overview.role') }}</dt>
              <dd>{{ pool.spec.role }}</dd>
              <dt class="text-on-surface-variant">{{ $t('overview.mode') }}</dt>
              <dd>{{ pool.spec.mode }}</dd>
              <dt class="text-on-surface-variant">{{ $t('overview.instances') }}</dt>
              <dd data-test="pool-instances">{{ $t('overview.serving', countsOf(pool.name)) }}</dd>
              <dt class="text-on-surface-variant">{{ $t('overview.players') }}</dt>
              <dd data-test="pool-players">{{ playersOf(pool.name, instances) }}</dd>
            </dl>
            <div v-if="pool.maintenance" class="mt-3">
              <M3Chip tone="warning" icon="mdi-alert-outline" small>{{ $t('overview.maintenance') }}</M3Chip>
            </div>
          </M3Card>
        </section>

        <M3Card variant="outlined" :title="$t('overview.feed.title')" data-test="activity-feed">
          <ul v-if="feed.length" class="grid gap-2">
            <li v-for="item in feed" :key="item.id" class="flex min-w-0 items-start gap-2 text-body-medium">
              <M3Chip :tone="item.tone" small class="shrink-0">{{ item.type }}</M3Chip>
              <span class="min-w-0 break-words">{{ item.line }}</span>
            </li>
          </ul>
          <p v-else class="text-body-medium text-on-surface-variant">{{ $t('overview.feed.empty') }}</p>
          <NuxtLink :to="'/events'" class="mt-3 inline-block text-label-large text-primary">
            {{ $t('overview.feed.all') }}
          </NuxtLink>
        </M3Card>
      </div>
    </template>

    <M3EmptyState v-else-if="!pending && !error" :title="$t('overview.empty')" />
  </div>
</template>

<script setup lang="ts">
import { grus } from '~/lib/grus'
import type { Instance } from '~/lib/grus'
import { isServing } from '~/lib/states'
import { describeEvent, eventTone } from '~/lib/events-view/describe'
import type { Translate } from '~/lib/events-view/describe'
import { createCoalescedRunner } from '~/lib/events-view/refresh'
import { playersOf, poolTotals } from '~/lib/overview/totals'

/** Activity feed length on the overview. The full stream lives on /events. */
const FEED_ITEMS = 20

// The first page: pools with their instance counts and totals, straight from the BFF. Live updates re-run the load,
// at most once per second, so a burst of events costs one request.
const { t } = useI18n()
const { status, onResync, onEvent, recent } = useGrusEvents()
const { data, error, pending, refresh } = await useAsyncData('overview', async () => {
  const [poolList, instanceList] = await Promise.all([grus.listPools(), grus.listInstances()])
  return { pools: poolList, instances: instanceList }
})

const pools = computed(() => data.value?.pools)
const instances = computed<Instance[]>(() => data.value?.instances ?? [])
const totals = computed(() => poolTotals(pools.value ?? [], instances.value))

const translate: Translate = (key, params) => t(key, params ?? {})
const feed = computed(() =>
  recent.value.slice(0, FEED_ITEMS).map(event => ({
    id: event.id,
    type: event.type,
    tone: eventTone(event),
    line: describeEvent(event, translate).line,
  })),
)

function poolHref(name: string): string {
  return `/pools/${encodeURIComponent(name)}`
}

function countsOf(pool: string): { serving: number, total: number } {
  const list = instances.value.filter(i => i.pool === pool)
  return { serving: list.filter(i => isServing(i.state)).length, total: list.length }
}

const refreshSoon = createCoalescedRunner(() => void refresh(), { intervalMs: 1000 })
onScopeDispose(() => refreshSoon.cancel())
onResync(() => void refresh())
onEvent(['pool.*', 'instance.*'], () => refreshSoon.request())
</script>
