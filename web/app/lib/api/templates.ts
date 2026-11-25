import type { Pool, Requester, TemplateTags } from '~/lib/grus'
import { bffUrl, browserRequest } from '~/lib/grus'

/** Template area of the Grus API: listing, tags and the archive download. Pools are read for their template refs. */
export function createTemplatesApi(request: Requester) {
  return {
    listTemplates: () => request(bffUrl('/v1/templates'), { method: 'GET' }) as Promise<TemplateTags>,
    listPools: () => request(bffUrl('/v1/pools'), { method: 'GET' }) as Promise<Pool[]>,
    /** Same-origin URL of a version archive; the browser downloads it straight through the BFF. */
    archiveUrl: (name: string, digest: string) =>
      bffUrl(`/v1/templates/${encodeURIComponent(name)}/versions/${encodeURIComponent(digest)}/archive`),
  }
}

export const templatesApi = createTemplatesApi(browserRequest)
