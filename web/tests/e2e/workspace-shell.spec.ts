/**
 * The shell every section is drawn in: the geometry it aligns them to, the
 * sidebar it collapses, and the overlays it portals out of them.
 *
 * The worker fixture in `support/served-product.ts` serves this suite
 * alone, over the data directory named below.
 */

import { expect } from '@playwright/test'

import { englishCopy } from './support/copy'
import { assertPainted, openFormats } from './support/flows'
import {
  bodyRows,
  categoryMore,
  choicePanel,
  deviceField,
  infoPanel,
  menuPanel,
} from './support/queries'
import { test } from './support/served-product'

// The surface negotiates its language from the browser, and English is the
// product's primary language. `productData` is this suite's own data
// directory: it starts empty, no other suite writes to it, and it is gone
// again when the suite ends.
test.use({ locale: 'en-US', productData: 'workspace-shell' })

/**
 * Every overlay on this surface is portalled out of the component that owns it,
 * and a scoped rule that stops matching there takes the panel's ground and edge
 * with it. Nothing else here would notice: an unpainted panel still has a box,
 * still carries its text, and still passes the axe sweep. No unit test can see
 * it either, because jsdom applies no styles at all.
 */
test('every portalled overlay arrives with its own ground and edge', async ({
  page,
  origin,
  assertProductAlive,
}) => {
  test.setTimeout(120000)
  const created = await page.request.post(`${origin}/v1/profiles`, {
    data: {
      categories: [],
      exclusions: [],
      name: 'Overlay ground',
      lists: ['youtube'],
    },
    headers: { Origin: origin, 'X-Routevane-Request': '1' },
  })
  expect(created.ok()).toBe(true)

  await page.goto(`${origin}/`)
  await page
    .getByRole('button', { name: 'Actions for profile Overlay ground' })
    .click()
  await assertPainted(menuPanel(page), 'menu')
  await page.keyboard.press('Escape')

  await page.goto(`${origin}/connections`)
  await deviceField(page, 'devices.field.target').click()
  await assertPainted(
    choicePanel(page, englishCopy('devices.field.target.pick')),
    'connection choice',
  )
  await page.keyboard.press('Escape')

  await page.goto(`${origin}/profiles/new`)
  await openFormats(page)
  await assertPainted(
    choicePanel(page, englishCopy('create.target.toggle')),
    'combobox',
  )
  await page.keyboard.press('Escape')

  await page
    .getByRole('button', { name: 'Open the contents of list Discord' })
    .click()
  const card = page.getByRole('dialog', { exact: true, name: 'Discord' })
  await card
    .getByRole('button', { name: englishCopy('listCard.domains.info') })
    .click()
  // An informer's panel is a dialog of its own, opened over the card that
  // offers it, so it is addressed by the name of the control it belongs to.
  await assertPainted(
    infoPanel(page, englishCopy('listCard.domains.info')),
    'informer',
  )
  assertProductAlive()
})

