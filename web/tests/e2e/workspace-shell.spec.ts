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
  forceStaleCatalog,
  PAGE_INSET,
  pageFoot,
  releaseCatalog,
} from './support/geometry'
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

// The sections whose content is a workspace frame that fills the window, each
// with the frame it owns. A frame is the box its table scrolls inside and has
// neither a role nor a name, so the test hook it carries is its only handle.
const WORKSPACE_FRAMES = [
  { path: '/profiles/new', testID: 'rv-list-picker-frame' },
  { path: '/lists', testID: 'rv-lists-workspace' },
]

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
  await page
    .getByRole('button', { name: englishCopy('devices.add'), exact: true })
    .click()
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
    }
    // Two of those sections fill the window with a workspace frame of their
    // own, which must fit its box without a scrollbar of its own or one on the
    // document. The frame belongs to the section, so the pair names it.
    for (const { path, testID } of WORKSPACE_FRAMES) {
      await page.goto(`${origin}${path}`)
      const frame = page.getByTestId(testID)
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

/**
 * A workspace frame keeps its own scroll when the catalog is taller than the
 * window: the page states the section's chrome and the frame states its rows.
 * The fixture catalog is three lists long, so the case that matters — a table
 * that cannot fit — has to be created before it can be measured. Nothing else
 * here sees it: the frame is readable at any height, and the document cannot
 * scroll either way, because the shell is exactly one window tall.
 */
test('a catalog taller than the window scrolls inside its frame, not down the page', async ({
  page,
  origin,
}) => {
  const headers = { Origin: origin, 'X-Routevane-Request': '1' }
  for (let index = 0; index < 20; index += 1) {
    const created = await page.request.post(`${origin}/v1/lists`, {
      data: { title: `Tall catalog ${index}`, domains: [`t${index}.example`] },
      headers,
    })
    expect(created.ok()).toBe(true)
  }
  // The third section that fills the window is one profile's own composition,
  // which needs a profile to belong to.
  const created = await page.request.post(`${origin}/v1/profiles`, {
    data: {
      categories: [],
      exclusions: [],
      lists: ['youtube'],
      name: 'Tall catalog',
    },
    headers,
  })
  expect(created.ok()).toBe(true)
  const { profile } = (await created.json()) as { profile: { id: string } }
  const frames = [
    ...WORKSPACE_FRAMES,
    { path: `/profiles/${profile.id}`, testID: 'rv-list-picker-frame' },
  ]

  for (const height of [900, 906]) {
    await page.setViewportSize({ width: 1440, height })
    for (const { path, testID } of frames) {
      const where = `${path} at ${height}`
      await page.goto(`${origin}${path}`)
      const frame = page.getByTestId(testID)
      await expect(frame).toBeVisible()
      expect(
        await frame.evaluate(
          (element) => element.scrollHeight - element.clientHeight,
        ),
        where,
      ).toBeGreaterThan(0)
      expect(
        await page
          .getByRole('main')
          .evaluate((element) => element.scrollHeight - element.clientHeight),
        where,
      ).toBe(0)
    }
  }
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
      // Every mark the rail carries: the product's, each section's, and the one
      // on the button doing the collapsing.
      const glyphs = [
        side.querySelector<HTMLElement>('.shell__product-mark')!,
        ...side.querySelectorAll<HTMLElement>('.shell__nav-icon'),
        button.querySelector<HTMLElement>('.rv-icon')!,
      ]
      const centre = (element: HTMLElement): number => {
        const box = element.getBoundingClientRect()
        return box.x + box.width / 2
      }
      button.click()
      const frames: {
        height: number
        overflow: number
        labels: number[]
        glyphs: number[]
        opacity: number
      }[] = []
      const start = performance.now()
      do {
        await new Promise(requestAnimationFrame)
        frames.push({
          height: button.getBoundingClientRect().height,
          overflow: side.scrollWidth - side.clientWidth,
          labels: labels.map((e) => e.getBoundingClientRect().height),
          glyphs: glyphs.map(centre),
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

    // Each mark travels to the rail's centre line rather than being re-aligned
    // to it in one frame and drifting back out under the closing column. The
    // path is read, not just its two ends: one direction the whole way, and no
    // step larger than the travel a single frame can honestly account for.
    for (let i = 0; i < frames[0]!.glyphs.length; i++) {
      const path = frames.map((f) => f.glyphs[i]!)
      const travel = Math.abs(path.at(-1)! - path[0]!)
      const steps = path
        .slice(1)
        .map((value, index) => value - path[index]!)
        .filter((step) => Math.abs(step) > 0.05)
      expect(
        Math.max(0, ...steps.map(Math.abs)),
        `${direction} ${i}`,
      ).toBeLessThanOrEqual(Math.max(3, travel / 2))
      expect(
        steps.every((step) => step > 0) || steps.every((step) => step < 0),
        `${direction} ${i}`,
      ).toBe(true)
    }
  }
})

/**
 * A notice on the new-profile page is an addition to the page, not a
 * subtraction from the catalog it appears above: the composer keeps the
 * standing height of its own row, and the page lengthens by the notice and
 * scrolls with its closing inset still under the last row. The notice is said
 * in the header row, which is what keeps it out of the composer's; a notice
 * that took that row would leave the composer with no height of its own and
 * the catalog running down the page.
 */
test('a stale-library notice lengthens the new-profile page instead of shortening its table', async ({
  page,
  origin,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto(`${origin}/profiles/new`)
  const frame = page.getByTestId('rv-list-picker-frame')
  await expect(frame).toBeVisible()
  const standing = (await frame.boundingBox())!.height
  const resting = await pageFoot(page)
  expect(resting.overflow).toBe(0)
  expect(resting.inset).toBeGreaterThanOrEqual(PAGE_INSET)

  try {
    const notice = await forceStaleCatalog(page)
    const added = (await notice.boundingBox())!.height
    expect((await frame.boundingBox())!.height).toBeCloseTo(standing, 0)
    const noticed = await pageFoot(page)
    expect(noticed.overflow).toBeGreaterThanOrEqual(added)
    expect(noticed.inset).toBeGreaterThanOrEqual(PAGE_INSET)
  } finally {
    await releaseCatalog(page)
  }
})
