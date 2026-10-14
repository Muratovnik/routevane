/**
 * The line a field keeps for its error.
 *
 * A field states what is wrong with a value the moment it is left. If that
 * line were added to the layout then, everything under the field would move
 * down, and a click aimed at the button below — pressed in the same motion that
 * left the field — would land beside it. The field therefore keeps the line
 * whether or not it is used.
 *
 * The worker fixture in `support/served-product.ts` serves this suite
 * alone, over the data directory named below.
 */
import { expect, type Locator, type Page } from '@playwright/test'

import { englishCopy } from './support/copy'
import { deviceField } from './support/queries'
import { test } from './support/served-product'

test.use({ locale: 'en-US', productData: 'field-error-space' })

// The first of the connection form's fields that takes a format: leaving it
// with a value that is not an address is the case the field must absorb.
const NOT_AN_ADDRESS = 'not an address'

const openCreateForm = async (page: Page, origin: string): Promise<void> => {
  await page.goto(`${origin}/connections`)
  await page
    .getByRole('button', { name: 'Add a connection', exact: true })
    .click()
  await deviceField(page, 'devices.field.target').click()
  await page.getByRole('option', { name: 'Keenetic' }).click()
  await expect(page.getByRole('listbox')).toHaveCount(0)
  await deviceField(page, 'devices.field.name').fill('Home router')
}

const box = async (
  locator: Locator,
): Promise<{ height: number; width: number; x: number; y: number }> => {
  const found = await locator.boundingBox()
  expect(found).not.toBeNull()
  return found!
}

test('an error that appears on leaving a field moves nothing under it', async ({
  page,
  origin,
}) => {
  await openCreateForm(page, origin)
  const address = deviceField(page, 'deploy.field.address.keenetic')
  const below = [
    deviceField(page, 'deploy.field.account.keenetic'),
    deviceField(page, 'deploy.field.interface.keenetic'),
    page.getByRole('button', { name: 'Save', exact: true }),
    page.getByRole('button', { name: 'Cancel', exact: true }),
  ]
  await address.fill(NOT_AN_ADDRESS)
  const before = await Promise.all([address, ...below].map(box))

  // Leaving the field is what states its problem.
  await page.keyboard.press('Tab')
  await expect(address).toHaveAttribute('aria-invalid', 'true')
  await expect(page.getByText(/^Expected an address like /)).toBeVisible()

  expect(await Promise.all([address, ...below].map(box))).toEqual(before)
})

test('typing a value that is refused and pressing Save in one motion presses Save', async ({
  page,
  origin,
}) => {
  await openCreateForm(page, origin)
  await deviceField(page, 'deploy.field.address.keenetic').fill(NOT_AN_ADDRESS)

  // No step between typing and the press: the press itself leaves the field.
  await page.getByRole('button', { name: 'Save', exact: true }).click()

  // Saving asks every field to state its problem, so the one that was never
  // visited speaks as well; had the press missed, only the address would.
  await expect(
    page.getByText(englishCopy('devices.validation.required')).first(),
  ).toBeVisible()
  await expect(
    deviceField(page, 'deploy.field.account.keenetic'),
  ).toHaveAttribute('aria-invalid', 'true')
})
