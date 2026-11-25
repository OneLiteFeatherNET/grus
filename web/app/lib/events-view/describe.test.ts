import { describe, expect, it } from 'vitest'
import type { GrusEvent } from '~/lib/events'
import { describeEvent, eventTime, eventTone } from './describe'
import type { Translate } from './describe'

/** Fake translator: returns the key and its parameters, so the tests assert which message is chosen. */
const t: Translate = (key, params) => (params ? `${key}${JSON.stringify(params)}` : key)

function event(type: string, data: unknown): GrusEvent {
  return { id: '1-1', type, data }
}

describe('describeEvent', () => {
  it('names the instance and the state change for instance.state', () => {
    const d = describeEvent(event('instance.state', { instance: 'lobby-ab12', from: 'Starting', to: 'Ready' }), t)
    expect(d.subject).toBe('lobby-ab12')
    expect(d.summary).toBe('events.summary.instanceState{"from":"Starting","to":"Ready"}')
    expect(d.line).toBe('events.line.instance{"subject":"lobby-ab12","summary":"events.summary.instanceState{\\"from\\":\\"Starting\\",\\"to\\":\\"Ready\\"}"}')
  })

  it('adds the reason of a state change when there is one', () => {
    const d = describeEvent(event('instance.state', { instance: 'a', from: 'Ready', to: 'Lost', reason: 'heartbeat' }), t)
    expect(d.summary).toBe('events.summary.instanceStateReason{"from":"Ready","to":"Lost","reason":"heartbeat"}')
  })

  it('takes the instance id from an instance payload for instance.created', () => {
    const d = describeEvent(event('instance.created', { id: 'lobby-cd34', pool: 'lobby', state: 'Requested' }), t)
    expect(d.subject).toBe('lobby-cd34')
    expect(d.summary).toBe('events.summary.instanceCreated{"pool":"lobby"}')
  })

  it('describes a removed instance from the instance field', () => {
    const d = describeEvent(event('instance.removed', { instance: 'lobby-ef56', pool: 'lobby', role: 'lobby' }), t)
    expect(d.subject).toBe('lobby-ef56')
    expect(d.summary).toBe('events.summary.instanceRemoved{"pool":"lobby"}')
  })

  it('reads a scale-up as the pool line with reason and counts', () => {
    const d = describeEvent(event('pool.scaled', { pool: 'lobby', direction: 'up', reason: 'buffer', current: 2, desired: 3 }), t)
    expect(d.subject).toBe('lobby')
    expect(d.line).toBe('events.line.pool{"pool":"lobby","summary":"events.summary.poolScaledUp{\\"reason\\":\\"buffer\\",\\"current\\":\\"2\\",\\"desired\\":\\"3\\"}"}')
  })

  it('uses the down message for a scale-down', () => {
    const d = describeEvent(event('pool.scaled', { pool: 'lobby', direction: 'down', reason: 'load', current: 4, desired: 3 }), t)
    expect(d.summary).toContain('events.summary.poolScaledDown')
  })

  it('shows the maintenance switch for a pool.updated with a maintenance flag', () => {
    expect(describeEvent(event('pool.updated', { pool: 'lobby', generation: 5, maintenance: true }), t).summary)
      .toBe('events.summary.poolMaintenanceOn')
    expect(describeEvent(event('pool.updated', { pool: 'lobby', generation: 5, maintenance: false }), t).summary)
      .toBe('events.summary.poolMaintenanceOff')
  })

  it('shows the generation for a pool.updated without a maintenance flag', () => {
    expect(describeEvent(event('pool.updated', { pool: 'lobby', generation: 6 }), t).summary)
      .toBe('events.summary.poolUpdated{"generation":"6"}')
  })

  it('falls back to the type name for an unknown event type', () => {
    const d = describeEvent(event('future.thing', { anything: true }), t)
    expect(d.subject).toBeUndefined()
    expect(d.line).toBe('future.thing')
    expect(d.summary).toBe('events.summary.unknown{"type":"future.thing"}')
  })

  it('does not fail on a payload that is not an object', () => {
    const d = describeEvent(event('instance.state', 'broken'), t)
    expect(d.subject).toBeUndefined()
    expect(d.summary).toBe('events.summary.instanceState{"from":"?","to":"?"}')
  })
})

describe('eventTone', () => {
  it('colours an instance state event by the state it moved to', () => {
    expect(eventTone(event('instance.state', { to: 'Ready' }))).toBe('success')
    expect(eventTone(event('instance.state', { to: 'Failed' }))).toBe('error')
  })

  it('gives unknown types the neutral tone', () => {
    expect(eventTone(event('future.thing', {}))).toBe('neutral')
  })
})

describe('eventTime', () => {
  it('uses a time field from the payload when it is a valid date', () => {
    expect(eventTime(event('pool.updated', { at: '2026-11-25T10:00:00Z' }), 999)).toBe(Date.parse('2026-11-25T10:00:00Z'))
  })

  it('uses the receive time when the payload has no time field', () => {
    expect(eventTime(event('pool.updated', { pool: 'lobby' }), 42)).toBe(42)
  })

  it('uses the receive time when the time field is not a date', () => {
    expect(eventTime(event('pool.updated', { at: 'soon' }), 42)).toBe(42)
  })
})
