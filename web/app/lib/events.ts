/**
 * Pure core of the live event stream: parsing, the bounded activity feed and the dispatch to handlers.
 * The browser wiring (EventSource) lives in composables/useGrusEvents.ts and only feeds this module.
 */

/** Event types the controller publishes on GET /v1/events. */
export const EVENT_TYPES = [
  'pool.updated',
  'pool.scaled',
  'instance.created',
  'instance.state',
  'instance.updated',
  'instance.removed',
  'template.tagged',
] as const

export const RESYNC_EVENT = 'resync'

export interface GrusEvent {
  /** SSE id ("<epoch>-<seq>"), used to drop duplicates. */
  id: string
  type: string
  data: unknown
}

/** Keeps the feed short: the live list shows the latest events only. */
export const FEED_LIMIT = 50

/** Parses one SSE message. Returns null when the payload is not JSON (never throws on bad input). */
export function parseEvent(type: string, id: string, raw: string): GrusEvent | null {
  try {
    return { id, type, data: JSON.parse(raw) as unknown }
  }
  catch {
    return null
  }
}

/** Adds an event to the front of the feed (newest first), ignoring duplicates and capping the length. */
export function appendEvent(feed: readonly GrusEvent[], event: GrusEvent, limit = FEED_LIMIT): GrusEvent[] {
  if (feed.some(e => e.id === event.id)) return [...feed]
  return [event, ...feed].slice(0, limit)
}

/** Matches an event type against a subscription. `undefined` subscribes to everything; `pool.*` matches a prefix. */
export function matchesTypes(type: string, patterns: readonly string[] | undefined): boolean {
  if (!patterns || patterns.length === 0) return true
  return patterns.some(p => (p.endsWith('.*') ? type.startsWith(p.slice(0, -1)) : type === p))
}

export interface EventHub {
  /** Hands one event to the matching subscribers, or runs the resync handlers for a `resync` event. */
  dispatch: (event: GrusEvent) => void
  /** Subscribes to events of the given types. Returns the unsubscribe function. */
  onEvent: (types: readonly string[] | undefined, handler: (event: GrusEvent) => void) => () => void
  /** Subscribes to resync requests (the server could not replay the stream, so state must be reloaded). */
  onResync: (handler: () => void) => () => void
}

export function createEventHub(): EventHub {
  const eventHandlers = new Set<{ types: readonly string[] | undefined, handler: (event: GrusEvent) => void }>()
  const resyncHandlers = new Set<() => void>()
  return {
    dispatch(event) {
      if (event.type === RESYNC_EVENT) {
        for (const handler of [...resyncHandlers]) handler()
        return
      }
      for (const sub of [...eventHandlers]) {
        if (matchesTypes(event.type, sub.types)) sub.handler(event)
      }
    },
    onEvent(types, handler) {
      const sub = { types, handler }
      eventHandlers.add(sub)
      return () => {
        eventHandlers.delete(sub)
      }
    },
    onResync(handler) {
      resyncHandlers.add(handler)
      return () => {
        resyncHandlers.delete(handler)
      }
    },
  }
}
