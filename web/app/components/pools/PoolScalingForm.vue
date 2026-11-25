<template>
  <form novalidate data-test="scaling-form" @submit.prevent="onSubmit">
    <div class="grid gap-4 sm:grid-cols-2">
      <M3TextField
        v-for="field in FIELDS"
        :key="field.key"
        :model-value="modelValue[field.key]"
        :label="$t(`pools.scaling.fields.${field.key}`)"
        :type="field.type"
        :inputmode="field.inputmode"
        :step="field.step"
        :min="field.min"
        :max="field.max"
        :name="field.key"
        :error="errorText(field.key)"
        :hint="field.hint ? $t(`pools.scaling.hints.${field.hint}`) : ''"
        class="min-w-0"
        @update:model-value="value => onInput(field.key, String(value))"
      />
    </div>

    <p v-if="dirty" class="mt-4 text-body-small text-on-surface-variant" data-test="scaling-unsaved">
      {{ $t('pools.scaling.unsaved') }}
    </p>

    <div class="mt-4 flex flex-wrap justify-end gap-2">
      <M3Button variant="text" :disabled="!dirty || busy" @click="emit('reset')">
        {{ $t('pools.scaling.reset') }}
      </M3Button>
      <M3Button type="submit" icon="mdi-content-save-outline" :disabled="!dirty || busy">
        {{ $t('pools.scaling.save') }}
      </M3Button>
    </div>
  </form>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'
import { validateScaling } from '~/lib/pools/scaling'
import type { Scaling, ScalingError, ScalingField, ScalingForm } from '~/lib/pools/scaling'

/**
 * The scaling part of a pool spec as an editable form. Nothing is emitted as `submit` unless every field is valid;
 * errors show once a field was edited or the form was submitted.
 */
const props = defineProps<{ modelValue: ScalingForm, mode: string, dirty: boolean, busy: boolean }>()
const emit = defineEmits<{
  'update:modelValue': [ScalingForm]
  'submit': [Scaling]
  'reset': []
}>()

interface FieldSpec {
  key: ScalingField
  type: 'number' | 'text'
  inputmode?: 'numeric' | 'decimal' | 'text'
  step?: string
  min?: string
  max?: string
  hint?: 'duration' | 'optional' | 'targetUtilization' | 'buffer'
}

const FIELDS: FieldSpec[] = [
  { key: 'min', type: 'number', inputmode: 'numeric', step: '1', min: '0' },
  { key: 'max', type: 'number', inputmode: 'numeric', step: '1', min: '0' },
  { key: 'buffer', type: 'number', inputmode: 'numeric', step: '1', min: '0', hint: 'buffer' },
  { key: 'capacity', type: 'number', inputmode: 'numeric', step: '1', min: '0', hint: 'optional' },
  { key: 'targetUtilization', type: 'number', inputmode: 'decimal', step: '0.01', min: '0', max: '1', hint: 'targetUtilization' },
  { key: 'scaleDownDelay', type: 'text', inputmode: 'text', hint: 'duration' },
  { key: 'drainTimeout', type: 'text', inputmode: 'text', hint: 'duration' },
  { key: 'maxSurge', type: 'number', inputmode: 'numeric', step: '1', min: '0', hint: 'optional' },
]

const { t } = useI18n()
const touched = reactive<Partial<Record<ScalingField, boolean>>>({})
const submitted = ref(false)

const result = computed(() => validateScaling(props.modelValue, { mode: props.mode }))

function shownError(field: ScalingField): ScalingError | undefined {
  const error = result.value.errors[field]
  return error && (touched[field] || submitted.value) ? error : undefined
}
function errorText(field: ScalingField): string {
  const error = shownError(field)
  return error ? t(`pools.scaling.errors.${error}`) : ''
}

function onInput(field: ScalingField, value: string) {
  touched[field] = true
  emit('update:modelValue', { ...props.modelValue, [field]: value })
}

function onSubmit() {
  submitted.value = true
  const { scaling } = result.value
  if (scaling) emit('submit', scaling)
}
</script>
