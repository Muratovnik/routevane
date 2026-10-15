import { describe, expect, it } from 'vitest'
import { render } from 'vitest-browser-vue'
import { defineComponent, h, ref } from 'vue'

import RvSection from '@/shared/ui/RvSection.vue'

const box = (element: Element): DOMRect => element.getBoundingClientRect()

describe('RvSection', () => {
  it('is a region named by its heading alone, with the count and actions beside it', async () => {
    const screen = await render(RvSection, {
      props: { title: 'Saved connections' },
      slots: {
        default: '<p>Rows</p>',
        meta: '<span>2</span>',
        actions: '<button type="button">Add a connection</button>',
      },
    })

    await expect
      .element(
        screen.getByRole('region', { name: 'Saved connections', exact: true }),
      )
      .toBeVisible()
    await expect
      .element(
        screen.getByRole('heading', {
          level: 2,
          name: 'Saved connections',
          exact: true,
        }),
      )
      .toBeVisible()
    await expect.element(screen.getByText('2', { exact: true })).toBeVisible()
    await expect
      .element(screen.getByRole('button', { name: 'Add a connection' }))
      .toBeVisible()
  })

  it('moves the keyboard to its heading when the host asks', async () => {
    const Host = defineComponent({
      setup() {
        const section = ref<InstanceType<typeof RvSection> | null>(null)
        return () =>
          h('div', [
            h(
              'button',
              { type: 'button', onClick: () => section.value?.focusHeading() },
              'Open the form',
            ),
            h(RvSection, { ref: section, title: 'Add a connection' }),
          ])
      },
    })
    const screen = await render(Host)

    await screen.getByRole('button', { name: 'Open the form' }).click()

    await expect
      .element(screen.getByRole('heading', { name: 'Add a connection' }))
      .toHaveFocus()
  })

  it('puts the heading beside the content while both fit, and above it when they do not', async () => {
    const shape = async (width: number): Promise<'beside' | 'above'> => {
      const screen = await render(RvSection, {
        attrs: { style: `width: ${width}px` },
        props: { layout: 'beside', title: 'Interface' },
        slots: { default: '<p>Language</p>' },
      })
      const heading = box(
        screen.getByRole('heading', { name: 'Interface' }).element(),
      )
      const content = box(screen.getByText('Language').element())
      const shape =
        content.left >= heading.right && content.top === heading.top
          ? 'beside'
          : 'above'
      screen.unmount()
      return shape
    }

    expect(await shape(1000)).toBe('beside')
    expect(await shape(480)).toBe('above')
  })

  it('scrolls a flush section’s rows inside its frame once the host caps its height', async () => {
    const rows = Array.from({ length: 30 }, (_, n) => `<li>Row ${n}</li>`)
    const screen = await render(RvSection, {
      attrs: { style: 'max-height: 200px' },
      props: { flush: true, title: 'Saved connections' },
      slots: { default: `<ul>${rows.join('')}</ul>` },
    })
    const section = screen
      .getByRole('region', { name: 'Saved connections' })
      .element()
    const content = section.lastElementChild!

    expect(box(section).height).toBeLessThanOrEqual(200)
    // Rows that only spill out of the frame would not move: the content has to
    // be the box that scrolls.
    content.scrollTop = 100
    expect(content.scrollTop).toBeGreaterThan(0)
    // The header stays in the frame while the rows scroll under it.
    expect(
      box(screen.getByRole('heading', { name: 'Saved connections' }).element())
        .top,
    ).toBeGreaterThanOrEqual(box(section).top)
  })
})
