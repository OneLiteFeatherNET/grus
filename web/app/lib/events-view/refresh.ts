/** Time source and timers, injected so the coalescing is testable without waiting. */
export interface Scheduler {
  now: () => number
  setTimeout: (run: () => void, ms: number) => unknown
  clearTimeout: (handle: unknown) => void
}

export const systemScheduler: Scheduler = {
  now: () => Date.now(),
  setTimeout: (run, ms) => setTimeout(run, ms),
  clearTimeout: handle => clearTimeout(handle as ReturnType<typeof setTimeout>),
}

export interface CoalescedRunner {
  /** Asks for a run. Requests arriving while one is already scheduled are merged into it. */
  request: () => void
  /** Drops a scheduled run, e.g. when the page is left. */
  cancel: () => void
}

/**
 * Runs `run` at most once per `intervalMs`. The first request runs at once; requests inside the interval wait for
 * the remaining time and merge into one run (a trailing run, never a lost one).
 */
export function createCoalescedRunner(
  run: () => void,
  options: { intervalMs?: number, scheduler?: Scheduler } = {},
): CoalescedRunner {
  const intervalMs = options.intervalMs ?? 1000
  const scheduler = options.scheduler ?? systemScheduler
  let scheduled = false
  let handle: unknown
  let lastRun: number | undefined

  function fire() {
    scheduled = false
    lastRun = scheduler.now()
    run()
  }

  return {
    request() {
      if (scheduled) return
      const wait = lastRun === undefined ? 0 : Math.max(0, lastRun + intervalMs - scheduler.now())
      if (wait === 0) {
        fire()
        return
      }
      scheduled = true
      handle = scheduler.setTimeout(fire, wait)
    },
    cancel() {
      if (scheduled) scheduler.clearTimeout(handle)
      scheduled = false
    },
  }
}
