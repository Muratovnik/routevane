import { readFileSync, readdirSync } from 'node:fs'
import { extname, join } from 'node:path'
import { describe, expect, it } from 'vitest'

const stylesRoot = join(process.cwd(), 'src')
const tokensPath = join(stylesRoot, 'assets', 'styles', 'tokens.css')

function sourceFiles(root: string): string[] {
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
