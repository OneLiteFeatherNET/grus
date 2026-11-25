<template>
  <M3StatusChip :tone="tone" :label="label" :icon="icon" />
</template>

<script setup lang="ts">
import { instanceStateTone } from '~/lib/states'

/**
 * Status of an instance as a coloured chip. The label comes from instances.states.<state>; a state this UI does not
 * know is shown as-is in neutral colour.
 */
const props = defineProps<{ state: string }>()
const { t, te } = useI18n()

const tone = computed(() => instanceStateTone(props.state))
const label = computed(() => (te(`instances.states.${props.state}`) ? t(`instances.states.${props.state}`) : props.state))
const icon = computed(() => {
  if (tone.value === 'success') return 'mdi-check-circle-outline'
  if (tone.value === 'error') return 'mdi-alert-circle-outline'
  if (tone.value === 'warning') return 'mdi-alert-outline'
  return undefined
})
</script>
