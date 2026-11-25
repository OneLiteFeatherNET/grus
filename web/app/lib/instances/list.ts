import type { Instance } from '~/lib/grus'
import type { GrusEvent } from '~/lib/events'

/** Payload of instance.state (internal/model InstanceStateData). Address and reason are only sent when set. */
export interface InstanceStateData {
  instance: string
  pool: string
  role: string
  from: string
  to: string
  address?: string
  reason?: string
}

export type SortKey = 'name' | 'age'
export type SortDir = 'asc' | 'desc'

export interface InstanceFilter {
  pool?: string
  state?: string
}

/**
 * Applies one instance.* event to a list of instances and returns the new list (the input is never mutated).
 * instance.created and instance.updated carry the full instance; instance.state carries only the transition, so
 * only state, stateSince, stateReason and an address it reports are changed. `at` is the time the event arrived.
 * Payloads that do not have the expected shape leave the list as it is.
 */
export function applyInstanceEvent(list: readonly Instance[], event: GrusEvent, at: string): Instance[] {
  switch (event.type) {
    case 'instance.created':
    case 'instance.updated': {
      const inst = asInstance(event.data)
      if (!inst) return [...list]
      return list.some(i => i.id === inst.id)
        ? list.map(i => (i.id === inst.id ? inst : i))
        : [...list, inst]
    }
    case 'instance.state': {
      const change = asStateData(event.data)
      if (!change) return [...list]
      return list.map(i => (i.id === change.instance ? withTransition(i, change, at) : i))
    }
    case 'instance.removed': {
      const id = isRecord(event.data) && typeof event.data.instance === 'string' ? event.data.instance : undefined
      return list.filter(i => i.id !== id)
    }
    default:
      return [...list]
  }
}

export function filterInstances(list: readonly Instance[], filter: InstanceFilter): Instance[] {
  return list.filter(i => (!filter.pool || i.pool === filter.pool) && (!filter.state || i.state === filter.state))
}

/**
 * Sorts by name (the instance id) or by age. Age is measured against `now`; ascending age puts the youngest first.
 * Ties fall back to the id, so the order is stable for equal keys.
 */
export function sortInstances(list: readonly Instance[], key: SortKey, dir: SortDir, now: number): Instance[] {
  const sign = dir === 'asc' ? 1 : -1
  return [...list].sort((a, b) => {
    const diff = key === 'age'
      ? ageMillis(a, now) - ageMillis(b, now)
      : a.id.localeCompare(b.id)
    return diff !== 0 ? sign * diff : a.id.localeCompare(b.id)
  })
}

const UNITS: ReadonlyArray<[Intl.RelativeTimeFormatUnit, number]> = [
  ['day', 86_400_000],
  ['hour', 3_600_000],
  ['minute', 60_000],
  ['second', 1000],
]

/** Relative age of a timestamp, e.g. "5 minutes ago" or "vor 5 Minuten". Future times count as zero. */
export function formatAge(createdAt: string, now: number, locale: string): string {
  const elapsed = Math.max(0, now - Date.parse(createdAt))
  const [unit, size] = UNITS.find(([, s]) => elapsed >= s) ?? ['second', 1000]
  const value = Math.floor(elapsed / size)
  return new Intl.RelativeTimeFormat(locale, { numeric: 'always' }).format(-value, unit)
}

/** Absolute timestamp for the detail view. Values that are not dates are shown unchanged. */
export function formatTimestamp(value: string, locale: string, timeZone?: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'medium', timeZone }).format(date)
}

/** Terminal states are final: nothing can be stopped there any more. Mirrors model.State.Terminal. */
export function isTerminal(state: string): boolean {
  return state === 'Stopped' || state === 'Failed' || state === 'Lost'
}

function ageMillis(inst: Instance, now: number): number {
  return now - Date.parse(inst.createdAt)
}

function withTransition(inst: Instance, change: InstanceStateData, at: string): Instance {
  return {
    ...inst,
    state: change.to,
    stateSince: at,
    stateReason: change.reason || undefined,
    address: change.address || inst.address,
  }
}

function asInstance(data: unknown): Instance | undefined {
  return isRecord(data) && typeof data.id === 'string' && typeof data.pool === 'string' && typeof data.state === 'string'
    ? (data as unknown as Instance)
    : undefined
}

/** Shape check for instance.state; exported so the history can reuse it. */
export function asStateData(data: unknown): InstanceStateData | undefined {
  return isRecord(data) && typeof data.instance === 'string' && typeof data.to === 'string'
    ? (data as unknown as InstanceStateData)
    : undefined
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}
