import { readFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

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
