import { afterEach, describe, expect, it } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render, type RenderResult } from 'vitest-browser-vue'

import { useLocale } from '@/shared/i18n/useLocale'

import CompositionPriorityList from '@/entities/profile-composition/ui/CompositionPriorityList.vue'

// Priority is the order of the rows. Reading the rows is therefore reading the
// whole statement — and a row that carried a visible ordinal would say so here.
const rows = (screen: RenderResult<unknown>): string[] =>
  screen
    .getByRole('listitem')
    .elements()
    .map((item) => item.textContent?.trim() ?? '')

describe('CompositionPriorityList', () => {
  afterEach(() => useLocale().setLocale('en'))

  it('shows the composition without visible ordinals and supports keyboard reordering', async () => {
    const screen = await render(CompositionPriorityList, {
      props: {
        items: [
          { id: 'alpha', title: 'Alpha' },
          { id: 'beta', title: 'Beta' },
        ],
      },
    })

    await expect
      .element(
        screen.getByText('List order sets the priority', { exact: false }),
      )
      .toBeVisible()
    expect(rows(screen)).toEqual(['Alpha', 'Beta'])

    // The handle states the position a reader cannot see drawn.
    const handle = screen.getByRole('button', { name: /position 1/ })
    await expect.element(handle).toBeVisible()
    handle.element().focus()
    await userEvent.keyboard('{ArrowDown}')

    expect(screen.emitted('reorder')?.at(-1)).toEqual([['beta', 'alpha']])
  })

  it('shows overlap tags, an honest unknown state, and an overrideable context', async () => {
    const screen = await render(CompositionPriorityList, {
      props: {
        description: 'Library-wide order',
        items: [
          {
            id: 'alpha',
            overlaps: ['Beta'] as string[] | null,
            title: 'Alpha',
          },
          { id: 'beta', overlaps: null as string[] | null, title: 'Beta' },
        ],
        overlapPending: false,
        overlapUnavailable: false,
        retryable: true,
        title: 'Default priority',
      },
    })

    await expect.element(screen.getByText('Default priority')).toBeVisible()
    await expect.element(screen.getByText('Library-wide order')).toBeVisible()
    await expect.element(screen.getByText('Overlap: Beta')).toBeVisible()
    await expect.element(screen.getByText('Overlaps unknown')).toBeVisible()

    await screen.getByRole('button', { name: 'Retry' }).click()
    expect(screen.emitted('retry')).toHaveLength(1)

    await screen.rerender({ overlapPending: true })
    await expect
      .element(screen.getByText('Calculating overlaps', { exact: false }))
      .toBeVisible()

    await screen.rerender({ overlapPending: false, overlapUnavailable: true })
    await expect
      .element(screen.getByText('Choose a format to check overlaps'))
      .toBeVisible()
    // Nothing to retry while no format has been chosen.
    await expect
      .element(screen.getByRole('button', { name: 'Retry' }))
      .not.toBeInTheDocument()
  })

  it('disables its drag handles with the owning operation', async () => {
    const screen = await render(CompositionPriorityList, {
      props: { disabled: true, items: [{ id: 'alpha', title: 'Alpha' }] },
    })

    await expect
      .element(screen.getByRole('button', { name: /position 1/ }))
      .toBeDisabled()
  })
})
