import { describe, expect, it } from 'vitest'
import { createInstancesApi } from './instances'

function recorder() {
  const calls: { url: string, method?: string, body?: unknown }[] = []
  const request = async (url: string, options: { method?: string, body?: unknown }) => {
    calls.push({ url, method: options.method, body: options.body })
    return {}
  }
  return { calls, api: createInstancesApi(request) }
}

describe('createInstancesApi', () => {
  it('lists instances with the pool and state filter only when given', async () => {
    const { calls, api } = recorder()
    await api.list()
    await api.list({ pool: 'lobby', state: 'Ready' })
    expect(calls.map(c => c.url)).toEqual([
      '/api/grus/v1/instances',
      '/api/grus/v1/instances?pool=lobby&state=Ready',
    ])
  })

  it('encodes the instance id in the path', async () => {
    const { calls, api } = recorder()
    await api.get('lobby a/1')
    expect(calls[0]).toEqual({ url: '/api/grus/v1/instances/lobby%20a%2F1', method: 'GET', body: undefined })
  })

  it('sends the console line as a JSON body', async () => {
    const { calls, api } = recorder()
    await api.command('lobby-a1', 'say hi')
    expect(calls[0]).toEqual({ url: '/api/grus/v1/instances/lobby-a1/command', method: 'POST', body: { line: 'say hi' } })
  })

  it('posts the stop options, empty when none are given', async () => {
    const { calls, api } = recorder()
    await api.stop('lobby-a1')
    await api.stop('lobby-a1', { drain: true })
    expect(calls.map(c => c.body)).toEqual([{}, { drain: true }])
    expect(calls[0]?.url).toBe('/api/grus/v1/instances/lobby-a1/stop')
  })

  it('points the log stream at the BFF, not at the Grus API', () => {
    const { api } = recorder()
    expect(api.logsStreamUrl('lobby-a1')).toBe('/api/grus/v1/instances/lobby-a1/logs/stream')
  })
})
