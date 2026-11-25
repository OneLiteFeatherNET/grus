import type { PoolSpec } from '~/lib/grus'
import { isServing } from '~/lib/states'

export interface PoolCounts {
  /** Instances that serve players (Ready or Allocated). */
  ready: number
  /** All instances of the pool in the list. */
  total: number
}

/** Ready and total instance counts of one pool, from an instance list. */
export function poolCounts(instances: readonly { pool: string, state: string }[], pool: string): PoolCounts {
  const own = instances.filter(i => i.pool === pool)
  return { ready: own.filter(i => isServing(i.state)).length, total: own.length }
}

export type AgeUnit = 's' | 'm' | 'h' | 'd'

/** Age of a timestamp as one unit and a whole number, for the locale text ("pools.age.<unit>"). Never negative. */
export function ageOf(iso: string, now: number): { unit: AgeUnit, value: number } {
  const seconds = Math.max(0, Math.floor((now - Date.parse(iso)) / 1000))
  if (seconds < 60) return { unit: 's', value: seconds }
  if (seconds < 3600) return { unit: 'm', value: Math.floor(seconds / 60) }
  if (seconds < 86400) return { unit: 'h', value: Math.floor(seconds / 3600) }
  return { unit: 'd', value: Math.floor(seconds / 86400) }
}

/** One row of the read-only spec view. `value` is null when the spec leaves the field unset. */
export interface SpecRow {
  key: string
  value: string | null
}

export interface SpecSection {
  key: 'basics' | 'run' | 'resources' | 'templates' | 'scaling' | 'lifecycle' | 'labels'
  rows: SpecRow[]
}

/** Sections of the pool spec as text rows for display, in the order the spec documents them. Never includes secret values. */
export function specSections(spec: PoolSpec): SpecSection[] {
  const row = (key: string, value: string | number | undefined | null | readonly string[]): SpecRow => ({
    key,
    value: formatValue(value),
  })
  const pairs = (map: Record<string, string> | undefined) =>
    map && Object.keys(map).length > 0 ? Object.entries(map).map(([k, v]) => `${k}=${v}`).join(', ') : undefined
  const scaling = spec.scaling ?? { min: 0, max: 0 }
  const lifecycle = spec.lifecycle ?? {}

  return [
    {
      key: 'basics',
      rows: [
        row('role', spec.role),
        row('mode', spec.mode),
        row('driver', spec.driver),
        row('image', spec.image),
        row('port', spec.port),
        row('priorityClassName', spec.priorityClassName),
      ],
    },
    {
      key: 'run',
      rows: [
        row('command', spec.command?.length ? spec.command : undefined),
        row('args', spec.args?.length ? spec.args : undefined),
        row('env', pairs(spec.env)),
        row('secretEnv', spec.secretEnv?.length
          ? spec.secretEnv.map(s => `${s.name} (${s.secret}/${s.key})`).join(', ')
          : undefined),
      ],
    },
    {
      key: 'resources',
      rows: [row('memoryMiB', spec.resources?.memoryMiB), row('cpu', spec.resources?.cpu)],
    },
    {
      key: 'templates',
      rows: [row('templates', spec.templates?.length
        ? spec.templates.map(t => `${t.name}${t.digest ? `@${t.digest}` : `:${t.tag ?? ''}`}`).join(', ')
        : undefined)],
    },
    {
      key: 'scaling',
      rows: [
        row('min', scaling.min),
        row('max', scaling.max),
        row('buffer', scaling.buffer),
        row('capacity', scaling.capacity),
        row('targetUtilization', scaling.targetUtilization),
        row('scaleDownDelay', scaling.scaleDownDelay),
        row('drainTimeout', scaling.drainTimeout),
        row('maxSurge', scaling.maxSurge),
      ],
    },
    {
      key: 'lifecycle',
      rows: [
        row('stopCommand', lifecycle.stopCommand),
        row('gracePeriod', lifecycle.gracePeriod),
        row('heartbeatTimeout', lifecycle.heartbeatTimeout),
        row('startTimeout', lifecycle.startTimeout),
      ],
    },
    {
      key: 'labels',
      rows: [row('labels', pairs(spec.labels))],
    },
  ]
}

function formatValue(value: string | number | undefined | null | readonly string[]): string | null {
  if (value === undefined || value === null || value === '') return null
  if (Array.isArray(value)) return value.join(' ')
  return String(value)
}

/** Page path of one pool; the name is encoded so names with odd characters stay one path segment. */
export function poolPath(name: string): string {
  return `/pools/${encodeURIComponent(name)}`
}
