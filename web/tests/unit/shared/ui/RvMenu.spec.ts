import { describe, expect, it, vi } from 'vitest'
import { page, userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-vue'
import { defineComponent, h } from 'vue'
import { createMemoryHistory, createRouter, RouterLink } from 'vue-router'

import type { MenuItem } from '@/shared/ui/types'
import RvMenu from '@/shared/ui/RvMenu.vue'

const GLOBAL = { stubs: { NuxtLink: true } }

// Every panel is a viewport overlay portalled to the document, so it is read
// there rather than inside the component that opened it. The innermost one is
// the last: a submenu is its own panel on top of the menu that opened it.
const panels = () => page.getByRole('menu')
const innermost = () => panels().last()

// An open panel is modal, so everything behind it — the trigger included — is
// hidden from assistive technology while it stands. Reading the trigger's
// expanded state during that time needs a query that reaches hidden content.
const hidden = (name: string) =>
  page.getByRole('button', { includeHidden: true, name })

const renderMenu = (props: { items: MenuItem[]; label: string }) =>
  render(RvMenu, { global: GLOBAL, props })

describe('RvMenu', () => {
  // The panel must not belong to whatever container the trigger sits in: a
  // table that owned it would grow its own scroll box around it.
  it('opens a panel that lives outside the trigger container', async () => {
    const screen = await renderMenu({
      items: [{ key: 'download', label: 'Keenetic · BAT' }],
      label: 'Выбрать формат',
    })

    const trigger = screen.getByRole('button', { name: 'Выбрать формат' })
    await expect.element(innermost()).not.toBeInTheDocument()

    await trigger.click()

    const opened = innermost()
    await expect.element(opened).toBeVisible()
    await expect.element(opened).toHaveClass('rv-menu__panel')
    expect(screen.container.contains(opened.element())).toBe(false)
    await expect
      .element(screen.getByRole('menuitem', { name: 'Keenetic · BAT' }))
      .toBeVisible()
    await expect
      .element(hidden('Выбрать формат'))
      .toHaveAttribute('aria-expanded', 'true')
  })

  it('emits the chosen key and closes', async () => {
    const screen = await renderMenu({
      items: [
        { key: 'open', label: 'Открыть' },
        { key: 'archive', label: 'В архив' },
      ],
      label: 'Действия',
    })

    const trigger = screen.getByRole('button', { name: 'Действия' })
    await trigger.click()
    await screen.getByRole('menuitem', { name: 'В архив' }).click()

    expect(screen.emitted('select')).toEqual([['archive']])
    await expect.element(trigger).toHaveAttribute('aria-expanded', 'false')
  })

  it('drills into grouped choices and returns without closing the menu', async () => {
    const screen = await renderMenu({
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

    const trigger = screen.getByRole('button', { name: 'Действия' })
    await trigger.click()

    const group = screen.getByRole('menuitem', { name: 'Скачать' })
    await expect.element(group).toHaveAttribute('aria-haspopup', 'menu')

    // Enter by keyboard so the pointer's delayed hover opener is not still
    // queued when ArrowLeft closes the group. Verify focus before each step.
    await userEvent.keyboard('{End}')
    await expect.element(group).toHaveFocus()
    await userEvent.keyboard('{ArrowRight}')
    await expect
      .element(screen.getByRole('menuitem', { name: 'JSON · все правила' }))
      .toHaveFocus()

    // The submenu is its own panel; the menu that opened it is still standing.
    await expect
      .element(screen.getByRole('menuitem', { name: 'JSON · все правила' }))
      .toBeVisible()
    expect(panels().all()).toHaveLength(2)

    // Leaving the group closes only the group: the keyboard walks into the
    // submenu and back out of it, and the menu behind is still standing.
    await userEvent.keyboard('{ArrowLeft}')
    await expect.element(group).toHaveFocus()
    await expect
      .element(screen.getByRole('menuitem', { name: 'JSON · все правила' }))
      .not.toBeInTheDocument()
    expect(panels().all()).toHaveLength(1)
    await expect
      .element(hidden('Действия'))
      .toHaveAttribute('aria-expanded', 'true')

    // Choosing inside the group still reports the child's key.
    await group.click()
    await screen.getByRole('menuitem', { name: 'JSON · все правила' }).click()
    expect(screen.emitted('select')).toEqual([['json']])
  })

  it('keeps a pointer-opened group closed after ArrowLeft until new input', async () => {
    const screen = await renderMenu({
      items: [
        { key: 'open', label: 'Open' },
        {
          key: 'download',
          label: 'Download',
          children: [{ key: 'json', label: 'JSON' }],
        },
      ],
      label: 'Actions',
    })
    await screen.getByRole('button', { name: 'Actions' }).click()
    const group = screen.getByRole('menuitem', { name: 'Download' })
    const choice = screen.getByRole('menuitem', { name: 'JSON' })

    // A native click first moves the pointer over the trigger. Hold that hover
    // timer until the keyboard has closed the group, then run pending work.
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    try {
      await group.click()
      await userEvent.keyboard('{ArrowRight}')
      await expect.element(choice).toHaveFocus()
      await userEvent.keyboard('{ArrowLeft}')
      await expect.element(group).toHaveFocus()
      await expect.element(group).toHaveAttribute('aria-expanded', 'false')

      await vi.runOnlyPendingTimersAsync()

      await expect.element(choice).not.toBeInTheDocument()
      await expect.element(group).toHaveAttribute('aria-expanded', 'false')
      await expect
        .element(hidden('Actions'))
        .toHaveAttribute('aria-expanded', 'true')

      // A new keyboard command and a fresh hover can still reopen the group.
      await userEvent.keyboard('{ArrowRight}')
      await expect.element(choice).toHaveFocus()
      await userEvent.keyboard('{ArrowLeft}')
      await expect.element(group).toHaveFocus()
      await expect.element(choice).not.toBeInTheDocument()

      await screen.getByRole('menuitem', { name: 'Open', exact: true }).hover()
      await group.hover()
      await vi.runOnlyPendingTimersAsync()
      await expect.element(choice).toBeVisible()
    } finally {
      vi.useRealTimers()
    }
  })

  it('keeps hover, disabled groups and reopening independent across groups', async () => {
    const screen = await renderMenu({
      items: [
        {
          key: 'files',
          label: 'Files',
          children: [{ key: 'json', label: 'JSON' }],
        },
        {
          key: 'routes',
          label: 'Routes',
          children: [{ key: 'bat', label: 'BAT' }],
        },
        {
          key: 'blocked',
          label: 'Unavailable',
          disabled: true,
          children: [{ key: 'blocked-child', label: 'Unavailable child' }],
        },
      ],
      label: 'Actions',
    })
    const trigger = screen.getByRole('button', { name: 'Actions' })
    const files = screen.getByRole('menuitem', { name: 'Files' })
    const routes = screen.getByRole('menuitem', { name: 'Routes' })
    await trigger.click()

    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    try {
      await files.hover()
      await vi.runOnlyPendingTimersAsync()
      await expect
        .element(screen.getByRole('menuitem', { name: 'JSON' }))
        .toBeVisible()

      await routes.hover()
      await vi.runOnlyPendingTimersAsync()
      await expect
        .element(screen.getByRole('menuitem', { name: 'JSON' }))
        .not.toBeInTheDocument()
      await expect
        .element(screen.getByRole('menuitem', { name: 'BAT' }))
        .toBeVisible()

      await screen
        .getByRole('menuitem', { name: 'Unavailable', exact: true })
        .hover()
      await vi.runOnlyPendingTimersAsync()
      await expect
        .element(screen.getByRole('menuitem', { name: 'Unavailable child' }))
        .not.toBeInTheDocument()

      await routes.click()
      await userEvent.keyboard('{ArrowRight}')
      await expect
        .element(screen.getByRole('menuitem', { name: 'BAT' }))
        .toHaveFocus()
      await userEvent.keyboard('{Escape}')
      await expect.element(trigger).toHaveAttribute('aria-expanded', 'false')
      await vi.runOnlyPendingTimersAsync()
    } finally {
      vi.useRealTimers()
    }

    await trigger.click()
    await expect.element(panels().first()).toBeVisible()
    expect(panels().all()).toHaveLength(1)
    await routes.click()
    await expect
      .element(screen.getByRole('menuitem', { name: 'BAT' }))
      .toBeVisible()
  })

  // Escape is the keyboard's way out, and the way out has to land somewhere:
  // focus returns to the control that opened the panel rather than to the top
  // of the document.
  it('closes on Escape and hands focus back to its trigger', async () => {
    const screen = await renderMenu({
      items: [{ key: 'archive', label: 'В архив' }],
      label: 'Действия',
    })

    const trigger = screen.getByRole('button', { name: 'Действия' })
    await trigger.click()
    await expect.element(innermost()).toBeVisible()

    await userEvent.keyboard('{Escape}')

    await expect.element(trigger).toHaveAttribute('aria-expanded', 'false')
    await expect.element(trigger).toHaveFocus()
  })

  it('closes when a click lands outside it', async () => {
    // An open menu is modal, so the page behind it stops taking the pointer.
    // The dismissing click is forced for that reason: what it must not do is
    // activate the control underneath, and what it must do is close the panel.
    const Host = defineComponent({
      setup() {
        return () =>
          h('div', [
            h(RvMenu, {
              items: [{ key: 'archive', label: 'В архив' }] as MenuItem[],
              label: 'Действия',
            }),
            h('button', { type: 'button' }, 'Elsewhere'),
          ])
      },
    })
    const screen = await render(Host, { global: GLOBAL })

    const trigger = screen.getByRole('button', { name: 'Действия' })
    await trigger.click()
    await expect
      .element(hidden('Действия'))
      .toHaveAttribute('aria-expanded', 'true')

    await hidden('Elsewhere').click({ force: true })

    await expect.element(trigger).toHaveAttribute('aria-expanded', 'false')
  })
})

it.each([false, true])(
  'routes a menu link with a real router (grouped=%s)',
  async (grouped) => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/', component: { template: '<div />' } },
        { path: '/send/output-a', component: { template: '<div />' } },
      ],
    })
    await router.push('/')
    const children: MenuItem[] = [
      { key: 'send', label: 'Send to router', to: '/send/output-a' },
      {
        key: 'blocked',
        label: 'Unavailable output',
        to: '/send/output-b',
        disabled: true,
      },
      {
        key: 'download',
        label: 'Download file',
        href: '/file.txt',
        download: true,
      },
    ]
    const screen = await render(RvMenu, {
      props: {
        label: 'Actions',
        items: grouped
          ? [{ key: 'outputs', label: 'Outputs', children }]
          : children,
      },
      global: { plugins: [router], components: { NuxtLink: RouterLink } },
    })
    await screen.getByRole('button', { name: 'Actions' }).click()
    if (grouped)
      await screen
        .getByRole('menuitem', { name: 'Outputs', exact: true })
        .click()
    await expect
      .element(screen.getByRole('menuitem', { name: 'Unavailable output' }))
      .toHaveAttribute('data-disabled')
    await expect
      .element(screen.getByRole('menuitem', { name: 'Unavailable output' }))
      .not.toHaveAttribute('href')
    await screen
      .getByRole('menuitem', { name: 'Unavailable output' })
      .click({ force: true })
    expect(router.currentRoute.value.path).toBe('/')
    expect(screen.emitted('select')).toBeUndefined()
    await expect
      .element(screen.getByRole('menuitem', { name: 'Download file' }))
      .toHaveAttribute('download', '')
    await expect
      .element(screen.getByRole('menuitem', { name: 'Send to router' }))
      .toHaveAttribute('href', '/send/output-a')
    await screen.getByRole('menuitem', { name: 'Send to router' }).click()
    expect(router.currentRoute.value.path).toBe('/send/output-a')
  },
)
