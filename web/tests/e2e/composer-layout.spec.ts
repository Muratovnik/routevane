/**
 * The composer and the profile editor in a window that cannot hold the table
 * and its settings side by side: where the actions stand, what the draft says
 * about itself beside them, and the table's own filter for what is selected.
 *
 * The worker fixture in `support/served-product.ts` serves this suite
 * alone, over the data directory named below.
 */
import { expect, type Locator, type Page } from '@playwright/test'

import { englishCopy } from './support/copy'
import { buildProfile, chooseFormat } from './support/flows'
import {
  bodyRows,
  composerSettings,
  deliveryField,
  listMembership,
  nameField,
  selectedListRows,
} from './support/queries'
import { test } from './support/served-product'

test.use({ locale: 'en-US', productData: 'composer-layout' })

const box = async (locator: Locator) => {
  const found = await locator.boundingBox()
  expect(found, 'the control is on the page').not.toBeNull()
  return found!
}

/** The bottom edge of a control against the window it is read in. */
const withinWindow = async (page: Page, locator: Locator) => {
  const found = await box(locator)
  const height = page.viewportSize()!.height
  expect(found.y).toBeGreaterThanOrEqual(0)
  expect(found.y + found.height).toBeLessThanOrEqual(height)
}

// Below 1100px the rail collapses by itself, so a window of 1024px holds the
// table and its settings side by side (the composer stacks them under 54rem of
// content). The stacked layout is read at 900px, where the collapsed rail still
// leaves less than that.
test('a short window keeps the create action on screen, above the table', async ({
  page,
  origin,
}) => {
  await page.setViewportSize({ width: 900, height: 700 })
  await page.goto(`${origin}/profiles/new`)
  await page.getByRole('searchbox', { name: 'Find a list' }).fill('discord')
  await listMembership(page, 'Discord').check()
  await chooseFormat(page, /Keenetic/)

  const submit = page.getByRole('button', { name: 'Create and prepare' })
  await withinWindow(page, submit)
  expect(await page.getByRole('main').evaluate((main) => main.scrollTop)).toBe(
    0,
  )

  const settings = await box(composerSettings(page))
  const table = await box(page.getByTestId('rv-list-picker-frame'))
  expect(settings.y + settings.height).toBeLessThanOrEqual(table.y + 1)

  // The name and the format stand side by side and the action ends the band.
  const name = await box(nameField(page))
  const format = await box(deliveryField(page))
  expect(Math.abs(name.y - format.y)).toBeLessThanOrEqual(2)
  const action = await box(submit)
  expect(settings.x + settings.width - (action.x + action.width)).toBeLessThan(
    32,
  )
})

test('a collapsed rail leaves room for the settings beside the table at 1024px', async ({
  page,
  origin,
}) => {
  await page.setViewportSize({ width: 1024, height: 700 })
  await page.goto(`${origin}/profiles/new`)
  // The window, not a choice, collapsed the rail.
  await expect(
    page.getByRole('button', { name: 'Expand sidebar', exact: true }),
  ).toBeVisible()
  await page.getByRole('searchbox', { name: 'Find a list' }).fill('discord')
  await listMembership(page, 'Discord').check()
  await chooseFormat(page, /Keenetic/)

  const submit = page.getByRole('button', { name: 'Create and prepare' })
  await withinWindow(page, submit)
  const settings = await box(composerSettings(page))
  const table = await box(page.getByTestId('rv-list-picker-frame'))
  expect(settings.x).toBeGreaterThanOrEqual(table.x + table.width - 1)
})

test('the editor keeps save in the window and says the draft is unsaved', async ({
  page,
  origin,
}) => {
  await buildProfile(page, origin)
  await page.setViewportSize({ width: 1024, height: 700 })
  const save = page.getByRole('button', {
    name: 'Save and rebuild',
    exact: true,
  })
  const unsaved = page
    .getByRole('status')
    .filter({ hasText: englishCopy('profile.edit.dirty') })
  await expect(unsaved).toBeHidden()

  await nameField(page).fill('Edited name')
  await expect(unsaved).toBeVisible()
  await withinWindow(page, save)

  // Discarding is one quiet button beside the save, not a full-width row over it.
  const discard = page.getByRole('button', {
    name: englishCopy('profile.edit.discard'),
    exact: true,
  })
  const settings = await box(composerSettings(page))
  const discardBox = await box(discard)
  const saveBox = await box(save)
  expect(discardBox.width).toBeLessThan(settings.width / 2)
  expect(Math.abs(discardBox.y - saveBox.y)).toBeLessThanOrEqual(2)
  expect(discardBox.x + discardBox.width).toBeLessThanOrEqual(saveBox.x)
  const status = await box(unsaved)
  expect(
    Math.abs(status.y + status.height / 2 - saveBox.y - saveBox.height / 2),
  ).toBeLessThanOrEqual(saveBox.height)

  await discard.click()
  await expect(unsaved).toBeHidden()
})

