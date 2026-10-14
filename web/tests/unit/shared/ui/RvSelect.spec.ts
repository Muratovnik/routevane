import { describe, expect, it } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-vue'
import { defineComponent } from 'vue'

import type { ChoiceGroup, ChoiceOption } from '@/shared/ui/types'
import RvField from '@/shared/ui/RvField.vue'
import RvSelect from '@/shared/ui/RvSelect.vue'

import { contrast, inTheme, paintedToken } from './support/contrast'

const OPTIONS: ChoiceOption[] = [
  { label: 'As in settings', value: 'default' },
  { label: 'Off', value: 'off' },
  { label: 'Daily', value: 'daily' },
  { disabled: true, label: 'Weekly', value: 'weekly' },
]

const GROUPS: ChoiceGroup[] = [
  {
    key: 'router',
    label: 'Routers',
    options: [{ label: 'Keenetic', mono: '.bat', value: 'keenetic' }],
  },
  {
    key: 'app',
    label: 'Applications',
    options: [{ label: 'sing-box', mono: '.json', value: 'singbox' }],
  },
]

const renderSelect = (props: Record<string, unknown>) =>
  render(RvSelect, {
    props: {
      disabled: false,
      inputId: 'choice',
      modelValue: '',
      placeholder: 'Choose one',
      ...props,
    },
  })

describe('RvSelect', () => {
  it('reads the placeholder until something is chosen, then the choice', async () => {
    const screen = await renderSelect({ modelValue: '', options: OPTIONS })

    const trigger = screen.getByRole('combobox')
    await expect.element(trigger).toHaveTextContent('Choose one')

    // The list opens from the keyboard as well as from the pointer, which is the
    // half a native select gives for free and this one has to state.
    trigger.element().focus()
    await userEvent.keyboard('{Enter}')
    await screen.getByRole('option', { name: 'Daily' }).click()

    expect(screen.emitted('update:modelValue')).toEqual([['daily']])
    await screen.rerender({ modelValue: 'daily' })
    await expect.element(trigger).toHaveTextContent('Daily')
  })

  // The empty string is how every caller stores "nothing chosen", so it must
  // survive the round trip through a primitive that has its own idea of absence.
  it('shows the placeholder for an empty model and never emits one back', async () => {
    const screen = await renderSelect({ modelValue: '', options: OPTIONS })

    const trigger = screen.getByRole('combobox')
    await expect.element(trigger).toHaveTextContent('Choose one')
    // Not merely blank: the control reports that it is showing a placeholder
    // rather than a chosen value it read back as empty.
    await expect.element(trigger).toHaveAttribute('data-placeholder')

    trigger.element().focus()
    await userEvent.keyboard('{Enter}')
    await screen.getByRole('option', { name: 'Off' }).click()

    expect(screen.emitted('update:modelValue')).toEqual([['off']])
  })

  it('refuses a disabled option and a disabled control', async () => {
    const screen = await renderSelect({ modelValue: '', options: OPTIONS })

    const trigger = screen.getByRole('combobox')
    trigger.element().focus()
    await userEvent.keyboard('{Enter}')

    // A list option commits on pointer release, the way an operating system's
    // own select does, so a disabled one has to refuse that release.
    const weekly = screen.getByRole('option', { name: 'Weekly' })
    await expect.element(weekly).toHaveAttribute('data-disabled')
    await weekly.click({ force: true })
    expect(screen.emitted('update:modelValue')).toBeUndefined()

    await userEvent.keyboard('{Escape}')
    await screen.rerender({ disabled: true })
    await expect.element(trigger).toBeDisabled()
  })

  // A list with nothing in it cannot be opened: a control that opens on an
  // empty panel says there is something to choose when there is not.
  it('disables itself when there is nothing to choose', async () => {
    const screen = await renderSelect({ modelValue: '', options: [] })

    await expect.element(screen.getByRole('combobox')).toBeDisabled()
  })

  it('keeps grouped choices under their own names', async () => {
    const screen = await renderSelect({ groups: GROUPS, modelValue: '' })

    const trigger = screen.getByRole('combobox')
    trigger.element().focus()
    await userEvent.keyboard('{Enter}')

    await expect.element(screen.getByText('Routers')).toBeVisible()
    await expect.element(screen.getByText('Applications')).toBeVisible()
    await expect.element(screen.getByText('.bat')).toBeVisible()

    await screen.getByRole('option', { name: /^sing-box/ }).click()
    expect(screen.emitted('update:modelValue')).toEqual([['singbox']])
  })

  // A choice's own glyph is drawn beside its name in the list and, once it is
  // chosen, in the field; decoration only, so it is found by its test hook.
  it('draws the glyph a choice carries, in the list and in the field', async () => {
    const screen = await renderSelect({
      modelValue: '',
      options: [
        { icon: 'keenetic', label: 'Keenetic', value: 'keenetic' },
        { label: 'Plain', value: 'plain' },
      ],
    })

    const trigger = screen.getByRole('combobox')
    await expect.element(trigger.getByTestId('rv-icon')).not.toBeInTheDocument()
    trigger.element().focus()
    await userEvent.keyboard('{Enter}')
    await expect
      .element(
        screen.getByRole('option', { name: 'Keenetic' }).getByTestId('rv-icon'),
      )
      .toBeVisible()
    await expect
      .element(
        screen.getByRole('option', { name: 'Plain' }).getByTestId('rv-icon'),
      )
      .not.toBeInTheDocument()

    await screen.rerender({ modelValue: 'keenetic' })
    await expect.element(trigger.getByTestId('rv-icon')).toBeInTheDocument()
  })
})

