import type { GrusEvent } from '~/lib/events'
import { asStateData } from './list'

/** One state transition seen on the live stream while the page was open. */
export interface StateChange {
  at: string
  from: string
  to: string
  reason?: string
}

/** The history only covers the time since the page opened, so it stays short. */
export const HISTORY_LIMIT = 200

/**
 * Prepends the transition of `instance` to the history (newest first). Other instances and other event types leave
 * the history unchanged. `at` is the time the event arrived; the stream itself carries no timestamp.
 */
export function appendStateChange(history: readonly StateChange[], instance: string, event: GrusEvent, at: string): StateChange[] {
  if (event.type !== 'instance.state') return [...history]
  const change = asStateData(event.data)
  if (!change || change.instance !== instance) return [...history]
  const entry: StateChange = { at, from: change.from, to: change.to, reason: change.reason || undefined }
  return [entry, ...history].slice(0, HISTORY_LIMIT)
}
