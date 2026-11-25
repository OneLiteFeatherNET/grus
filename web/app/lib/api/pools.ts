import type { Instance, Pool, PoolSpec, Requester } from '~/lib/grus'
import { bffUrl, browserRequest } from '~/lib/grus'

/** Pool endpoints of the Grus API as seen through the BFF. Built on the shared requester, like the other area clients. */
export function createPoolsApi(request: Requester) {
  return {
    listPools: () => request(bffUrl('/v1/pools'), { method: 'GET' }) as Promise<Pool[]>,
    getPool: (name: string) => request(bffUrl(`/v1/pools/${encodeURIComponent(name)}`), { method: 'GET' }) as Promise<Pool>,
    /** Create or replace: the body is the whole spec, so callers send back what they read and change only one part. */
    putPoolSpec: (name: string, spec: PoolSpec) =>
      request(bffUrl(`/v1/pools/${encodeURIComponent(name)}`), { method: 'PUT', body: spec }) as Promise<Pool>,
    setMaintenance: (name: string, enabled: boolean) =>
      request(bffUrl(`/v1/pools/${encodeURIComponent(name)}/maintenance`), { method: 'PUT', body: { enabled } }) as Promise<Pool>,
    listPoolInstances: (name: string) =>
      request(bffUrl('/v1/instances', { pool: name }), { method: 'GET' }) as Promise<Instance[]>,
  }
}

export const poolsApi = createPoolsApi(browserRequest)
