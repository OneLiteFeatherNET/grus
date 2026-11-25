import { describe, expect, it } from 'vitest'
import { ageOf, poolCounts, poolPath, specSections } from './view'

describe('poolCounts', () => {
  const instances = [
    { id: 'a', pool: 'lobby', state: 'Ready' },
    { id: 'b', pool: 'lobby', state: 'Allocated' },
    { id: 'c', pool: 'lobby', state: 'Starting' },
    { id: 'd', pool: 'survival', state: 'Ready' },
  ]

  it('counts the serving instances of one pool against all its instances', () => {
    expect(poolCounts(instances, 'lobby')).toEqual({ ready: 2, total: 3 })
  })

  it('returns zeros for a pool without instances', () => {
    expect(poolCounts(instances, 'empty')).toEqual({ ready: 0, total: 0 })
  })
})

describe('ageOf', () => {
  const now = Date.parse('2026-11-25T12:00:00Z')

  it('uses seconds below one minute', () => {
    expect(ageOf('2026-11-25T11:59:30Z', now)).toEqual({ unit: 's', value: 30 })
  })

  it('uses minutes below one hour', () => {
    expect(ageOf('2026-11-25T11:15:00Z', now)).toEqual({ unit: 'm', value: 45 })
  })

  it('uses hours below one day', () => {
    expect(ageOf('2026-11-25T06:00:00Z', now)).toEqual({ unit: 'h', value: 6 })
  })

  it('uses days from one day on', () => {
    expect(ageOf('2026-11-22T12:00:00Z', now)).toEqual({ unit: 'd', value: 3 })
  })

  it('never shows a negative age for a timestamp slightly in the future (clock skew)', () => {
    expect(ageOf('2026-11-25T12:00:05Z', now)).toEqual({ unit: 's', value: 0 })
  })
})

describe('specSections', () => {
  const spec = {
    role: 'game',
    mode: 'dynamic',
    driver: 'kubernetes',
    image: 'registry.example/lobby:1',
    port: 25565,
    command: ['java', '-jar'],
    args: ['server.jar'],
    env: { EULA: 'true' },
    secretEnv: [{ name: 'RCON_PW', secret: 'lobby-secrets', key: 'rcon' }],
    resources: { memoryMiB: 2048, cpu: '2' },
    templates: [{ name: 'base', tag: 'latest' }, { name: 'lobby', digest: 'sha256:abc' }],
    scaling: { min: 1, max: 4, buffer: 1, targetUtilization: 0.8, scaleDownDelay: '5m0s' },
    lifecycle: { stopCommand: 'stop', gracePeriod: '30s' },
    labels: { tier: 'lobby' },
  } as never

  function rowsOf(key: string) {
    return specSections(spec).find(s => s.key === key)?.rows ?? []
  }

  it('lists the sections in spec order', () => {
    expect(specSections(spec).map(s => s.key)).toEqual(['basics', 'run', 'resources', 'templates', 'scaling', 'lifecycle', 'labels'])
  })

  it('shows the names of secret references, never a value', () => {
    expect(rowsOf('run').find(r => r.key === 'secretEnv')?.value).toBe('RCON_PW (lobby-secrets/rcon)')
  })

  it('shows a template by tag or by digest', () => {
    expect(rowsOf('templates')[0]?.value).toBe('base:latest, lobby@sha256:abc')
  })

  it('marks fields the spec leaves unset with null', () => {
    expect(rowsOf('scaling').find(r => r.key === 'capacity')?.value).toBeNull()
    expect(rowsOf('lifecycle').find(r => r.key === 'heartbeatTimeout')?.value).toBeNull()
  })

  it('joins command and arguments with spaces and labels as key=value', () => {
    expect(rowsOf('run').find(r => r.key === 'command')?.value).toBe('java -jar')
    expect(rowsOf('labels')[0]?.value).toBe('tier=lobby')
  })
})

describe('poolPath', () => {
  it('builds the detail path and encodes the name as one segment', () => {
    expect(poolPath('lobby')).toBe('/pools/lobby')
    expect(poolPath('a/b')).toBe('/pools/a%2Fb')
  })
})
