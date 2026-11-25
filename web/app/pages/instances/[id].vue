<template>
  <div>
    <PageHeader :title="id" :subtitle="$t('instances.detail.subtitle')">
      <template #actions>
        <NuxtLink to="/instances" class="text-label-large text-primary">{{ $t('instances.back') }}</NuxtLink>
        <span class="text-label-medium text-on-surface-variant" data-test="live-status">
          {{ $t(`common.live.${status}`) }}
        </span>
      </template>
    </PageHeader>

    <AppProblem :error="error" />
    <M3Alert v-if="removed" tone="warning" class="mb-4" data-test="removed-notice">
      {{ $t('instances.removed') }}
    </M3Alert>

    <div v-if="instance" class="grid gap-4 lg:grid-cols-2">
      <M3Card variant="elevated" :title="$t('instances.detail.details')" class="lg:col-span-2">
        <dl class="grid grid-cols-1 gap-x-6 gap-y-3 text-body-medium sm:grid-cols-2 xl:grid-cols-3">
          <!-- One group per field, so label and value stay together in every column count. -->
          <div v-for="row in fields" :key="row.label">
            <dt class="text-label-medium text-on-surface-variant">{{ row.label }}</dt>
            <dd class="break-all" :class="row.mono ? 'font-mono text-body-small' : ''">{{ row.value }}</dd>
          </div>
        </dl>
        <div class="mt-4 flex flex-wrap items-center gap-3">
          <StatusChip :state="instance.state" />
          <M3Button
            tone="error"
            variant="outlined"
            :disabled="isTerminal(instance.state)"
            data-test="stop-button"
            @click="stopOpen = true"
          >
            {{ $t('instances.detail.stop') }}
          </M3Button>
        </div>
        <AppProblem :error="stopError" class="mt-3" />
      </M3Card>

      <M3Card variant="elevated" :title="$t('instances.detail.history')">
        <p v-if="!history.length" class="text-body-medium text-on-surface-variant">{{ $t('instances.detail.historyEmpty') }}</p>
        <ol v-else class="flex flex-col gap-2 text-body-medium" data-test="state-history">
          <li v-for="(entry, i) in history" :key="`${entry.at}-${i}`" class="flex flex-wrap gap-x-2">
            <span class="text-on-surface-variant">{{ formatTimestamp(entry.at, locale) }}</span>
            <span>{{ entry.from }} → {{ entry.to }}</span>
            <span v-if="entry.reason" class="text-on-surface-variant">({{ entry.reason }})</span>
          </li>
        </ol>
      </M3Card>

      <M3Card variant="elevated" :title="$t('instances.detail.logs')" class="lg:col-span-2">
        <p class="mb-2 text-label-medium text-on-surface-variant" data-test="log-status">
          {{ $t(`instances.detail.logStatus.${logStatus}`) }}
        </p>
        <div
          ref="logBox"
          role="log"
          tabindex="0"
          data-test="log-tail"
          class="h-80 overflow-auto rounded-md bg-surface-container-highest p-3 font-mono text-body-small text-on-surface"
          @scroll="onLogScroll"
        >
          <div v-for="(line, i) in lines" :key="i" class="whitespace-pre-wrap break-all">{{ line }}</div>
        </div>
        <M3Button v-if="!follow" variant="tonal" class="mt-2" @click="jumpToEnd">
          {{ $t('instances.detail.resumeScroll') }}
        </M3Button>

        <form class="mt-4 flex flex-wrap items-end gap-2" @submit.prevent="sendCommand">
          <label class="flex min-w-0 flex-1 flex-col gap-1 text-label-medium text-on-surface-variant">
            {{ $t('instances.detail.commandLabel') }}
            <input
              v-model="command"
              type="text"
              autocomplete="off"
              class="rounded-md border border-outline bg-surface-container px-3 py-2 font-mono text-body-medium text-on-surface"
              :placeholder="$t('instances.detail.commandPlaceholder')"
              data-test="command-input"
            >
          </label>
          <M3Button type="submit" :disabled="!normalizedCommand || sending" :loading="sending">
            {{ $t('instances.detail.send') }}
          </M3Button>
        </form>
        <AppProblem :error="commandError" class="mt-3" />
      </M3Card>
    </div>

    <ConfirmDialog
      v-model="stopOpen"
      :title="$t('instances.detail.stopTitle')"
      :message="$t('instances.detail.stopMessage')"
      :confirm-label="$t('instances.detail.stop')"
      tone="error"
      :busy="stopping"
      @confirm="stop"
    />
  </div>
</template>

