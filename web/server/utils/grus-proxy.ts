/**
 * Pure helpers of the BFF proxy: where a browser request goes upstream and which headers travel with it.
 * They carry no framework types, so the security-relevant rules are covered by plain unit tests.
 */

/** The only upstream API prefix the proxy forwards to. */
export const UPSTREAM_PREFIX = 'v1/'

/** Request headers the browser may send upstream. Everything else (Authorization, Cookie, Host, ...) is dropped. */
const FORWARDED_REQUEST_HEADERS = ['accept', 'content-type', 'last-event-id', 'if-match', 'if-none-match'] as const

/** Hop-by-hop and framing headers never copied from the upstream response. */
const DROPPED_RESPONSE_HEADERS = new Set([
  'connection',
  'keep-alive',
  'proxy-authenticate',
  'proxy-authorization',
  'te',
  'trailer',
  'transfer-encoding',
  'upgrade',
  'content-length',
  // fetch() already decoded the body, so the original encoding no longer applies.
  'content-encoding',
  'set-cookie',
])

/**
 * Builds the upstream URL for a proxied request.
 * `rawPath` is the request path below /api/grus/ as it arrived (still percent-encoded), `search` the query string
 * including its leading "?". Throws when the path leaves the /v1 prefix or contains dot segments.
 */
export function buildUpstreamUrl(apiBase: string, rawPath: string, search = ''): string {
  const decoded = safeDecode(rawPath)
  if (decoded === null || !decoded.startsWith(UPSTREAM_PREFIX)) {
    throw new Error('path is outside the Grus API')
  }
  if (decoded.split('/').some(segment => segment === '.' || segment === '..')) {
    throw new Error('dot segments are not allowed')
  }
  const base = apiBase.replace(/\/+$/, '')
  const query = search.startsWith('?') || search === '' ? search : `?${search}`
  return `${base}/${rawPath}${query}`
}

/**
 * Builds the headers sent upstream: an allow-listed subset of the client headers, plus the server-side bearer token.
 * A client-supplied Authorization header never survives, so the token cannot be replaced from the browser.
 */
export function buildUpstreamHeaders(incoming: Record<string, string | undefined>, token: string): Headers {
  const headers = new Headers()
  for (const name of FORWARDED_REQUEST_HEADERS) {
    const value = incoming[name]
    if (value !== undefined && value !== '') headers.set(name, value)
  }
  headers.set('authorization', `Bearer ${token}`)
  return headers
}

/** Copies the upstream response headers that are safe to pass to the browser. */
export function buildClientResponseHeaders(upstream: Headers): Headers {
  const headers = new Headers()
  upstream.forEach((value, name) => {
    if (!DROPPED_RESPONSE_HEADERS.has(name.toLowerCase())) headers.set(name, value)
  })
  // Streams must reach the browser unbuffered (SSE, live logs); no proxy or compression layer may hold them back.
  if (upstream.get('content-type')?.startsWith('text/event-stream')) {
    headers.set('cache-control', 'no-cache, no-transform')
    headers.set('x-accel-buffering', 'no')
  }
  return headers
}

function safeDecode(value: string): string | null {
  try {
    return decodeURIComponent(value)
  }
  catch {
    return null
  }
}

const SAFE_METHODS = new Set(['GET', 'HEAD', 'OPTIONS'])

/**
 * Cross-site request guard for mutating requests. The BFF attaches the admin token to every request, so without
 * this check any web page open in the same browser could stop instances through a form POST while a port-forward
 * is active. Browsers send Sec-Fetch-Site and Origin; requests without both (curl, scripts) cannot be forged by a
 * web page and are allowed.
 */
export function isAllowedOrigin(method: string, headers: Record<string, string | undefined>, host: string | undefined): boolean {
  if (SAFE_METHODS.has(method.toUpperCase())) return true
  const fetchSite = headers['sec-fetch-site']
  if (fetchSite !== undefined) return fetchSite === 'same-origin'
  const origin = headers.origin
  if (origin === undefined) return true
  try {
    return host !== undefined && new URL(origin).host === host
  }
  catch {
    return false
  }
}
