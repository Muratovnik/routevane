/**
 * The space a field's error takes.
 *
 * A field keeps no line for its error. Nothing is stated about a value when it
 * is left; pressing Save asks every field to state its problem, and from then
 * on the statements follow the values. An error is drawn when it exists, at the
 * width of its field, wrapping as any text does: what is under it moves down by
 * what it takes, and nothing moves sideways, however long its text is.
 *
 * The worker fixture in `support/served-product.ts` serves this suite
 * alone, over the data directory named below.
 */
import { expect, type Locator, type Page } from '@playwright/test'

import { englishCopy } from './support/copy'
import { deviceField } from './support/queries'
import { test } from './support/served-product'

test.use({ locale: 'en-US', productData: 'field-error-space' })

// The first of the connection form's fields that takes a format: submitting it
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

// Where the controls stand across the page: a column that widened would move
// or stretch them.
const columns = async (
  controls: Locator[],
): Promise<{ width: number; x: number }[]> =>
  Promise.all(
    controls.map(async (control) => {
      const { width, x } = await box(control)
      return { width, x }
    }),
  )

test('an error that appears on Save pushes what is under it down and moves nothing sideways', async ({
  page,
  origin,
}) => {
  await openCreateForm(page, origin)
  const address = deviceField(page, 'deploy.field.address.keenetic')
  const account = deviceField(page, 'deploy.field.account.keenetic')
  await address.fill(NOT_AN_ADDRESS)
  const save = page.getByRole('button', { name: 'Save', exact: true })

  // Leaving the field states nothing.
  await page.keyboard.press('Tab')
  await expect(address).not.toHaveAttribute('aria-invalid', 'true')
  await expect(page.getByText(/^Expected an address like /)).toHaveCount(0)
  const before = {
    columns: await columns([address, account]),
    save: await box(save),
  }
  const accountBefore = await box(account)

  await save.click()
  await expect(address).toHaveAttribute('aria-invalid', 'true')
  await expect(page.getByText(/^Expected an address like /)).toBeVisible()

  expect(await columns([address, account])).toEqual(before.columns)
  // The fields under the error moved down, not across.
  expect((await box(account)).y).toBeGreaterThanOrEqual(accountBefore.y)
  expect((await box(save)).x).toBe(before.save.x)
})

// How wide an error's text runs depends on the platform's font, the language
// and the column. A text that did not wrap once widened the whole form column
// (874px for a 301px one). The letters are spread and an unbreakable token is
// put in the error here so that the text is wider than its field on any
// platform: it must wrap inside its field and leave the column as it was.
test('a long error wraps inside its field and leaves the form column its width', async ({
  page,
  origin,
}) => {
  await openCreateForm(page, origin)
  const address = deviceField(page, 'deploy.field.address.keenetic')
  const account = deviceField(page, 'deploy.field.account.keenetic')
  const interfaceName = deviceField(page, 'deploy.field.interface.keenetic')
  await address.fill(NOT_AN_ADDRESS)
  const save = page.getByRole('button', { name: 'Save', exact: true })
  const controls = [
    address,
    account,
    interfaceName,
    save,
    page.getByRole('button', { name: 'Cancel', exact: true }),
  ]
  const before = await columns(controls)

  await save.click()
  const error = page.getByText(/^Expected an address like /)
  await expect(error).toBeVisible()
  expect(await columns(controls)).toEqual(before)

  // Wider than the field by its spacing, as a long phrase in a narrow column.
  await error.evaluate((line) => {
    line.style.letterSpacing = '1em'
  })
  expect(await columns(controls)).toEqual(before)
  const wrapped = await error.evaluate((line) => {
    const lineHeight = Number.parseFloat(getComputedStyle(line).lineHeight)
    return line.getBoundingClientRect().height > lineHeight * 1.5
  })
  expect(wrapped).toBe(true)

  // A token with no place to break is cut at the edge of the field, not given
  // a column of its own.
  await error.evaluate((line) => {
    line.style.letterSpacing = 'normal'
    line.textContent = `Expected ${'x'.repeat(400)}`
  })
  expect(await columns(controls)).toEqual(before)
})

test('typing a value that is refused and pressing Save in one motion presses Save', async ({
  page,
  origin,
}) => {
  await openCreateForm(page, origin)
  const address = deviceField(page, 'deploy.field.address.keenetic')
  await address.fill(NOT_AN_ADDRESS)

  // No step between typing and the press, and nothing is stated on leaving the
  // field: so an error that appears is proof the press landed.
  await page.getByRole('button', { name: 'Save', exact: true }).click()

  // Saving asks every field to state its problem, so the one that was never
  // touched speaks as well, and focus goes to the first problem.
  await expect(address).toBeFocused()
  await expect(page.getByText(/^Expected an address like /)).toBeVisible()
  await expect(
    page.getByText(englishCopy('devices.validation.required')).first(),
  ).toBeVisible()
  await expect(
    deviceField(page, 'deploy.field.account.keenetic'),
  ).toHaveAttribute('aria-invalid', 'true')

  // From then on the statements follow the value.
  await address.fill('http://10.255.0.1')
  await expect(page.getByText(/^Expected an address like /)).toHaveCount(0)
})
