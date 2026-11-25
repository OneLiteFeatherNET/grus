import { describe, expect, it } from 'vitest'
import { formatClock } from './clock'

describe('formatClock', () => {
  it('shows hours, minutes and seconds in the given timezone', () => {
    const at = Date.parse('2026-11-25T08:05:09Z')
    expect(formatClock(at, 'en-GB', 'UTC')).toBe('08:05:09')
  })
})
