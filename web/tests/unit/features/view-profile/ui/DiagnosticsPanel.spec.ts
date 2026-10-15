import { describe, expect, it } from 'vitest'
import { render, type RenderResult } from 'vitest-browser-vue'

import type { DiagnosticRule } from '@/shared/api/artifacts'

import DiagnosticsPanel from '@/features/view-profile/ui/DiagnosticsPanel.vue'

const TITLES: Record<string, string> = { b: 'Beta', a: 'Alpha' }

const rule = (
  listID: string,
  value: string,
  excluded = false,
): DiagnosticRule => ({
  excluded,
  listID,
  reasons: [excluded ? 'unsupported_by_target' : 'manual_rule'],
  value,
})

const renderPanel = (rules: DiagnosticRule[]) =>
  render(DiagnosticsPanel, {
    props: {
      listTitle: (id: string) => TITLES[id] ?? id,
      reasonLabel: (code: string) => code,
      rules,
    },
  })

// A body row is a row that holds cells; read each one's state, list and value.
const bodyRows = (screen: RenderResult<unknown>): string[][] =>
  screen
    .getByRole('row')
    .elements()
    .map((row) =>
      [...row.querySelectorAll('td')].map(
        (cell) => cell.textContent?.trim() ?? '',
      ),
    )
    .filter((cells) => cells.length > 1)
    .map((cells) => cells.slice(0, -1))

describe('DiagnosticsPanel', () => {
  it('reads addresses as numbers, names after them, and the list as the tie-break', async () => {
    // The build's own order: by kind, then by value as text.
    const screen = await renderPanel([
      rule('a', 'youtube.com'),
      rule('b', '10.0.0.10'),
      rule('b', '10.0.0.2'),
      rule('a', '2001:db8::10'),
      rule('a', '2001:db8::2'),
      rule('b', '10.0.0.0/8'),
      rule('b', '127.0.0.1', true),
      rule('a', '127.0.0.1', true),
    ])

    await expect
      .poll(() => bodyRows(screen))
      .toEqual([
        ['Included', 'Beta', '10.0.0.0/8'],
        ['Included', 'Beta', '10.0.0.2'],
        ['Included', 'Beta', '10.0.0.10'],
        ['Excluded', 'Alpha', '127.0.0.1'],
        ['Excluded', 'Beta', '127.0.0.1'],
        ['Included', 'Alpha', '2001:db8::2'],
        ['Included', 'Alpha', '2001:db8::10'],
        ['Included', 'Alpha', 'youtube.com'],
      ])
  })

  it('shows only the excluded rules when asked, and every rule again after', async () => {
    const screen = await renderPanel([
      rule('a', 'example.com'),
      rule('a', '192.0.2.1'),
      rule('b', '127.0.0.1', true),
      rule('a', 'localhost', true),
    ])

    // The words of an option take the pointer, as they do for the operator.
    await screen.getByText('Excluded 2', { exact: true }).click()
    await expect
      .element(screen.getByRole('radio', { name: 'Excluded 2' }))
      .toBeChecked()
    await expect
      .poll(() => bodyRows(screen))
      .toEqual([
        ['Excluded', 'Beta', '127.0.0.1'],
        ['Excluded', 'Alpha', 'localhost'],
      ])

    await screen.getByText('All', { exact: true }).click()
    await expect
      .element(screen.getByRole('radio', { name: 'All', exact: true }))
      .toBeChecked()
    await expect.poll(() => bodyRows(screen)).toHaveLength(4)
  })

  it('offers no state filter when the build excluded nothing', async () => {
    const screen = await renderPanel([rule('a', 'example.com')])

    await expect
      .poll(() => bodyRows(screen))
      .toEqual([['Alpha', 'example.com']])
    expect(screen.getByRole('radio').all()).toEqual([])
  })
})
