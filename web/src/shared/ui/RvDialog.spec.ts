import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { defineComponent, h, ref } from 'vue'

import RvDialog from '@/shared/ui/RvDialog.vue'

// The lock reserves the width the scrollbar was taking, which it can only work
// out from a viewport that states one. A document with no layout states none.
function stubViewportWidth(clientWidth: number): void {
  Object.defineProperty(document.documentElement, 'clientWidth', {
    configurable: true,
    value: clientWidth,
  })
}

async function settle(): Promise<void> {
  await flushPromises()
  await new Promise((resolve) => {
    setTimeout(resolve, 0)
  })
}

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
          class: 'host__opener',
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
          onUpdate: undefined,
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

function dialog(): HTMLElement | null {
  return document.body.querySelector<HTMLElement>('[role="dialog"]')
}

function scrims(): HTMLElement[] {
  return [...document.body.querySelectorAll<HTMLElement>('.rv-dialog__scrim')]
}

// The scrims that actually paint: a nested one keeps its layer and drops its
// ground.
function dimming(): HTMLElement[] {
  return scrims().filter(
    (scrim) => !scrim.classList.contains('rv-dialog__scrim--nested'),
  )
}

describe('RvDialog', () => {
  let host: ReturnType<typeof mount> | null = null

  beforeEach(() => {
    stubViewportWidth(1009)
    document.body.style.overflow = 'auto'
  })

  afterEach(() => {
    host?.unmount()
    host = null
    document.body.style.overflow = ''
    document.body.innerHTML = ''
  })

  it('delegates the sheet to Nuxt UI without its CSP-unsafe keyframes', async () => {
    const USlideoverStub = defineComponent({
      props: {
        dismissible: Boolean,
        open: Boolean,
        side: String,
        title: String,
        transition: Boolean,
      },
      setup(_props, { slots }) {
        return () => h('div', { 'data-library-sheet': '' }, slots.body?.())
      },
    })
    const wrapper = mount(RvDialog, {
      props: {
        closeLabel: 'Close',
        open: true,
        title: 'Google AI',
      },
      global: { components: { USlideover: USlideoverStub } },
    })
    host = wrapper

    const sheet = wrapper.getComponent(USlideoverStub)
    expect(sheet.props()).toMatchObject({
      dismissible: true,
      open: true,
      side: 'right',
      title: 'Google AI',
      transition: false,
    })
    await wrapper.setProps({ open: false })
    expect(sheet.props('transition')).toBe(false)
    expect(wrapper.text()).toBe('')
  })

  // The page behind a modal must not move, and it must be exactly as it was
  // when the modal goes: a lock that leaves its own styles behind is a bug the
  // next screen inherits.
  it('locks the page scroll while it is open and restores it afterwards', async () => {
    const wrapper = mount(Host, { attachTo: document.body })
    host = wrapper

    expect(document.body.style.overflow).toBe('auto')

    await wrapper.get('.host__opener').trigger('click')
    await settle()

    expect(dialog()).not.toBeNull()
    expect(document.body.style.overflow).toBe('hidden')
    // The scrollbar's width is reserved so the page underneath does not shift
    // sideways the moment it stops scrolling.
    expect(document.body.style.paddingRight).toBe('15px')

    document.dispatchEvent(
      new KeyboardEvent('keydown', { bubbles: true, key: 'Escape' }),
    )
    await settle()

    expect(dialog()).toBeNull()
    expect(document.body.style.overflow).toBe('auto')
    expect(document.body.style.paddingRight).toBe('')
  })

  // The keyboard goes in with the panel and comes back out with it.
  it('takes the focus into the panel and hands it back on close', async () => {
    const wrapper = mount(Host, { attachTo: document.body })
    host = wrapper

    const opener = wrapper.get<HTMLButtonElement>('.host__opener').element
    opener.focus()
    expect(document.activeElement).toBe(opener)

    await wrapper.get('.host__opener').trigger('click')
    await settle()

    const panel = dialog()
    expect(panel).not.toBeNull()
    expect(panel?.contains(document.activeElement)).toBe(true)

    document.dispatchEvent(
      new KeyboardEvent('keydown', { bubbles: true, key: 'Escape' }),
    )
    await settle()

    expect(document.activeElement).toBe(opener)
  })

  // A panel that exists to be filled in starts in its field: the next keystroke
  // belongs there, not on the control that would throw the panel away.
  it('hands the keyboard to its first field when it has one', async () => {
    const FormHost = defineComponent({
      setup() {
        const open = ref(true)
        return { open }
      },
      render() {
        return h(
          RvDialog,
          {
            closeLabel: 'Close',
            open: this.open,
            title: 'New category',
            variant: 'panel',
            'onUpdate:open': (value: boolean) => {
              this.open = value
            },
          },
          {
            default: () => h('input', { 'aria-label': 'Name', type: 'text' }),
            footer: () => h('button', { type: 'button' }, 'Create'),
          },
        )
      },
    })
    const wrapper = mount(FormHost, { attachTo: document.body })
    host = wrapper
    await settle()

    const panel = dialog()
    expect(panel).not.toBeNull()
    expect(document.activeElement).toBe(
      panel?.querySelector('input[aria-label="Name"]'),
    )
  })

  // The title and the standing caption are what a screen reader announces, so
  // both are wired to the panel rather than merely drawn on it.
  it('names and describes itself from its own title and description', async () => {
    const wrapper = mount(Host, { attachTo: document.body })
    host = wrapper

    await wrapper.get('.host__opener').trigger('click')
    await settle()

    const panel = dialog()
    const titleID = panel?.getAttribute('aria-labelledby') ?? ''
    const descriptionID = panel?.getAttribute('aria-describedby') ?? ''

    expect(document.getElementById(titleID)?.textContent).toBe('Discord')
    expect(document.getElementById(descriptionID)?.textContent?.trim()).toBe(
      'Edits here apply to every list.',
    )
    expect(panel?.textContent).toContain('Inside')
    expect(panel?.textContent).toContain('Add to list')
  })

  // The scrim is a way out, not decoration.
  it('closes when the pointer lands on the scrim', async () => {
    const wrapper = mount(Host, { attachTo: document.body })
    host = wrapper

    await wrapper.get('.host__opener').trigger('click')
    await settle()
    const scrim = document.body.querySelector('.rv-dialog__scrim')
    expect(scrim).not.toBeNull()

    scrim?.dispatchEvent(new MouseEvent('pointerdown', { bubbles: true }))
    scrim?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await settle()

    expect(dialog()).toBeNull()
  })

  it('keeps an in-flight decision open until its owner makes it dismissible', async () => {
    const wrapper = mount(RvDialog, {
      attachTo: document.body,
      props: {
        closeLabel: 'Close',
        dismissible: false,
        open: true,
        title: 'Deleting list',
        variant: 'panel',
      },
      slots: { default: 'Deleting…' },
      global: { stubs: { RvIcon: true } },
    })
    host = wrapper
    await settle()

    const close =
      document.body.querySelector<HTMLButtonElement>('.rv-dialog__close')
    expect(close?.disabled).toBe(true)
    document.dispatchEvent(
      new KeyboardEvent('keydown', { bubbles: true, key: 'Escape' }),
    )
    await settle()
    expect(wrapper.emitted('update:open')).toBeUndefined()

    await wrapper.setProps({ dismissible: true })
    close?.click()
    await settle()
    expect(wrapper.emitted('update:open')?.at(-1)).toEqual([false])
  })

  // A panel opened over a sheet is one modal stack, and a stack has one ground.
  // Two scrims painting the same dimming made the page behind read as twice as
  // far away as it is.
  it('dims the page once for a stack of two', async () => {
    const StackHost = defineComponent({
      setup() {
        const panel = ref(false)
        const sheet = ref(false)
        return { panel, sheet }
      },
      render() {
        return h('div', [
          h(
            RvDialog,
            {
              closeLabel: 'Close',
              open: this.sheet,
              title: 'Discord',
              variant: 'panel',
              'onUpdate:open': (value: boolean) => {
                this.sheet = value
              },
            },
            { default: () => h('button', { type: 'button' }, 'Inside') },
          ),
          h(
            RvDialog,
            {
              closeLabel: 'Close',
              open: this.panel,
              title: 'Add entries',
              variant: 'panel',
              'onUpdate:open': (value: boolean) => {
                this.panel = value
              },
            },
            { default: () => h('button', { type: 'button' }, 'Add') },
          ),
        ])
      },
    })

    const wrapper = mount(StackHost, { attachTo: document.body })
    host = wrapper

    wrapper.vm.sheet = true
    await settle()
    wrapper.vm.panel = true
    await settle()

    expect(scrims()).toHaveLength(2)
    expect(dimming()).toHaveLength(1)

    // Escape takes the panel off the stack; opening it again finds the sheet
    // still there, so it is still the second ground rather than the first.
    document.dispatchEvent(
      new KeyboardEvent('keydown', { bubbles: true, key: 'Escape' }),
    )
    await settle()
    expect(scrims()).toHaveLength(1)
    expect(dimming()).toHaveLength(1)

    wrapper.vm.panel = true
    await settle()
    expect(scrims()).toHaveLength(2)
    expect(dimming()).toHaveLength(1)

    // With the stack emptied the count returns to zero, so a panel opened on
    // its own dims the page itself.
    wrapper.vm.panel = false
    wrapper.vm.sheet = false
    await settle()
    expect(scrims()).toHaveLength(0)

    wrapper.vm.panel = true
    await settle()
    expect(scrims()).toHaveLength(1)
    expect(dimming()).toHaveLength(1)
  })
})
