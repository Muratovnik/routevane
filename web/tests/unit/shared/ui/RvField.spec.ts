import { describe, expect, it } from 'vitest'
import { render } from 'vitest-browser-vue'
import { defineComponent, ref } from 'vue'

import RvField from '@/shared/ui/RvField.vue'
import RvSelect from '@/shared/ui/RvSelect.vue'
import RvTextInput from '@/shared/ui/RvTextInput.vue'

// A host the way every form writes one: the field hands its control the ids
// and the invalid state the control is to announce.
const TextField = defineComponent({
  components: { RvField, RvTextInput },
  props: {
    error: { type: String, default: undefined },
    hint: { type: String, default: undefined },
    inputId: { type: String, default: 'device-address' },
  },
  setup() {
    return { value: ref('') }
  },
  template: `
    <RvField :error="error" :hint="hint" :input-id="inputId" label="Address">
      <template #default="{ describedBy, invalid }">
        <RvTextInput
          v-model="value"
          :described-by="describedBy"
          :input-id="inputId"
          :invalid="invalid"
        />
      </template>
    </RvField>`,
})

const describedTexts = (element: Element): string[] =>
  (element.getAttribute('aria-describedby') ?? '')
    .split(' ')
    .filter((id) => id !== '')
    .map((id) => document.getElementById(id)?.textContent?.trim() ?? '')

describe('RvField', () => {
  it('names its control by the id the caller gave it', async () => {
    const screen = await render(TextField)

    const field = screen.getByLabelText('Address', { exact: true })
    await expect.element(field).toHaveAttribute('id', 'device-address')
    await expect.element(field).not.toHaveAttribute('aria-invalid')
  })

  it('describes the control by its hint while nothing is wrong', async () => {
    const screen = await render(TextField, {
      props: { hint: 'An address and a port.' },
    })

    const field = screen.getByLabelText('Address', { exact: true })
    expect(describedTexts(field.element())).toEqual(['An address and a port.'])
  })

  // The hint is the library's help text, and help gives way to the error in
  // the same place under the control. The description must follow what is on
  // the screen, never an id that is not rendered.
  it('replaces the hint with the error and points at the error alone', async () => {
    const screen = await render(TextField, {
      props: { error: 'Enter the address.', hint: 'An address and a port.' },
    })

    const field = screen.getByLabelText('Address', { exact: true })
    await expect.element(field).toHaveAttribute('aria-invalid', 'true')
    expect(describedTexts(field.element())).toEqual(['Enter the address.'])
    await expect
      .element(screen.getByText('An address and a port.'))
      .not.toBeInTheDocument()

    await screen.rerender({ error: undefined })
    await expect.element(field).not.toHaveAttribute('aria-invalid')
    expect(describedTexts(field.element())).toEqual(['An address and a port.'])
  })

  it('treats an empty error as no error', async () => {
    const screen = await render(TextField, { props: { error: '' } })

    const field = screen.getByLabelText('Address', { exact: true })
    await expect.element(field).not.toHaveAttribute('aria-invalid')
    await expect.element(field).not.toHaveAttribute('aria-describedby')
  })

  // A choice is drawn on Reka rather than by the library, so it cannot
  // register itself with the field; the label must still name it.
  it('labels a control that is not a library input', async () => {
    const screen = await render(
      defineComponent({
        components: { RvField, RvSelect },
        setup: () => ({ value: ref('') }),
        template: `
          <RvField error="Pick a format." input-id="feed-format" label="Format">
            <template #default="{ describedBy, invalid }">
              <RvSelect
                v-model="value"
                :described-by="describedBy"
                input-id="feed-format"
                :invalid="invalid"
                :options="[{ label: 'Text', value: 'text' }]"
                placeholder="Choose"
              />
            </template>
          </RvField>`,
      }),
    )

    const choice = screen.getByRole('combobox', { name: 'Format' })
    await expect.element(choice).toHaveAttribute('id', 'feed-format')
    await expect.element(choice).toHaveAttribute('aria-invalid', 'true')
    expect(describedTexts(choice.element())).toEqual(['Pick a format.'])
  })

  it('moves the label to a new id with its control', async () => {
    const screen = await render(TextField)

    await screen.rerender({ inputId: 'device-account' })
    await expect
      .element(screen.getByLabelText('Address', { exact: true }))
      .toHaveAttribute('id', 'device-account')
  })
})
