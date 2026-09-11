/**
 * The forecast a draft is weighed by: the overlaps it explains and the lists
 * it cannot calculate, neither of which may change the library.
 *
 * The worker fixture in `support/served-product.ts` serves this suite
 * alone, over the data directory named below.
 */
import { join } from 'node:path'

import AxeBuilder from '@axe-core/playwright'
import { expect } from '@playwright/test'

import { reviewRoot } from './support/audits'
import { englishCopy } from './support/copy'
import { chooseFormat, FORECAST_PATH } from './support/flows'
import {
  forecastStatus,
  listMembership,
  listRow,
  overlapsCell,
} from './support/queries'
import { test } from './support/served-product'

// The surface negotiates its language from the browser, and English is the
// product's primary language. `productData` is this suite's own data
// directory: it starts empty, no other suite writes to it, and it is gone
// again when the suite ends.
test.use({ locale: 'en-US', productData: 'forecast' })

test('the forecast explains overlaps in create and edit without rewriting the library', async ({
  page,
  origin,
  assertProductAlive,
}) => {
  test.setTimeout(90000)
  const headers = { 'X-Routevane-Request': '1' }
  const ids: string[] = []
  for (const [title, domains, values] of [
    ['Overlap Alpha', ['shared.example', 'alpha.example'], ['192.0.2.0/24']],
    ['Overlap Beta', ['shared.example', 'beta.example'], ['192.0.2.1']],
    ['Overlap Gamma', ['gamma.example'], ['198.51.100.9']],
  ] as const) {
    const created = await page.request.post(`${origin}/v1/lists`, {
      headers,
      data: { title, domains },
    })
    expect(created.status()).toBe(201)
    const { list: list } = (await created.json()) as {
      list: { id: string }
    }
    ids.push(list.id)
    // Every fixture above states the addresses its overlaps are computed from,
    // so the include is part of building the fixture rather than a case of it.
    expect(
      (
        await page.request.post(`${origin}/v1/lists/${list.id}/domains`, {
          headers,
          data: { values, verdict: 'include' },
        })
      ).ok(),
    ).toBe(true)
    expect(
      (
        await page.request.post(`${origin}/v1/lists/${list.id}/refresh`, {
          headers,
          data: {},
        })
      ).ok(),
    ).toBe(true)
  }
  const beforeRoutes = await (
    await page.request.get(`${origin}/v1/profiles`)
  ).text()
  const beforeContents = await Promise.all(
    ids.map(async (id) =>
      (await page.request.get(`${origin}/v1/lists/${id}/contents`)).text(),
    ),
  )
  let inspecting = true
  const writes: string[] = []
  const errors: string[] = []
  page.on('pageerror', (error) => errors.push(error.message))
  page.on('request', (request) => {
    const path = new URL(request.url()).pathname
    if (inspecting && request.method() === 'POST' && path !== FORECAST_PATH)
      writes.push(path)
  })
  await page.goto(`${origin}/profiles/new`)
  const select = async (index: number, title: string, checked: boolean) => {
    await page.getByRole('searchbox', { name: 'Find a list' }).fill(title)
    await listMembership(page, title).setChecked(checked)
  }
  await select(0, 'Overlap Alpha', true)
  await select(1, 'Overlap Beta', true)
  await page.getByRole('searchbox', { name: 'Find a list' }).fill('')
  await chooseFormat(page, /sing-box/)
  const alphaRow = listRow(page, 'Overlap Alpha')
  const betaRow = listRow(page, 'Overlap Beta')
  await expect(alphaRow).toContainText('Overlap: Overlap Beta')
  // The count is the control that lists the overlaps, and it starts at the
  // leading edge of the cell it sits in rather than at an inherited indent.
  const countInset = await alphaRow
    .getByRole('button', { name: 'Overlap: Overlap Beta', exact: true })
    .evaluate((count) => {
      const cell = count.closest('td')!
      const range = document.createRange()
      range.selectNodeContents(count)
      return (
        range.getBoundingClientRect().left -
        cell.getBoundingClientRect().left -
        Number.parseFloat(getComputedStyle(cell).paddingLeft)
      )
    })
  expect(Math.abs(countInset)).toBeLessThanOrEqual(1)
  await expect(betaRow).toContainText('Overlap: Overlap Alpha')
  await alphaRow
    .getByRole('button', { name: 'Overlap: Overlap Beta', exact: true })
    .click()
  await expect(page.getByRole('dialog')).toContainText('Overlaps with lists')
  await expect(page.getByRole('dialog').getByRole('listitem')).toHaveText(
    'Overlap Beta',
  )
  await page.keyboard.press('Escape')
  await expect(
    page.getByRole('button', { name: 'How overlaps are resolved' }),
  ).toHaveCount(0)
  for (const width of [320, 768, 1024, 1440]) {
    await page.setViewportSize({ width, height: 1000 })
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true)
    const axe = await new AxeBuilder({ page }).analyze()
    expect(
      axe.violations.filter(
        (item) => item.impact === 'serious' || item.impact === 'critical',
      ),
    ).toEqual([])
    await page.screenshot({
      path: join(reviewRoot, `overlap-tags-create-${width}.png`),
      fullPage: true,
    })
  }
  let fail = false
  let release = () => {}
  let held: Promise<void> | undefined
  await page.route(`**${FORECAST_PATH}`, async (route) => {
    if (held !== undefined) await held
    if (fail)
      await route.fulfill({
        status: 503,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'unavailable' }),
      })
    else await route.continue()
  })
  try {
    held = new Promise<void>((resolve) => {
      release = resolve
    })
    await select(2, 'Overlap Gamma', true)
    await expect(forecastStatus(page, 'Calculating overlaps…')).toBeVisible()
    const pendingTableY = (await page
      .getByTestId('rv-list-picker-frame')
      .boundingBox())!.y
    const recalculated = page.waitForResponse(
      (response) => new URL(response.url()).pathname === FORECAST_PATH,
    )
    release()
    held = undefined
    const recalculatedResponse = await recalculated
    expect(
      recalculatedResponse.status(),
      await recalculatedResponse.text(),
    ).toBe(200)
    await expect(forecastStatus(page, 'Calculating overlaps…')).toBeHidden()
    expect(
      (await page.getByTestId('rv-list-picker-frame').boundingBox())!.y,
    ).toBeCloseTo(pendingTableY, 0)
    fail = true
    const failed = page.waitForResponse(
      (response) => new URL(response.url()).pathname === FORECAST_PATH,
    )
    await select(2, 'Overlap Gamma', false)
    expect((await failed).status()).toBe(503)
    await expect(
      page.getByText('Calculation unavailable. Try again.'),
    ).toBeVisible()
    const retry = page.getByRole('button', {
      name: 'Recalculate',
      exact: true,
    })
    await expect(retry).toBeVisible()
    fail = false
    await retry.click()
    await page.getByRole('searchbox', { name: 'Find a list' }).fill('')
    await expect(
      alphaRow.getByRole('button', {
        name: 'Overlap: Overlap Beta',
        exact: true,
      }),
    ).toHaveCount(1)
    await select(2, 'Overlap Gamma', true)
    await select(1, 'Overlap Beta', false)
    await page.getByRole('searchbox', { name: 'Find a list' }).fill('')
    await expect(
      alphaRow.getByRole('button', {
        name: 'Overlap: Overlap Beta',
        exact: true,
      }),
    ).toHaveCount(0)
    await select(1, 'Overlap Beta', true)
    await select(2, 'Overlap Gamma', false)
    await page.getByRole('searchbox', { name: 'Find a list' }).fill('')
    await expect(
      alphaRow.getByRole('button', {
        name: 'Overlap: Overlap Beta',
        exact: true,
      }),
    ).toHaveCount(1)
  } finally {
    release()
  }
  expect(writes).toEqual([])
  expect(await (await page.request.get(`${origin}/v1/profiles`)).text()).toBe(
    beforeRoutes,
  )
  for (const [index, id] of ids.entries())
    expect(
      await (
        await page.request.get(`${origin}/v1/lists/${id}/contents`)
      ).text(),
    ).toBe(beforeContents[index])
  inspecting = false
  const built = page.waitForResponse(
    (response) =>
      response.request().method() === 'POST' &&
      /\/v1\/outputs\/[a-f0-9]{32}\/build$/.test(
        new URL(response.url()).pathname,
      ),
  )
  await page.getByRole('button', { name: 'Create and prepare' }).click()
  expect((await built).ok()).toBe(true)
  await page.getByRole('tab', { name: 'Contents', exact: true }).click()
  // The saved profile's own composition tab holds the editor.
  const editor = page.getByRole('tabpanel', { name: 'Contents' })
  await expect(listRow(editor, 'Overlap Alpha')).toContainText(
    'Overlap: Overlap Beta',
  )
  await expect(
    editor.getByRole('button', { name: 'How overlaps are resolved' }),
  ).toHaveCount(0)
  await expect(
    editor.getByRole('button', { name: 'Save and rebuild', exact: true }),
  ).toBeDisabled()
  expect(errors).toEqual([])
  assertProductAlive()
})

