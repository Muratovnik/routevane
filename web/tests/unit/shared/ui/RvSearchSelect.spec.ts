import { describe, expect, it } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-vue'

import type { ChoiceOption } from '@/shared/ui/types'
import RvSearchSelect from '@/shared/ui/RvSearchSelect.vue'

const CATEGORIES: ChoiceOption[] = [
  { label: 'Communication', value: 'communication' },
  { label: 'Video', value: 'video' },
  { label: 'Games', value: 'games' },
]

// The category filter: the compact size, a multiple choice, named by the
// collection it filters and what it currently shows.
const renderFilter = (props: Record<string, unknown> = {}) =>
  render(RvSearchSelect<string[]>, {
    props: {
      emptyLabel: 'No matching categories',
      label: 'Categories',
      modelValue: [] as string[],
      options: CATEGORIES,
      placeholder: 'More',
      searchLabel: 'Find a category',
      ...props,
    },
  })

describe('RvSearchSelect, compact', () => {
  it('names its trigger by the collection and what it shows', async () => {
    const screen = await renderFilter({ triggerLabel: 'Video, Games' })

    await expect
      .element(screen.getByRole('button', { name: 'Categories: Video, Games' }))
      .toHaveAttribute('aria-haspopup', 'dialog')
  })

  // Choosing applies at once and keeps the panel open for the next category;
  // the selection it reports is the whole set, not a single value.
  it('toggles a multiple choice and keeps the panel open', async () => {
    const screen = await renderFilter({ modelValue: ['video'] })

    await screen.getByRole('button', { name: 'Categories: More' }).click()
    const panel = screen.getByRole('dialog', { name: 'Categories' })
    await expect.element(panel).toBeVisible()
    await expect
      .element(screen.getByRole('option', { name: 'Video' }))
      .toHaveAttribute('aria-selected', 'true')

    await screen.getByRole('option', { name: 'Games' }).click()
    expect(screen.emitted('update:modelValue')?.at(-1)).toEqual([
      ['video', 'games'],
    ])
    await expect.element(panel).toBeVisible()

    await screen.getByRole('option', { name: 'Video' }).click()
    expect(screen.emitted('update:modelValue')?.at(-1)).toEqual([[]])
  })

  it('narrows by name, states an empty result and forgets it on close', async () => {
    const screen = await renderFilter()

    const trigger = screen.getByRole('button', { name: 'Categories: More' })
    await trigger.click()
    const search = screen.getByRole('textbox', { name: 'Find a category' })
    await expect.element(search).toHaveFocus()
    await search.fill('vid')
    expect(screen.getByRole('option').all()).toHaveLength(1)
    await search.fill('nothing here')
    await expect
      .element(screen.getByText('No matching categories'))
      .toBeVisible()

    // Escape keeps the filter it had and hands the keyboard back.
    await userEvent.keyboard('{Escape}')
    await expect
      .element(screen.getByRole('dialog', { name: 'Categories' }))
      .not.toBeInTheDocument()
    await expect.element(trigger).toHaveFocus()
    expect(screen.emitted('update:modelValue')).toBeUndefined()
    await trigger.click()
    await expect
      .element(screen.getByRole('textbox', { name: 'Find a category' }))
      .toHaveValue('')
  })

  it('refuses to open with nothing to choose, and refuses a disabled choice', async () => {
    const empty = await renderFilter({ options: [] })
    await expect
      .element(empty.getByRole('button', { name: 'Categories: More' }))
      .toBeDisabled()
    empty.unmount()

    const screen = await renderFilter({
      options: [{ disabled: true, label: 'Blocked', value: 'blocked' }],
    })
    await screen.getByRole('button', { name: 'Categories: More' }).click()
    await screen.getByRole('option', { name: 'Blocked' }).click({ force: true })
    expect(screen.emitted('update:modelValue')).toBeUndefined()
  })

  it('draws a caller option through its own slot', async () => {
    const screen = await render(RvSearchSelect<string[]>, {
      props: {
        label: 'Categories',
        modelValue: [] as string[],
        options: CATEGORIES,
        placeholder: 'More',
      },
      slots: { option: '<b>Category row</b>' },
    })

    await screen.getByRole('button', { name: 'Categories: More' }).click()
    expect(screen.getByText('Category row').all()).toHaveLength(3)
  })
})

describe('RvSearchSelect, field', () => {
  const renderField = (props: Record<string, unknown> = {}) =>
    render(RvSearchSelect<string>, {
      props: {
        inputId: 'target',
        invalid: false,
        loading: false,
        loadingLabel: 'Loading formats',
        modelValue: '',
        options: CATEGORIES,
        placeholder: 'Choose a format',
        size: 'default',
        ...props,
      },
    })

  // With a field label the trigger takes its name from that label, so the
  // facade names it only while it is busy and its content says so.
  it('leaves its name to the field label and states a busy trigger', async () => {
    const screen = await renderField()

    const trigger = screen.getByRole('button', { name: 'Choose a format' })
    await expect.element(trigger).not.toHaveAttribute('aria-label')
    await screen.rerender({ loading: true })
    const busy = screen.getByRole('button', { name: 'Loading formats' })
    await expect.element(busy).toBeDisabled()
    await expect.element(busy).toHaveAttribute('aria-busy', 'true')
  })

  it('asks the library for its error ring on a refused value', async () => {
    const screen = await renderField({
      describedBy: 'target-error',
      invalid: true,
    })

    const trigger = screen.getByRole('button', { name: 'Choose a format' })
    await expect.element(trigger).toHaveAttribute('aria-invalid', 'true')
    await expect
      .element(trigger)
      .toHaveAttribute('aria-describedby', 'target-error')
    expect(trigger.element().dataset.color).toBe('error')
    expect(trigger.element().dataset.size).toBe('xl')
    expect(trigger.element().dataset.variant).toBe('outline')
  })

  // A single choice is made once: the panel closes on it, and the choice made
  // from a narrowed list is the one reported.
  it('closes on a single choice and names its panel for what it chooses', async () => {
    const screen = await renderField()

    await screen.getByRole('button', { name: 'Choose a format' }).click()
    await expect
      .element(screen.getByRole('dialog', { name: 'Choose a format' }))
      .toBeVisible()
    await screen.getByRole('textbox', { name: 'Search' }).fill('vid')
    await screen.getByRole('option', { name: 'Video' }).click()
    expect(screen.emitted('update:modelValue')).toEqual([['video']])
    await expect
      .element(screen.getByRole('dialog', { name: 'Choose a format' }))
      .not.toBeInTheDocument()
  })
})
