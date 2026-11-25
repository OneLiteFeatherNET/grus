import type { H3Event } from 'h3'
import { buildClientResponseHeaders, buildUpstreamHeaders, buildUpstreamUrl } from '../../../utils/grus-proxy'

// BFF: every /api/grus/<path> request is forwarded to <grusApiUrl>/<path> with the server-side admin token.
// Responses are passed through as a stream, so SSE (/v1/events, live logs) reaches the browser unbuffered.
// TODO(auth): the prototype has no login (access only via kubectl port-forward). Entra OIDC comes with Grus ADR 07.
export default defineEventHandler(async (event) => {
  const config = useRuntimeConfig(event)
  const url = getRequestURL(event)
  const rawPath = url.pathname.slice('/api/grus/'.length)

  let target: string
  try {
    target = buildUpstreamUrl(config.grusApiUrl, rawPath, url.search)
  }
  catch {
    return problem(event, 404, 'Not Found', 'This path is not part of the Grus API.')
  }
  if (!config.grusToken) {
    return problem(event, 503, 'Service Unavailable', 'The Grus token is not configured on the web server (NUXT_GRUS_TOKEN).')
  }

  const hasBody = event.method !== 'GET' && event.method !== 'HEAD'
  const body = hasBody ? await readRawBody(event, false) : undefined
  // Abort the upstream request when the browser goes away (closed tab, reconnect) before the response ended.
  const controller = new AbortController()
  event.node.res.on('close', () => {
    if (!event.node.res.writableFinished) controller.abort()
  })

  let upstream: Response
  try {
    upstream = await fetch(target, {
      method: event.method,
      headers: buildUpstreamHeaders(getRequestHeaders(event) as Record<string, string | undefined>, config.grusToken),
      body: body ? new Uint8Array(body) : undefined,
      redirect: 'manual',
      signal: controller.signal,
    })
  }
  catch {
    if (controller.signal.aborted) return new Response(null, { status: 499 })
    return problem(event, 502, 'Bad Gateway', 'The Grus API could not be reached.')
  }

  return new Response(upstream.body, {
    status: upstream.status,
    statusText: upstream.statusText,
    headers: buildClientResponseHeaders(upstream.headers),
  })
})

function problem(event: H3Event, status: number, title: string, detail: string) {
  setResponseStatus(event, status)
  setResponseHeader(event, 'content-type', 'application/problem+json')
  return { type: 'about:blank', title, status, detail }
}
