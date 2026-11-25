import { describe, expect, it } from 'vitest'
import { createCoalescer } from './coalesce'

/** A scheduler that only runs timers when the test says so: no wall clock, no sleeps. */
function fakeScheduler() {
  let nextId = 1
  const timers = new Map<number, () => void>()
  return {
    schedule: (run: () => void, _delayMs: number) => {
      const id = nextId++
      timers.set(id, run)
      return id
    },
    cancel: (id: unknown) => {
      timers.delete(id as number)
    },
    /** Fires every pending timer, as if the delay had elapsed. */
    fire() {
      const pending = [...timers.values()]
      timers.clear()
      for (const run of pending) run()
    },
    get pending() {
      return timers.size
    },
  }
}

describe('createCoalescer', () => {
  it('runs nothing until a request is made', () => {
    const clock = fakeScheduler()
    let runs = 0
    createCoalescer(() => runs++, { delayMs: 1000, ...clock })
    expect(clock.pending).toBe(0)
    expect(runs).toBe(0)
  })

  it('runs once for a burst of requests inside the window', () => {
    const clock = fakeScheduler()
    let runs = 0
    const c = createCoalescer(() => runs++, { delayMs: 1000, ...clock })
    c.request()
    c.request()
    c.request()
    expect(clock.pending).toBe(1)
    clock.fire()
    expect(runs, 'a burst of events must cause one refetch').toBe(1)
  })

  it('runs again for a request that arrives after the window ran', () => {
    const clock = fakeScheduler()
    let runs = 0
    const c = createCoalescer(() => runs++, { delayMs: 1000, ...clock })
    c.request()
    clock.fire()
    c.request()
    clock.fire()
    expect(runs).toBe(2)
  })

  it('passes the configured delay to the scheduler', () => {
    const delays: number[] = []
    const c = createCoalescer(() => undefined, {
      delayMs: 1000,
      schedule: (_run, delay) => {
        delays.push(delay)
        return 1
      },
      cancel: () => undefined,
    })
    c.request()
    expect(delays).toEqual([1000])
  })

  it('drops the pending run on cancel so an unmounted page does not refetch', () => {
    const clock = fakeScheduler()
    let runs = 0
    const c = createCoalescer(() => runs++, { delayMs: 1000, ...clock })
    c.request()
    c.cancel()
    expect(clock.pending).toBe(0)
    expect(runs).toBe(0)
  })
})
