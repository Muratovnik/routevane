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
    info: { type: String, default: undefined },
    infoLabel: { type: String, default: undefined },
    inputId: { type: String, default: 'device-address' },
  },
  setup() {
    return { value: ref('') }
  },
  template: `
    <RvField :announce-error="announceError" :error="error" :info="info" :info-label="infoLabel" :input-id="inputId" label="Address">
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
  // A live region is announced when its text changes, not when it is added with
  // its text, so the region has to stand before the error arrives and keep the
  // same node while the text comes and goes.
  it('keeps one live region standing and only changes its text for an opted-in error', async () => {
    const screen = await render(TextField, {
      props: { announceError: true, error: '' },
    })
    const region = screen.getByRole('alert')
    const standing = region.element()
    await expect.element(region).toBeEmptyDOMElement()
    await screen.rerender({ error: 'The file could not be read.' })
    await expect
      .element(screen.getByRole('alert'))
      .toHaveTextContent('The file could not be read.')
    expect(screen.getByRole('alert').element()).toBe(standing)
    expect(
      describedTexts(
        screen.getByLabelText('Address', { exact: true }).element(),
      ),
    ).toEqual(['The file could not be read.'])
    expect(
      screen.getByText('The file could not be read.', { exact: true }).all(),
    ).toHaveLength(1)
    await screen.rerender({ error: '' })
    await expect.element(screen.getByRole('alert')).toBeEmptyDOMElement()
    expect(screen.getByRole('alert').element()).toBe(standing)
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

  // An explanation is offered from an informer in the label's row, not drawn as
  // a line under it. The informer is outside the label, so the control keeps
  // the label's own words as its name; the explanation is the control's
  // description all the same, kept in the field and visually hidden, so it is
  // heard without opening the informer. The informer's own name never repeats
  // the label, so a query by label finds the field alone.
  it('offers an explanation from an informer in the label row and describes the control by it', async () => {
    const explanation = 'Subdomains are covered automatically.'
    const screen = await render(TextField, { props: { info: explanation } })

    const field = screen.getByLabelText('Address', { exact: true })
    await expect.element(field).toBeVisible()
    expect(screen.getByLabelText('Address').all()).toHaveLength(1)
    expect(describedTexts(field.element())).toEqual([explanation])
    // Kept in the field under the product's one visually-hidden role, so it is
    // in the description and not on the screen.
    await expect
      .element(screen.getByText(explanation))
      .toHaveClass('rv-visually-hidden')

    const informer = screen.getByRole('button', { name: 'More information' })
    await informer.click()
    await expect
      .element(screen.getByText(explanation, { exact: true }).last())
      .toBeVisible()
    // In the label's row: the informer is not inside the label, and it shares
    // the label's line rather than adding one.
    const label = screen.container.querySelector('label')!
    expect(label.contains(informer.element())).toBe(false)
    const [labelBox, informerBox] = [
      label.getBoundingClientRect(),
      informer.element().getBoundingClientRect(),
    ]
    expect(informerBox.left).toBeGreaterThanOrEqual(labelBox.right)
    // The informer's target is taller than the label's text and shares its
    // line, so its top rises past the label's by an amount the platform font's
    // line height decides (4px on Windows, 6px on Linux CI). Its middle stays
    // on the label's line on every platform; an informer pushed to a line of
    // its own would put it below the label.
    const informerMiddle = informerBox.top + informerBox.height / 2
    expect(informerMiddle).toBeGreaterThanOrEqual(labelBox.top)
    expect(informerMiddle).toBeLessThanOrEqual(labelBox.bottom)
  })

  it('describes the control by the explanation and the error together', async () => {
    const screen = await render(TextField, {
      props: {
        error: 'Enter the address.',
        info: 'An address and a port.',
      },
    })

    const field = screen.getByLabelText('Address', { exact: true })
    expect(describedTexts(field.element()).toSorted()).toEqual([
      'An address and a port.',
      'Enter the address.',
    ])
    await screen.rerender({ error: undefined })
    expect(describedTexts(field.element())).toEqual(['An address and a port.'])
  })

  // A form with two informers names each by what it explains, so a reader
  // moving between them can tell which is which.
  it('names its informer as the caller asks', async () => {
    const screen = await render(TextField, {
      props: {
        info: 'An address and a port.',
        infoLabel: 'What the address takes',
      },
    })

    await expect
      .element(screen.getByRole('button', { name: 'What the address takes' }))
      .toBeVisible()
    await expect
      .element(screen.getByRole('button', { name: 'More information' }))
      .not.toBeInTheDocument()
  })

  it('draws no informer when there is nothing to explain', async () => {
    const screen = await render(TextField)

    await expect
      .element(screen.getByRole('button', { name: 'More information' }))
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
