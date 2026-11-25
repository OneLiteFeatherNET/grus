import type { components } from '~/types/grus-api'
import { toClientError, toTransportError } from './problem'

export type Pool = components['schemas']['Pool']
export type PoolSpec = components['schemas']['PoolSpec']
export type Instance = components['schemas']['Instance']
/** Template name -> tag -> digest (see GET /v1/templates). */
export type TemplateTags = Record<string, Record<string, string>>

/** Same-origin path of the BFF. The browser never talks to the Grus API directly. */
export const GRUS_BASE = '/api/grus'

export interface RequestOptions {
  method?: 'GET' | 'PUT' | 'POST' | 'DELETE'
  body?: unknown
}

/** Performs one request and resolves with the parsed JSON body. Injected so the client is testable without fetch. */
export type Requester = (url: string, options: RequestOptions) => Promise<unknown>

/** Appends defined query parameters to a path. Empty values are skipped. */
export function withQuery(path: string, query: Record<string, string | undefined> = {}): string {
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(query)) {
    if (value) params.set(key, value)
  }
  const search = params.toString()
  return search ? `${path}?${search}` : path
}

/** Typed, thin client for the Grus API as seen through the BFF. Add one method per endpoint a page needs. */
export function createGrusClient(request: Requester) {
  const url = bffUrl
  return {
    listPools: () => request(url('/v1/pools'), { method: 'GET' }) as Promise<Pool[]>,
    getPool: (name: string) => request(url(`/v1/pools/${encodeURIComponent(name)}`), { method: 'GET' }) as Promise<Pool>,
    listInstances: (query: { pool?: string, state?: string } = {}) =>
      request(url('/v1/instances', query), { method: 'GET' }) as Promise<Instance[]>,
    listTemplates: () => request(url('/v1/templates'), { method: 'GET' }) as Promise<TemplateTags>,
  }
}

/** Same-origin URL of an API path below the BFF, with optional query parameters. */
export function bffUrl(path: string, query?: Record<string, string | undefined>): string {
  return withQuery(`${GRUS_BASE}${path}`, query)
}

/**
 * Browser requester: $fetch against the BFF. Failures are thrown as Nuxt errors whose data carries the ApiError or
 * NetworkError; `toClientError` rebuilds the original from them.
 * Area clients in app/lib/api/ build on it the same way createGrusClient does.
 */
export const browserRequest: Requester = async (url, options) => {
  try {
    return await $fetch(url, { method: options.method, body: options.body as BodyInit | undefined })
  }
  catch (err) {
    // Raised through createError so Nuxt keeps status and problem on the server render instead of masking them.
    throw createError(toTransportError(toClientError(err)))
  }
}

/** Browser client for the endpoints shared by several pages. */
export const grus = createGrusClient(browserRequest)