test('workspace pages share geometry and the category panel supports keyboard search', async ({
  page,
  origin,
}) => {
  for (const width of [1366, 1920]) {
    await page.setViewportSize({ width, height: 900 })
    let reference: { x: number; width: number; background: string } | undefined
    for (const path of [
      '/profiles/new',
      '/lists',
      '/connections',
      '/settings',
      '/',
    ]) {
      await page.goto(`${origin}${path}`)
      await expect(page.getByRole('heading', { level: 1 })).toBeVisible()
      // The content column is the page's own landmark, and it is the box the
      // shell aligns; every section must place it identically.
      const geometry = await page.getByRole('main').evaluate((element) => ({
        x: element.getBoundingClientRect().x,
        width: element.getBoundingClientRect().width,
        background: getComputedStyle(document.documentElement).backgroundColor,
      }))
      reference ??= geometry
      expect(geometry).toEqual(reference)
      if (path === '/profiles/new' || path === '/lists') {
        const frame = page.getByTestId(
          path === '/profiles/new'
            ? 'rv-list-picker-frame'
            : 'rv-lists-workspace',
        )
        await expect(frame).toBeVisible()
        expect(
          await frame.evaluate(
            (element) => element.scrollWidth - element.clientWidth,
          ),
        ).toBe(0)
        expect(
          await page.evaluate(
            () => document.documentElement.scrollHeight - innerHeight,
          ),
        ).toBeLessThanOrEqual(1)
      }
    }
  }
  await page.goto(`${origin}/profiles/new`)
  const collapse = page.getByRole('button', { name: 'Collapse sidebar' })
  await collapse.click()
  // The control states the collapsed state itself, which is also what the
  // reload has to bring back.
  await expect(
    page.getByRole('button', { name: 'Expand sidebar' }),
  ).toBeVisible()
  await page.reload()
  await expect(
    page.getByRole('button', { name: 'Expand sidebar' }),
  ).toBeVisible()
  const nav = page.getByRole('link', { name: 'Lists', exact: true })
  await nav.focus()
  const tooltip = page.getByTestId('rv-tooltip')
  await expect(tooltip).toBeVisible()
  await expect(nav).toHaveAccessibleDescription('Lists')
  expect(
    await tooltip.evaluate((element) => {
      const rect = element.getBoundingClientRect()
      return element.contains(
        document.elementFromPoint(
          rect.x + rect.width / 2,
          rect.y + rect.height / 2,
        ),
      )
    }),
  ).toBe(true)
  await page.getByRole('button', { name: 'Expand sidebar' }).click()
  const more = categoryMore(page)
  await more.click()
  const search = page.getByRole('textbox', { name: 'Find a category' })
  await expect(search).toBeFocused()
  const panel = choicePanel(page, englishCopy('listPicker.collections'))
  await expect(panel).not.toHaveCSS('background-color', 'rgba(0, 0, 0, 0)')
  await expect(panel).toHaveCSS('border-top-width', '1px')
  await search.fill('no category matches this')
  await expect(page.getByText('No matching categories')).toBeVisible()
  await search.fill('Video')
  await search.press('ArrowDown')
  await search.press('Enter')
  await expect(search).toBeVisible()
  await search.press('Escape')
  await expect(search).toBeHidden()
  await expect(more).toBeFocused()
  await expect(bodyRows(page)).toHaveCount(1)
  await expect(bodyRows(page)).toContainText('YouTube')
  await more.click()
  await expect(page.getByRole('option', { name: 'Video' })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await search.fill('Communication')
  await search.press('Escape')
  await expect(search).toBeHidden()
  await expect(more).toBeFocused()
  await expect(bodyRows(page)).toContainText('YouTube')
})

test('sidebar labels and buttons retain their geometry throughout expansion and collapse', async ({
  page,
  origin,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto(`${origin}/`)
  await expect(
    page.getByRole('button', { name: 'Collapse sidebar' }),
  ).toBeVisible()
  for (const direction of ['collapse', 'expand']) {
    const frames = await page.evaluate(async () => {
      const button =
        document.querySelector<HTMLButtonElement>('.shell__collapse')!
      const side = document.querySelector<HTMLElement>('.shell__side')!
      const labels = [
        ...side.querySelectorAll<HTMLElement>('.shell__nav-label'),
      ]
      button.click()
      const frames: {
        height: number
        overflow: number
        labels: number[]
        opacity: number
      }[] = []
      const start = performance.now()
      do {
        await new Promise(requestAnimationFrame)
        frames.push({
          height: button.getBoundingClientRect().height,
          overflow: side.scrollWidth - side.clientWidth,
          labels: labels.map((e) => e.getBoundingClientRect().height),
          opacity: Number(getComputedStyle(labels[0]!).opacity),
        })
      } while (performance.now() - start < 350)
      return frames
    })
    expect(frames.length, direction).toBeGreaterThan(2)
    expect(
      Math.max(...frames.map((f) => f.height)) -
        Math.min(...frames.map((f) => f.height)),
      direction,
    ).toBeLessThanOrEqual(1)
    expect(
      Math.max(...frames.map((f) => f.overflow)),
      direction,
    ).toBeLessThanOrEqual(1)
    for (let i = 0; i < frames[0]!.labels.length; i++) {
      const heights = frames.map((f) => f.labels[i]!)
      expect(
        Math.max(...heights) - Math.min(...heights),
        direction,
      ).toBeLessThanOrEqual(1)
    }
    expect(
      frames.some((f) => f.opacity > 0 && f.opacity < 1),
      direction,
    ).toBe(true)
  }
})
