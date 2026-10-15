/**
 * The line a field keeps for its error.
 *
 * A field states what is wrong with a value the moment it is left. If that
 * line were added to the layout then, everything under the field would move
 * down, and a click aimed at the button below — pressed in the same motion that
 * left the field — would land beside it. The field therefore keeps the line
 * whether or not it is used, and an error longer than the line is cut on it
 * rather than given a second one.
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

// How wide an error's text runs depends on the platform's font, the language
// and the column, and one that wrapped onto a second line moved everything
// under the field on Linux. The line's letters are spread here so that the
// text is wider than its line on any platform.
test('an error longer than its line moves nothing under it and keeps its whole text', async ({
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
  await address.evaluate((control) => {
    let field: Element | null = control.parentElement
    while (field !== null && field.querySelector('.rv-field__error') === null)
      field = field.parentElement
    const line = field?.querySelector<HTMLElement>('.rv-field__error')
    if (line === null || line === undefined)
      throw new Error('the field keeps no error line')
    line.style.letterSpacing = '1em'
  })
  await address.fill(NOT_AN_ADDRESS)
  const before = await Promise.all([address, ...below].map(box))

  await page.keyboard.press('Tab')
  const error = page.getByText(/^Expected an address like /)
  await expect(error).toBeVisible()

  expect(await Promise.all([address, ...below].map(box))).toEqual(before)
  // The text did not fit its line, so the line above held it by cutting it.
  expect(
    await error.evaluate((line) => line.scrollWidth > line.clientWidth),
  ).toBe(true)
  // Cut on the screen, whole for whoever reads it another way.
  const text = (await error.textContent()) ?? ''
  expect(text).toMatch(/^Expected an address like .+\.$/)
  await expect(error).toHaveAttribute('title', text)
  await expect(address).toHaveAccessibleDescription(
    new RegExp(text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')),
  )
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
