import { describe, expect, it } from 'vitest'
import type { LoggedEvent } from './stream'
import { addEvent, clearLog, emptyLog, isPaused, LOG_LIMIT, pauseLog, resumeLog, visibleEvents } from './stream'

function logged(n: number): LoggedEvent {
  return { id: `1-${n}`, type: 'instance.state', data: {}, at: n }
}

function fill(count: number) {
  let log = emptyLog()
  for (let i = 1; i <= count; i++) log = addEvent(log, logged(i))
  return log
}

describe('addEvent', () => {
  it('puts the newest event first', () => {
    const log = fill(2)
    expect(visibleEvents(log).map(e => e.id)).toEqual(['1-2', '1-1'])
  })

  it('ignores an event with an id it already has', () => {
    const log = addEvent(fill(1), logged(1))
    expect(visibleEvents(log)).toHaveLength(1)
  })

  it('keeps at most the limit and drops the oldest', () => {
    const log = fill(LOG_LIMIT + 3)
    expect(visibleEvents(log)).toHaveLength(LOG_LIMIT)
    expect(visibleEvents(log)[0]?.id).toBe(`1-${LOG_LIMIT + 3}`)
  })
})

describe('pause and resume', () => {
  it('shows a frozen list while paused and counts the new events', () => {
    let log = pauseLog(fill(2))
    log = addEvent(log, logged(3))
    log = addEvent(log, logged(4))
    expect(isPaused(log)).toBe(true)
    expect(log.pending).toBe(2)
    expect(visibleEvents(log).map(e => e.id)).toEqual(['1-2', '1-1'])
  })

  it('shows the new events and resets the count on resume', () => {
    let log = pauseLog(fill(1))
    log = addEvent(log, logged(2))
    log = resumeLog(log)
    expect(isPaused(log)).toBe(false)
    expect(log.pending).toBe(0)
    expect(visibleEvents(log).map(e => e.id)).toEqual(['1-2', '1-1'])
  })

  it('does not count events that arrive while not paused', () => {
    expect(addEvent(fill(1), logged(2)).pending).toBe(0)
  })
})

describe('clearLog', () => {
  it('empties the list and keeps the running state', () => {
    const log = clearLog(fill(3))
    expect(visibleEvents(log)).toHaveLength(0)
    expect(isPaused(log)).toBe(false)
  })

  it('stays paused after clearing while paused', () => {
    const log = clearLog(pauseLog(fill(3)))
    expect(isPaused(log)).toBe(true)
    expect(visibleEvents(log)).toHaveLength(0)
  })
})
