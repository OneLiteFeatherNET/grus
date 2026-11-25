import type { GrusEvent } from '~/lib/events'

export interface EventFilter {
  /** Event types to show. Empty means every type. */
  types: readonly string[]
  /** Case-insensitive text searched in the type and the raw payload (ids, pools, states). */
  text: string
}

export const NO_FILTER: EventFilter = { types: [], text: '' }

/** Keeps the events that match the type selection and the text search. The input order is kept. */
export function filterEvents<T extends GrusEvent>(events: readonly T[], filter: EventFilter): T[] {
  const needle = filter.text.trim().toLowerCase()
  return events.filter((event) => {
    if (filter.types.length > 0 && !filter.types.includes(event.type)) return false
    if (!needle) return true
    const haystack = `${event.type} ${JSON.stringify(event.data) ?? ''}`.toLowerCase()
    return haystack.includes(needle)
  })
}

/** Toggles one type in the selection. Keeps the list in the order the types were picked. */
export function toggleType(types: readonly string[], type: string): string[] {
  return types.includes(type) ? types.filter(t => t !== type) : [...types, type]
}
