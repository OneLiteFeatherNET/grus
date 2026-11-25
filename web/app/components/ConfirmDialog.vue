<template>
  <M3Dialog
    :model-value="modelValue"
    :title="title"
    width="sm"
    alert
    :describedby="descriptionId"
    @update:model-value="emit('update:modelValue', $event)"
  >
    <p :id="descriptionId" class="text-body-large text-on-surface-variant">{{ message }}</p>
    <template #actions>
      <M3Button variant="text" @click="cancel">{{ cancelLabel || t('common.cancel') }}</M3Button>
      <M3Button :tone="tone" :disabled="busy" @click="confirm">{{ confirmLabel || t('common.confirm') }}</M3Button>
    </template>
  </M3Dialog>
</template>

<script setup lang="ts">
import { useId } from 'vue'

/**
 * Asks before a mutating action (pool edit, stop, console command, maintenance). The parent owns the open state:
 * `confirm` is emitted when the user accepts; the dialog closes itself by emitting update:modelValue.
 */
withDefaults(defineProps<{
  modelValue: boolean
  title: string
  message: string
  confirmLabel?: string
  cancelLabel?: string
  /** `error` for destructive actions (stop, delete). */
  tone?: 'primary' | 'error'
  busy?: boolean
}>(), { confirmLabel: '', cancelLabel: '', tone: 'primary', busy: false })

const emit = defineEmits<{ 'update:modelValue': [boolean], 'confirm': [] }>()
const { t } = useI18n()
const descriptionId = `confirm-${useId()}`

function cancel() {
  emit('update:modelValue', false)
}
function confirm() {
  emit('confirm')
  emit('update:modelValue', false)
}
</script>
