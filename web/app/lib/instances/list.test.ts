import { describe, expect, it } from 'vitest'
import type { Instance } from '~/lib/grus'
import type { GrusEvent } from '~/lib/events'
import {
  applyInstanceEvent,
  filterInstances,
  formatAge,
  formatTimestamp,
  isTerminal,
  sortInstances,
} from './list'

const AT = '2026-11-25T10:00:00.000Z'
const NOW = Date.parse('2026-11-25T10:10:00.000Z')

function instance(overrides: Partial<Instance> = {}): Instance {
  return {
    id: 'lobby-a1',
    pool: 'lobby',
    role: 'game',
    poolGeneration: 1,
    host: '10.0.0.5',
    state: 'Ready',
    stateSince: '2026-11-25T09:59:00.000Z',
    players: 3,
    maxPlayers: 20,
    createdAt: '2026-11-25T09:50:00.000Z',
    ...overrides,
  }
}

function event(type: string, data: unknown, id = '1-1'): GrusEvent {
  return { id, type, data }
}

describe('applyInstanceEvent', () => {
  it('adds an instance on instance.created', () => {
    const next = applyInstanceEvent([], event('instance.created', instance()), AT)
    expect(next.map(i => i.id)).toEqual(['lobby-a1'])
  })

  it('replaces the whole instance on instance.updated', () => {
    const next = applyInstanceEvent([instance()], event('instance.updated', instance({ players: 9 })), AT)
    expect(next).toHaveLength(1)
    expect(next[0]?.players).toBe(9)
  })

  it('inserts an instance on instance.updated when the list does not have it yet', () => {
    const next = applyInstanceEvent([], event('instance.updated', instance()), AT)
    expect(next.map(i => i.id)).toEqual(['lobby-a1'])
  })

  it('applies the new state, address and reason on instance.state', () => {
    const data = { instance: 'lobby-a1', pool: 'lobby', role: 'game', from: 'Starting', to: 'Ready', address: '10.0.0.9:25565' }
    const [next] = applyInstanceEvent([instance({ state: 'Starting' })], event('instance.state', data), AT)
    expect(next?.state).toBe('Ready')
    expect(next?.stateSince).toBe(AT)
    expect(next?.address).toBe('10.0.0.9:25565')
    expect(next?.stateReason).toBeUndefined()
  })

  it('keeps the known address when a state event carries none', () => {
    const data = { instance: 'lobby-a1', pool: 'lobby', role: 'game', from: 'Ready', to: 'Draining' }
    const [next] = applyInstanceEvent([instance({ address: '10.0.0.5:25565' })], event('instance.state', data), AT)
    expect(next?.address).toBe('10.0.0.5:25565')
    expect(next?.state).toBe('Draining')
  })

  it('ignores a state event for an instance that is not in the list', () => {
    const data = { instance: 'other', pool: 'lobby', role: 'game', from: 'Ready', to: 'Stopped' }
    const list = [instance()]
    expect(applyInstanceEvent(list, event('instance.state', data), AT)).toEqual(list)
  })

  it('removes the instance on instance.removed', () => {
    const next = applyInstanceEvent([instance(), instance({ id: 'lobby-a2' })], event('instance.removed', { instance: 'lobby-a1', pool: 'lobby', role: 'game' }), AT)
    expect(next.map(i => i.id)).toEqual(['lobby-a2'])
  })

  it('ignores payloads that do not have the expected shape', () => {
    const list = [instance()]
    expect(applyInstanceEvent(list, event('instance.created', 'garbage'), AT)).toEqual(list)
    expect(applyInstanceEvent(list, event('instance.state', null), AT)).toEqual(list)
    expect(applyInstanceEvent(list, event('instance.removed', {}), AT)).toEqual(list)
  })

  it('does not change the input list', () => {
    const list = [instance()]
    applyInstanceEvent(list, event('instance.removed', { instance: 'lobby-a1' }), AT)
    expect(list).toHaveLength(1)
  })
})

describe('filterInstances', () => {
  const list = [
    instance({ id: 'a', pool: 'lobby', state: 'Ready' }),
    instance({ id: 'b', pool: 'lobby', state: 'Failed' }),
    instance({ id: 'c', pool: 'survival', state: 'Ready' }),
  ]

  it('keeps everything without a filter', () => {
    expect(filterInstances(list, {})).toHaveLength(3)
  })

  it('filters by pool and state together', () => {
    expect(filterInstances(list, { pool: 'lobby', state: 'Ready' }).map(i => i.id)).toEqual(['a'])
  })

  it('treats empty filter values as no filter', () => {
    expect(filterInstances(list, { pool: '', state: '' })).toHaveLength(3)
  })
})

describe('sortInstances', () => {
  const list = [
    instance({ id: 'b', createdAt: '2026-11-25T09:50:00.000Z' }),
    instance({ id: 'a', createdAt: '2026-11-25T09:00:00.000Z' }),
    instance({ id: 'c', createdAt: '2026-11-25T09:59:00.000Z' }),
  ]

  it('sorts by name ascending and descending', () => {
    expect(sortInstances(list, 'name', 'asc', NOW).map(i => i.id)).toEqual(['a', 'b', 'c'])
    expect(sortInstances(list, 'name', 'desc', NOW).map(i => i.id)).toEqual(['c', 'b', 'a'])
  })

  it('sorts by age: ascending puts the youngest first', () => {
    expect(sortInstances(list, 'age', 'asc', NOW).map(i => i.id)).toEqual(['c', 'b', 'a'])
    expect(sortInstances(list, 'age', 'desc', NOW).map(i => i.id)).toEqual(['a', 'b', 'c'])
  })

  it('does not mutate the input', () => {
    sortInstances(list, 'name', 'desc', NOW)
    expect(list.map(i => i.id)).toEqual(['b', 'a', 'c'])
  })
})

describe('formatAge', () => {
  const createdAt = '2026-11-25T10:00:00.000Z'

  it('uses the largest fitting unit', () => {
    expect(formatAge(createdAt, Date.parse('2026-11-25T10:00:30.000Z'), 'en')).toBe('30 seconds ago')
    expect(formatAge(createdAt, Date.parse('2026-11-25T10:05:00.000Z'), 'en')).toBe('5 minutes ago')
    expect(formatAge(createdAt, Date.parse('2026-11-25T13:00:00.000Z'), 'en')).toBe('3 hours ago')
    expect(formatAge(createdAt, Date.parse('2026-11-27T10:00:00.000Z'), 'en')).toBe('2 days ago')
  })

  it('follows the locale', () => {
    expect(formatAge(createdAt, Date.parse('2026-11-25T10:05:00.000Z'), 'de')).toBe('vor 5 Minuten')
  })

  it('clamps a creation time in the future to zero', () => {
    expect(formatAge(createdAt, Date.parse('2026-11-25T09:59:00.000Z'), 'en')).toBe('0 seconds ago')
  })
})

describe('formatTimestamp', () => {
  it('renders an ISO timestamp for the given locale and time zone', () => {
    expect(formatTimestamp('2026-11-25T10:00:00.000Z', 'en', 'UTC')).toMatch(/Nov 25, 2026/)
  })

  it('returns the raw value when it is not a date', () => {
    expect(formatTimestamp('not a date', 'en', 'UTC')).toBe('not a date')
  })
})

describe('isTerminal', () => {
  it('is true for Stopped, Failed and Lost only', () => {
    expect(['Stopped', 'Failed', 'Lost'].map(isTerminal)).toEqual([true, true, true])
    expect(['Ready', 'Draining', 'Stopping'].map(isTerminal)).toEqual([false, false, false])
  })
})