test('beside the rail the discard button is not a full-width row either', async ({
  page,
  origin,
}) => {
  await buildProfile(page, origin)
  await page.setViewportSize({ width: 1440, height: 900 })
  await nameField(page).fill('Edited name')
  const discard = page.getByRole('button', {
    name: englishCopy('profile.edit.discard'),
    exact: true,
  })
  const settings = await box(composerSettings(page))
  expect((await box(discard)).width).toBeLessThan(settings.width * 0.7)
})

test('a pair that cannot fully build says what stops the file beside create', async ({
  page,
  origin,
}) => {
  test.setTimeout(120000)
  const headers = { 'X-Routevane-Request': '1' }
  const ids: string[] = []
  // One list states an address and one states none, so Keenetic can weigh
  // only the first while a domain format can weigh both.
  const fixtures = [
    { addresses: ['192.0.2.1'], title: 'Readable Alpha' },
    { addresses: [], title: 'Unreadable Beta' },
  ] as const
  try {
    for (const { addresses, title } of fixtures) {
      const created = await page.request.post(`${origin}/v1/lists`, {
        headers,
        data: { title, domains: ['pair.invalid'] },
      })
      expect(created.status()).toBe(201)
      const { list } = (await created.json()) as { list: { id: string } }
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
    await page.setViewportSize({ width: 1440, height: 900 })
    await page.goto(`${origin}/profiles/new`)
    for (const { title } of fixtures) await listMembership(page, title).check()
    await chooseFormat(page, /Keenetic/)

    const settings = composerSettings(page)
    const consequence = settings.getByText(
      /file will not be published until these lists have usable data: Unreadable Beta/,
    )
    await expect(consequence).toBeVisible({ timeout: 60000 })
    await expect(consequence).toContainText('Keenetic')
    // It is a statement, not a refusal: creating stays available.
    await expect(
      settings.getByRole('button', { name: 'Create and prepare' }),
    ).toBeEnabled()

    // A format that can weigh both lists says nothing of the kind.
    await chooseFormat(page, /sing-box/)
    await expect(consequence).toBeHidden()
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

test('the in-profile filter shows only what is selected and never moves a row', async ({
  page,
  origin,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto(`${origin}/profiles/new`)
  const search = page.getByRole('searchbox', { name: 'Find a list' })
  for (const title of ['YouTube', 'Discord']) {
    await search.fill(title)
    await listMembership(page, title).check()
  }
  await search.fill('')
  const rows = bodyRows(page.getByRole('main'))
  const total = await rows.count()
  expect(total).toBeGreaterThan(2)
  const selected = selectedListRows(page.getByRole('main')).getByRole(
    'rowheader',
  )
  const before = await selected.allTextContents()

  const chip = page.getByRole('button', { name: /^In profile \(\d+\)$/ })
  await expect(chip).toHaveAccessibleName('In profile (2)')
  await expect(chip).toHaveAttribute('aria-pressed', 'false')
  await chip.click()
  await expect(chip).toHaveAttribute('aria-pressed', 'true')
  await expect(rows).toHaveCount(2)
  await expect(rows.getByRole('rowheader')).toHaveText(before)

  // Unchecking inside the filter keeps the row in place until the filter is
  // left: selection never reorders, and it does not make a row vanish under the
  // pointer either.
  await listMembership(page, 'Discord').uncheck()
  await expect(rows).toHaveCount(2)
  await expect(rows.getByRole('rowheader')).toHaveText(before)
  await expect(chip).toHaveAccessibleName('In profile (1)')
  await expect(chip).toHaveAttribute('aria-pressed', 'true')

  await chip.click()
  await expect(rows).toHaveCount(total)
  await expect(selected).toHaveText(
    before.filter((title) => !title.includes('Discord')),
  )
})
