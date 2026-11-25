import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

// Vue renders an unknown component tag as nothing, and neither lint nor typecheck notices. This test compares every
// PascalCase tag in the app templates with the components Nuxt registered (.nuxt/components.d.ts, written by
// `nuxt prepare`, which runs on install).
const APP_DIR = join(__dirname)
const BUILT_IN = new Set(['NuxtLayout', 'NuxtPage', 'NuxtLink', 'ClientOnly', 'Teleport', 'Transition', 'TransitionGroup', 'KeepAlive'])

function vueFiles(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name)
    if (statSync(path).isDirectory()) return vueFiles(path)
    return path.endsWith('.vue') ? [path] : []
  })
}

describe('component tags', () => {
  it('only uses components that Nuxt registered', () => {
    const declared = readFileSync(join(APP_DIR, '..', '.nuxt', 'components.d.ts'), 'utf8')
    const known = new Set([...declared.matchAll(/^export const ([A-Za-z0-9]+)/gm)].map(m => m[1]))
    const unknown = vueFiles(APP_DIR).flatMap((file) => {
      const template = readFileSync(file, 'utf8').split('<script')[0] ?? ''
      return [...new Set([...template.matchAll(/<([A-Z][A-Za-z0-9]+)/g)].map(m => m[1]!))]
        .filter(tag => !known.has(tag) && !BUILT_IN.has(tag))
        .map(tag => `${file.slice(APP_DIR.length + 1)}: <${tag}>`)
    })
    expect(unknown, 'unresolved component tags').toEqual([])
  })
})
