import { describe, expect, it } from 'vitest'
import type { Requester, RequestOptions } from '~/lib/grus'
import { createPoolsApi } from './pools'

/** Records every call and answers with the given body. */
function recorder(answer: unknown = {}) {
  const calls: { url: string, options: RequestOptions }[] = []
  const request: Requester = async (url, options) => {
    calls.push({ url, options })
    return answer
  }
  return { calls, request }
}

describe('createPoolsApi', () => {
  it('lists pools through the BFF', async () => {
    const { calls, request } = recorder([])
    await createPoolsApi(request).listPools()
    expect(calls).toEqual([{ url: '/api/grus/v1/pools', options: { method: 'GET' } }])
  })

  it('reads one pool with the name encoded', async () => {
    const { calls, request } = recorder({})
    await createPoolsApi(request).getPool('lobby 1')
    expect(calls[0]?.url).toBe('/api/grus/v1/pools/lobby%201')
  })

  it('replaces the spec with a PUT of the full spec as the body', async () => {
    const { calls, request } = recorder({})
    const spec = { role: 'game', mode: 'dynamic', driver: 'kubernetes', resources: { memoryMiB: 512 }, scaling: { min: 1, max: 2 } }
    await createPoolsApi(request).putPoolSpec('lobby', spec as never)
    expect(calls).toEqual([{ url: '/api/grus/v1/pools/lobby', options: { method: 'PUT', body: spec } }])
  })

  it('sets maintenance with the enabled flag in the body', async () => {
    const { calls, request } = recorder({})
    await createPoolsApi(request).setMaintenance('lobby', true)
    expect(calls).toEqual([{ url: '/api/grus/v1/pools/lobby/maintenance', options: { method: 'PUT', body: { enabled: true } } }])
  })

  it('lists the instances of one pool with the pool filter', async () => {
    const { calls, request } = recorder([])
    await createPoolsApi(request).listPoolInstances('lobby')
    expect(calls[0]?.url).toBe('/api/grus/v1/instances?pool=lobby')
  })
})
