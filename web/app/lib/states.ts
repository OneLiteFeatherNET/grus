/** Instance lifecycle states (internal/model: Requested -> ... -> Stopped, plus Lost and Failed). */
export const INSTANCE_STATES = [
  'Requested',
  'Scheduled',
  'Preparing',
  'Starting',
  'Ready',
  'Allocated',
  'Unhealthy',
  'Draining',
  'Stopping',
  'Stopped',
  'Failed',
  'Lost',
] as const

export type InstanceState = typeof INSTANCE_STATES[number]

/** Colour roles of a status chip; they map to the M3 tones of the Chip primitive. */
export type ChipTone = 'neutral' | 'primary' | 'secondary' | 'tertiary' | 'error' | 'success' | 'warning'

const TONE_BY_STATE: Record<InstanceState, ChipTone> = {
  Requested: 'secondary',
  Scheduled: 'secondary',
  Preparing: 'secondary',
  Starting: 'secondary',
  Ready: 'success',
  Allocated: 'primary',
  Unhealthy: 'warning',
  Draining: 'warning',
  Stopping: 'warning',
  Stopped: 'neutral',
  Failed: 'error',
  Lost: 'error',
}

/** Colour of an instance state. Unknown states from a newer controller stay neutral instead of failing. */
export function instanceStateTone(state: string): ChipTone {
  return (TONE_BY_STATE as Record<string, ChipTone | undefined>)[state] ?? 'neutral'
}

/** Whether an instance in this state still counts as serving capacity for the overview (Ready or Allocated). */
export function isServing(state: string): boolean {
  return state === 'Ready' || state === 'Allocated'
}
