import type { SortableEvent } from 'sortablejs'
import { describe, expect, it } from 'vitest'
import { render } from 'vitest-browser-vue'
import { defineComponent, h } from 'vue'

import { useTableDragGeometry } from '@/shared/model/useTableDragGeometry'

describe('useTableDragGeometry', () => {
  it('restores captured row styles when its component is unmounted', async () => {
    let choose: ((event: SortableEvent) => void) | undefined
    const Host = defineComponent({
      setup() {
        const geometry = useTableDragGeometry()
        choose = geometry.onChoose
        return () =>
          h('table', [
            h('tbody', [
              h('tr', { 'aria-label': 'Discord' }, [
                h('td', { style: 'color: rgb(12, 34, 56)' }, 'Discord'),
                h('td', 'Communication'),
              ]),
            ]),
          ])
      },
    })
    const screen = await render(Host)
    const row = screen.getByRole('row', { name: 'Discord' }).element()
    const firstCell = row.querySelector<HTMLElement>('td')!
    const original = firstCell.getAttribute('style')

    choose?.({ item: row } as SortableEvent)
    expect(firstCell.style.width).not.toBe('')
    expect(firstCell.style.fontFamily).not.toBe('')

    await screen.unmount()
    expect(firstCell.getAttribute('style')).toBe(original)
  })
})
