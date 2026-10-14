import { expect } from '@playwright/test'
import { mkdir } from 'node:fs/promises'
import { join } from 'node:path'

import { reviewRoot } from './support/audits'
import { test } from './support/served-product'

test.use({ locale: 'en-US', productData: 'connections-forget-recovery' })

test('a forgotten connection leaves the real registry and the empty view keeps GET-only recovery', async ({
  page,
  origin,
  assertProductAlive,
}) => {
  const created = await page.request.post(`${origin}/v1/devices`, {
    headers: { Origin: origin, 'X-Routevane-Request': '1' },
    data: {
      target_id: 'keenetic',
      name: 'Forgotten router',
      address: 'http://192.0.2.1',
      account: 'admin',
      interface: 'Wireguard0',
    },
  })
  expect(created.ok()).toBe(true)
  const { device } = (await created.json()) as { device: { id: string } }
  await page.goto(`${origin}/connections`)
  await page
    .getByRole('button', { name: 'Add a connection', exact: true })
    .click()
  await page.getByLabel('Format', { exact: true }).click()
  await page.getByRole('option', { name: 'Keenetic', exact: true }).click()
  await page
    .getByLabel('Connection name', { exact: true })
    .fill('Retained draft')
  await page
    .getByRole('button', {
      name: 'Configure connection Forgotten router',
      exact: true,
    })
    .click()

  let writes = 0
  let reads = 0
  page.on('request', (request) => {
    if (request.url() === `${origin}/v1/devices/${device.id}/forget`)
      writes += 1
  })
  await page.route(`${origin}/v1/devices`, async (route) => {
    reads += 1
    if (reads === 1) await route.abort('failed')
    else await route.continue()
  })
  await page
    .getByRole('button', {
      name: 'Actions for connection Forgotten router',
    })
    .click()
  await page.getByRole('menuitem', { name: 'Forget this connection' }).click()
  await page
    .getByRole('button', {
      name: 'Forget this connection',
      exact: true,
    })
    .click()

  const stale = page.getByText('Connections could not be refreshed')
  await expect(stale).toBeVisible()
  await expect(
    page.getByRole('heading', {
      name: 'Forgotten router',
      exact: true,
    }),
  ).toHaveCount(0)
  const empty = page.getByRole('heading', {
    name: 'No connections yet',
    exact: true,
  })
  await expect(empty).toBeVisible()
  await expect(empty).toBeFocused()
  const registry = await page.request.get(`${origin}/v1/devices`)
  expect(registry.ok()).toBe(true)
  expect(await registry.json()).toMatchObject({ devices: [] })
  await mkdir(reviewRoot, { recursive: true })
  await page.screenshot({
    animations: 'disabled',
    path: join(reviewRoot, 'connections-forgotten-empty-stale.png'),
    fullPage: true,
  })

  await page.getByRole('button', { name: 'Retry', exact: true }).click()
  await expect(stale).toHaveCount(0)
  await expect(empty).toBeVisible()
  await page
    .getByRole('button', { name: 'Add a connection', exact: true })
    .click()
  await expect(page.getByLabel('Connection name', { exact: true })).toHaveValue(
    'Retained draft',
  )
  expect(writes).toBe(1)
  expect(reads).toBe(2)
  assertProductAlive()
})
