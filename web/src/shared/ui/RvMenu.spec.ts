import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'

import type { MenuItem } from '@/shared/ui/kinds'
import RvMenu from '@/shared/ui/RvMenu.vue'

// The panel is a viewport overlay portalled to the document, so it is read
// there rather than inside the component that opened it.
function panel(): HTMLElement | null {
  const panels = document.body.querySelectorAll<HTMLElement>('[role="menu"]')
  return panels.item(panels.length - 1)
}

function items(scope: HTMLElement | null): HTMLElement[] {
  return [
    ...(scope?.querySelectorAll<HTMLElement>(
      '[role="menuitem"], [data-menu-key]',
    ) ?? []),
  ]
}

function itemWithText(text: string): HTMLElement | undefined {
  return items(panel()).find((item) => item.textContent?.includes(text))
}

// Closing a panel hands the focus back on a macrotask, so a test that asks
// where the keyboard went waits for one.
async function settle(): Promise<void> {
  await flushPromises()
  await new Promise((resolve) => {
    setTimeout(resolve, 0)
  })
}

function mountMenu(props: { items: MenuItem[]; label: string }) {
  return mount(RvMenu, {
    attachTo: document.body,
    props,
    global: { stubs: { NuxtLink: true, RvIcon: true } },
  })
}

describe('RvMenu', () => {
  // Every panel is portalled to the document, so a component left mounted by a
  // failing assertion would still be answering the next test's queries.
  let open: { unmount: () => void } | null = null

  afterEach(() => {
    open?.unmount()
    open = null
    document.body.innerHTML = ''
  })

  // The panel must not belong to whatever container the trigger sits in: a
  // table that owned it would grow its own scroll box around it.
  it('opens a panel that lives outside the trigger container', async () => {
    const wrapper = mountMenu({
      items: [{ key: 'download', label: 'Keenetic · BAT' }],
      label: 'Выбрать формат',
    })
    open = wrapper

    const trigger = wrapper.get('.rv-menu__trigger')
    expect(panel()).toBeNull()

    await trigger.trigger('click')
    await flushPromises()

    const opened = panel()
    expect(opened).not.toBeNull()
    expect(opened?.classList.contains('rv-menu__panel')).toBe(true)
    expect(wrapper.element.contains(opened)).toBe(false)
    expect(opened?.textContent).toContain('Keenetic · BAT')
    expect(trigger.attributes('aria-expanded')).toBe('true')
  })

  it('emits the chosen key and closes', async () => {
    const wrapper = mountMenu({
      items: [
        { key: 'open', label: 'Открыть' },
        { key: 'archive', label: 'В архив' },
      ],
      label: 'Действия',
    })
    open = wrapper

    await wrapper.get('.rv-menu__trigger').trigger('click')
    await flushPromises()
    itemWithText('В архив')?.click()
    await flushPromises()

    expect(wrapper.emitted('select')).toEqual([['archive']])
    expect(wrapper.get('.rv-menu__trigger').attributes('aria-expanded')).toBe(
      'false',
    )
  })

  it('drills into grouped choices and returns without closing the menu', async () => {
    const wrapper = mountMenu({
      items: [
        { key: 'open', label: 'Открыть' },
        {
          children: [
            { key: 'json', label: 'JSON · все правила' },
            { key: 'bat', label: 'BAT · маршруты' },
          ],
          key: 'download',
          label: 'Скачать',
        },
      ],
      label: 'Действия',
    })
    open = wrapper

    const trigger = wrapper.get('.rv-menu__trigger')
    await trigger.trigger('click')
    await flushPromises()

    const group = itemWithText('Скачать')
    expect(group).toBeDefined()
    expect(group?.getAttribute('aria-haspopup')).toBe('menu')

    group?.click()
    await flushPromises()

    // The submenu is its own panel; the menu that opened it is still standing.
    const panels = document.body.querySelectorAll('[role="menu"]')
    expect(panels.length).toBe(2)
    expect(panel()?.textContent).toContain('JSON · все правила')

    // Leaving the group closes only the group.
    panel()?.dispatchEvent(
      new KeyboardEvent('keydown', { bubbles: true, key: 'ArrowLeft' }),
    )
    await flushPromises()
    expect(document.body.querySelectorAll('[role="menu"]').length).toBe(1)
    expect(trigger.attributes('aria-expanded')).toBe('true')

    // Choosing inside the group still reports the child's key.
    group?.click()
    await flushPromises()
    itemWithText('JSON · все правила')?.click()
    await flushPromises()
    expect(wrapper.emitted('select')).toEqual([['json']])
  })

  // Escape is the keyboard's way out, and the way out has to land somewhere:
  // focus returns to the control that opened the panel rather than to the top
  // of the document.
  it('closes on Escape and hands focus back to its trigger', async () => {
    const wrapper = mountMenu({
      items: [{ key: 'archive', label: 'В архив' }],
      label: 'Действия',
    })
    open = wrapper

    const trigger = wrapper.get<HTMLButtonElement>('.rv-menu__trigger')
    await trigger.trigger('click')
    await flushPromises()
    expect(panel()).not.toBeNull()

    document.dispatchEvent(
      new KeyboardEvent('keydown', { bubbles: true, key: 'Escape' }),
    )
    await settle()

    expect(trigger.attributes('aria-expanded')).toBe('false')
    expect(document.activeElement).toBe(trigger.element)
  })

  it('closes when a click lands outside it', async () => {
    const wrapper = mountMenu({
      items: [{ key: 'archive', label: 'В архив' }],
      label: 'Действия',
    })
    open = wrapper

    const trigger = wrapper.get('.rv-menu__trigger')
    await trigger.trigger('click')
    // The panel starts listening for the next gesture, not for the one that
    // opened it, so the dismissing click is a separate turn of the loop.
    await settle()
    expect(trigger.attributes('aria-expanded')).toBe('true')

    const outside = document.createElement('button')
    document.body.append(outside)
    outside.dispatchEvent(new MouseEvent('pointerdown', { bubbles: true }))
    outside.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await settle()

    expect(trigger.attributes('aria-expanded')).toBe('false')
    outside.remove()
  })
})
