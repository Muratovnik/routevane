import { describe, expect, it } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-vue'
import { defineComponent, ref } from 'vue'

import RvTextInput from '@/shared/ui/RvTextInput.vue'

// The mono face as the browser resolves it, read off a probe rather than from
// the token's text, whose quoting the computed style rewrites.
const monoFace = (): string => {
  const probe = document.createElement('span')
  probe.style.fontFamily = 'var(--rv-font-mono)'
  document.body.append(probe)
  const face = getComputedStyle(probe).fontFamily
  probe.remove()
  return face
}

const renderInput = (props: Record<string, unknown> = {}) =>
  render(RvTextInput, {
    attrs: { 'aria-label': 'Address' },
    // Every prop a case changes later is named here, so the rerender that
    // changes it is typed against it.
    props: { invalid: false, modelValue: '', mono: false, ...props },
  })

describe('RvTextInput', () => {
  it('reports what is typed and shows what it is given', async () => {
    const screen = await renderInput({ modelValue: '192.0.2.1' })

    const field = screen.getByRole('textbox', { name: 'Address' })
    await expect.element(field).toHaveValue('192.0.2.1')
    await userEvent.fill(field, '192.0.2.7')
    expect(screen.emitted('update:modelValue')?.at(-1)).toEqual(['192.0.2.7'])
  })

  it('announces a refused value and asks the library for its error ring', async () => {
    const screen = await renderInput({
      describedBy: 'address-error',
      invalid: true,
    })

    const field = screen.getByRole('textbox', { name: 'Address' })
    await expect.element(field).toHaveAttribute('aria-invalid', 'true')
    await expect
      .element(field)
      .toHaveAttribute('aria-describedby', 'address-error')
    // The library draws the state; the facade's part is asking for it, which
    // the double states on the element it renders.
    const root = field.element().parentElement!
    expect(root.dataset.color).toBe('error')
    expect(root.dataset.highlight).toBe('true')
    expect(root.dataset.size).toBe('xl')

    await screen.rerender({ invalid: false })
    await expect.element(field).not.toHaveAttribute('aria-invalid')
    expect(root.dataset.color).toBe('primary')
  })

  it('refuses input while disabled', async () => {
    const screen = await renderInput({ disabled: true })

    await expect
      .element(screen.getByRole('textbox', { name: 'Address' }))
      .toBeDisabled()
  })

  // Only a value compared character by character takes the mono face, and the
  // face has to reach the input itself rather than stop at its frame.
  it('sets a value in the mono role only when asked to', async () => {
    const screen = await renderInput({ mono: true })
    const field = screen.getByRole('textbox', { name: 'Address' })
    expect(getComputedStyle(field.element()).fontFamily).toBe(monoFace())

    await screen.rerender({ mono: false })
    expect(getComputedStyle(field.element()).fontFamily).not.toBe(monoFace())
  })

  it('draws a search box as a search field with the search glyph before it', async () => {
    const screen = await render(RvTextInput, {
      attrs: { 'aria-label': 'Search lists' },
      props: { modelValue: '', type: 'search' },
    })

    const field = screen.getByRole('searchbox', { name: 'Search lists' })
    await expect.element(field).toBeVisible()
    // The glyph is decoration, hidden from a reader, so it is read off the
    // field's own frame rather than by a role.
    const glyph = field.element().parentElement!.querySelector('svg')
    expect(glyph?.getAttribute('aria-hidden')).toBe('true')
  })

  it('hands a host the input to move focus into', async () => {
    const screen = await render(
      defineComponent({
        components: { RvTextInput },
        setup() {
          const field = ref<{ element: () => HTMLInputElement | null }>()
          const value = ref('')
          const focus = () => field.value?.element()?.focus()
          return { field, focus, value }
        },
        template: `
          <RvTextInput ref="field" v-model="value" aria-label="Filter" />
          <button type="button" @click="focus">Filter the rows</button>`,
      }),
    )

    await screen.getByRole('button', { name: 'Filter the rows' }).click()
    await expect
      .element(screen.getByRole('textbox', { name: 'Filter' }))
      .toHaveFocus()
  })
})