<script setup lang="ts">
import { instancesApi } from '~/lib/api/instances'
import type { Instance } from '~/lib/grus'
import { toClientError } from '~/lib/problem'
import { normalizeCommand } from '~/lib/instances/command'
import { applyInstanceEvent, formatTimestamp, isTerminal } from '~/lib/instances/list'
import { appendStateChange } from '~/lib/instances/history'
import type { StateChange } from '~/lib/instances/history'
import { isAtBottom } from '~/lib/instances/logs'

const route = useRoute()
const id = String(route.params.id)
const { status, onEvent, onResync } = useGrusEvents()
const { locale, t } = useI18n()

const { data, error, refresh } = await useAsyncData(`instance-${id}`, () => instancesApi.get(id))
const instance = computed<Instance | undefined>(() => data.value ?? undefined)
const removed = ref(false)
const history = ref<StateChange[]>([])

const { lines, status: logStatus } = useInstanceLogs(id)

// Log follow: auto-scroll while the view sits at the bottom; scrolling up pauses it.
const logBox = ref<HTMLElement | null>(null)
const follow = ref(true)
function onLogScroll() {
  const el = logBox.value
  if (el) follow.value = isAtBottom(el.scrollTop, el.clientHeight, el.scrollHeight)
}
function jumpToEnd() {
  follow.value = true
  scrollToEnd()
}
function scrollToEnd() {
  const el = logBox.value
  if (el) el.scrollTop = el.scrollHeight
}
watch(lines, () => {
  if (follow.value) nextTick(scrollToEnd)
})

onEvent(['instance.*'], (event) => {
  const at = new Date().toISOString()
  const current = data.value ?? undefined
  if (current) {
    const next = applyInstanceEvent([current], event, at)
    if (next.length === 0) removed.value = true
    data.value = next[0] ?? undefined
  }
  history.value = appendStateChange(history.value, id, event, at)
})
onResync(() => void refresh())

// Console command: no confirmation per command (too noisy); the stop action is the one that asks.
const command = ref('')
const normalizedCommand = computed(() => normalizeCommand(command.value))
const sending = ref(false)
const commandError = ref<Error | null>(null)
const { show: showSnackbar } = useSnackbar()

async function sendCommand() {
  const line = normalizedCommand.value
  if (!line) return
  sending.value = true
  commandError.value = null
  try {
    await instancesApi.command(id, line)
    command.value = ''
    showSnackbar(t('instances.detail.commandSent'))
  }
  catch (err) {
    commandError.value = toClientError(err)
  }
  finally {
    sending.value = false
  }
}

// Stop: asks first. With players online the controller refuses a plain stop, so drain is requested.
const stopOpen = ref(false)
const stopping = ref(false)
const stopError = ref<Error | null>(null)

async function stop() {
  if (!instance.value) return
  const drain = instance.value.players > 0
  stopping.value = true
  stopError.value = null
  try {
    data.value = await instancesApi.stop(id, drain ? { drain: true } : {})
    showSnackbar(t('instances.detail.stopped'))
  }
  catch (err) {
    stopError.value = toClientError(err)
  }
  finally {
    stopping.value = false
  }
}

const fields = computed(() => {
  const i = instance.value
  if (!i) return []
  const date = (value?: string) => (value ? formatTimestamp(value, locale.value) : '')
  const labels = Object.entries(i.labels ?? {}).map(([k, v]) => `${k}=${v}`).join(', ')
  return [
    { label: t('instances.detail.fields.pool'), value: i.pool },
    { label: t('instances.detail.fields.role'), value: i.role },
    { label: t('instances.detail.fields.poolGeneration'), value: String(i.poolGeneration) },
    { label: t('instances.detail.fields.state'), value: t(`instances.states.${i.state}`) },
    { label: t('instances.detail.fields.stateReason'), value: i.stateReason ?? '' },
    { label: t('instances.detail.fields.stateSince'), value: date(i.stateSince) },
    { label: t('instances.detail.fields.host'), value: i.host, mono: true },
    { label: t('instances.detail.fields.address'), value: i.address ?? '', mono: true },
    { label: t('instances.detail.fields.players'), value: `${i.players} / ${i.maxPlayers}` },
    { label: t('instances.detail.fields.lastHeartbeat'), value: date(i.lastHeartbeat) },
    { label: t('instances.detail.fields.allocatedAt'), value: date(i.allocatedAt) },
    { label: t('instances.detail.fields.createdAt'), value: date(i.createdAt) },
    { label: t('instances.detail.fields.driverRef'), value: i.driverRef ?? '', mono: true },
    { label: t('instances.detail.fields.labels'), value: labels, mono: true },
  ]
})
</script>