test('one list without IP coverage preserves other lists and domain-format forecasts', async ({
  page,
  origin,
}) => {
  const headers = { 'X-Routevane-Request': '1' }
  const ids: string[] = []
  // The subject of this test is the one list with no address of its own, so
  // each fixture states the addresses it carries and the empty case is a case
  // of the table rather than a branch in the loop below.
  const fixtures = [
    { addresses: ['192.0.2.1'], title: 'Partial Alpha' },
    { addresses: ['192.0.2.1'], title: 'Partial Beta' },
    { addresses: [], title: 'Partial Missing' },
    { addresses: ['192.0.2.2'], title: 'Partial Separate' },
  ] as const
  try {
    for (const { addresses, title } of fixtures) {
      const created = await page.request.post(`${origin}/v1/lists`, {
        headers,
        data: { title, domains: ['forecast.invalid'] },
      })
      expect(created.status()).toBe(201)
      const { list: list } = (await created.json()) as {
        list: { id: string }
      }
      ids.push(list.id)
      for (const address of addresses)
        expect(
          (
            await page.request.post(`${origin}/v1/lists/${list.id}/domains`, {
              headers,
              data: { values: [address], verdict: 'include' },
            })
          ).ok(),
        ).toBe(true)
      expect(
        (
          await page.request.post(`${origin}/v1/lists/${list.id}/refresh`, {
            headers,
            data: {},
          })
        ).ok(),
      ).toBe(true)
    }
    const response = await page.request.post(`${origin}/v1/profiles/preview`, {
      headers,
      data: { lists: ids, priority: ids },
    })
    expect(response.status()).toBe(200)
    const { targets } = (await response.json()) as {
      targets: {
        target_id: string
        incomplete_lists?: string[]
        projected_rules: number
        fits: boolean
      }[]
    }
    expect(
      targets.find((x) => x.target_id === 'keenetic')?.incomplete_lists,
    ).toEqual([ids[2]])
    expect(targets.find((x) => x.target_id === 'keenetic')?.fits).toBe(false)
    expect(
      targets.find((x) => x.target_id === 'singbox')?.incomplete_lists ?? [],
    ).toEqual([])
    expect(targets.find((x) => x.target_id === 'singbox')?.fits).toBe(true)
    await page.setViewportSize({ width: 1440, height: 900 })
    await page.goto(`${origin}/profiles/new`)
    for (const { title } of fixtures) await listMembership(page, title).check()
    await chooseFormat(page, /Keenetic/)
    await expect(
      forecastStatus(page, 'Some lists could not be calculated'),
    ).toBeVisible({ timeout: 60000 })
    // Each of the two lists that share an address credits the other, and the
    // count is the control that names it.
    for (const { title } of fixtures.slice(0, 2))
      await expect(
        listRow(page, title).getByRole('button', { name: /^Overlap: / }),
      ).toHaveText('1')
    // The separate list overlaps nothing, so its cell states a dash and offers
    // nothing to open.
    await expect(overlapsCell(listRow(page, 'Partial Separate'))).toHaveText(
      '—',
    )
    await page
      .getByRole('button', { name: englishCopy('forecast.partial.missing') })
      .click()
    await expect(
      page.getByText(
        'These lists have no data usable by the selected format. Refresh sources or choose another format:',
      ),
    ).toBeVisible()
    await expect(page.getByRole('dialog')).toContainText('Partial Missing')
    await page.keyboard.press('Escape')
    // The list without usable data has no forecast at all, which is the same
    // thing its cell is named after.
    await expect(
      listRow(page, 'Partial Missing').getByRole('cell', {
        name: englishCopy('listPicker.rules.unknown'),
        exact: true,
      }),
    ).toHaveText('—')
    await expect(
      page.getByRole('button', { name: 'Create and prepare', exact: true }),
    ).toBeEnabled()
  } finally {
    for (const id of ids)
      expect(
        (
          await page.request.post(`${origin}/v1/lists/${id}/remove`, {
            headers,
            data: {},
          })
        ).ok(),
      ).toBe(true)
  }
})
