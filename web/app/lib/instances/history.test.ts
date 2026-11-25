import { describe, expect, it } from 'vitest'
import type { GrusEvent } from '~/lib/events'
import { appendStateChange, HISTORY_LIMIT } from './history'

const AT = '2026-11-25T10:00:00.000Z'

function stateEvent(instance: string, from: string, to: string, reason?: string): GrusEvent {
  return { id: '1-1', type: 'instance.state', data: { instance, pool: 'lobby', role: 'game', from, to, reason } }
}

describe('appendStateChange', () => {
  it('records a state change of the watched instance, newest first', () => {
    const first = appendStateChange([], 'lobby-a1', stateEvent('lobby-a1', 'Preparing', 'Starting'), AT)
    const second = appendStateChange(first, 'lobby-a1', stateEvent('lobby-a1', 'Starting', 'Ready', 'healthy'), AT)
    expect(second).toEqual([
      { at: AT, from: 'Starting', to: 'Ready', reason: 'healthy' },
      { at: AT, from: 'Preparing', to: 'Starting', reason: undefined },
    ])
  })

  it('ignores state changes of other instances', () => {
    const history = [{ at: AT, from: 'Ready', to: 'Stopped' }]
    expect(appendStateChange(history, 'lobby-a1', stateEvent('lobby-a2', 'Ready', 'Stopped'), AT)).toEqual(history)
  })

  it('ignores events that are not instance.state', () => {
    const event: GrusEvent = { id: '1-2', type: 'instance.updated', data: { instance: 'lobby-a1' } }
    expect(appendStateChange([], 'lobby-a1', event, AT)).toEqual([])
  })

  it('ignores a malformed payload', () => {
    const event: GrusEvent = { id: '1-3', type: 'instance.state', data: { instance: 'lobby-a1' } }
    expect(appendStateChange([], 'lobby-a1', event, AT)).toEqual([])
  })

  it('keeps only the newest entries', () => {
    let history: ReturnType<typeof appendStateChange> = []
    for (let i = 0; i < HISTORY_LIMIT + 5; i++) {
      history = appendStateChange(history, 'lobby-a1', stateEvent('lobby-a1', 'Ready', `S${i}`), AT)
    }
    expect(history).toHaveLength(HISTORY_LIMIT)
    expect(history[0]?.to).toBe(`S${HISTORY_LIMIT + 4}`)
  })
})
