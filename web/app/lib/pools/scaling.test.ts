import { describe, expect, it } from 'vitest'
import type { PoolSpec } from '~/lib/grus'
import {
  applyScaling,
  isDurationText,
  rebaseForm,
  sameForm,
  toScalingForm,
  validateScaling,
} from './scaling'
import type { Scaling, ScalingForm } from './scaling'

const SCALING: Scaling = {
  min: 2,
  max: 10,
  buffer: 1,
  capacity: 40,
  targetUtilization: 0.8,
  scaleDownDelay: '5m0s',
  drainTimeout: '1m0s',
  maxSurge: 2,
}

function form(overrides: Partial<ScalingForm> = {}): ScalingForm {
  return { ...toScalingForm(SCALING), ...overrides }
}

describe('isDurationText', () => {
  it.each(['30s', '5m0s', '1h30m', '1.5h', '250ms', '0', '2h45m0.5s'])('accepts the Go duration %s', (value) => {
    expect(isDurationText(value)).toBe(true)
  })

  it.each(['', '5', '5 min', '-1s', '+5s', 'ten', '5d', '1h-30m'])('rejects %j', (value) => {
    expect(isDurationText(value)).toBe(false)
  })
})

describe('toScalingForm', () => {
  it('turns every field into editable text and keeps the server duration text', () => {
    expect(toScalingForm(SCALING)).toEqual({
      min: '2',
      max: '10',
      buffer: '1',
      capacity: '40',
      targetUtilization: '0.8',
      scaleDownDelay: '5m0s',
      drainTimeout: '1m0s',
      maxSurge: '2',
    })
  })

  it('leaves optional fields empty when the server did not send them', () => {
    const f = toScalingForm({ min: 0, max: 0 })
    expect(f.capacity).toBe('')
    expect(f.scaleDownDelay).toBe('')
    expect(f.targetUtilization).toBe('')
  })
})

describe('validateScaling', () => {
  it('accepts the form it was built from', () => {
    const result = validateScaling(form(), { mode: 'dynamic' })
    expect(result.errors).toEqual({})
    expect(result.scaling).toEqual(SCALING)
  })

  it('requires min and max', () => {
    const result = validateScaling(form({ min: '', max: '' }), { mode: 'dynamic' })
    expect(result.errors).toEqual({ min: 'required', max: 'required' })
    expect(result.scaling).toBeUndefined()
  })

  it('rejects a negative or fractional min', () => {
    expect(validateScaling(form({ min: '-1' }), { mode: 'dynamic' }).errors.min).toBe('integer')
    expect(validateScaling(form({ min: '1.5' }), { mode: 'dynamic' }).errors.min).toBe('integer')
  })

  it('rejects max below min and reports it on max', () => {
    const result = validateScaling(form({ min: '5', max: '4' }), { mode: 'dynamic' })
    expect(result.errors).toEqual({ max: 'maxBelowMin' })
  })

  it('accepts max equal to min', () => {
    const result = validateScaling(form({ min: '3', max: '3' }), { mode: 'dynamic' })
    expect(result.errors).toEqual({})
  })

  it('rejects a negative buffer', () => {
    expect(validateScaling(form({ buffer: '-2' }), { mode: 'dynamic' }).errors.buffer).toBe('integer')
  })

  it('requires a zero buffer for static pools, as the server does', () => {
    expect(validateScaling(form({ buffer: '1' }), { mode: 'static' }).errors.buffer).toBe('staticBuffer')
    expect(validateScaling(form({ buffer: '0' }), { mode: 'static' }).errors).toEqual({})
  })

  it('keeps targetUtilization in the half-open range (0, 1]', () => {
    expect(validateScaling(form({ targetUtilization: '0' }), { mode: 'dynamic' }).errors.targetUtilization).toBe('range')
    expect(validateScaling(form({ targetUtilization: '1.2' }), { mode: 'dynamic' }).errors.targetUtilization).toBe('range')
    expect(validateScaling(form({ targetUtilization: '1' }), { mode: 'dynamic' }).errors).toEqual({})
    expect(validateScaling(form({ targetUtilization: 'high' }), { mode: 'dynamic' }).errors.targetUtilization).toBe('number')
  })

  it('checks the duration fields in Go syntax', () => {
    const result = validateScaling(form({ scaleDownDelay: '5 minutes', drainTimeout: '-1s' }), { mode: 'dynamic' })
    expect(result.errors).toEqual({ scaleDownDelay: 'duration', drainTimeout: 'duration' })
  })

  it('treats empty optional fields as unset instead of as errors', () => {
    const result = validateScaling(form({ capacity: '', scaleDownDelay: '', drainTimeout: '', maxSurge: '' }), { mode: 'dynamic' })
    expect(result.errors).toEqual({})
    expect(result.scaling).toEqual({ min: 2, max: 10, buffer: 1, targetUtilization: 0.8 })
  })
})

describe('applyScaling', () => {
  const spec: PoolSpec = {
    $schema: 'https://example.com/schemas/PoolSpec.json',
    role: 'game',
    mode: 'dynamic',
    driver: 'kubernetes',
    image: 'registry.example/lobby:1',
    resources: { memoryMiB: 1024, cpu: '1' },
    scaling: { min: 1, max: 3 },
    labels: { tier: 'lobby' },
    templates: [{ name: 'base', tag: 'latest' }],
  }

  it('replaces only the scaling part and keeps everything else of the full spec', () => {
    const next = applyScaling(spec, { min: 2, max: 5 })
    expect(next).toEqual({
      role: 'game',
      mode: 'dynamic',
      driver: 'kubernetes',
      image: 'registry.example/lobby:1',
      resources: { memoryMiB: 1024, cpu: '1' },
      labels: { tier: 'lobby' },
      templates: [{ name: 'base', tag: 'latest' }],
      scaling: { min: 2, max: 5 },
    })
  })

  it('does not send the read-only $schema back', () => {
    expect(applyScaling(spec, { min: 1, max: 1 })).not.toHaveProperty('$schema')
  })

  it('does not mutate the spec it was given', () => {
    applyScaling(spec, { min: 9, max: 9 })
    expect(spec.scaling).toEqual({ min: 1, max: 3 })
    expect(spec.$schema).toBeDefined()
  })
})

describe('sameForm and rebaseForm', () => {
  it('compares every field', () => {
    expect(sameForm(form(), form())).toBe(true)
    expect(sameForm(form(), form({ maxSurge: '3' }))).toBe(false)
  })

  it('takes the new server values when the user has not edited the form', () => {
    const before = toScalingForm(SCALING)
    const after = toScalingForm({ ...SCALING, max: 12 })
    expect(rebaseForm(before, before, after)).toEqual(after)
  })

  it('keeps the user edits when the server changes underneath', () => {
    const before = toScalingForm(SCALING)
    const edited = form({ max: '7' })
    const after = toScalingForm({ ...SCALING, max: 12 })
    expect(rebaseForm(edited, before, after)).toEqual(edited)
  })
})
