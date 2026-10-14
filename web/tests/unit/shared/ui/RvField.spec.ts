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
    announceError: Boolean,
    error: { type: String, default: undefined },
    hint: { type: String, default: undefined },
    info: { type: String, default: undefined },
    inputId: { type: String, default: 'device-address' },
  },
  setup() {
    return { value: ref('') }
  },
  template: `
    <RvField :announce-error="announceError" :error="error" :hint="hint" :info="info" :input-id="inputId" label="Address">
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
  it('announces an opted-in error once and clears the live region when resolved', async () => {
    const screen = await render(TextField, {
      props: { announceError: true, error: '' },
    })
    await expect.element(screen.getByRole('alert')).not.toBeInTheDocument()
    await screen.rerender({ error: 'The file could not be read.' })
    await expect
      .element(screen.getByRole('alert'))
      .toHaveTextContent('The file could not be read.')
    expect(
      describedTexts(
        screen.getByLabelText('Address', { exact: true }).element(),
      ),
    ).toEqual(['The file could not be read.'])
    expect(
      screen.getByText('The file could not be read.', { exact: true }).all(),
    ).toHaveLength(1)
    await screen.rerender({ error: '' })
    await expect.element(screen.getByRole('alert')).not.toBeInTheDocument()
    await expect
      .element(screen.getByLabelText('Address', { exact: true }))
      .not.toHaveAttribute('aria-describedby')
  })

  it('keeps ordinary inline errors out of a live region', async () => {
    const screen = await render(TextField, {
      props: { error: 'Enter the address.' },
    })
    await expect.element(screen.getByText('Enter the address.')).toBeVisible()
    await expect.element(screen.getByRole('alert')).not.toBeInTheDocument()
  })
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

  // The hint is the library's description, drawn between the label and the
  // control, so it stays on screen while an error is shown under the control:
  // the format is what a refused value needs most. The description follows
  // what is on the screen, never an id that is not rendered.
  it('keeps the hint beside an error and points at both', async () => {
    const screen = await render(TextField, {
      props: { error: 'Enter the address.', hint: 'An address and a port.' },
    })

    const field = screen.getByLabelText('Address', { exact: true })
    await expect.element(field).toHaveAttribute('aria-invalid', 'true')
    expect(describedTexts(field.element()).toSorted()).toEqual([
      'An address and a port.',
      'Enter the address.',
    ])
    await expect
      .element(screen.getByText('An address and a port.'))
      .toBeVisible()

    await screen.rerender({ error: undefined })
    await expect.element(field).not.toHaveAttribute('aria-invalid')
    expect(describedTexts(field.element())).toEqual(['An address and a port.'])
  })

  // An explanation is offered from an informer beside the label, not drawn as
  // a line under it. The informer is outside the label, so the control keeps
  // the label's own words as its name, and the explanation is not announced
  // with every visit to the field.
  it('offers an explanation from an informer beside the label', async () => {
    const screen = await render(TextField, {
      props: { info: 'Subdomains are covered automatically.' },
    })

    const field = screen.getByLabelText('Address', { exact: true })
    await expect.element(field).toBeVisible()
    await expect
      .element(screen.getByText('Subdomains are covered automatically.'))
      .not.toBeInTheDocument()
    expect(field.element().getAttribute('aria-describedby')).toBeNull()

    await screen.getByRole('button', { name: 'About Address' }).click()
    await expect
      .element(screen.getByText('Subdomains are covered automatically.'))
      .toBeVisible()
    // Beside the label: the informer is not inside it, and it shares the
    // label's line rather than adding one.
    const label = screen.container.querySelector('label')!
    const informer = screen.getByRole('button', { name: 'About Address' })
    expect(label.contains(informer.element())).toBe(false)
    const [labelBox, informerBox] = [
      label.getBoundingClientRect(),
      informer.element().getBoundingClientRect(),
    ]
    expect(informerBox.left).toBeGreaterThanOrEqual(labelBox.right)
    expect(informerBox.top).toBeGreaterThanOrEqual(labelBox.top - 4)
    expect(informerBox.bottom).toBeLessThanOrEqual(labelBox.bottom + 4)
  })

  it('draws no informer when there is nothing to explain', async () => {
    const screen = await render(TextField)

    await expect
      .element(screen.getByRole('button', { name: 'About Address' }))
      .not.toBeInTheDocument()
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
