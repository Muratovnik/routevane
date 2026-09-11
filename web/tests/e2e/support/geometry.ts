/**
 * Page-geometry probes shared by the layout suites: where a page ends at full
 * scroll, and the stale-library notice forced the way the product produces it.
 */

import { expect, type Locator, type Page } from '@playwright/test'

import { englishCopy } from './copy'

/** The page's own outer inset, `--rv-space-6`, in CSS pixels. */
export const PAGE_INSET = 24

/** The chrome band's closing inset under its last control, `--rv-space-5`. */
export const CHROME_FOOT_INSET = 20

/** The reads the catalog is refreshed from when the window is returned to. */
const CATALOG_ROUTES = ['**/v1/lists', '**/v1/targets']

/**
 * Puts the stale-catalog message on the page the way the product does: the
 * catalog is re-read when the operator comes back to the window, and a read
 * that cannot reach the service is what leaves the library stale.
 */
export const forceStaleCatalog = async (page: Page): Promise<Locator> => {
  for (const route of CATALOG_ROUTES)
    await page.route(route, (request) => request.abort())
  await page.evaluate(() => {
    window.dispatchEvent(new Event('focus'))
  })
  const notice = page
    .getByRole('status')
    .filter({ hasText: englishCopy('catalog.stale') })
  await expect(notice).toBeVisible()
  return notice
}

export const releaseCatalog = async (page: Page): Promise<void> => {
  for (const route of CATALOG_ROUTES) await page.unroute(route)
}

/**
 * The foot of the page at full scroll: how much room is left under the lowest
 * thing the page itself lays out, and how far the page scrolls.
 *
 * A box inside a scroller of its own belongs to that frame rather than to the
 * page — a table's rows run far past the frame that clips them — so the walk
 * counts the scroller and stops there.
 */
export const pageFoot = (
  page: Page,
): Promise<{ inset: number; overflow: number }> =>
  page.getByRole('main').evaluate((main) => {
    main.scrollTop = main.scrollHeight
    const column = main.firstElementChild!
    let lowest = Number.NEGATIVE_INFINITY
    for (const node of column.querySelectorAll('*')) {
      const box = node.getBoundingClientRect()
      if (box.width === 0 || box.height === 0) continue
      let clipped = false
      for (
        let parent = node.parentElement;
        parent !== null && parent !== main;
        parent = parent.parentElement
      ) {
        if (getComputedStyle(parent).overflowY !== 'visible') clipped = true
      }
      if (clipped) continue
      lowest = Math.max(lowest, box.bottom)
    }
    return {
      inset: main.getBoundingClientRect().bottom - lowest,
      overflow: main.scrollHeight - main.clientHeight,
    }
  })
