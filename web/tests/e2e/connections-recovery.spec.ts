import { expect } from '@playwright/test'

import { test } from './support/served-product'

test.use({ locale: 'en-US', productData: 'connections-recovery' })

test('a saved connection survives a failed registry read and retries without another write', async ({
  page,
  origin,
  assertProductAlive,
}) => {
  const created = await page.request.post(`${origin}/v1/devices`, {
    headers: { 'X-Routevane-Request': '1' },
    data: {
      target_id: 'keenetic',
      name: 'Recovery router',
      address: 'http://192.168.1.1',
      account: 'admin',
      interface: 'Wireguard0',
    },
  })
  expect(created.ok()).toBe(true)
  const { device } = (await created.json()) as { device: { id: string } }
  await page.goto(`${origin}/connections`)
  await expect(page.getByLabel('Device address')).toHaveValue(
    'http://192.168.1.1',
  )

  let writes = 0
  let reads = 0
  page.on('request', (request) => {
    if (request.url() === `${origin}/v1/devices/${device.id}/update`)
      writes += 1
  })
  await page.route(`${origin}/v1/devices`, async (route) => {
    reads += 1
    if (reads === 1) await route.abort('failed')
    else await route.continue()
  })

  await page.getByLabel('Connection name').fill('Saved router')
  await page.getByLabel('Device address').fill('http://192.168.1.9')
  await page.getByRole('button', { name: 'Save', exact: true }).click()

  await expect(
    page.getByText('Connections could not be refreshed'),
  ).toBeVisible()
  await expect(
    page.getByRole('heading', { name: 'Saved router', exact: true }),
  ).toBeVisible()
  await expect(page.getByLabel('Device address')).toHaveValue(
    'http://192.168.1.9',
  )
  await expect(
    page.getByRole('button', { name: 'Save', exact: true }),
  ).toBeDisabled()
  const persisted = await page.request.get(`${origin}/v1/devices`)
  expect(await persisted.json()).toMatchObject({
    devices: [
      { id: device.id, name: 'Saved router', address: 'http://192.168.1.9' },
    ],
  })

  await page.getByRole('button', { name: 'Retry', exact: true }).click()
  await expect(
    page.getByText('Connections could not be refreshed'),
  ).toHaveCount(0)
  await expect(page.getByLabel('Device address')).toHaveValue(
    'http://192.168.1.9',
  )
  expect(writes).toBe(1)
  expect(reads).toBe(2)
  assertProductAlive()
})
