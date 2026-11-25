<template>
  <M3DataTable
    :columns="columns"
    :items="pools"
    row-key="name"
    data-test="pool-table"
  >
    <template #item.name="{ item }">
      <NuxtLink :to="poolPath(item.name)" class="font-medium text-primary hover:underline">{{ item.name }}</NuxtLink>
    </template>
    <template #item.role="{ item }">{{ item.spec.role }}</template>
    <template #item.mode="{ item }">{{ item.spec.mode }}</template>
    <template #item.driver="{ item }">{{ item.spec.driver }}</template>
    <template #item.scaling="{ item }">
      <span class="tabular-nums">
        {{ item.spec.scaling.min }} / {{ item.spec.scaling.max }} / {{ item.spec.scaling.buffer ?? 0 }}
      </span>
    </template>
    <template #item.instances="{ item }">
      <span class="tabular-nums" data-test="pool-instances">
        {{ countsOf(item.name).ready }} / {{ countsOf(item.name).total }}
      </span>
    </template>
    <template #item.maintenance="{ item }">
      <M3Chip v-if="item.maintenance" tone="warning" icon="mdi-wrench-outline" small>
        {{ $t('pools.badges.maintenance') }}
      </M3Chip>
    </template>
  </M3DataTable>
</template>

<script setup lang="ts">
import type { Instance, Pool } from '~/lib/grus'
import { poolCounts, poolPath } from '~/lib/pools/view'

const props = defineProps<{ pools: Pool[], instances: Instance[] }>()
const { t } = useI18n()

const columns = computed(() => [
  { key: 'name', title: t('pools.table.name'), sortable: false },
  { key: 'role', title: t('pools.table.role'), sortable: false },
  { key: 'mode', title: t('pools.table.mode'), sortable: false },
  { key: 'driver', title: t('pools.table.driver'), sortable: false },
  { key: 'scaling', title: t('pools.table.scaling'), sortable: false },
  { key: 'instances', title: t('pools.table.instances'), sortable: false },
  { key: 'maintenance', title: t('pools.table.maintenance'), sortable: false },
])

function countsOf(name: string) {
  return poolCounts(props.instances, name)
}
</script>
