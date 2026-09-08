/**
 * The composition table: what a draft may reorder, what a bulk selection
 * covers, what every format would weigh, and the geometry of both.
 *
 * The worker fixture in `support/served-product.ts` serves this suite
 * alone, over the data directory named below.
 */

import { expect } from '@playwright/test'

import { assertNoOverflow, fits } from './support/audits'
import { englishCopy } from './support/copy'
import { FORECAST_PATH, openFormats } from './support/flows'
import {
  bodyRows,
  cardRow,
  categoryChip,
  categoryFilters,
  categoryMore,
  composerSettings,
  compositionRefresh,
  deliveryField,
  dragGhost,
  listMembership,
  nameField,
  priorityHandle,
} from './support/queries'
import { test } from './support/served-product'

// The surface negotiates its language from the browser, and English is the
// product's primary language. `productData` is this suite's own data
// directory: it starts empty, no other suite writes to it, and it is gone
// again when the suite ends.
test.use({ locale: 'en-US', productData: 'composition' })

// The failure above is the one this forecast exists to prevent, which is why
// it runs against the same fixture right after it.
test('the composer sizes every format and refuses the pair that cannot hold the profile', async ({
  page,
  origin,
  assertProductAlive,
}) => {
  test.setTimeout(120000)
  // The forecast speaks the data a build would read; a list whose sources
  // were never observed has no forecast yet. Observing it first keeps this
  // test independent of which test refreshed the fixture before it.
  const observed = await page.request.post(
    `${origin}/v1/lists/limit-fixture/refresh`,
    { data: {}, headers: { Origin: origin, 'X-Routevane-Request': '1' } },
  )
  expect(observed.ok()).toBe(true)
  await page.goto(`${origin}/profiles/new`)
  // The fixture belongs to no category, but still appears in the one table.
  await listMembership(page, 'Limit fixture').check()

  // Every format states what this draft would weigh in it, against its own
  // bound, in the same list the choice is made from.
  const formats = await openFormats(page)
  await expect(
    formats.getByRole('option', { name: /Limited fixture/ }),
  ).toContainText('≈ 2 of 1 rules')
  await expect(
    formats.getByRole('option', { name: /Limited fixture/ }),
  ).toContainText('Cannot hold this profile')
  await expect(formats.getByRole('option', { name: /Keenetic/ })).toContainText(
    '≈ 2 of 1,024 rules',
  )

  // Choosing the format that cannot hold it stops the creation and names one
  // that can. The choice stays selectable: the refusal explains, it does not
  // hide it.
  const submit = page.getByRole('button', { name: 'Create and prepare' })
  await page.getByRole('option', { name: /Limited fixture/ }).click()
  await expect(page.getByText('≈ 2 of 1 rules', { exact: true })).toBeVisible()
  await expect(submit).toBeDisabled()
  await expect(
    page.getByText('The profile does not fit Limited fixture.'),
  ).toBeVisible()
  await expect(page.getByText('sing-box would fit.')).toBeVisible()

  await page.getByRole('button', { name: 'Choose sing-box' }).click()
  await expect(deliveryField(page)).toHaveText('sing-box')
  await expect(submit).toBeEnabled()
  assertProductAlive()
})

/**
 * The composing card reads the list; the one act the profile owns is its footer.
 *
 * A switch on every row used to promise what the product cannot do. The
 * per-list override a composition carries replaces the catalog's domain seeds
 * alone, and it accepts domains alone — so switching an address or an observed
 * rule off changed nothing the build would read, and had the forecast refused
 * outright the moment it was sent. This states the result against the running
 * product: the rows carry nothing to press, and the forecast keeps answering
 * while the card is open and membership is used.
 */
