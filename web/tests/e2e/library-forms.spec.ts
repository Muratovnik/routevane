/**
 * The library's small surfaces: the row menu, the category and list dialogs,
 * the column names a reader's software announces, and the refusal that names
 * the profiles holding what the operator tried to delete.
 *
 * The worker fixture in `support/served-product.ts` serves this suite
 * alone, over the data directory named below.
 */
import { expect, type Locator, type Page } from '@playwright/test'

import { englishCopy } from './support/copy'
import { libraryRegion, libraryRow, linkTo } from './support/queries'
import { test } from './support/served-product'

test.use({ locale: 'en-US', productData: 'library-forms' })

const box = async (locator: Locator) => {
  const found = await locator.boundingBox()
  expect(found, 'the element is on the page').not.toBeNull()
  return found!
}

test('menu items start their names on one line, with or without a glyph', async ({
  page,
  origin,
}) => {
  await page.goto(`${origin}/lists`)
  await page
    .getByRole('button', { name: /^Actions for list / })
    .first()
    .click()
  const items = page.getByRole('menuitem')
  await expect(items.nth(1)).toBeVisible()
  const lefts = await items.evaluateAll((elements) =>
    elements.map(
      (element) =>
        element.querySelector('.rv-menu__item-label')!.getBoundingClientRect()
          .left,
    ),
  )
  expect(lefts.length).toBeGreaterThanOrEqual(2)
  for (const left of lefts) expect(left).toBeCloseTo(lefts[0]!, 0)
})

for (const surface of [
  { opener: 'New category', field: 'Name' },
  { opener: 'New list', field: 'Name' },
] as const) {
  test(`"${surface.opener}" is a small dialog whose actions stand together at its end`, async ({
    page,
    origin,
  }) => {
    await page.setViewportSize({ width: 1440, height: 900 })
    await page.goto(`${origin}/lists`)
    await page
      .getByRole('button', { name: surface.opener, exact: true })
      .click()
    const dialog = page.getByRole('dialog', {
      name: surface.opener,
      exact: true,
    })
    await expect(dialog).toBeVisible()
    await dialog.evaluate(async (element) => {
      await Promise.all(
        element.getAnimations({ subtree: true }).map((a) => a.finished),
      )
    })

    const panel = await box(dialog)
    expect(panel.width).toBeLessThanOrEqual(560)
    expect(panel.height).toBeLessThan(700)
    expect(panel.x).toBeGreaterThan(200)

    // One name: as wide as a name field is drawn and no wider.
    const name = await box(dialog.getByLabel(surface.field, { exact: true }))
    expect(name.width).toBeLessThanOrEqual(385)
    expect(name.width).toBeGreaterThan(250)

    // The acts that end the dialog stand together at its end.
    const cancel = await box(
      dialog.getByRole('button', { name: 'Cancel', exact: true }),
    )
    const confirm = await box(
      dialog.getByRole('button', { name: /^(Create|Save)$/ }),
    )
    expect(cancel.x).toBeGreaterThan(panel.x + panel.width / 2)
    expect(confirm.x - (cancel.x + cancel.width)).toBeLessThan(24)
    expect(panel.x + panel.width - (confirm.x + confirm.width)).toBeLessThan(40)
  })
}