// A panel named by its placeholder repeats what the trigger shows instead of
// saying what the choice is.
describe('RvSelect, panel name', () => {
  const FieldHost = defineComponent({
    components: { RvField, RvSelect },
    setup: () => ({ OPTIONS }),
    template: `
      <RvField input-id="delivery" label="Delivery">
        <template #default="{ describedBy }">
          <RvSelect
            :described-by="describedBy"
            input-id="delivery"
            model-value=""
            :options="OPTIONS"
            placeholder="Choose one"
          />
        </template>
      </RvField>`,
  })

  it('names its list by the label of its field', async () => {
    const screen = await render(FieldHost)

    screen.getByRole('combobox').element().focus()
    await userEvent.keyboard('{Enter}')
    await expect
      .element(screen.getByRole('listbox', { name: 'Delivery' }))
      .toBeVisible()
  })

  it('names its list by the heading it is pointed at', async () => {
    const Host = defineComponent({
      components: { RvSelect },
      setup: () => ({ OPTIONS }),
      template: `
        <div>
          <h2 id="schedule-title">Refresh</h2>
          <RvSelect
            input-id="schedule"
            labelled-by="schedule-title"
            model-value=""
            :options="OPTIONS"
            placeholder="Choose one"
          />
        </div>`,
    })
    const screen = await render(Host)

    screen.getByRole('combobox').element().focus()
    await userEvent.keyboard('{Enter}')
    await expect
      .element(screen.getByRole('listbox', { name: 'Refresh' }))
      .toBeVisible()
  })
})

// A trigger that cannot be used now has to read as unavailable beside the same
// trigger when it can: flat, dimmer, and drawn with the faint rule.
describe('RvSelect, unavailable', () => {
  it.each(['dark', 'light'] as const)(
    'draws a disabled trigger apart from an enabled one in the %s theme',
    async (theme) => {
      await inTheme(theme, async () => {
        const enabled = await renderSelect({
          inputId: 'choice-enabled',
          options: OPTIONS,
        })
        const disabled = await renderSelect({
          disabled: true,
          inputId: 'choice-disabled',
          options: OPTIONS,
        })

        const on = getComputedStyle(
          enabled.getByRole('combobox').first().element(),
        )
        const off = getComputedStyle(
          disabled.getByRole('combobox').last().element(),
        )
        expect(off.color).not.toBe(on.color)
        expect(off.color).toBe(paintedToken('--rv-color-ink-disabled'))
        expect(off.backgroundColor).not.toBe(on.backgroundColor)
        expect(off.boxShadow).not.toBe(on.boxShadow)
        expect(off.opacity).toBe('1')
        expect(
          contrast(off.color, paintedToken('--rv-color-canvas')),
        ).toBeGreaterThanOrEqual(3)
      })
    },
  )
})