test('the composing card carries no row control and never spoils the forecast', async ({
  page,
  origin,
  assertProductAlive,
}) => {
  test.setTimeout(120000)
  // Only an observed list can be weighed, and this fixture is observed from a
  // source of its own, so the test does not depend on what ran before it.
  const observed = await page.request.post(
    `${origin}/v1/lists/limit-fixture/refresh`,
    { data: {}, headers: { Origin: origin, 'X-Routevane-Request': '1' } },
  )
  expect(observed.ok()).toBe(true)

  const forecastStatuses: number[] = []
  page.on('response', (response) => {
    if (new URL(response.url()).pathname === FORECAST_PATH)
      forecastStatuses.push(response.status())
  })
  const writes: string[] = []
  page.on('request', (request) => {
    const path = new URL(request.url()).pathname
    if (request.method() === 'POST' && path !== FORECAST_PATH) writes.push(path)
  })

  await page.goto(`${origin}/profiles/new`)
  await page
    .getByRole('button', { name: 'Open the contents of list Limit fixture' })
    .click()
  const card = page.getByRole('dialog', { exact: true, name: 'Limit fixture' })
  await expect(
    card.getByRole('heading', { name: 'List contents' }),
  ).toBeVisible()

  // The fixture stands for addresses and for no domain at all: exactly the
  // rows the removed switch could never have taken out of a profile.
  await expect(cardRow(card, '192.0.2.20')).toContainText('catalog')
  await expect(cardRow(card, '198.51.100.20')).toContainText('catalog')
  await expect(card.getByRole('checkbox')).toHaveCount(0)

  // The one control the card offers, used both ways with the card open.
  const add = card.getByRole('button', { name: 'Add to profile' })
  const drop = card.getByRole('button', { name: 'Remove from profile' })
  await add.click()
  await expect(drop).toBeVisible()
  await drop.click()
  await expect(add).toBeVisible()
  await add.click()
  await expect(drop).toBeVisible()

  // The composer asks what the draft weighs while the card is still open, and
  // is answered rather than refused: nothing the card did put a value into the
  // draft that the endpoint has to reject.
  await expect
    .poll(() => forecastStatuses.length, { timeout: 20000 })
    .toBeGreaterThan(0)
  expect(forecastStatuses.filter((status) => status >= 400)).toEqual([])
  // And the card itself wrote nothing at all while it was open.
  expect(writes).toEqual([])

  await card.getByRole('button', { name: 'Close' }).last().click()
  assertProductAlive()
})

test('the composition table blocks unselected drags and bulk-selects only its category', async ({
  page,
  origin,
  assertProductAlive,
}) => {
  await page.goto(`${origin}/profiles/new`)
  const rows = bodyRows(page)
  await expect(rows.first()).toBeVisible()
  // The order the table is in, read the way it is displayed: the name each row
  // states in its own header.
  const order = (): Promise<string[]> =>
    rows.getByRole('rowheader').allInnerTexts()
  const before = await order()
  const handle = priorityHandle(rows.first())
  await expect(handle).toBeDisabled()
  const from = (await handle.boundingBox())!
  const to = (await rows.nth(1).boundingBox())!
  await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2)
  await page.mouse.down()
  await page.mouse.move(to.x + to.width / 2, to.y + to.height, { steps: 12 })
  await expect(dragGhost(page)).toHaveCount(0)
  await page.mouse.up()
  expect(await order()).toEqual(before)
  await listMembership(page, 'YouTube').check()
  await categoryChip(page, 'Communication').click()
  const bulk = page.getByRole('checkbox', {
    name: 'Select or clear all visible lists',
  })
  await bulk.check()
  await expect(listMembership(page, 'Discord')).toBeChecked()
  await bulk.uncheck()
  await categoryChip(page, englishCopy('listPicker.filter.all')).click()
  await expect(listMembership(page, 'YouTube')).toBeChecked()
  await expect(listMembership(page, 'Discord')).not.toBeChecked()
  const text = nameField(page)
  const choice = deliveryField(page)
  expect(
    await text.evaluate((element) => getComputedStyle(element).backgroundColor),
  ).toBe(
    await choice.evaluate(
      (element) => getComputedStyle(element).backgroundColor,
    ),
  )
  const refresh = compositionRefresh(page)
  await expect(refresh).toBeEnabled()
  const read = page.waitForResponse(
    (response) =>
      response.url().endsWith('/v1/lists/youtube/refresh') &&
      response.request().method() === 'POST',
  )
  const forecast = page.waitForResponse(
    (response) =>
      response.url().endsWith(FORECAST_PATH) &&
      response.request().method() === 'POST',
  )
  await refresh.click()
  expect((await read).ok()).toBe(true)
  expect((await forecast).ok()).toBe(true)
  assertProductAlive()
})

