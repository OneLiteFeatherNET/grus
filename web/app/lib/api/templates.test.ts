import { describe, expect, it } from 'vitest'
import { createTemplatesApi } from './templates'

describe('createTemplatesApi', () => {
  it('lists templates and pools through the BFF', async () => {
    const calls: string[] = []
    const api = createTemplatesApi(async (url) => {
      calls.push(url)
      return []
    })
    await api.listTemplates()
    await api.listPools()
    expect(calls).toEqual(['/api/grus/v1/templates', '/api/grus/v1/pools'])
  })

  it('builds the archive URL with the digest encoded', () => {
    const api = createTemplatesApi(async () => ({}))
    expect(api.archiveUrl('lobby config', 'sha256:abc')).toBe(
      '/api/grus/v1/templates/lobby%20config/versions/sha256%3Aabc/archive',
    )
  })
})
