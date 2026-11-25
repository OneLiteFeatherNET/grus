import { describe, expect, it, vi } from 'vitest'
import type { GrusEvent } from './events'
import { appendEvent, createEventHub, FEED_LIMIT, matchesTypes, parseEvent } from './events'

function event(id: string, type = 'instance.state', data: unknown = {}): GrusEvent {
  return { id, type, data }
}

describe('parseEvent', () => {
  it('parses the JSON payload and keeps the SSE id and type', () => {
    expect(parseEvent('pool.updated', '1-7', '{"pool":"lobby"}')).toEqual({
      id: '1-7',
      type: 'pool.updated',
      data: { pool: 'lobby' },
    })
  })

  it('returns null for a payload that is not JSON', () => {
    expect(parseEvent('pool.updated', '1-8', 'not json')).toBeNull()
  })
})

describe('appendEvent', () => {
  it('puts the newest event first', () => {
    const feed = appendEvent([event('1-1')], event('1-2'))
    expect(feed.map(e => e.id)).toEqual(['1-2', '1-1'])
  })

  it('ignores an event it already has (replay after a reconnect)', () => {
    const feed = appendEvent([event('1-1')], event('1-1'))
    expect(feed.map(e => e.id)).toEqual(['1-1'])
  })

  it('caps the feed at the limit, dropping the oldest', () => {
    let feed: GrusEvent[] = []
    for (let i = 0; i < FEED_LIMIT + 5; i++) feed = appendEvent(feed, event(`1-${i}`))
    expect(feed).toHaveLength(FEED_LIMIT)
    expect(feed[0]?.id).toBe(`1-${FEED_LIMIT + 4}`)
  })

  it('does not change the input array', () => {
    const feed = [event('1-1')]
    appendEvent(feed, event('1-2'))
    expect(feed.map(e => e.id)).toEqual(['1-1'])
  })
})

describe('matchesTypes', () => {
  it('matches everything without patterns', () => {
    expect(matchesTypes('pool.updated', undefined)).toBe(true)
    expect(matchesTypes('pool.updated', [])).toBe(true)
  })

  it('matches exact names and dotted prefixes', () => {
    expect(matchesTypes('instance.state', ['instance.state'])).toBe(true)
    expect(matchesTypes('instance.created', ['instance.*'])).toBe(true)
    expect(matchesTypes('pool.updated', ['instance.*'])).toBe(false)
    expect(matchesTypes('instance.created', ['instance.state'])).toBe(false)
  })
})

describe('createEventHub', () => {
  it('delivers an event only to subscribers of its type', () => {
    const hub = createEventHub()
    const pools = vi.fn()
    const instances = vi.fn()
    hub.onEvent(['pool.updated'], pools)
    hub.onEvent(['instance.*'], instances)

    hub.dispatch(event('1-1', 'instance.state'))

    expect(pools).not.toHaveBeenCalled()
    expect(instances).toHaveBeenCalledWith(event('1-1', 'instance.state'))
  })

  it('stops delivering after unsubscribe', () => {
    const hub = createEventHub()
    const handler = vi.fn()
    const off = hub.onEvent(undefined, handler)
    off()

    hub.dispatch(event('1-1'))

    expect(handler).not.toHaveBeenCalled()
  })

  it('runs resync handlers instead of event handlers on a resync event', () => {
    const hub = createEventHub()
    const handler = vi.fn()
    const resync = vi.fn()
    hub.onEvent(undefined, handler)
    hub.onResync(resync)

    hub.dispatch(event('1-9', 'resync', { reason: 'last-event-id cannot be replayed' }))

    expect(resync).toHaveBeenCalledOnce()
    expect(handler).not.toHaveBeenCalled()
  })
})
