import { describe, expect, it } from 'vitest'
import type { Scheduler } from './refresh'
import { createCoalescedRunner } from './refresh'

/** Manual clock and timer queue: the test decides when time passes, nothing waits for real. */
function fakeScheduler() {
  let now = 0
  const timers: { at: number, run: () => void, cancelled: boolean }[] = []
  const scheduler: Scheduler = {
    now: () => now,
    setTimeout: (run, ms) => {
      const timer = { at: now + ms, run, cancelled: false }
      timers.push(timer)
      return timer
    },
    clearTimeout: (handle) => {
      (handle as { cancelled: boolean }).cancelled = true
    },
  }
  /** Moves the clock forward, running each timer that is due at its own due time, in order. */
  function advance(ms: number) {
    const target = now + ms
    for (;;) {
      const due = timers.filter(t => !t.cancelled && t.at <= target).sort((a, b) => a.at - b.at)[0]
      if (!due) break
      now = due.at
      due.cancelled = true
      due.run()
    }
    now = target
  }
  return { scheduler, advance }
}

describe('createCoalescedRunner', () => {
  it('runs the first request at once', () => {
    const { scheduler } = fakeScheduler()
    let runs = 0
    const runner = createCoalescedRunner(() => runs++, { scheduler })
    runner.request()
    expect(runs).toBe(1)
  })

  it('merges a burst of requests into one trailing run', () => {
    const { scheduler, advance } = fakeScheduler()
    let runs = 0
    const runner = createCoalescedRunner(() => runs++, { scheduler, intervalMs: 1000 })
    runner.request()
    advance(0)
    for (let i = 0; i < 20; i++) runner.request()
    expect(runs).toBe(1)
    advance(1000)
    expect(runs).toBe(2)
  })

  it('never runs twice within the interval', () => {
    const { scheduler, advance } = fakeScheduler()
    const stamps: number[] = []
    const runner = createCoalescedRunner(() => stamps.push(scheduler.now()), { scheduler, intervalMs: 1000 })
    runner.request()
    advance(300)
    runner.request()
    advance(300)
    runner.request()
    advance(1000)
    expect(stamps).toEqual([0, 1000])
  })

  it('runs again straight away when the interval has passed', () => {
    const { scheduler, advance } = fakeScheduler()
    let runs = 0
    const runner = createCoalescedRunner(() => runs++, { scheduler, intervalMs: 1000 })
    runner.request()
    advance(5000)
    runner.request()
    expect(runs).toBe(2)
  })

  it('does not run a cancelled request', () => {
    const { scheduler, advance } = fakeScheduler()
    let runs = 0
    const runner = createCoalescedRunner(() => runs++, { scheduler, intervalMs: 1000 })
    runner.request()
    runner.request()
    runner.cancel()
    advance(5000)
    expect(runs).toBe(1)
  })
})
