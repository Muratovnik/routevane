/**
 * A link that names one connection lands on it: the Publishing tab sends the
 * operator to `/connections#connection=<id>`, and the page has to open that
 * connection rather than the first one. The address is a fragment because the
 * embedded server refuses query strings.
 *
 * The worker fixture in `support/served-product.ts` serves this suite
 * alone, over the data directory named below.
 */

import { expect, type Page } from '@playwright/test'

import { buildProfile } from './support/flows'
import { chooseFormatAction } from './support/queries'
import { test } from './support/served-product'

test.use({
  locale: 'en-US',
  productData: 'connection-deep-link',
})

const NAMES = ['Rack router', 'Spare router'] as const

const seed = async (
  page: Page,
  origin: string,
  names: readonly string[] = NAMES,
): Promise<string[]> => {
  const ids: string[] = []
  for (const name of names) {
    const response = await page.request.post(`${origin}/v1/devices`, {
      data: {
        account: 'admin',
        address: 'http://10.255.0.1',
        interface: 'Wireguard0',
        name,
        target_id: 'keenetic',
      },
      headers: { Origin: origin, 'X-Routevane-Request': '1' },
    })
    expect(response.ok()).toBe(true)
    ids.push(((await response.json()) as { device: { id: string } }).device.id)
  }
  return ids
}

// A row is named by what it shows, and the connection's name comes first.
const row = (page: Page, name: string) =>
  page.getByRole('button', { name: new RegExp(`^${name} `) })

test('opens the connection a link names and focuses its heading', async ({
  page,
  origin,
  assertProductAlive,
}) => {
  const [, spare] = await seed(page, origin)
  await page.goto(`${origin}/connections#connection=${spare}`)

  const heading = page.getByRole('heading', { level: 2, name: NAMES[1] })
  await expect(heading).toBeVisible()
  await expect(heading).toBeFocused()
  await expect(row(page, NAMES[1])).toHaveAttribute('aria-current', 'true')
  await expect(row(page, NAMES[0])).not.toHaveAttribute('aria-current')

  // Picking another connection moves the address with it, so a refresh stays
  // on what the operator is looking at.
  await row(page, NAMES[0]).click()
  await expect(page).toHaveURL(/#connection=[a-f0-9]+$/)
  await page.reload()
  await expect(row(page, NAMES[0])).toHaveAttribute('aria-current', 'true')
  assertProductAlive()
})

test('opens the default connection when the link names one that is gone', async ({
  page,
  origin,
  assertProductAlive,
}) => {
  await seed(page, origin, ['Dock router', 'Shelf router'])
  // The suite's worker keeps one registry across its tests, so the default is
  // the first row of the list the seeded rows are in, not a row named here.
  const rows = page
    .getByRole('list')
    .filter({ has: row(page, 'Dock router') })
    .getByRole('listitem')
  const first = rows.first().getByRole('button')
  const second = rows.nth(1).getByRole('button')

  // What the page selects with no link at all is the default: the first.
  await page.goto(`${origin}/connections`)
  await expect(first).toHaveAttribute('aria-current', 'true')
  await expect(second).not.toHaveAttribute('aria-current')

  await page.goto(`${origin}/connections#connection=no-such-connection`)
  await expect(first).toHaveAttribute('aria-current', 'true')
  await expect(second).not.toHaveAttribute('aria-current')
  assertProductAlive()
})

test('the Publishing link of a format bound to the second connection opens that connection', async ({
  page,
  origin,
  assertProductAlive,
}) => {
  const { outputId } = await buildProfile(page, origin)
  const names = ['Alpha router', 'Beta router']
  const ids: string[] = []
  for (const name of names) {
    const response = await page.request.post(`${origin}/v1/devices`, {
      data: {
        account: 'admin',
        address: 'http://10.255.0.2',
        interface: 'Wireguard0',
        name,
        target_id: 'keenetic',
      },
      headers: { Origin: origin, 'X-Routevane-Request': '1' },
    })
    expect(response.ok()).toBe(true)
    ids.push(((await response.json()) as { device: { id: string } }).device.id)
  }
  const bound = await page.request.post(
    `${origin}/v1/outputs/${outputId}/device`,
    {
      data: { device_id: ids[1] },
      headers: { Origin: origin, 'X-Routevane-Request': '1' },
    },
  )
  expect(bound.ok()).toBe(true)

  await page.reload()
  await page
    .getByRole('tablist', { name: 'Profile sections' })
    .getByRole('tab', { name: 'Publishing' })
    .click()
  await chooseFormatAction(page, 'Keenetic', 'outputs.menu.open')

  await expect(page).toHaveURL(new RegExp(`/connections#connection=${ids[1]}$`))
  await expect(
    page.getByRole('heading', { level: 2, name: names[1] }),
  ).toBeFocused()
  await expect(row(page, names[1]!)).toHaveAttribute('aria-current', 'true')
  assertProductAlive()
})
