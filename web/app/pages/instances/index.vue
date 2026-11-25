<template>
  <div>
    <M3PageHeader :title="$t('instances.title')" :subtitle="$t('instances.subtitle')">
      <template #actions>
        <span class="text-label-medium text-on-surface-variant" data-test="live-status">
          {{ $t(`common.live.${status}`) }}
        </span>
      </template>
    </M3PageHeader>

    <M3Problem :error="error" />

    <form class="mb-4 flex flex-wrap items-end gap-3" role="search" @submit.prevent>
      <M3Select
        :model-value="query.pool ?? ''"
        :items="poolItems"
        :label="$t('instances.filter.pool')"
        data-test="filter-pool"
        @update:model-value="pool => update({ pool: String(pool) })"
      />
      <M3Select
        :model-value="query.state ?? ''"
        :items="stateItems"
        :label="$t('instances.filter.state')"
        data-test="filter-state"
        @update:model-value="state => update({ state: String(state) })"
      />
    </form>

    <M3DataTable
      v-if="visible.length"
      :columns="columns"
      :items="visible"
      row-key="id"
      data-test="instance-table"
    >
      <template #item.id="{ item }">
        <NuxtLink :to="`/instances/${encodeURIComponent(item.id)}`" class="font-mono text-primary underline-offset-2 hover:underline">
          {{ item.id }}
        </NuxtLink>
      </template>
      <template #item.state="{ item }">
        <StatusChip :state="item.state" />
      </template>
      <template #item.address="{ item }">
        <span class="font-mono text-body-small">{{ item.address || item.host }}</span>
      </template>
      <template #item.players="{ item }">
        <span class="tabular-nums">{{ item.players }} / {{ item.maxPlayers }}</span>
      </template>
      <template #item.age="{ item }">
        <span class="text-on-surface-variant">{{ ages.get(item.id) ?? '' }}</span>
      </template>
    </M3DataTable>
    <M3EmptyState v-else-if="!pending && !error" :title="$t('instances.empty')" icon="mdi-cube-outline" />
  </div>
</template>

<script setup lang="ts">
import { instancesApi } from '~/lib/api/instances'
import type { Instance } from '~/lib/grus'
import { applyInstanceEvent, filterInstances, formatAge, sortInstances } from '~/lib/instances/list'
import { parseInstanceQuery, toInstanceRouteQuery } from '~/lib/instances/query'
import type { InstanceListQuery } from '~/lib/instances/query'
import { INSTANCE_STATES } from '~/lib/states'

// The list is loaded once; instance.* events are applied to it locally. A resync reloads everything.
const { status, onEvent, onResync } = useGrusEvents()
const route = useRoute()
const router = useRouter()
const { locale, t } = useI18n()

const { data, error, pending, refresh } = await useAsyncData('instances', () => instancesApi.list())

const query = computed(() => parseInstanceQuery(route.query))
const all = computed<Instance[]>(() => data.value ?? [])
const poolOptions = computed(() => {
  const pools = new Set(all.value.map(i => i.pool))
  if (query.value.pool) pools.add(query.value.pool)
  return [...pools].sort()
})
const poolItems = computed(() => [
  { value: '', title: t('instances.filter.all') },
  ...poolOptions.value.map(pool => ({ value: pool, title: pool })),
])
const stateItems = computed(() => [
  { value: '', title: t('instances.filter.all') },
  ...INSTANCE_STATES.map(state => ({ value: state, title: t(`instances.states.${state}`) })),
])

// Only the name column is sortable in the table. The sort query parameters are not applied to it.
const columns = computed(() => [
  { key: 'id', title: t('instances.columns.name') },
  { key: 'pool', title: t('instances.columns.pool'), sortable: false },
  { key: 'state', title: t('instances.columns.state'), sortable: false },
  { key: 'address', title: t('instances.columns.address'), sortable: false },
  { key: 'players', title: t('instances.columns.players'), sortable: false },
  { key: 'age', title: t('instances.columns.age'), sortable: false },
])

// "now" is only known in the browser; SSR renders no ages to avoid a hydration mismatch.
const now = ref<number | null>(null)
let timer: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  now.value = Date.now()
  timer = setInterval(() => {
    now.value = Date.now()
  }, 30_000)
})
onBeforeUnmount(() => clearInterval(timer))

const visible = computed(() => {
  const q = query.value
  return sortInstances(filterInstances(all.value, q), q.sort, q.dir, now.value ?? 0)
})

const ages = computed(() => {
  const map = new Map<string, string>()
  if (now.value === null) return map
  for (const inst of all.value) map.set(inst.id, formatAge(inst.createdAt, now.value as number, locale.value))
  return map
})

onEvent(['instance.*'], (event) => {
  data.value = applyInstanceEvent(data.value ?? [], event, new Date().toISOString())
})
onResync(() => void refresh())

function update(patch: Partial<InstanceListQuery>) {
  void router.replace({ query: toInstanceRouteQuery({ ...query.value, ...patch }) })
}
</script>
