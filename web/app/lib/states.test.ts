import { describe, expect, it } from 'vitest'
import { INSTANCE_STATES, instanceStateTone, isServing } from './states'

describe('instanceStateTone', () => {
  it('colours every known state', () => {
    for (const state of INSTANCE_STATES) {
      expect(instanceStateTone(state)).toBeDefined()
    }
  })

  it('shows healthy running states green and failures red', () => {
    expect(instanceStateTone('Ready')).toBe('success')
    expect(instanceStateTone('Failed')).toBe('error')
    expect(instanceStateTone('Lost')).toBe('error')
  })

  it('falls back to neutral for a state this UI does not know', () => {
    expect(instanceStateTone('Hibernating')).toBe('neutral')
  })
})

describe('isServing', () => {
  it('counts Ready and Allocated instances only', () => {
    expect(isServing('Ready')).toBe(true)
    expect(isServing('Allocated')).toBe(true)
    expect(isServing('Starting')).toBe(false)
    expect(isServing('Draining')).toBe(false)
  })
})
