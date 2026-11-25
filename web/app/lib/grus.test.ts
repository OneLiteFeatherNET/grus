import { describe, expect, it } from 'vitest'
import { createGrusClient, withQuery } from './grus'

describe('withQuery', () => {
  it('returns the path unchanged without parameters', () => {
    expect(withQuery('/api/grus/v1/instances')).toBe('/api/grus/v1/instances')
  })

  it('skips empty parameters and encodes the rest', () => {
    expect(withQuery('/x', { pool: 'lobby 1', state: '' })).toBe('/x?pool=lobby+1')
  })
})

describe('createGrusClient', () => {
  it('requests pools through the BFF path', async () => {
    const calls: string[] = []
    const client = createGrusClient(async (url) => {
      calls.push(url)
      return []
    })
    await client.listPools()
    expect(calls).toEqual(['/api/grus/v1/pools'])
  })

  it('filters instances by pool and state only when given', async () => {
    const calls: string[] = []
    const client = createGrusClient(async (url) => {
      calls.push(url)
      return []
    })
    await client.listInstances()
    await client.listInstances({ pool: 'lobby', state: 'Ready' })
    expect(calls).toEqual([
      '/api/grus/v1/instances',
      '/api/grus/v1/instances?pool=lobby&state=Ready',
    ])
  })

  it('encodes the pool name in the path', async () => {
    const calls: string[] = []
    const client = createGrusClient(async (url) => {
      calls.push(url)
      return {}
    })
    await client.getPool('a/b')
    expect(calls).toEqual(['/api/grus/v1/pools/a%2Fb'])
  })
})
