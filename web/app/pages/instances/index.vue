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
      <label class="flex flex-col gap-1 text-label-medium text-on-surface-variant">
        {{ $t('instances.filter.pool') }}
        <select
          class="rounded-md border border-outline bg-surface-container px-3 py-2 text-body-medium text-on-surface"
          data-test="filter-pool"
          :value="query.pool ?? ''"
          @change="update({ pool: ($event.target as HTMLSelectElement).value })"
        >
          <option value="">{{ $t('instances.filter.all') }}</option>
          <option v-for="pool in poolOptions" :key="pool" :value="pool">{{ pool }}</option>
        </select>
      </label>
      <label class="flex flex-col gap-1 text-label-medium text-on-surface-variant">
        {{ $t('instances.filter.state') }}
        <select
          class="rounded-md border border-outline bg-surface-container px-3 py-2 text-body-medium text-on-surface"
          data-test="filter-state"
          :value="query.state ?? ''"
          @change="update({ state: ($event.target as HTMLSelectElement).value })"
        >
          <option value="">{{ $t('instances.filter.all') }}</option>
          <option v-for="state in INSTANCE_STATES" :key="state" :value="state">{{ $t(`instances.states.${state}`) }}</option>
        </select>
      </label>
    </form>

    <div v-if="visible.length" class="overflow-x-auto rounded-xl border border-outline-variant">
      <table class="w-full min-w-[40rem] text-left text-body-medium">
        <thead class="bg-surface-container text-label-large text-on-surface-variant">
          <tr>
            <th scope="col" class="px-4 py-3" :aria-sort="ariaSort('name')">
              <button type="button" class="font-medium" data-test="sort-name" @click="toggleSort('name')">
                {{ $t('instances.columns.name') }}{{ sortMark('name') }}
              </button>
            </th>
            <th scope="col" class="px-4 py-3">{{ $t('instances.columns.pool') }}</th>
            <th scope="col" class="px-4 py-3">{{ $t('instances.columns.state') }}</th>
            <th scope="col" class="px-4 py-3">{{ $t('instances.columns.address') }}</th>
            <th scope="col" class="px-4 py-3">{{ $t('instances.columns.players') }}</th>
            <th scope="col" class="px-4 py-3" :aria-sort="ariaSort('age')">
              <button type="button" class="font-medium" data-test="sort-age" @click="toggleSort('age')">
                {{ $t('instances.columns.age') }}{{ sortMark('age') }}
              </button>
            </th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="inst in visible" :key="inst.id" class="border-t border-outline-variant" data-test="instance-row">
            <td class="px-4 py-3">
              <NuxtLink :to="`/instances/${encodeURIComponent(inst.id)}`" class="font-mono text-primary underline-offset-2 hover:underline">
                {{ inst.id }}
              </NuxtLink>
            </td>
            <td class="px-4 py-3">{{ inst.pool }}</td>
            <td class="px-4 py-3"><StatusChip :state="inst.state" /></td>
            <td class="px-4 py-3 font-mono text-body-small">{{ inst.address || inst.host }}</td>
            <td class="px-4 py-3 tabular-nums">{{ inst.players }} / {{ inst.maxPlayers }}</td>
            <td class="px-4 py-3 text-on-surface-variant">{{ ages.get(inst.id) ?? '' }}</td>
          </tr>
        </tbody>
      </table>
    </div>
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
const { locale } = useI18n()

const { data, error, pending, refresh } = await useAsyncData('instances', () => instancesApi.list())

const query = computed(() => parseInstanceQuery(route.query))
const all = computed<Instance[]>(() => data.value ?? [])
const poolOptions = computed(() => {
  const pools = new Set(all.value.map(i => i.pool))
  if (query.value.pool) pools.add(query.value.pool)
  return [...pools].sort()
})

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

function toggleSort(key: 'name' | 'age') {
  const q = query.value
  if (q.sort === key) update({ dir: q.dir === 'asc' ? 'desc' : 'asc' })
  else update({ sort: key, dir: 'asc' })
}

function ariaSort(key: 'name' | 'age') {
  if (query.value.sort !== key) return 'none'
  return query.value.dir === 'asc' ? 'ascending' : 'descending'
}

function sortMark(key: 'name' | 'age') {
  if (query.value.sort !== key) return ''
  return query.value.dir === 'asc' ? ' ▲' : ' ▼'
}
</script>
