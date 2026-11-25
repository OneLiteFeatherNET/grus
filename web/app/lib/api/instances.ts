import type { Requester, Instance } from '../grus'
import { browserRequest, bffUrl } from '../grus'

/** Body of POST /v1/instances/{id}/stop. Without drain or force, an instance with players is refused. */
export interface StopOptions {
  drain?: boolean
  force?: boolean
}

export function createInstancesApi(request: Requester) {
  const path = (id: string, suffix = '') => `/v1/instances/${encodeURIComponent(id)}${suffix}`
  return {
    list: (query: { pool?: string, state?: string } = {}) =>
      request(bffUrl('/v1/instances', query), { method: 'GET' }) as Promise<Instance[]>,
    get: (id: string) => request(bffUrl(path(id)), { method: 'GET' }) as Promise<Instance>,
    command: (id: string, line: string) =>
      request(bffUrl(path(id, '/command')), { method: 'POST', body: { line } }),
    stop: (id: string, options: StopOptions = {}) =>
      request(bffUrl(path(id, '/stop')), { method: 'POST', body: options }) as Promise<Instance>,
    /** Same-origin URL of the live log stream for EventSource. Only the browser opens it. */
    logsStreamUrl: (id: string) => bffUrl(path(id, '/logs/stream')),
  }
}

export const instancesApi = createInstancesApi(browserRequest)
