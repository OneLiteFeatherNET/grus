<template>
  <div>
    <PageHeader :title="$t('events.title')" :subtitle="$t('events.subtitle')">
      <template #actions>
        <span class="text-label-medium text-on-surface-variant" data-test="live-status">
          {{ $t(`common.live.${status}`) }}
        </span>
        <M3Button variant="tonal" icon="mdi-pause" data-test="pause" @click="togglePause">
          <template v-if="paused">
            {{ $t('events.resume') }}
            <span v-if="log.pending > 0" class="font-semibold" data-test="pending-count">
              {{ $t('events.pendingNew', { count: log.pending }) }}
            </span>
          </template>
          <template v-else>{{ $t('events.pause') }}</template>
        </M3Button>
        <M3Button variant="text" icon="mdi-delete-sweep-outline" data-test="clear" @click="clear">
          {{ $t('events.clear') }}
        </M3Button>
      </template>
    </PageHeader>

    <section class="mb-4 grid gap-3" :aria-label="$t('events.filterTypes')">
      <div class="flex flex-wrap gap-2" role="group" :aria-label="$t('events.filterTypes')">
        <button
          v-for="type in EVENT_TYPES"
          :key="type"
          type="button"
          :aria-pressed="typeFilter.includes(type)"
          :data-test="`type-${type}`"
          :class="[
            'state-layer rounded-sm border px-3 py-1 text-label-large',
            typeFilter.includes(type)
              ? 'border-primary bg-primary-container text-on-primary-container'
              : 'border-outline-variant text-on-surface-variant',
          ]"
          @click="typeFilter = toggleType(typeFilter, type)"
        >
          {{ type }}
        </button>
      </div>
      <label class="flex max-w-md flex-col gap-1 text-label-medium text-on-surface-variant">
        {{ $t('events.filterText') }}
        <input
          v-model="text"
          type="search"
          class="rounded-sm border border-outline-variant bg-surface px-3 py-2 text-body-large text-on-surface"
          :placeholder="$t('events.filterTextPlaceholder')"
          data-test="text-filter"
        >
      </label>
      <p class="text-label-medium text-on-surface-variant">{{ $t('events.limit', { limit: LOG_LIMIT }) }}</p>
    </section>

    <M3EmptyState v-if="!log.events.length" :title="$t('events.empty')" icon="mdi-timeline-text-outline" />
    <M3EmptyState v-else-if="!rows.length" :title="$t('events.noMatch')" icon="mdi-filter-off-outline" />
    <div v-else class="overflow-x-auto rounded-md bg-surface-container-low">
      <table class="w-full min-w-[40rem] text-left text-body-medium" data-test="events-table">
        <thead class="text-label-large text-on-surface-variant">
          <tr>
            <th scope="col" class="px-3 py-2 font-medium">{{ $t('events.columns.time') }}</th>
            <th scope="col" class="px-3 py-2 font-medium">{{ $t('events.columns.type') }}</th>
            <th scope="col" class="px-3 py-2 font-medium">{{ $t('events.columns.subject') }}</th>
            <th scope="col" class="px-3 py-2 font-medium">{{ $t('events.columns.summary') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="row in rows" :key="row.id" class="border-t border-outline-variant align-top" data-test="event-row">
            <td class="whitespace-nowrap px-3 py-2 tabular-nums">{{ row.time }}</td>
            <td class="px-3 py-2"><M3Chip :tone="row.tone" small>{{ row.type }}</M3Chip></td>
            <td class="break-all px-3 py-2">{{ row.subject ?? '—' }}</td>
            <td class="px-3 py-2">
              {{ row.summary }}
              <details class="mt-1">
                <summary class="cursor-pointer text-label-medium text-primary">{{ $t('events.showRaw') }}</summary>
                <pre class="mt-2 max-h-64 overflow-auto rounded-sm bg-surface p-2 text-label-medium">{{ row.raw }}</pre>
              </details>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<script setup lang="ts">
import { EVENT_TYPES } from '~/lib/events'
import type { GrusEvent } from '~/lib/events'
import { describeEvent, eventTime, eventTone } from '~/lib/events-view/describe'
import type { Translate } from '~/lib/events-view/describe'
import { formatClock, systemClock } from '~/lib/events-view/clock'
import { filterEvents, toggleType } from '~/lib/events-view/filter'
import type { EventFilter } from '~/lib/events-view/filter'
import type { LiveLog, LoggedEvent } from '~/lib/events-view/stream'
import { addEvent, clearLog, emptyLog, isPaused, LOG_LIMIT, pauseLog, resumeLog, visibleEvents } from '~/lib/events-view/stream'

// The live stream as a table. The page keeps its own list (up to LOG_LIMIT, newest first) fed by the shared
// connection; the shared feed of the overview is not touched.
const { t, locale } = useI18n()
const { status, onEvent } = useGrusEvents()
const log = ref<LiveLog>(emptyLog())
const typeFilter = ref<string[]>([])
const text = ref('')
const paused = computed(() => isPaused(log.value))

const filter = computed<EventFilter>(() => ({ types: typeFilter.value, text: text.value }))
const translate: Translate = (key, params) => t(key, params ?? {})

const rows = computed(() =>
  filterEvents(visibleEvents(log.value), filter.value).map((event: LoggedEvent) => {
    const description = describeEvent(event, translate)
    return {
      id: event.id,
      type: event.type,
      tone: eventTone(event),
      time: formatClock(event.at, locale.value),
      subject: description.subject,
      summary: description.summary,
      raw: JSON.stringify(event.data, null, 2),
    }
  }),
)

onEvent(undefined, (event: GrusEvent) => {
  log.value = addEvent(log.value, { ...event, at: eventTime(event, systemClock.now()) })
})

function togglePause() {
  log.value = paused.value ? resumeLog(log.value) : pauseLog(log.value)
}

function clear() {
  log.value = clearLog(log.value)
}
</script>
