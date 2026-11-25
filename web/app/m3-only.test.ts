import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

// Material 3 is mandatory for OLF UIs: pages and components use the Pavo fields, tables and switches, not the raw
// form and table elements. Pavo's components render the raw elements inside themselves, so only app templates are
// checked here. Only the template part is scanned; a script that mentions a tag does not count.
const APP_DIR = join(__dirname)
const SCANNED = ['pages', 'components'].map(dir => join(APP_DIR, dir))
const RAW_TAGS = ['input', 'select', 'textarea', 'table']
const HINT = 'use M3TextField, M3Select, M3Checkbox, M3Switch or M3DataTable instead'

function vueFiles(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name)
    if (statSync(path).isDirectory()) return vueFiles(path)
    return path.endsWith('.vue') ? [path] : []
  })
}

function rawTagsIn(source: string): string[] {
  const template = source.split('<script')[0] ?? ''
  return RAW_TAGS.filter(tag => new RegExp(`<${tag}\\b`).test(template))
}

describe('material 3 elements', () => {
  it('finds raw form and table tags in a template', () => {
    expect(rawTagsIn('<template><div><input><table></table></div></template>')).toEqual(['input', 'table'])
  })

  it('ignores raw tags inside the script block', () => {
    expect(rawTagsIn('<template><M3Select /></template>\n<script setup>const x = \'<select>\'</script>')).toEqual([])
  })

  it('uses no raw form or table element in pages and components', () => {
    const violations = SCANNED.flatMap(dir => vueFiles(dir)).flatMap((file) => {
      const source = readFileSync(file, 'utf8')
      return rawTagsIn(source).map(tag => `${file.slice(APP_DIR.length + 1)}: <${tag}>`)
    })
    expect(violations, `raw elements found (${HINT})`).toEqual([])
  })
})
