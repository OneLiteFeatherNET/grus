import { describe, expect, it } from 'vitest'
import { appendLogLine, isAtBottom, LOG_CAPACITY } from './logs'
import { normalizeCommand } from './command'

describe('appendLogLine', () => {
  it('appends a line', () => {
    expect(appendLogLine(['a'], 'b')).toEqual(['a', 'b'])
  })

  it('drops the oldest lines beyond the capacity', () => {
    const lines = appendLogLine(['a', 'b', 'c'], 'd', 3)
    expect(lines).toEqual(['b', 'c', 'd'])
  })

  it('keeps the default capacity at 1000 lines', () => {
    let lines: string[] = []
    for (let i = 0; i < LOG_CAPACITY + 10; i++) lines = appendLogLine(lines, `line ${i}`)
    expect(lines).toHaveLength(LOG_CAPACITY)
    expect(lines[0]).toBe('line 10')
    expect(lines.at(-1)).toBe(`line ${LOG_CAPACITY + 9}`)
  })

  it('does not change the input', () => {
    const input = ['a']
    appendLogLine(input, 'b')
    expect(input).toEqual(['a'])
  })
})

describe('isAtBottom', () => {
  it('is true when the view shows the end', () => {
    expect(isAtBottom(900, 100, 1000)).toBe(true)
  })

  it('tolerates a few pixels of slack', () => {
    expect(isAtBottom(890, 100, 1000)).toBe(true)
  })

  it('is false once the user scrolled up', () => {
    expect(isAtBottom(500, 100, 1000)).toBe(false)
  })

  it('is true for content that does not overflow', () => {
    expect(isAtBottom(0, 300, 300)).toBe(true)
  })
})

describe('normalizeCommand', () => {
  it('trims the command', () => {
    expect(normalizeCommand('  say hi  ')).toBe('say hi')
  })

  it('rejects an empty command', () => {
    expect(normalizeCommand('   ')).toBeNull()
  })

  it('rejects line breaks, which the controller refuses', () => {
    expect(normalizeCommand('say a\nsay b')).toBeNull()
    expect(normalizeCommand('say a\rsay b')).toBeNull()
  })
})
