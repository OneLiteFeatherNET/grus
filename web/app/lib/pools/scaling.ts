import type { components } from '~/types/grus-api'
import type { PoolSpec } from '~/lib/grus'

export type Scaling = components['schemas']['Scaling']

/** The scaling fields as the form edits them: all text, so half-typed values survive re-renders. */
export interface ScalingForm {
  min: string
  max: string
  buffer: string
  capacity: string
  targetUtilization: string
  scaleDownDelay: string
  drainTimeout: string
  maxSurge: string
}

export type ScalingField = keyof ScalingForm

/** Why a field is invalid. The page turns these codes into text. */
export type ScalingError =
  | 'required'
  | 'integer'
  | 'number'
  | 'range'
  | 'maxBelowMin'
  | 'staticBuffer'
  | 'duration'

export interface ScalingValidation {
  errors: Partial<Record<ScalingField, ScalingError>>
  /** Set only when every field is valid. Optional fields left empty are omitted, so the server applies its default. */
  scaling?: Scaling
}

/**
 * A Go duration string as model.Duration reads it: decimal numbers with a unit (ns, us, µs, μs, ms, s, m, h), several
 * parts allowed, or a bare "0". Signs are not allowed: durations here are never negative.
 */
const DURATION = /^(?:\d+(?:\.\d+)?(?:ns|us|µs|μs|ms|s|m|h))+$|^0$/
const INTEGER = /^\d+$/
const NUMBER = /^(?:\d+\.?\d*|\.\d+)$/

export function isDurationText(value: string): boolean {
  return DURATION.test(value)
}

export function toScalingForm(scaling: Partial<Scaling>): ScalingForm {
  const text = (v: number | string | undefined) => (v === undefined ? '' : String(v))
  return {
    min: text(scaling.min),
    max: text(scaling.max),
    buffer: text(scaling.buffer),
    capacity: text(scaling.capacity),
    targetUtilization: text(scaling.targetUtilization),
    scaleDownDelay: text(scaling.scaleDownDelay),
    drainTimeout: text(scaling.drainTimeout),
    maxSurge: text(scaling.maxSurge),
  }
}

/**
 * Checks the form the way the controller does (PoolSpec.Validate), so most mistakes are caught before a request.
 * The server stays the authority: its problem answer is still shown when something slips through.
 */
export function validateScaling(form: ScalingForm, context: { mode: string }): ScalingValidation {
  const errors: ScalingValidation['errors'] = {}

  const min = integerField(form.min, 'min', errors, { required: true })
  const max = integerField(form.max, 'max', errors, { required: true })
  const buffer = integerField(form.buffer, 'buffer', errors, {})
  const capacity = integerField(form.capacity, 'capacity', errors, {})
  const maxSurge = integerField(form.maxSurge, 'maxSurge', errors, {})

  if (min !== undefined && max !== undefined && max < min) errors.max = 'maxBelowMin'
  if (context.mode === 'static' && buffer !== undefined && buffer !== 0) errors.buffer = 'staticBuffer'

  let targetUtilization: number | undefined
  if (form.targetUtilization.trim() !== '') {
    const value = NUMBER.test(form.targetUtilization.trim()) ? Number(form.targetUtilization) : Number.NaN
    if (Number.isNaN(value)) errors.targetUtilization = 'number'
    else if (value <= 0 || value > 1) errors.targetUtilization = 'range'
    else targetUtilization = value
  }

  const scaleDownDelay = durationField(form.scaleDownDelay, 'scaleDownDelay', errors)
  const drainTimeout = durationField(form.drainTimeout, 'drainTimeout', errors)

  if (Object.keys(errors).length > 0) return { errors }

  const scaling: Scaling = { min: min!, max: max! }
  if (buffer !== undefined) scaling.buffer = buffer
  if (capacity !== undefined) scaling.capacity = capacity
  if (targetUtilization !== undefined) scaling.targetUtilization = targetUtilization
  if (scaleDownDelay !== undefined) scaling.scaleDownDelay = scaleDownDelay
  if (drainTimeout !== undefined) scaling.drainTimeout = drainTimeout
  if (maxSurge !== undefined) scaling.maxSurge = maxSurge
  return { errors, scaling }
}

/**
 * The full spec to send to PUT /v1/pools/{pool}: the spec as the server last returned it, with only the scaling part
 * replaced. The read-only $schema is left out.
 */
export function applyScaling(spec: PoolSpec, scaling: Scaling): PoolSpec {
  const { $schema: _readOnly, ...rest } = spec
  return { ...rest, scaling }
}

export function sameForm(a: ScalingForm, b: ScalingForm): boolean {
  return (Object.keys(a) as ScalingField[]).every(key => a[key] === b[key])
}

/**
 * Keeps the user's unsaved edits when live data changes the pool: the form follows the server only while it still
 * shows the previous server values.
 */
export function rebaseForm(current: ScalingForm, previous: ScalingForm, next: ScalingForm): ScalingForm {
  return sameForm(current, previous) ? next : current
}

function integerField(
  value: string,
  field: ScalingField,
  errors: ScalingValidation['errors'],
  options: { required?: boolean },
): number | undefined {
  const text = value.trim()
  if (text === '') {
    if (options.required) errors[field] = 'required'
    return undefined
  }
  if (!INTEGER.test(text)) {
    errors[field] = 'integer'
    return undefined
  }
  return Number(text)
}

function durationField(
  value: string,
  field: ScalingField,
  errors: ScalingValidation['errors'],
): string | undefined {
  const text = value.trim()
  if (text === '') return undefined
  if (!isDurationText(text)) {
    errors[field] = 'duration'
    return undefined
  }
  return text
}
