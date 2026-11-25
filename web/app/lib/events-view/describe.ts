import type { GrusEvent } from '~/lib/events'
import type { ChipTone } from '~/lib/states'
import { instanceStateTone } from '~/lib/states'

/** Translates an i18n key (the events namespace) with named parameters. Injected so the formatter stays pure. */
export type Translate = (key: string, params?: Record<string, string | number>) => string

export interface EventDescription {
  /** Pool or instance the event is about, when the payload names one. */
  subject?: string
  /** What happened, without the subject. */
  summary: string
  /** One human-readable line for feeds, e.g. "lobby-ab12: Starting → Ready". */
  line: string
}

type Data = Record<string, unknown>

function asRecord(data: unknown): Data {
  return data !== null && typeof data === 'object' && !Array.isArray(data) ? data as Data : {}
}

function text(data: Data, key: string): string | undefined {
  const value = data[key]
  return typeof value === 'string' && value !== '' ? value : undefined
}

/** Strings and numbers as text; anything missing shows as "?" so a partial payload still reads. */
function value(data: Data, key: string): string {
  const v = data[key]
  if (typeof v === 'string' && v !== '') return v
  if (typeof v === 'number' && Number.isFinite(v)) return String(v)
  return '?'
}

function instanceLine(subject: string | undefined, summary: string, t: Translate): EventDescription {
  if (!subject) return { summary, line: summary }
  return { subject, summary, line: t('events.line.instance', { subject, summary }) }
}

function poolLine(pool: string | undefined, summary: string, t: Translate): EventDescription {
  if (!pool) return { summary, line: summary }
  return { subject: pool, summary, line: t('events.line.pool', { pool, summary }) }
}

/**
 * Human-readable description of one event. Unknown types or payloads fall back to the type name, so a newer
 * controller never breaks the feed.
 */
export function describeEvent(event: GrusEvent, t: Translate): EventDescription {
  const d = asRecord(event.data)
  switch (event.type) {
    case 'instance.state': {
      const from = value(d, 'from')
      const to = value(d, 'to')
      const reason = text(d, 'reason')
      const summary = reason
        ? t('events.summary.instanceStateReason', { from, to, reason })
        : t('events.summary.instanceState', { from, to })
      return instanceLine(text(d, 'instance'), summary, t)
    }
    case 'instance.created':
      return instanceLine(text(d, 'id'), t('events.summary.instanceCreated', { pool: value(d, 'pool') }), t)
    case 'instance.updated':
      return instanceLine(text(d, 'id'), t('events.summary.instanceUpdated', { state: value(d, 'state') }), t)
    case 'instance.removed':
      return instanceLine(text(d, 'instance'), t('events.summary.instanceRemoved', { pool: value(d, 'pool') }), t)
    case 'pool.updated': {
      const pool = text(d, 'pool')
      if (typeof d.maintenance === 'boolean') {
        const key = d.maintenance ? 'events.summary.poolMaintenanceOn' : 'events.summary.poolMaintenanceOff'
        return poolLine(pool, t(key), t)
      }
      return poolLine(pool, t('events.summary.poolUpdated', { generation: value(d, 'generation') }), t)
    }
    case 'pool.scaled': {
      const direction = text(d, 'direction')
      const params = { reason: value(d, 'reason'), current: value(d, 'current'), desired: value(d, 'desired') }
      if (direction === 'up') return poolLine(text(d, 'pool'), t('events.summary.poolScaledUp', params), t)
      if (direction === 'down') return poolLine(text(d, 'pool'), t('events.summary.poolScaledDown', params), t)
      return poolLine(text(d, 'pool'), t('events.summary.unknown', { type: event.type }), t)
    }
    case 'template.tagged': {
      const template = text(d, 'template')
      const tag = text(d, 'tag')
      if (!template || !tag) return { summary: t('events.summary.unknown', { type: event.type }), line: event.type }
      return { subject: template, summary: t('events.summary.templateTagged', { tag }), line: t('events.line.template', { template, tag }) }
    }
    default:
      return { summary: t('events.summary.unknown', { type: event.type }), line: event.type }
  }
}

/** Colour of the type chip. Instance state events take the colour of the state they moved to. */
export function eventTone(event: GrusEvent): ChipTone {
  const d = asRecord(event.data)
  switch (event.type) {
    case 'instance.state': return instanceStateTone(value(d, 'to'))
    case 'instance.created': return 'secondary'
    case 'instance.removed': return 'neutral'
    case 'instance.updated': return 'tertiary'
    case 'pool.updated': return 'primary'
    case 'pool.scaled': return 'tertiary'
    case 'template.tagged': return 'secondary'
    default: return 'neutral'
  }
}

/**
 * Time of an event in epoch ms. A time field in the payload wins (`at`, `time` or `timestamp` as ISO text);
 * otherwise the time the page received the event is used.
 */
export function eventTime(event: GrusEvent, receivedAt: number): number {
  const d = asRecord(event.data)
  for (const key of ['at', 'time', 'timestamp']) {
    const raw = text(d, key)
    if (raw === undefined) continue
    const parsed = Date.parse(raw)
    if (!Number.isNaN(parsed)) return parsed
  }
  return receivedAt
}
