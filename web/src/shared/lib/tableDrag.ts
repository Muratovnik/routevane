import type { SortableEvent } from 'sortablejs'

/** Capture cell metrics before Sortable moves its fallback outside the table. */
export function tableDragGeometry() {
  const originals = new Map<HTMLElement, string | null>()
  function onChoose(event: SortableEvent): void {
    const cells = Array.from(event.item.children)
      .filter((cell): cell is HTMLElement => cell instanceof HTMLElement)
      .map((cell) => {
        const style = getComputedStyle(cell)
        const rect = cell.getBoundingClientRect()
        return {
          cell,
          width: rect.width,
          height: rect.height,
          padding: style.padding,
          borderBottom: style.borderBottom,
          font: style.font,
          color: style.color,
        }
      })
    for (const {
      cell,
      width,
      height,
      padding,
      borderBottom,
      font,
      color,
    } of cells) {
      originals.set(cell, cell.getAttribute('style'))
      Object.assign(cell.style, {
        width: `${width}px`,
        height: `${height}px`,
        padding,
        borderBottom,
        font,
        color,
      })
    }
  }
  function onUnchoose(): void {
    for (const [cell, style] of originals) {
      if (style === null) cell.removeAttribute('style')
      else cell.setAttribute('style', style)
    }
    originals.clear()
  }
  return { onChoose, onUnchoose }
}
