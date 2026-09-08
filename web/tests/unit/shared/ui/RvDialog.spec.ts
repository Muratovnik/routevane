import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { page, userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-vue'
import { defineComponent, h, ref, watch } from 'vue'

import RvDialog from '@/shared/ui/RvDialog.vue'

// The lock reserves the width the scrollbar was taking, so the page has to
// actually be long enough to have one.
const givePageAScrollbar = (): void => {
  document.body.style.minHeight = '400vh'
}

// The scrims that actually paint. A nested one keeps its layer, so a pointer
// landing outside still closes the panel, and drops its ground so a stack of
// two does not darken the page twice. The painted colour is what a reader sees,
// so that is what is measured rather than the class that produces it.
const grounds = (): Element[] =>
  page
    .getByTestId('rv-dialog-scrim')
    .elements()
    .filter(
      (scrim) => getComputedStyle(scrim).backgroundColor !== 'rgba(0, 0, 0, 0)',
    )

const scrimCount = (): number =>
  page.getByTestId('rv-dialog-scrim').all().length

// A host that owns the open state, because that is how every caller uses it:
// the dialog is opened by what the screen knows, not by a trigger inside it.
const Host = defineComponent({
  setup() {
    const open = ref(false)
    return { open }
  },
  render() {
    return h('div', [
      h(
        'button',
        {
          onClick: () => {
            this.open = true
          },
          type: 'button',
        },
        'Open',
      ),
      h(
        RvDialog,
        {
          closeLabel: 'Close',
          description: 'Edits here apply to every list.',
          open: this.open,
          title: 'Discord',
          variant: 'panel',
          'onUpdate:open': (value: boolean) => {
            this.open = value
          },
        },
        {
          default: () => h('button', { type: 'button' }, 'Inside'),
          footer: () => h('button', { type: 'button' }, 'Add to list'),
        },
      ),
    ])
  },
})

// Two panels of one stack. The host owns the state a real caller owns, and a
// prop change is a request to open or close: Escape closes a panel through the
// host, so the state cannot simply be the prop.
const StackHost = defineComponent({
  props: { panel: Boolean, sheet: Boolean },
  setup(props) {
    const panel = ref(props.panel)
    const sheet = ref(props.sheet)
    watch(
      () => props.panel,
      (next) => {
        panel.value = next
      },
    )
    watch(
      () => props.sheet,
      (next) => {
        sheet.value = next
      },
    )
    return () =>
      h('div', [
        h(
          RvDialog,
          {
            closeLabel: 'Close',
            open: sheet.value,
            title: 'Discord',
            variant: 'panel',
            'onUpdate:open': (value: boolean) => {
              sheet.value = value
            },
          },
          { default: () => h('button', { type: 'button' }, 'Inside') },
        ),
        h(
          RvDialog,
          {
            closeLabel: 'Close',
            open: panel.value,
            title: 'Add entries',
            variant: 'panel',
            'onUpdate:open': (value: boolean) => {
              panel.value = value
            },
          },
          { default: () => h('button', { type: 'button' }, 'Add') },
        ),
      ])
  },
})

describe('RvDialog', () => {
  beforeEach(() => {
    document.body.style.overflow = 'auto'
  })

  afterEach(() => {
    document.body.style.overflow = ''
    document.body.style.minHeight = ''
  })

  it('delegates the complete sheet transition to Nuxt UI', async () => {
    const screen = await render(RvDialog, {
      props: { closeLabel: 'Close', open: true, title: 'Google AI' },
    })

    const sheet = screen.getByRole('dialog')
    await expect.element(sheet).toHaveAccessibleName('Google AI')
    await expect.element(sheet).toHaveAttribute('data-side', 'right')
    await expect.element(sheet).toHaveAttribute('data-dismissible', 'true')
    await expect.element(sheet).toHaveAttribute('data-transition', 'true')
    // Cancels the library's compact width so the Routevane token owns it.
    await expect.element(sheet).toHaveClass('max-w-none')
    await expect.element(sheet).toHaveClass('rv-dialog--sheet')

    await screen.rerender({ open: false })
    await expect.element(sheet).not.toBeInTheDocument()
  })

  // The page behind a modal must not move, and it must be exactly as it was
  // when the modal goes: a lock that leaves its own styles behind is a bug the
  // next screen inherits.
  it('locks the page scroll while it is open and restores it afterwards', async () => {
    givePageAScrollbar()
    const layoutWidth = document.documentElement.clientWidth
    const screen = await render(Host)

    expect(document.body.style.overflow).toBe('auto')

    await screen.getByRole('button', { name: 'Open' }).click()

    await expect.element(screen.getByRole('dialog')).toBeVisible()
    expect(document.body.style.overflow).toBe('hidden')
    // The scrollbar's width is reserved so the page underneath does not shift
    // sideways the moment it stops scrolling. Measured as the layout width the
    // page keeps, because the number of pixels a scrollbar takes is the
    // browser's business and an overlay one takes none.
    expect(document.documentElement.clientWidth).toBe(layoutWidth)

    await userEvent.keyboard('{Escape}')

    await expect.element(screen.getByRole('dialog')).not.toBeInTheDocument()
    expect(document.body.style.overflow).toBe('auto')
    expect(document.body.style.paddingRight).toBe('')
  })

  // The keyboard goes in with the panel and comes back out with it.
  it('takes the focus into the panel and hands it back on close', async () => {
    const screen = await render(Host)

    const opener = screen.getByRole('button', { name: 'Open' })
    await expect.element(opener).toBeVisible()
    opener.element().focus()
    await expect.element(opener).toHaveFocus()

    await opener.click()

    const panel = screen.getByRole('dialog')
    await expect.element(panel).toBeVisible()
    expect(panel.element().contains(document.activeElement)).toBe(true)

    await userEvent.keyboard('{Escape}')

    await expect.element(opener).toHaveFocus()
  })

  // A panel that exists to be filled in starts in its field: the next keystroke
  // belongs there, not on the control that would throw the panel away.
  it('hands the keyboard to its first field when it has one', async () => {
    const screen = await render(RvDialog, {
      props: {
        closeLabel: 'Close',
        open: true,
        title: 'New category',
        variant: 'panel',
      },
      slots: {
        default: '<input aria-label="Name" type="text" />',
        footer: '<button type="button">Create</button>',
      },
    })

    await expect.element(screen.getByRole('dialog')).toBeVisible()
    await expect.element(screen.getByLabelText('Name')).toHaveFocus()
  })

  // The title and the standing caption are what a screen reader announces, so
  // both are wired to the panel rather than merely drawn on it.
  it('names and describes itself from its own title and description', async () => {
    const screen = await render(Host)

    await screen.getByRole('button', { name: 'Open' }).click()

    const panel = screen.getByRole('dialog')
    await expect.element(panel).toHaveAccessibleName('Discord')
    await expect
      .element(panel)
      .toHaveAccessibleDescription('Edits here apply to every list.')
    await expect
      .element(screen.getByRole('button', { name: 'Inside' }))
      .toBeVisible()
    await expect
      .element(screen.getByRole('button', { name: 'Add to list' }))
      .toBeVisible()
  })

  // The scrim is a way out, not decoration.
  it('closes when the pointer lands on the scrim', async () => {
    const screen = await render(Host)

    await screen.getByRole('button', { name: 'Open' }).click()
    const scrim = page.getByTestId('rv-dialog-scrim')
    await expect.element(scrim).toBeVisible()

    await scrim.click({ position: { x: 4, y: 4 } })

    await expect.element(screen.getByRole('dialog')).not.toBeInTheDocument()
  })

  it('keeps an in-flight decision open until its owner makes it dismissible', async () => {
    const screen = await render(RvDialog, {
      props: {
        closeLabel: 'Close',
        dismissible: false,
        open: true,
        title: 'Deleting list',
        variant: 'panel',
      },
      slots: { default: 'Deleting…' },
    })

    const close = screen.getByRole('button', { name: 'Close' })
    await expect.element(close).toBeDisabled()

    await userEvent.keyboard('{Escape}')
    expect(screen.emitted('update:open')).toBeUndefined()

    await screen.rerender({ dismissible: true })
    await close.click()
    expect(screen.emitted('update:open')?.at(-1)).toEqual([false])
  })

  // A panel opened over a sheet is one modal stack, and a stack has one ground.
  // Two scrims painting the same dimming made the page behind read as twice as
  // far away as it is.
  it('dims the page once for a stack of two', async () => {
    const screen = await render(StackHost, {
      props: { panel: false, sheet: true },
    })

    await expect
      .element(screen.getByRole('dialog', { name: 'Discord' }))
      .toBeVisible()

    await screen.rerender({ panel: true })
    await expect
      .element(screen.getByRole('dialog', { name: 'Add entries' }))
      .toBeVisible()

    expect(scrimCount()).toBe(2)
    expect(grounds()).toHaveLength(1)

    // Escape takes the panel off the stack; opening it again finds the sheet
    // still there, so it is still the second ground rather than the first.
    await userEvent.keyboard('{Escape}')
    await expect
      .element(screen.getByRole('dialog', { name: 'Add entries' }))
      .not.toBeInTheDocument()
    expect(scrimCount()).toBe(1)
    expect(grounds()).toHaveLength(1)

    await screen.rerender({ panel: false })
    await screen.rerender({ panel: true })
    await expect
      .element(screen.getByRole('dialog', { name: 'Add entries' }))
      .toBeVisible()
    expect(scrimCount()).toBe(2)
    expect(grounds()).toHaveLength(1)

    // With the stack emptied the count returns to zero, so a panel opened on
    // its own dims the page itself.
    await screen.rerender({ panel: false, sheet: false })
    await expect.element(screen.getByRole('dialog')).not.toBeInTheDocument()
    expect(scrimCount()).toBe(0)

    await screen.rerender({ panel: true })
    await expect
      .element(screen.getByRole('dialog', { name: 'Add entries' }))
      .toBeVisible()
    expect(scrimCount()).toBe(1)
    expect(grounds()).toHaveLength(1)
  })
})
