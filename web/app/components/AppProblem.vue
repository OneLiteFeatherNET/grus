<template>
  <M3Alert v-if="error" tone="error" role="alert" :title="title" class="mb-4">
    <p v-if="detail">{{ detail }}</p>
    <ul v-if="apiError && apiError.errors.length" class="mt-2 list-disc pl-5" data-test="problem-errors">
      <li v-for="(e, i) in apiError.errors" :key="i">
        <span v-if="e.location" class="font-mono">{{ e.location }}: </span>{{ e.message }}
      </li>
    </ul>
  </M3Alert>
</template>

<script setup lang="ts">
import { ApiError, NetworkError } from '~/lib/problem'

/**
 * Shows an error from the API. RFC 9457 problems show their title, detail and field errors; a network failure gets
 * its own text; anything else shows its message.
 */
const props = defineProps<{ error?: Error | null }>()
const { t } = useI18n()

const apiError = computed(() => (props.error instanceof ApiError ? props.error : null))
const title = computed(() => {
  if (props.error instanceof NetworkError) return t('common.problem.network')
  if (apiError.value) return `${apiError.value.status} ${apiError.value.problem.title}`
  return t('common.problem.unexpected')
})
const detail = computed(() => {
  if (props.error instanceof NetworkError) return t('common.problem.networkHint')
  if (apiError.value) return apiError.value.problem.detail ?? ''
  return props.error?.message ?? ''
})
</script>
