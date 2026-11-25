import { describe, expect, it } from 'vitest'
import type { GrusEvent } from '~/lib/events'
import { filterEvents, NO_FILTER, toggleType } from './filter'

const events: GrusEvent[] = [
  { id: '1-1', type: 'instance.state', data: { instance: 'lobby-ab12', to: 'Ready' } },
  { id: '1-2', type: 'pool.scaled', data: { pool: 'lobby', direction: 'up' } },
  { id: '1-3', type: 'instance.created', data: { id: 'bedwars-x1', pool: 'bedwars' } },
]

describe('filterEvents', () => {
  it('keeps everything without a filter', () => {
    expect(filterEvents(events, NO_FILTER)).toEqual(events)
  })

  it('keeps only the selected types', () => {
    const result = filterEvents(events, { types: ['pool.scaled', 'instance.created'], text: '' })
    expect(result.map(e => e.id)).toEqual(['1-2', '1-3'])
  })

  it('searches the type and the raw payload case-insensitively', () => {
    expect(filterEvents(events, { types: [], text: 'BEDWARS' }).map(e => e.id)).toEqual(['1-3'])
    expect(filterEvents(events, { types: [], text: 'scaled' }).map(e => e.id)).toEqual(['1-2'])
  })

  it('combines the type selection with the text search', () => {
    expect(filterEvents(events, { types: ['instance.state'], text: 'lobby' }).map(e => e.id)).toEqual(['1-1'])
  })

  it('ignores surrounding spaces in the search text', () => {
    expect(filterEvents(events, { types: [], text: '  ready  ' }).map(e => e.id)).toEqual(['1-1'])
  })

  it('does not change the input', () => {
    const copy = [...events]
    filterEvents(events, { types: ['pool.scaled'], text: '' })
    expect(events).toEqual(copy)
  })
})

describe('toggleType', () => {
  it('adds a type that is not selected yet', () => {
    expect(toggleType(['pool.scaled'], 'instance.state')).toEqual(['pool.scaled', 'instance.state'])
  })

  it('removes a type that is already selected', () => {
    expect(toggleType(['pool.scaled', 'instance.state'], 'pool.scaled')).toEqual(['instance.state'])
  })
})
