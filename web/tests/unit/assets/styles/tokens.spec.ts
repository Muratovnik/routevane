import { readFileSync, readdirSync } from 'node:fs'
import { extname, join } from 'node:path'
import { describe, expect, it } from 'vitest'

import { contrast } from '../../shared/ui/support/contrast'

const stylesRoot = join(process.cwd(), 'src')
const tokensPath = join(stylesRoot, 'assets', 'styles', 'tokens.css')

const sourceFiles = (root: string): string[] => {
  const files: string[] = []
  const visit = (directory: string): void => {
    for (const entry of readdirSync(directory, { withFileTypes: true })) {
      const path = join(directory, entry.name)
      if (entry.isDirectory()) visit(path)
      else if (['.css', '.vue'].includes(extname(entry.name))) files.push(path)
    }
  }
  visit(root)
  return files
}

describe('design token contract', () => {
  it('resolves every Routevane custom property used by the interface', () => {
    const tokens = readFileSync(tokensPath, 'utf8')
    const definitions = new Set(
      [...tokens.matchAll(/(--rv-[a-z0-9-]+)\s*:/g)].map((match) => match[1]),
    )
    const unresolved = new Set<string>()

    for (const path of sourceFiles(stylesRoot)) {
      const source = readFileSync(path, 'utf8')
      for (const match of source.matchAll(/var\((--rv-[a-z0-9-]+)/g)) {
        const token = match[1]
        if (token !== undefined && !definitions.has(token))
          unresolved.add(token)
      }
    }

    expect([...unresolved].sort()).toEqual([])
  })
})

// Dark is the document's own scene: the roles written before the light blocks
// begin. A light block restates them, so the first declaration is the dark one.
const darkRole = (name: string): string => {
  const tokens = readFileSync(tokensPath, 'utf8')
  const dark = tokens.slice(0, tokens.indexOf('@media (prefers-color-scheme'))
  const declared = new RegExp(String.raw`${name}:\s*(#[0-9a-f]{6})`).exec(
    dark,
  )?.[1]
  if (declared === undefined) throw new Error(`${name} is not a dark hex role`)
  return declared
}

const opaque = (hex: string): string =>
  `rgb(${[1, 3, 5].map((at) => Number.parseInt(hex.slice(at, at + 2), 16)).join(' ')})`

describe('dark edge roles', () => {
  it('keeps the strong edge apart from the hairline', () => {
    expect(darkRole('--rv-color-rule-strong')).not.toBe(
      darkRole('--rv-color-rule'),
    )
  })

  // A disabled control falls back from the strong edge to the hairline, and a
  // switch or a secondary button wears the strong one: the step between them
  // has to be one an eye can take on every ground those controls stand on.
  it.each(['canvas', 'surface', 'surface-muted'])(
    'draws the strong edge at least half a step above the hairline on %s',
    (ground) => {
      const surface = opaque(darkRole(`--rv-color-${ground}`))
      const hairline = contrast(opaque(darkRole('--rv-color-rule')), surface)
      const strong = contrast(
        opaque(darkRole('--rv-color-rule-strong')),
        surface,
      )
      expect(strong).toBeGreaterThan(hairline + 0.5)
    },
  )
})
