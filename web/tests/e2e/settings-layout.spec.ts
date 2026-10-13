/**
 * Settings stands on the workspace every other section shares.
 *
 * The worker fixture in `support/served-product.ts` serves this suite alone,
 * over the data directory named below.
 */

import { expect } from '@playwright/test'

import { test } from './support/served-product'

test.use({ locale: 'en-US', productData: 'settings-layout' })

test('settings is as wide as the other sections, not a narrower column of its own', async ({
  page,
  origin,
}) => {
  await page.setViewportSize({ width: 1600, height: 900 })
  await page.goto(`${origin}/settings`)
  const interfaceSection = page.getByRole('region', {
    name: 'Interface',
    exact: true,
  })
  await expect(interfaceSection).toBeVisible()
  const settings = (await interfaceSection.boundingBox())!

  await page.goto(`${origin}/connections`)
  const connections = page.getByRole('region', {
    name: 'Connections',
    exact: true,
  })
  await expect(connections).toBeVisible()
  const workspace = (await connections.boundingBox())!

  // Both fill the shell's workspace between its insets, so their edges agree.
  expect(settings.x).toBeCloseTo(workspace.x, 0)
  expect(settings.width).toBeCloseTo(workspace.width, 0)
})