test('the categories dialog names itself once and each row has one way to open it', async ({
  page,
  origin,
}) => {
  await page.goto(`${origin}/lists`)
  await page.getByRole('button', { name: 'Categories', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Categories' })
  await expect(dialog).toBeVisible()
  await expect(dialog.getByRole('heading')).toHaveCount(1)

  // A row is opened by the whole row, so the glyphs left in the dialog are the
  // row menus' own and the one that closes it.
  const menus = dialog.getByRole('button', { name: /^Actions for category / })
  expect(await menus.count()).toBeGreaterThan(0)
  await expect(dialog.getByTestId('rv-icon')).toHaveCount(
    (await menus.count()) + 1,
  )
})

test('column headers are announced once each, and the info button says what it explains', async ({
  page,
  origin,
}) => {
  await page.goto(`${origin}/lists`)
  const headers = libraryRegion(page).getByRole('columnheader')
  const names = (await headers.allTextContents())
    .map((value) => value.trim())
    .filter((value) => value !== '')
  expect(new Set(names).size).toBe(names.length)
  await expect(
    headers.filter({ hasText: englishCopy('lists.column.actions') }),
  ).toHaveCount(1)

  await page.goto(`${origin}/profiles/new`)
  const overlaps = page.getByRole('columnheader', { name: /^Overlaps/ })
  await expect(overlaps).toHaveCount(1)
  await expect(overlaps).toHaveAccessibleName(/^Overlaps\s+About overlaps$/)
  await expect(
    overlaps.getByRole('button', { name: 'About overlaps', exact: true }),
  ).toBeVisible()
})

/** A list and two profiles holding it, one of them archived. */
const holdList = async (page: Page, origin: string) => {
  const headers = { 'X-Routevane-Request': '1' }
  const created = await page.request.post(`${origin}/v1/lists`, {
    headers,
    data: { title: 'Held list', domains: ['held.example'] },
  })
  expect(created.status()).toBe(201)
  const { list } = (await created.json()) as { list: { id: string } }
  const profiles: Record<'live' | 'archived', string> = {
    archived: '',
    live: '',
  }
  for (const key of ['live', 'archived'] as const) {
    const response = await page.request.post(`${origin}/v1/profiles`, {
      headers,
      data: {
        categories: [],
        exclusions: [],
        lists: [list.id],
        name: `Holder ${key}`,
        priority: [list.id],
      },
    })
    expect(response.status()).toBe(201)
    profiles[key] = (
      (await response.json()) as { profile: { id: string } }
    ).profile.id
  }
  expect(
    (
      await page.request.post(
        `${origin}/v1/profiles/${profiles.archived}/archive`,
        { headers, data: {} },
      )
    ).ok(),
  ).toBe(true)
  return profiles
}

test('a refused deletion links the profiles that hold the list and marks archived ones', async ({
  page,
  origin,
}) => {
  const profiles = await holdList(page, origin)
  const profileReads: string[] = []
  page.on('request', (request) => {
    const path = new URL(request.url()).pathname
    if (request.method() === 'GET' && path.startsWith('/v1/profiles/'))
      profileReads.push(path)
  })
  await page.goto(`${origin}/lists`)
  await page.getByRole('button', { name: 'Actions for list Held list' }).click()
  await page.getByRole('menuitem', { name: 'Delete the list' }).click()
  const dialog = page.getByRole('dialog', { name: 'Delete the list' })
  await dialog.getByRole('button', { name: 'Delete', exact: true }).click()

  const live = dialog.getByRole('link', { name: 'Holder live' })
  const archived = dialog.getByRole('link', { name: 'Holder archived' })
  await expect(live).toHaveAttribute('href', `/profiles/${profiles.live}`)
  await expect(archived).toHaveAttribute(
    'href',
    `/profiles/${profiles.archived}`,
  )
  await expect(
    dialog.getByText(englishCopy('lists.inUse.archived')),
  ).toHaveCount(1)
  await expect(
    dialog.getByText(englishCopy('lists.inUse.archived.next')),
  ).toBeVisible()
  await expect(
    dialog.getByText(englishCopy('lists.list.inUse.next')),
  ).toBeVisible()
  // The refusal itself says which profile is archived: no profile was read to
  // find out.
  expect(profileReads).toEqual([])
  // The list is still there: a refusal changes nothing.
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click()
  await expect(libraryRow(libraryRegion(page), 'Held list')).toBeVisible()
  await expect(linkTo(page, `/profiles/${profiles.live}`)).toHaveCount(0)
})
