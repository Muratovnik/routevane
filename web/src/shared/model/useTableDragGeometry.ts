import type { SortableEvent } from 'sortablejs'
import { onBeforeUnmount } from 'vue'

/** Preserve a row when Sortable moves its fallback outside the table container. */
export const useTableDragGeometry = () => {
  const originals = new Map<HTMLElement, string | null>()

  const restore = (): void => {
    for (const [node, style] of originals) {
      if (style === null) node.removeAttribute('style')
      else node.setAttribute('style', style)
    }
    originals.clear()
  }

  const onChoose = (event: SortableEvent): void => {
    restore()
    // Read everything before writing: fixed cell typography must not change the
    // inherited metrics we subsequently capture for compact labels.
    const snapshots = Array.from(event.item.querySelectorAll('*'))
      .filter((node): node is HTMLElement => node instanceof HTMLElement)
      .map((node) => {
        const style = getComputedStyle(node)
        const rect = node.getBoundingClientRect()
        return {
          node,
          original: node.getAttribute('style'),
          styles: {
            ...(node.parentElement === event.item
              ? {
                  width: `${rect.width}px`,
                  height: `${rect.height}px`,
                  padding: style.padding,
                  borderBottom: style.borderBottom,
                }
              : {}),
            display: style.display,
            fontFamily: style.fontFamily,
            fontSize: style.fontSize,
            fontWeight: style.fontWeight,
            lineHeight: style.lineHeight,
            fontVariantNumeric: style.fontVariantNumeric,
            letterSpacing: style.letterSpacing,
            color: style.color,
            float: style.cssFloat,
            clear: style.clear,
            textAlign: style.textAlign,
            verticalAlign: style.verticalAlign,
            whiteSpace: style.whiteSpace,
          },
        }
      })
    for (const { node, original, styles } of snapshots) {
      originals.set(node, original)
      Object.assign(node.style, styles)
    }
  }

  onBeforeUnmount(restore)

  return { onChoose, onUnchoose: restore }
}
