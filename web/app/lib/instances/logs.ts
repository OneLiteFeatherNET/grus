/** Log lines kept in the browser; older lines drop off the top. */
export const LOG_CAPACITY = 1000

/** Appends a line and keeps only the newest `capacity` lines. Returns a new array. */
export function appendLogLine(lines: readonly string[], line: string, capacity = LOG_CAPACITY): string[] {
  const next = [...lines, line]
  return next.length > capacity ? next.slice(next.length - capacity) : next
}

/** Whether a scroll position shows the end of the log, within a few pixels of slack. Used to pause auto-scroll. */
export function isAtBottom(scrollTop: number, clientHeight: number, scrollHeight: number, slack = 16): boolean {
  return scrollHeight - scrollTop - clientHeight <= slack
}
