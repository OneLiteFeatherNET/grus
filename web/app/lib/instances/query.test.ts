import { describe, expect, it } from 'vitest'
import { parseInstanceQuery, toInstanceRouteQuery } from './query'

describe('parseInstanceQuery', () => {
  it('defaults to name ascending without filters', () => {
    expect(parseInstanceQuery({})).toEqual({ sort: 'name', dir: 'asc' })
  })

  it('reads pool, state, sort and dir from the URL', () => {
    expect(parseInstanceQuery({ pool: 'lobby', state: 'Ready', sort: 'age', dir: 'desc' }))
      .toEqual({ pool: 'lobby', state: 'Ready', sort: 'age', dir: 'desc' })
  })

  it('falls back to the defaults for unknown sort keys and directions', () => {
    expect(parseInstanceQuery({ sort: 'players', dir: 'sideways' })).toEqual({ sort: 'name', dir: 'asc' })
  })

  it('takes the first value of a repeated parameter and ignores non-string values', () => {
    expect(parseInstanceQuery({ pool: ['lobby', 'survival'], state: null })).toEqual({ sort: 'name', dir: 'asc', pool: 'lobby' })
  })
})

describe('toInstanceRouteQuery', () => {
  it('omits empty filters and default sorting', () => {
    expect(toInstanceRouteQuery({ sort: 'name', dir: 'asc' })).toEqual({})
  })

  it('writes only the non-default values', () => {
    expect(toInstanceRouteQuery({ pool: 'lobby', sort: 'age', dir: 'asc', state: '' }))
      .toEqual({ pool: 'lobby', sort: 'age' })
  })

  it('round-trips through parseInstanceQuery', () => {
    const query = { pool: 'lobby', state: 'Failed', sort: 'age' as const, dir: 'desc' as const }
    expect(parseInstanceQuery(toInstanceRouteQuery(query))).toEqual(query)
  })
})
