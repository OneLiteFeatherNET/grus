import { describe, expect, it } from 'vitest'
import type { Pool } from '~/lib/grus'
import { archiveFileName, buildTemplateViews, isDigest, shortDigest } from './model'

const A = `sha256:${'a'.repeat(64)}`
const B = `sha256:${'b'.repeat(64)}`

function pool(name: string, templates: Array<{ name: string, tag?: string, digest?: string }>): Pool {
  return { name, spec: { templates } } as unknown as Pool
}

describe('isDigest', () => {
  it('accepts lower-case sha256 digests only', () => {
    expect(isDigest(A)).toBe(true)
    expect(isDigest('sha256:ABC')).toBe(false)
    expect(isDigest('md5:abc')).toBe(false)
  })
})

describe('shortDigest', () => {
  it('keeps the algorithm and the first 12 hex characters', () => {
    expect(shortDigest(A)).toBe('sha256:aaaaaaaaaaaa')
  })
})

describe('archiveFileName', () => {
  it('replaces characters that do not belong in a file name', () => {
    expect(archiveFileName('lobby config/x', A)).toBe('lobby_config_x-aaaaaaaaaaaa.tar.gz')
  })
})

describe('buildTemplateViews', () => {
  it('groups tags by digest and sorts them', () => {
    const [view] = buildTemplateViews({ lobby: { stable: A, latest: A, old: B } }, [])
    expect(view?.versions).toEqual([
      { digest: A, tags: ['latest', 'stable'] },
      { digest: B, tags: ['old'] },
    ])
  })

  it('lists the pools that reference a template with their tag or pinned digest', () => {
    const views = buildTemplateViews(
      { lobby: { stable: A } },
      [pool('lobby-1', [{ name: 'lobby', tag: 'stable' }]), pool('pvp', [{ name: 'lobby', digest: A }])],
    )
    expect(views[0]?.pools).toEqual([
      { pool: 'lobby-1', ref: 'stable', resolved: true },
      { pool: 'pvp', ref: 'sha256:aaaaaaaaaaaa', resolved: true },
    ])
  })

  it('marks a pool reference whose tag does not exist as unresolved', () => {
    const [view] = buildTemplateViews({ lobby: { stable: A } }, [pool('lobby-1', [{ name: 'lobby', tag: 'gone' }])])
    expect(view?.pools).toEqual([{ pool: 'lobby-1', ref: 'gone', resolved: false }])
  })

  it('keeps a template that only a pool references, without versions', () => {
    const views = buildTemplateViews({}, [pool('lobby-1', [{ name: 'missing', tag: 'stable' }])])
    expect(views).toEqual([
      { name: 'missing', versions: [], pools: [{ pool: 'lobby-1', ref: 'stable', resolved: false }] },
    ])
  })

  it('sorts the templates by name', () => {
    const views = buildTemplateViews({ zeta: {}, alpha: {} }, [])
    expect(views.map(v => v.name)).toEqual(['alpha', 'zeta'])
  })
})
