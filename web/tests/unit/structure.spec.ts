import { readFileSync, readdirSync } from 'node:fs'
import { basename, join } from 'node:path'
import postcssHtml from 'postcss-html'
import { describe, expect, it } from 'vitest'
import { parse as parseComponent } from 'vue/compiler-sfc'

// Vitest runs from the `web` package root.
const WEB_ROOT = process.cwd()
const SOURCE_ROOT = 'src'
const STYLES_HOME = 'src/assets/styles'
const UNIT_HOME = 'tests/unit'
// The opening tag with its own `src` attribute, not the word `src` anywhere in
// a declaration or a URL.
const STYLE_SRC_TAG = /<style(?=[\s>])[^>]*\ssrc\s*=/
const TEST_FILE = /\.(?:spec|test)\.ts$/

const toPosix = (path: string) => path.replaceAll('\\', '/')

const sourcePaths = readdirSync(join(WEB_ROOT, SOURCE_ROOT), {
  encoding: 'utf8',
  recursive: true,
}).map((entry) => `${SOURCE_ROOT}/${toPosix(entry)}`)

const rootEntries = readdirSync(WEB_ROOT)

// `RvSearchSelect.vue` → `rv-search-select`: the block class a component's own
// rules begin with.
const blockOf = (path: string) =>
  basename(path, '.vue')
    .replaceAll(/(?<!^)([A-Z])/g, '-$1')
    .toLowerCase()

// The selector's subject chain up to its first combinator, with every
// parenthesised argument emptied so a `:not(a, b)` cannot split it.
const leadingCompound = (selector: string) => {
  let flat = selector
  for (let inner = flat; ; inner = flat) {
    flat = inner.replaceAll(/\([^()]*\)/g, '()')
    if (flat === inner) break
  }
  return flat.trim().split(/\s*[\s>+~]\s*/)[0] ?? ''
}

// Every selector and keyframe name in a component's unscoped blocks whose
// leading compound does not carry its own block class, so it could reach
// another component's elements.
const unscopedLeaks = (path: string, source: string) => {
  const block = blockOf(path)
  const ownClass = new RegExp(`\\.${block}(?:$|__|--|[.:[])`)
  const ownSelector = (selector: string) =>
    ownClass.test(leadingCompound(selector))
  const ownName = new RegExp(`^${block}(?:$|-)`)
  const unscoped = parseComponent(source, {
    filename: path,
  }).descriptor.styles.filter((style) => style.scoped !== true)
  const leaks: string[] = []
  const { parse: parseStyles } = postcssHtml()
  if (parseStyles === undefined) throw new Error('postcss-html has no parser')
  for (const { content } of unscoped) {
    const [root] = parseStyles(`<style>${content}</style>`).nodes
    if (root?.type !== 'root') continue
    root.walkAtRules('keyframes', (rule) => {
      if (!ownName.test(rule.params)) leaks.push(`@keyframes ${rule.params}`)
    })
    root.walkRules((rule) => {
      if (rule.parent?.type === 'atrule' && /keyframes$/.test(rule.parent.name))
        return
      for (const selector of rule.selectors)
        if (!ownSelector(selector)) leaks.push(selector)
    })
  }
  return leaks.map((leak) => `${path}: ${leak}`)
}

describe('source tree structure', () => {
  it('keeps every stylesheet in a component or in the global styles folder', () => {
    const loose = sourcePaths.filter(
      (path) => path.endsWith('.css') && !path.startsWith(`${STYLES_HOME}/`),
    )
    expect(
      loose,
      `A component's styles belong in its own <style scoped> block; the only stylesheets outside a component live in ${STYLES_HOME}/. Loose: ${loose.join(', ')}`,
    ).toEqual([])
  })

  it('declares component styles inline instead of loading a file', () => {
    const external = sourcePaths
      .filter((path) => path.endsWith('.vue'))
      .filter((path) =>
        STYLE_SRC_TAG.test(readFileSync(join(WEB_ROOT, path), 'utf8')),
      )
    expect(
      external,
      `<style src> is not used: paste the stylesheet into the component's own <style scoped> block. Loading a file: ${external.join(', ')}`,
    ).toEqual([])
  })

  // A library or a portal can render a component's elements outside its scope,
  // so a component may keep those rules unscoped; they then name its own block.
  it('keeps every unscoped component rule inside its own block class', () => {
    const leaks = sourcePaths
      .filter((path) => path.endsWith('.vue'))
      .flatMap((path) =>
        unscopedLeaks(path, readFileSync(join(WEB_ROOT, path), 'utf8')),
      )
    expect(
      leaks,
      `An unscoped <style> block reaches the whole page: put the component's own block class in each selector's leading compound and at the start of each keyframe name, or scope the block. Leaking: ${leaks.join(', ')}`,
    ).toEqual([])
  })

  it('holds no test file in the source tree or the package root', () => {
    const misplaced = [
      ...sourcePaths.filter((path) => TEST_FILE.test(path)),
      ...rootEntries.filter((entry) => TEST_FILE.test(entry)),
    ]
    expect(
      misplaced,
      `Unit specs live in ${UNIT_HOME}/ mirroring ${SOURCE_ROOT}/, and browser suites in tests/{e2e,desktop,dev,update}/. Misplaced: ${misplaced.join(', ')}`,
    ).toEqual([])
  })
})

describe('unscoped style check', () => {
  it('tells a leaking unscoped rule from an owned or scoped one', () => {
    const component = (style: string) =>
      `<template><div class="rv-chip" /></template>\n${style}\n`
    const owned = component(
      `<style>\n.rv-chip, .rv-chip--on .rv-chip__label:hover {}\n.rv-button.rv-chip:hover:not(:disabled, [aria-disabled='true']) {}\n@keyframes rv-chip-in { from {} to {} }\n@media (width <= 36rem) { .rv-chip {} }\n</style>`,
    )
    const scoped = component(`<style scoped>\nbutton {}\n</style>`)
    const leaking = component(
      `<style>\n.rv-chipset {}\n.rv-chip, button {}\n@keyframes fade {}\n@media print { .rv-dialog {} }\n.rv-dialog .rv-chip {}\n.x:not(.a .rv-chip) {}\n</style>`,
    )

    expect(unscopedLeaks('src/RvChip.vue', owned)).toEqual([])
    expect(unscopedLeaks('src/RvChip.vue', scoped)).toEqual([])
    expect(unscopedLeaks('src/RvChip.vue', leaking)).toEqual([
      'src/RvChip.vue: @keyframes fade',
      'src/RvChip.vue: .rv-chipset',
      'src/RvChip.vue: button',
      'src/RvChip.vue: .rv-dialog',
      'src/RvChip.vue: .rv-dialog .rv-chip',
      'src/RvChip.vue: .x:not(.a .rv-chip)',
    ])
  })
})
