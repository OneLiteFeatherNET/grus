import type { Ref } from 'vue'
import { appendEvent, createEventHub, EVENT_TYPES, parseEvent, RESYNC_EVENT } from '~/lib/events'
import type { GrusEvent } from '~/lib/events'

export type ConnectionStatus = 'idle' | 'connecting' | 'open' | 'reconnecting' | 'closed'

interface FeedState {
  status: ConnectionStatus
  recent: GrusEvent[]
}

/** The stream the BFF passes through (same origin, no token in the browser). */
export const EVENTS_URL = '/api/grus/v1/events'
/** Pause before opening a new EventSource after the browser gave up on the old one (HTTP error, not a network blip). */
export const RECONNECT_DELAY_MS = 3000

// One EventSource per tab, shared by every component. Only ever created in the browser, never during SSR.
const hub = createEventHub()
let source: EventSource | undefined
let retryTimer: ReturnType<typeof setTimeout> | undefined

/**
 * Live Grus events for the current tab. The first call opens the single EventSource; later calls only subscribe.
 * The browser resumes after network drops with Last-Event-ID. When the server answers with a `resync` event, every
 * onResync handler runs and should reload its data. Handlers registered inside a component are removed on unmount.
 */
export function useGrusEvents() {
  const feed = useState<FeedState>('grus.events', () => ({ status: 'idle', recent: [] }))

  // Opened after hydration: connecting during setup would change the status text before the server HTML is hydrated.
  if (import.meta.client) onNuxtReady(() => connect(feed))

  function subscribe(register: () => () => void): () => void {
    const off = register()
    if (getCurrentScope()) onScopeDispose(off)
    return off
  }

  return {
    status: computed(() => feed.value.status),
    recent: computed(() => feed.value.recent),
    /** Runs `handler` for events of `types` (exact names or `prefix.*`). Omit `types` to receive everything. */
    onEvent: (types: readonly string[] | undefined, handler: (event: GrusEvent) => void) =>
      subscribe(() => (import.meta.client ? hub.onEvent(types, handler) : () => {})),
    /** Runs `handler` when the server asks for a full reload (resync). Call it from the page's refresh function. */
    onResync: (handler: () => void) =>
      subscribe(() => (import.meta.client ? hub.onResync(handler) : () => {})),
  }
}

function connect(feed: Ref<FeedState>) {
  if (source || typeof EventSource === 'undefined') return
  open(feed)
}

function open(feed: Ref<FeedState>) {
  feed.value.status = feed.value.status === 'idle' ? 'connecting' : 'reconnecting'
  const es = new EventSource(EVENTS_URL)
  source = es

  es.onopen = () => {
    feed.value.status = 'open'
  }

  for (const type of EVENT_TYPES) {
    es.addEventListener(type, (message: Event) => {
      const parsed = parseEvent(type, (message as MessageEvent<string>).lastEventId, (message as MessageEvent<string>).data)
      if (!parsed) return
      feed.value.recent = appendEvent(feed.value.recent, parsed)
      hub.dispatch(parsed)
    })
  }

  es.addEventListener(RESYNC_EVENT, (message: Event) => {
    hub.dispatch({ id: (message as MessageEvent<string>).lastEventId, type: RESYNC_EVENT, data: null })
  })

  es.onerror = () => {
    // The browser retries by itself while readyState is CONNECTING. CLOSED means it gave up (for example a 401
    // from the BFF), so we retry on our own after a pause and resync, because events may have been missed.
    if (es.readyState !== EventSource.CLOSED) {
      feed.value.status = 'reconnecting'
      return
    }
    es.close()
    source = undefined
    feed.value.status = 'closed'
    clearTimeout(retryTimer)
    retryTimer = setTimeout(() => {
      open(feed)
      hub.dispatch({ id: '', type: RESYNC_EVENT, data: null })
    }, RECONNECT_DELAY_MS)
  }
}
