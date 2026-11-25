import type { InstanceFilter, SortDir, SortKey } from './list'

/** The list view state that lives in the URL query, so a filtered view can be shared and survives a reload. */
export interface InstanceListQuery extends InstanceFilter {
  sort: SortKey
  dir: SortDir
}

const SORT_KEYS: readonly SortKey[] = ['name', 'age']
const SORT_DIRS: readonly SortDir[] = ['asc', 'desc']

/** Reads the list state from route query values. Unknown sort keys fall back to the default; repeats use the first value. */
export function parseInstanceQuery(query: Record<string, unknown>): InstanceListQuery {
  const pool = firstString(query.pool)
  const state = firstString(query.state)
  const sort = firstString(query.sort) as SortKey
  const dir = firstString(query.dir) as SortDir
  return {
    ...(pool ? { pool } : {}),
    ...(state ? { state } : {}),
    sort: SORT_KEYS.includes(sort) ? sort : 'name',
    dir: SORT_DIRS.includes(dir) ? dir : 'asc',
  }
}

/** Writes the list state back to the URL. Defaults and empty filters are left out to keep the URL short. */
export function toInstanceRouteQuery(query: InstanceListQuery): Record<string, string> {
  const out: Record<string, string> = {}
  if (query.pool) out.pool = query.pool
  if (query.state) out.state = query.state
  if (query.sort !== 'name') out.sort = query.sort
  if (query.dir !== 'asc') out.dir = query.dir
  return out
}

function firstString(value: unknown): string {
  if (typeof value === 'string') return value
  if (Array.isArray(value) && typeof value[0] === 'string') return value[0]
  return ''
}
