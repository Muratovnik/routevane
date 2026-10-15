import { describe, expect, it } from 'vitest'
import { page, userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-vue'
import { defineComponent } from 'vue'

import type { ChoiceOption } from '@/shared/ui/types'
import RvField from '@/shared/ui/RvField.vue'
import RvSearchSelect from '@/shared/ui/RvSearchSelect.vue'

import { contrast, inTheme, paintedToken } from './support/contrast'

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

// The painted style of a trigger that is named for the collection it filters.
const style = (name: string) =>
  getComputedStyle(
    page.getByRole('button', { name: `${name}: More` }).element(),
  )

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

  // The trigger's own content is its name when no label is drawn for it, and
  // a busy trigger keeps saying what it shows.
  it('is named by what it shows and states a busy trigger', async () => {
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
  // from a narrowed list is the one reported. With no field label to name it,
  // the panel falls back to the placeholder, the one thing it can say.
  it('closes on a single choice', async () => {
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

const LONG_VALUE =
  'A choice whose name is far longer than the narrow column its field stands in'

// A field the way every form writes one, in a column narrower than the longest
// choice it lists.
const FieldHost = defineComponent({
  components: { RvField, RvSearchSelect },
  props: {
    loading: Boolean,
    modelValue: { type: String, default: '' },
  },
  setup: () => ({ LONG_VALUE }),
  template: `
    <div style="width: 15rem">
      <RvField input-id="format" label="Format">
        <template #default="{ describedBy }">
          <RvSearchSelect
            :model-value="modelValue"
            :described-by="describedBy"
            input-id="format"
            :loading="loading"
            loading-label="Loading formats"
            :options="[
              { label: 'Short', value: 'short' },
              { label: LONG_VALUE, value: 'long' },
            ]"
            placeholder="Choose a format"
            size="default"
          />
        </template>
      </RvField>
    </div>`,
})

describe('RvSearchSelect, in a labelled field', () => {
  // A bound value read as the opposite fact while the control was busy: the
  // trigger swapped it for the placeholder.
  it('keeps the chosen value visible while busy and says it is busy', async () => {
    const screen = await render(FieldHost, {
      props: { loading: true, modelValue: 'short' },
    })

    const trigger = screen.getByRole('button', { name: /Format/ })
    await expect.element(trigger).toHaveTextContent('Short')
    await expect.element(trigger).not.toHaveTextContent('Choose a format')
    await expect.element(trigger).toHaveAttribute('aria-busy', 'true')
    await expect.element(trigger).toBeDisabled()
  })

  it('says it is loading while nothing is chosen yet', async () => {
    const screen = await render(FieldHost, { props: { loading: true } })

    await expect
      .element(screen.getByRole('button', { name: /Format/ }))
      .toHaveTextContent('Loading formats')
  })

  // The field's label named the trigger on its own, so a reader heard the
  // field and never what was chosen in it.
  it('is named by its field label and the value it holds', async () => {
    const screen = await render(FieldHost, { props: { modelValue: 'short' } })

    await expect
      .element(screen.getByRole('button', { name: 'Format Short' }))
      .toBeVisible()
    await screen.rerender({ modelValue: '' })
    await expect
      .element(screen.getByRole('button', { name: 'Format Choose a format' }))
      .toBeVisible()
  })

  // A panel named by its placeholder repeats the trigger's own content instead
  // of saying what it chooses.
  it('names its panel by the field label, not by the placeholder', async () => {
    const screen = await render(FieldHost, { props: { modelValue: 'short' } })

    await screen.getByRole('button', { name: /Format/ }).click()
    await expect
      .element(screen.getByRole('dialog', { name: 'Format' }))
      .toBeVisible()
  })
})

// "Add a format" with nothing left to add must not look like the enabled
// trigger beside it: flat, dimmer, and drawn with the faint rule.
describe('RvSearchSelect, unavailable', () => {
  it.each(['dark', 'light'] as const)(
    'draws a disabled trigger apart from an enabled one in the %s theme',
    async (theme) => {
      await inTheme(theme, async () => {
        await renderFilter({ label: 'Open formats' })
        await renderFilter({ disabled: true, label: 'Closed formats' })

        const on = style('Open formats')
        const off = style('Closed formats')
        expect(off.color).toBe(paintedToken('--rv-color-ink-disabled'))
        expect(off.color).not.toBe(on.color)
        expect(off.backgroundColor).not.toBe(on.backgroundColor)
        expect(off.boxShadow).not.toBe(on.boxShadow)
        expect(
          contrast(off.color, paintedToken('--rv-color-canvas')),
        ).toBeGreaterThanOrEqual(3)
      })
    },
  )
})

// Among filter chips the trigger rests as an unpressed chip does and takes the
// pressed chip's accent only while it names a chosen value.
describe('RvSearchSelect, as a chip', () => {
  it.each(['dark', 'light'] as const)(
    'rests in the chip ink and takes the accent when marked in the %s theme',
    async (theme) => {
      await inTheme(theme, async () => {
        await renderFilter({ chip: true, label: 'Resting' })
        await renderFilter({ chip: true, label: 'Chosen', marked: true })
        await renderFilter({ label: 'Library' })

        const rest = style('Resting')
        const chosen = style('Chosen')
        expect(rest.color).toBe(paintedToken('--rv-color-ink-muted'))
        expect(rest.backgroundColor).toBe(paintedToken('--rv-color-surface'))
        expect(chosen.color).toBe(paintedToken('--rv-color-accent-ink'))
        expect(chosen.backgroundColor).toBe(
          paintedToken('--rv-color-accent-quiet'),
        )
        // Without the chip look the trigger keeps the library's own ink.
        expect(style('Library').color).not.toBe(rest.color)
      })
    },
  )
})
