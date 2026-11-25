<template>
  <form novalidate data-test="scaling-form" @submit.prevent="onSubmit">
    <div class="grid gap-4 sm:grid-cols-2">
      <div v-for="field in FIELDS" :key="field.key" class="flex min-w-0 flex-col gap-1">
        <label :for="idOf(field.key)" class="text-label-large text-on-surface">
          {{ $t(`pools.scaling.fields.${field.key}`) }}
        </label>
        <input
          :id="idOf(field.key)"
          :value="modelValue[field.key]"
          :type="field.type"
          :inputmode="field.inputmode"
          :step="field.step"
          :min="field.min"
          :max="field.max"
          :name="field.key"
          :aria-invalid="shownError(field.key) ? 'true' : 'false'"
          :aria-describedby="describedBy(field.key)"
          class="w-full rounded-xs border border-outline bg-surface px-3 py-2 text-body-large text-on-surface focus:outline-2 focus:outline-primary aria-[invalid=true]:border-error"
          @input="onInput(field.key, ($event.target as HTMLInputElement).value)"
        >
        <p v-if="shownError(field.key)" :id="errorIdOf(field.key)" class="text-body-small text-error">
          {{ $t(`pools.scaling.errors.${shownError(field.key)}`) }}
        </p>
        <p v-else-if="field.hint" :id="hintIdOf(field.key)" class="text-body-small text-on-surface-variant">
          {{ $t(`pools.scaling.hints.${field.hint}`) }}
        </p>
      </div>
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
import { reactive, ref, useId } from 'vue'
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

const uid = useId()
const touched = reactive<Partial<Record<ScalingField, boolean>>>({})
const submitted = ref(false)

const result = computed(() => validateScaling(props.modelValue, { mode: props.mode }))

function idOf(field: ScalingField) {
  return `${uid}-${field}`
}
function errorIdOf(field: ScalingField) {
  return `${idOf(field)}-error`
}
function hintIdOf(field: ScalingField) {
  return `${idOf(field)}-hint`
}
function shownError(field: ScalingField): ScalingError | undefined {
  const error = result.value.errors[field]
  return error && (touched[field] || submitted.value) ? error : undefined
}
function describedBy(field: ScalingField): string | undefined {
  if (shownError(field)) return errorIdOf(field)
  return FIELDS.find(f => f.key === field)?.hint ? hintIdOf(field) : undefined
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