test('the composition editor keeps its geometry across selections and shell breakpoints', async ({
  page,
  origin,
}) => {
  await page.goto(`${origin}/profiles/new`)
  const table = page.getByTestId('rv-list-picker-frame')
  const rail = composerSettings(page)
  await expect(table).toBeVisible()

  await page.setViewportSize({ height: 900, width: 1440 })
  const tableBox = await table.boundingBox()
  const railBox = await rail.boundingBox()
  expect(railBox!.x).toBeGreaterThanOrEqual(tableBox!.x + tableBox!.width)
  await expect(
    page.getByRole('columnheader', { name: 'Category', exact: true }),
  ).toBeVisible()
  await expect(
    page.getByRole('columnheader', { name: 'Rules', exact: true }),
  ).toBeVisible()
  await expect(
    page.getByRole('button', { name: 'Create and prepare', exact: true }),
  ).toBeInViewport()
  // The reference is a dense table: a normal desktop row must not become
  // a 70px two-line card when the priority rail is beside it.
  const row = await bodyRows(page).first().boundingBox()
  expect(row!.height).toBeLessThanOrEqual(48)
  const before = await table.boundingBox()
  expect(before).not.toBeNull()
  // Which lists the table holds and where each row sits: membership must move
  // neither.
  const layout = async (): Promise<{ lists: string[]; tops: number[] }> => ({
    lists: await bodyRows(page).getByRole('rowheader').allInnerTexts(),
    tops: await bodyRows(page).evaluateAll((rows) =>
      rows.map((element) => element.getBoundingClientRect().y),
    ),
  })
  const settled = await layout()
  const checkbox = listMembership(page, 'Discord')
  await checkbox.check()
  await expect(checkbox).toBeFocused()
  expect(await layout()).toEqual(settled)
  const chips = await categoryChip(
    page,
    englishCopy('listPicker.filter.all'),
  ).boundingBox()
  const more = await categoryMore(page).boundingBox()
  expect(more!.height).toBe(chips!.height)
  const after = await table.boundingBox()
  expect(after).not.toBeNull()
  expect(after?.x).toBeCloseTo(before?.x ?? 0)
  expect(after?.y).toBeCloseTo(before?.y ?? 0)
  expect(after?.width).toBeCloseTo(before?.width ?? 0)
  expect(after?.height).toBeCloseTo(before?.height ?? 0)

  for (const width of [1280, 1024, 768, 320]) {
    await page.setViewportSize({ height: 900, width })
    if (width <= 768) {
      // Resizing also updates the measured quick-filter count. Compare both
      // boxes in one layout snapshot after that reactive update settles.
      await expect
        .poll(async () => {
          const stacked = await table.boundingBox()
          const settings = await rail.boundingBox()
          return (
            (settings?.y ?? 0) - ((stacked?.y ?? 0) + (stacked?.height ?? 0))
          )
        })
        .toBeGreaterThanOrEqual(0)
    }
    expect(
      await table.evaluate(
        (element) => element.scrollWidth - element.clientWidth,
      ),
    ).toBeLessThanOrEqual(1)
    await assertNoOverflow(page, `composition-editor-${width}`)
  }
})

test('the composition editor remains operable with text enlarged to 200%', async ({
  page,
  origin,
}) => {
  await page.goto(`${origin}/profiles/new`)
  await page.evaluate(() => {
    document.documentElement.style.fontSize = '200%'
  })

  await expect(
    page.getByRole('heading', {
      level: 1,
      name: 'New profile',
    }),
  ).toBeVisible()
  await expect(page.getByRole('table')).toBeVisible()
  expect(await fits(page)).toBe(true)

  const search = page.getByRole('searchbox', { name: 'Find a list' })
  await search.fill('Discord')
  await listMembership(page, 'Discord').check()
  await expect(page.getByText('1 list in the profile')).toBeVisible()
  expect(await fits(page)).toBe(true)
})

test('source refresh stays available during a forecast and reports its own busy state', async ({
  page,
  origin,
}) => {
  let releaseForecast!: () => void
  let releaseRefresh!: () => void
  const forecastHeld = new Promise<void>((resolve) => {
    releaseForecast = resolve
  })
  const refreshHeld = new Promise<void>((resolve) => {
    releaseRefresh = resolve
  })
  let forecasts = 0
  let refreshes = 0
  await page.route('**/v1/profiles/preview', async (route) => {
    forecasts += 1
    if (forecasts === 1) await forecastHeld
    await route.fulfill({ json: { targets: [] } })
  })
  await page.route('**/v1/lists/discord/refresh', async (route) => {
    refreshes += 1
    await refreshHeld
    await route.fulfill({ json: { refresh: {} } })
  })
  try {
    await page.goto(`${origin}/profiles/new`)
    await listMembership(page, 'Discord').check()
    await expect.poll(() => forecasts).toBe(1)
    const refresh = compositionRefresh(page)
    await expect(refresh).toBeEnabled()
    await refresh.click()
    await expect.poll(() => refreshes).toBe(1)
    await expect(refresh).toBeDisabled()
    await expect(refresh).toHaveAttribute('aria-busy', 'true')
    releaseRefresh()
    await expect.poll(() => forecasts).toBeGreaterThan(1)
    await expect(refresh).toBeEnabled()
    releaseForecast()
    await expect(
      page.getByText('List order sets the priority.', { exact: true }),
    ).toHaveCount(0)
    const search = (await page
      .getByRole('searchbox', { name: 'Find a list' })
      .boundingBox())!
    const categories = (await categoryFilters(page).boundingBox())!
    expect(search.y).toBeGreaterThanOrEqual(categories.y + categories.height)
  } finally {
    releaseForecast()
    releaseRefresh()
    await page.unrouteAll({ behavior: 'wait' })
  }
})
