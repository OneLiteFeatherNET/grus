import type { Pool, TemplateTags } from '~/lib/grus'

/** Matches a version digest as the controller stores it: sha256 with lower-case hex. */
const DIGEST_PATTERN = /^sha256:[0-9a-f]+$/

/** A version of a template with every tag that points at it. */
export interface TemplateVersion {
  digest: string
  tags: string[]
}

/** A pool that references a template, with the tag or digest it selects. */
export interface TemplateUsage {
  pool: string
  /** The tag (when the pool follows a tag) or the short digest (when it pins one). */
  ref: string
  /** False when the tag or digest does not exist (anymore), so the pool cannot resolve the template. */
  resolved: boolean
}

export interface TemplateView {
  name: string
  versions: TemplateVersion[]
  pools: TemplateUsage[]
}

export function isDigest(value: string): boolean {
  return DIGEST_PATTERN.test(value)
}

/** Short form of a digest for tables: the algorithm prefix and the first 12 hex characters. */
export function shortDigest(digest: string): string {
  const [algorithm, hex = ''] = digest.split(':')
  return `${algorithm}:${hex.slice(0, 12)}`
}

/**
 * Joins the tag listing with the pools that use the templates. Templates are sorted by name, versions by their
 * tags (tagged versions first), tags alphabetically. Names only referenced by a pool still get a view, so a missing
 * template shows up instead of vanishing.
 */
export function buildTemplateViews(tags: TemplateTags, pools: readonly Pool[]): TemplateView[] {
  const names = new Set<string>([...Object.keys(tags), ...pools.flatMap(p => (p.spec.templates ?? []).map(t => t.name))])
  return [...names].sort(byText).map((name) => {
    const byDigest = tags[name] ?? {}
    const versions = groupByDigest(byDigest)
    const usages = pools.flatMap(pool => (pool.spec.templates ?? [])
      .filter(ref => ref.name === name)
      .map(ref => usageOf(pool.name, ref, byDigest)))
    return { name, versions, pools: usages.sort((a, b) => byText(a.pool, b.pool)) }
  })
}

function groupByDigest(byDigest: Record<string, string>): TemplateVersion[] {
  const map = new Map<string, string[]>()
  for (const [tag, digest] of Object.entries(byDigest)) {
    map.set(digest, [...(map.get(digest) ?? []), tag])
  }
  return [...map.entries()]
    .map(([digest, list]) => ({ digest, tags: list.sort(byText) }))
    .sort((a, b) => byText(a.digest, b.digest))
}

function usageOf(
  pool: string,
  ref: { tag?: string | null, digest?: string | null },
  byDigest: Record<string, string>,
): TemplateUsage {
  if (ref.tag) {
    return { pool, ref: ref.tag, resolved: ref.tag in byDigest }
  }
  const digest = ref.digest ?? ''
  const resolved = Object.values(byDigest).includes(digest)
  return { pool, ref: digest ? shortDigest(digest) : '-', resolved }
}

/** Locale-independent string order, so the list does not depend on the browser language. */
function byText(a: string, b: string): number {
  return a < b ? -1 : a > b ? 1 : 0
}

/** Sanitised download file name for an archive: name plus short digest, without characters that break file systems. */
export function archiveFileName(name: string, digest: string): string {
  const hex = digest.split(':')[1]?.slice(0, 12) ?? 'archive'
  return `${name.replace(/[^\w.-]+/g, '_')}-${hex}.tar.gz`
}
