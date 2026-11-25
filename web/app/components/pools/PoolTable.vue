<template>
  <div class="overflow-x-auto rounded-md bg-surface-container-low shadow-elevation-1">
    <table class="w-full min-w-[44rem] text-left text-body-medium" data-test="pool-table">
      <thead class="text-label-large text-on-surface-variant">
        <tr>
          <th scope="col" class="px-4 py-3 font-medium">{{ $t('pools.table.name') }}</th>
          <th scope="col" class="px-4 py-3 font-medium">{{ $t('pools.table.role') }}</th>
          <th scope="col" class="px-4 py-3 font-medium">{{ $t('pools.table.mode') }}</th>
          <th scope="col" class="px-4 py-3 font-medium">{{ $t('pools.table.driver') }}</th>
          <th scope="col" class="px-4 py-3 font-medium">{{ $t('pools.table.scaling') }}</th>
          <th scope="col" class="px-4 py-3 font-medium">{{ $t('pools.table.instances') }}</th>
          <th scope="col" class="px-4 py-3 font-medium">{{ $t('pools.table.maintenance') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="pool in pools" :key="pool.name" class="border-t border-outline-variant" data-test="pool-row">
          <td class="px-4 py-3">
            <NuxtLink :to="poolPath(pool.name)" class="font-medium text-primary hover:underline">{{ pool.name }}</NuxtLink>
          </td>
          <td class="px-4 py-3">{{ pool.spec.role }}</td>
          <td class="px-4 py-3">{{ pool.spec.mode }}</td>
          <td class="px-4 py-3">{{ pool.spec.driver }}</td>
          <td class="px-4 py-3 tabular-nums">
            {{ pool.spec.scaling.min }} / {{ pool.spec.scaling.max }} / {{ pool.spec.scaling.buffer ?? 0 }}
          </td>
          <td class="px-4 py-3 tabular-nums" data-test="pool-instances">
            {{ countsOf(pool.name).ready }} / {{ countsOf(pool.name).total }}
          </td>
          <td class="px-4 py-3">
            <M3Chip v-if="pool.maintenance" tone="warning" icon="mdi-wrench-outline" small>
              {{ $t('pools.badges.maintenance') }}
            </M3Chip>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<script setup lang="ts">
import type { Instance, Pool } from '~/lib/grus'
import { poolCounts, poolPath } from '~/lib/pools/view'

const props = defineProps<{ pools: Pool[], instances: Instance[] }>()

function countsOf(name: string) {
  return poolCounts(props.instances, name)
}
</script>
