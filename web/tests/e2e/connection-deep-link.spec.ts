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

import { englishCopy } from './support/copy'
import { buildProfile } from './support/flows'
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

const row = (page: Page, name: string) =>
  page.getByRole('button', {
    name: englishCopy('devices.configure.aria').replace('{name}', name),
  })

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
  await expect(row(page, NAMES[1])).toHaveAttribute('aria-pressed', 'true')
  await expect(row(page, NAMES[0])).toHaveAttribute('aria-pressed', 'false')

  // Picking another connection moves the address with it, so a refresh stays
  // on what the operator is looking at.
  await row(page, NAMES[0]).click()
  await expect(page).toHaveURL(/#connection=[a-f0-9]+$/)
  await page.reload()
  await expect(row(page, NAMES[0])).toHaveAttribute('aria-pressed', 'true')
  assertProductAlive()
})

test('opens the default connection when the link names one that is gone', async ({
  page,
  origin,
  assertProductAlive,
}) => {
  await seed(page, origin, ['Dock router', 'Shelf router'])
  const selected = page.getByRole('button', {
    pressed: true,
    name: /^Configure connection /,
  })

  // What the page selects with no link at all is the default.
  await page.goto(`${origin}/connections`)
  await expect(selected).toHaveCount(1)
  const fallback = await selected.getAttribute('aria-label')

  await page.goto(`${origin}/connections#connection=no-such-connection`)
  await expect(selected).toHaveCount(1)
  await expect(selected).toHaveAttribute('aria-label', fallback ?? '')
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
  await page
    .getByRole('link', { name: englishCopy('outputs.readiness.open') })
    .click()

  await expect(page).toHaveURL(new RegExp(`/connections#connection=${ids[1]}$`))
  await expect(
    page.getByRole('heading', { level: 2, name: names[1] }),
  ).toBeFocused()
  await expect(row(page, names[1]!)).toHaveAttribute('aria-pressed', 'true')
  assertProductAlive()
})
