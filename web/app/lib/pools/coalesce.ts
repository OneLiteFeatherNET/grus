/** Schedules and cancels a delayed call. Injected so the coalescing logic runs without wall-clock waits. */
export interface Scheduler {
  schedule: (run: () => void, delayMs: number) => unknown
  cancel: (handle: unknown) => void
}

export interface CoalescerOptions extends Partial<Scheduler> {
  /** Length of the window: requests inside one window share a single run. */
  delayMs: number
}

export interface Coalescer {
  /** Asks for a run. Several requests within one window cause exactly one run, at the end of the window. */
  request: () => void
  /** Drops a pending run (for example when the page is left). */
  cancel: () => void
}

/**
 * Trailing-edge throttle: the first request opens a window, later requests inside it are absorbed, and the run
 * happens once the window closes. Live events arrive in bursts (one pool change emits several instance events), so
 * this keeps a page from refetching on every single event.
 */
export function createCoalescer(run: () => void, options: CoalescerOptions): Coalescer {
  const schedule = options.schedule ?? ((fn: () => void, ms: number) => setTimeout(fn, ms))
  const cancelHandle = options.cancel ?? ((handle: unknown) => clearTimeout(handle as ReturnType<typeof setTimeout>))
  let handle: unknown
  let pending = false

  return {
    request() {
      if (pending) return
      pending = true
      handle = schedule(() => {
        pending = false
        handle = undefined
        run()
      }, options.delayMs)
    },
    cancel() {
      if (!pending) return
      cancelHandle(handle)
      pending = false
      handle = undefined
    },
  }
}

/** Live refreshes of the pool pages run at most once per second, however many events arrive in between. */
export const LIVE_REFRESH_DELAY_MS = 1000
