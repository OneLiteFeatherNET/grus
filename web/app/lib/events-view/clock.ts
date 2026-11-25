/** Source of "now" in epoch milliseconds. Injected so tests never read the system time. */
export interface Clock {
  now: () => number
}

export const systemClock: Clock = { now: () => Date.now() }

/** Clock time (HH:MM:SS) of an epoch-ms timestamp in the given locale. The timezone is optional, for tests. */
export function formatClock(epochMs: number, locale: string, timeZone?: string): string {
  return new Intl.DateTimeFormat(locale, { hour: '2-digit', minute: '2-digit', second: '2-digit', timeZone }).format(epochMs)
}
