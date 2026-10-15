/**
 * A button that stands beside a field stands on the field's control.
 *
 * A field keeps no line for its error: an error exists only after Save has
 * found a problem (`field-error-space.spec.ts`), and a button beside the field
 * stands in the field's own row (the saved-password form) or on its control's
 * bottom edge. This suite measures the places where a button shares a row with
 * a field: the saved-password form, the list card's rename and the profile's
 * Publishing toolbar.
 *
 * The worker fixture in `support/served-product.ts` serves this suite alone,
 * over the data directory named below.
 */
import { expect, type Locator, type Page } from '@playwright/test'

import { englishCopy as copy } from './support/copy'
import { routeFakeDevices, routerDevice } from './support/fakeDevices'
import {
  bodyRows,
  deviceField,
  libraryRegion,
  libraryRowName,
  scheduleField,
  titleField,
} from './support/queries'
import { test } from './support/served-product'

test.use({ locale: 'en-US', productData: 'field-action-alignment' })

const headers = { 'X-Routevane-Request': '1' }

const box = async (
  locator: Locator,
): Promise<{ bottom: number; top: number }> => {
  const found = await locator.boundingBox()
  expect(found).not.toBeNull()
  return { bottom: found!.y + found!.height, top: found!.y }
}

/** The button's bottom edge is the control's, to a pixel. */
const expectOnTheControl = async (
  button: Locator,
  control: Locator,
): Promise<void> => {
  const [action, field] = await Promise.all([box(button), box(control)])
  expect(Math.abs(action.bottom - field.bottom)).toBeLessThanOrEqual(1)
}

test('the password buttons stand on the password control', async ({
  page,
  origin,
}) => {
  await routeFakeDevices(page, [routerDevice()])
  await page.goto(`${origin}/connections`)
  const detail = page.getByRole('region', { name: 'Home router', exact: true })
  const credential = detail.getByRole('region', {
    name: copy('devices.credential.title'),
    exact: true,
  })
  const password = deviceField(credential, 'deploy.field.password.keenetic')
  await expect(password).toBeVisible()

  await expectOnTheControl(
    credential.getByRole('button', {
      name: copy('devices.credential.save'),
      exact: true,
    }),
    password,
  )
})

test('the replace-password buttons stand on the password control', async ({
  page,
  origin,
}) => {
  await routeFakeDevices(page, [routerDevice({ credential_stored: true })])
  await page.goto(`${origin}/connections`)
  const credential = page
    .getByRole('region', { name: 'Home router', exact: true })
    .getByRole('region', {
      name: copy('devices.credential.title'),
      exact: true,
    })
  await credential
    .getByRole('button', { name: copy('devices.credential.replace') })
    .click()
  const password = deviceField(credential, 'deploy.field.password.keenetic')
  await expect(password).toBeVisible()

  for (const name of [copy('devices.credential.save'), copy('action.cancel')])
    await expectOnTheControl(
      credential.getByRole('button', { name, exact: true }),
      password,
    )
})

test('the rename button stands on the name control', async ({
  page,
  origin,
}) => {
  const title = 'Alignment fixture'
  const created = await page.request.post(`${origin}/v1/lists`, {
    headers,
    data: { domains: ['alignment.example'], title },
  })
  expect(created.status()).toBe(201)
  const { list } = (await created.json()) as { list: { id: string } }
  try {
    await page.goto(`${origin}/lists`)
    const row = bodyRows(libraryRegion(page)).filter({ hasText: title })
    await libraryRowName(row).click()
    const card = page.getByRole('dialog', { name: title, exact: true })
    await expect(titleField(card)).toBeVisible()

    await expectOnTheControl(
      card.getByRole('button', { name: copy('listCard.title.save') }),
      titleField(card),
    )
  } finally {
    const removed = await page.request.post(
      `${origin}/v1/lists/${list.id}/remove`,
      { headers, data: {} },
    )
    expect(removed.status()).toBe(204)
  }
})

const openPublishing = async (page: Page, origin: string): Promise<void> => {
  const created = await page.request.post(`${origin}/v1/profiles`, {
    headers,
    data: {
      categories: [],
      exclusions: [],
      lists: ['youtube'],
      name: 'Alignment profile',
    },
  })
  expect(created.status()).toBe(201)
  const { profile } = (await created.json()) as { profile: { id: string } }
  await page.goto(`${origin}/profiles/${profile.id}#tab=outputs`)
}

test('the Add a format control stands on the refresh field control', async ({
  page,
  origin,
}) => {
  await openPublishing(page, origin)
  const schedule = scheduleField(page)
  await expect(schedule).toBeVisible()

  await expectOnTheControl(
    page.getByRole('button', { name: copy('outputs.add') }),
    schedule,
  )
})
