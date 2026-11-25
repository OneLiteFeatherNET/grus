import { describe, expect, it } from 'vitest'
import { buildClientResponseHeaders, buildUpstreamHeaders, buildUpstreamUrl } from './grus-proxy'

describe('buildUpstreamUrl', () => {
  it('joins the API base, the /v1 path and the query string', () => {
    expect(buildUpstreamUrl('http://grus:8080', 'v1/pools', '?name=a')).toBe('http://grus:8080/v1/pools?name=a')
  })

  it('ignores a trailing slash on the API base', () => {
    expect(buildUpstreamUrl('http://grus:8080///', 'v1/events')).toBe('http://grus:8080/v1/events')
  })

  it('keeps percent-encoded path segments as they arrived', () => {
    expect(buildUpstreamUrl('http://grus:8080', 'v1/templates/web%20ui/tags/latest')).toBe(
      'http://grus:8080/v1/templates/web%20ui/tags/latest',
    )
  })

  it('rejects paths outside the /v1 prefix', () => {
    expect(() => buildUpstreamUrl('http://grus:8080', 'healthz')).toThrow()
    expect(() => buildUpstreamUrl('http://grus:8080', 'v2/pools')).toThrow()
  })

  it('rejects dot segments, also when percent-encoded', () => {
    expect(() => buildUpstreamUrl('http://grus:8080', 'v1/../healthz')).toThrow()
    expect(() => buildUpstreamUrl('http://grus:8080', 'v1/%2e%2e/healthz')).toThrow()
  })

  it('rejects malformed percent-encoding', () => {
    expect(() => buildUpstreamUrl('http://grus:8080', 'v1/%E0%A4%A')).toThrow()
  })
})

describe('buildUpstreamHeaders', () => {
  it('adds the server-side bearer token', () => {
    const headers = buildUpstreamHeaders({ accept: 'application/json' }, 'secret-token')
    expect(headers.get('authorization')).toBe('Bearer secret-token')
  })

  it('replaces an Authorization header sent by the browser', () => {
    const headers = buildUpstreamHeaders({ authorization: 'Bearer from-browser' }, 'secret-token')
    expect(headers.get('authorization')).toBe('Bearer secret-token')
  })

  it('drops cookies, host and other non-allow-listed headers', () => {
    const headers = buildUpstreamHeaders(
      { cookie: 'session=1', host: 'evil.example', 'x-forwarded-for': '10.0.0.1', 'user-agent': 'test' },
      'secret-token',
    )
    expect(headers.has('cookie')).toBe(false)
    expect(headers.has('host')).toBe(false)
    expect(headers.has('x-forwarded-for')).toBe(false)
    expect(headers.has('user-agent')).toBe(false)
  })

  it('forwards Last-Event-ID so the stream can resume', () => {
    const headers = buildUpstreamHeaders({ 'last-event-id': '1700-42' }, 'secret-token')
    expect(headers.get('last-event-id')).toBe('1700-42')
  })

  it('never leaks the token into the browser-facing response headers', () => {
    const upstream = new Headers({ 'content-type': 'application/json', 'x-debug-token': 'secret-token' })
    expect(buildClientResponseHeaders(upstream).get('authorization')).toBeNull()
  })
})

describe('buildClientResponseHeaders', () => {
  it('drops hop-by-hop, length and encoding headers', () => {
    const upstream = new Headers({
      'content-type': 'application/json',
      'content-length': '12',
      'content-encoding': 'gzip',
      'transfer-encoding': 'chunked',
      connection: 'keep-alive',
    })
    const headers = buildClientResponseHeaders(upstream)
    expect(headers.get('content-type')).toBe('application/json')
    expect(headers.has('content-length')).toBe(false)
    expect(headers.has('content-encoding')).toBe(false)
    expect(headers.has('transfer-encoding')).toBe(false)
    expect(headers.has('connection')).toBe(false)
  })

  it('marks event streams as unbuffered and uncacheable', () => {
    const upstream = new Headers({ 'content-type': 'text/event-stream; charset=utf-8' })
    const headers = buildClientResponseHeaders(upstream)
    expect(headers.get('cache-control')).toBe('no-cache, no-transform')
    expect(headers.get('x-accel-buffering')).toBe('no')
  })

  it('leaves ordinary responses without the streaming headers', () => {
    const headers = buildClientResponseHeaders(new Headers({ 'content-type': 'application/json' }))
    expect(headers.has('x-accel-buffering')).toBe(false)
  })
})
