import type { GrusEvent } from '~/lib/events'

/** Events the events page keeps locally. Older ones are dropped. */
export const LOG_LIMIT = 500

export interface LoggedEvent extends GrusEvent {
  /** Epoch ms: the payload's own time when it has one, otherwise the receive time. */
  at: number
}

/**
 * The events page state. `events` is newest first and always receives new events. While paused `frozen` holds the
 * list as it was when the pause started, so the table does not move under the reader; `pending` counts what came in since.
 */
export interface LiveLog {
  events: LoggedEvent[]
  frozen: LoggedEvent[] | null
  pending: number
}

export function emptyLog(): LiveLog {
  return { events: [], frozen: null, pending: 0 }
}

export function isPaused(log: LiveLog): boolean {
  return log.frozen !== null
}

/** The list the table shows: the frozen snapshot while paused, otherwise the live list. */
export function visibleEvents(log: LiveLog): readonly LoggedEvent[] {
  return log.frozen ?? log.events
}

/** Adds one event (newest first). Duplicates by SSE id are ignored, e.g. replays after a reconnect. */
export function addEvent(log: LiveLog, event: LoggedEvent, limit = LOG_LIMIT): LiveLog {
  if (log.events.some(e => e.id === event.id)) return log
  return {
    events: [event, ...log.events].slice(0, limit),
    frozen: log.frozen,
    pending: log.frozen ? log.pending + 1 : log.pending,
  }
}

export function pauseLog(log: LiveLog): LiveLog {
  if (log.frozen) return log
  return { events: log.events, frozen: log.events, pending: 0 }
}

export function resumeLog(log: LiveLog): LiveLog {
  return { events: log.events, frozen: null, pending: 0 }
}

/** Empties the list. Pause state is kept: clearing while paused leaves an empty frozen list. */
export function clearLog(log: LiveLog): LiveLog {
  return { events: [], frozen: log.frozen ? [] : null, pending: 0 }
}
