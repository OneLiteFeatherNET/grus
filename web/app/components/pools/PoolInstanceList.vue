<template>
  <div>
    <p v-if="!instances.length" class="text-body-large text-on-surface-variant" data-test="pool-instances-empty">
      {{ $t('pools.instances.empty') }}
    </p>
    <M3DataTable
      v-else
      :columns="columns"
      :items="instances"
      row-key="id"
      data-test="pool-instance-table"
    >
      <template #item.id="{ item }">
        <NuxtLink :to="`/instances/${encodeURIComponent(item.id)}`" class="font-mono text-primary hover:underline">
          {{ item.id }}
        </NuxtLink>
      </template>
      <template #item.state="{ item }"><StatusChip :state="item.state" /></template>
      <template #item.players="{ item }">
        <span class="tabular-nums">{{ item.players }} / {{ item.maxPlayers }}</span>
      </template>
      <template #item.address="{ item }">
        <span class="font-mono">{{ item.address || item.host }}</span>
      </template>
      <template #item.createdAt="{ item }">
        <span class="tabular-nums">{{ ageText(item.createdAt) }}</span>
      </template>
    </M3DataTable>
  </div>
</template>

<script setup lang="ts">
import type { Instance } from '~/lib/grus'
import { ageOf } from '~/lib/pools/view'

/** Instances of one pool. `now` is the reference time for the age column; null before the client has mounted. */
const props = defineProps<{ instances: Instance[], now: number | null }>()
const { t } = useI18n()

const columns = computed(() => [
  { key: 'id', title: t('pools.instances.id'), sortable: false },
  { key: 'state', title: t('pools.instances.state'), sortable: false },
  { key: 'players', title: t('pools.instances.players'), sortable: false },
  { key: 'address', title: t('pools.instances.address'), sortable: false },
  { key: 'createdAt', title: t('pools.instances.age'), sortable: false },
])

function ageText(createdAt: string): string {
  if (props.now === null) return '–'
  const { unit, value } = ageOf(createdAt, props.now)
  return t(`pools.age.${unit}`, { value })
}
</script>
