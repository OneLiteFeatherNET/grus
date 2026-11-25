import { describe, expect, it } from 'vitest'
import { ApiError, NetworkError, problemFromBody, toClientError } from './problem'

describe('problemFromBody', () => {
  it('keeps the RFC 9457 fields of a problem document', () => {
    const err = problemFromBody(409, 'Conflict', {
      type: 'about:blank',
      title: 'Conflict',
      status: 409,
      detail: 'pool is in maintenance',
    })
    expect(err).toBeInstanceOf(ApiError)
    expect(err.status).toBe(409)
    expect(err.problem.title).toBe('Conflict')
    expect(err.message).toBe('pool is in maintenance')
  })

  it('falls back to a generic problem for a non-problem body', () => {
    const err = problemFromBody(502, 'Bad Gateway', '<html>upstream down</html>')
    expect(err.status).toBe(502)
    expect(err.problem.title).toBe('Bad Gateway')
    expect(err.problem.type).toBe('about:blank')
    expect(err.errors).toEqual([])
  })

  it('uses the HTTP status when the status is missing', () => {
    expect(problemFromBody(500, '', {}).problem.title).toBe('HTTP 500')
  })

  it('exposes validation errors by location and message', () => {
    const err = problemFromBody(422, 'Unprocessable Entity', {
      title: 'Unprocessable Entity',
      status: 422,
      errors: [{ location: 'body.scaling.max', message: 'must be >= min' }],
    })
    expect(err.errors).toEqual([{ location: 'body.scaling.max', message: 'must be >= min' }])
  })
})

describe('toClientError', () => {
  it('maps a thrown error with a response to an ApiError', () => {
    const thrown = { response: { status: 404, statusText: 'Not Found', _data: { title: 'Not Found', status: 404 } } }
    const err = toClientError(thrown)
    expect(err).toBeInstanceOf(ApiError)
    expect((err as ApiError).status).toBe(404)
  })

  it('maps an error without a response to a NetworkError', () => {
    expect(toClientError(new TypeError('fetch failed'))).toBeInstanceOf(NetworkError)
  })

  it('passes known client errors through unchanged', () => {
    const known = new NetworkError('gone')
    expect(toClientError(known)).toBe(known)
  })
})
