<template>
  <div>
    <p v-if="!instances.length" class="text-body-large text-on-surface-variant" data-test="pool-instances-empty">
      {{ $t('pools.instances.empty') }}
    </p>
    <div v-else class="overflow-x-auto rounded-md bg-surface-container-low shadow-elevation-1">
      <table class="w-full min-w-[36rem] text-left text-body-medium" data-test="pool-instance-table">
        <thead class="text-label-large text-on-surface-variant">
          <tr>
            <th scope="col" class="px-4 py-3 font-medium">{{ $t('pools.instances.id') }}</th>
            <th scope="col" class="px-4 py-3 font-medium">{{ $t('pools.instances.state') }}</th>
            <th scope="col" class="px-4 py-3 font-medium">{{ $t('pools.instances.players') }}</th>
            <th scope="col" class="px-4 py-3 font-medium">{{ $t('pools.instances.address') }}</th>
            <th scope="col" class="px-4 py-3 font-medium">{{ $t('pools.instances.age') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="instance in instances" :key="instance.id" class="border-t border-outline-variant" data-test="pool-instance-row">
            <td class="px-4 py-3">
              <NuxtLink :to="`/instances/${encodeURIComponent(instance.id)}`" class="font-mono text-primary hover:underline">
                {{ instance.id }}
              </NuxtLink>
            </td>
            <td class="px-4 py-3"><StatusChip :state="instance.state" /></td>
            <td class="px-4 py-3 tabular-nums">{{ instance.players }} / {{ instance.maxPlayers }}</td>
            <td class="px-4 py-3 font-mono">{{ instance.address || instance.host }}</td>
            <td class="px-4 py-3 tabular-nums">{{ ageText(instance.createdAt) }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { Instance } from '~/lib/grus'
import { ageOf } from '~/lib/pools/view'

/** Instances of one pool. `now` is the reference time for the age column; null before the client has mounted. */
const props = defineProps<{ instances: Instance[], now: number | null }>()
const { t } = useI18n()

function ageText(createdAt: string): string {
  if (props.now === null) return '–'
  const { unit, value } = ageOf(createdAt, props.now)
  return t(`pools.age.${unit}`, { value })
}
</script>
