/**
 * A connection's password is its own saved fact, and automatic delivery is a
 * separate switch that follows it (ADR 0050).
 *
 * The built product keeps a password in the operating system's credential
 * store, which a browser suite must never write to, so the password and
 * delivery routes are answered by `support/fakeDevices.ts` with the contract
 * the real routes keep. The credential-free walk at the end runs against the
 * product itself.
 *
 * The worker fixture in `support/served-product.ts` serves this suite alone,
 * over the data directory named below.
 */

import { expect, type Page } from '@playwright/test'

import { englishCopy as copy } from './support/copy'
import { localConfigAddress } from './support/environment'
import {
  routeFakeDevices,
  routerDevice,
  type FakeDevice,
} from './support/fakeDevices'
import { deviceField } from './support/queries'
import { test } from './support/served-product'

test.use({ locale: 'en-US', productData: 'connection-credential' })

const TYPED = 'dummy-router-value'

const openConnection = async (
  page: Page,
  origin: string,
  device: FakeDevice,
) => {
  const registry = await routeFakeDevices(page, [device])
  await page.goto(`${origin}/connections`)
  const detail = page.getByRole('region', { name: device.name, exact: true })
  await expect(detail).toBeVisible()
  return {
    registry,
    detail,
    credential: detail.getByRole('region', {
      name: copy('devices.credential.title'),
      exact: true,
    }),
    delivery: detail.getByRole('region', {
      name: copy('devices.auto.title'),
      exact: true,
    }),
  }
}

test('saving a password, switching delivery on, and removing the password are separate facts', async ({
  page,
  origin,
}) => {
  const { registry, credential, delivery } = await openConnection(
    page,
    origin,
    routerDevice(),
  )
  const toggle = delivery.getByRole('switch', {
    name: copy('devices.auto.switch'),
  })

  // No password yet: the switch is unavailable and says why in words beside
  // it, and the password's own consequence stands beside its action.
  await expect(toggle).toBeDisabled()
  await expect(
    delivery.getByText(copy('devices.auto.needsCredential')),
  ).toBeVisible()
  await expect(
    credential.getByText(copy('devices.credential.where')),
  ).toBeVisible()
  await expect(
    delivery.getByRole('button', { name: /turn on automatic delivery/i }),
  ).toHaveCount(0)

  await deviceField(credential, 'deploy.field.password.keenetic').fill(TYPED)
  await credential
    .getByRole('button', { name: copy('devices.credential.save'), exact: true })
    .click()
  await expect(
    credential.getByText(copy('devices.credential.saved')),
  ).toBeVisible()
  // The password goes to the server once, and the page keeps no field for it.
  expect(registry.writes).toEqual([
    { path: 'fake-router/credential', body: { credential: TYPED } },
  ])
  await expect(
    deviceField(credential, 'deploy.field.password.keenetic'),
  ).toHaveCount(0)

  // The switch now answers for the server's state and carries no password.
  await expect(toggle).toBeEnabled()
  await expect(toggle).not.toBeChecked()
  await toggle.click()
  await expect(toggle).toBeChecked()
  expect(registry.writes.at(-1)).toEqual({
    path: 'fake-router/auto-delivery',
    body: { enabled: true },
  })
  await expect(
    page.getByRole('button', { name: 'Configure connection Home router' }),
  ).toContainText(copy('devices.auto.on'))

  // Turning delivery off keeps the saved password.
  await toggle.click()
  await expect(toggle).not.toBeChecked()
  await expect(
    credential.getByText(copy('devices.credential.saved')),
  ).toBeVisible()
  expect(registry.device('fake-router').credential_stored).toBe(true)

  // Removing the password takes the switch's precondition away with it.
  await toggle.click()
  await expect(toggle).toBeChecked()
  await expect(
    credential.getByText(copy('devices.credential.removeEffect')),
  ).toBeVisible()
  await credential
    .getByRole('button', {
      name: copy('devices.credential.remove'),
      exact: true,
    })
    .click()
  await expect(
    credential.getByText(copy('devices.credential.saved')),
  ).toHaveCount(0)
  await expect(
    credential.getByRole('button', {
      name: copy('devices.credential.save'),
      exact: true,
    }),
  ).toBeVisible()
  await expect(toggle).not.toBeChecked()
  await expect(toggle).toBeDisabled()
})

test('changing the address states the consequence beside Save and then clears the password', async ({
  page,
  origin,
}) => {
  const { detail, credential, delivery } = await openConnection(
    page,
    origin,
    routerDevice({ credential_stored: true, auto_deliver: true }),
  )
  const name = deviceField(detail, 'devices.field.name')
  const address = deviceField(detail, 'deploy.field.address.keenetic')
  const warning = copy('devices.revoke.title')

  // A name-only change says nothing of the kind.
  await name.fill('Renamed router')
  await expect(detail.getByText(warning)).toHaveCount(0)
  await name.fill('Home router')

  await address.fill('http://192.168.1.77')
  await expect(detail.getByText(warning)).toBeVisible()
  await expect(
    detail.getByText(copy('devices.consequence.credential')),
  ).toBeVisible()
  await expect(
    detail.getByText(copy('devices.consequence.autoDelivery')),
  ).toBeVisible()
  await expect(
    detail.getByText(copy('devices.consequence.routes')),
  ).toBeVisible()

  // The form stays the same element across the write, and focus stays inside
  // it: re-reading what the registry holds does not rebuild the card.
  const form = await name.elementHandle()
  await detail.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(
    detail.getByText(
      `${copy('devices.updated')} ${copy('devices.updated.credential')} ${copy('devices.updated.autoDelivery')}`,
    ),
  ).toBeVisible()
  expect(await form!.evaluate((element) => element.isConnected)).toBe(true)
  // Pressing Save with the pointer leaves the operator in the field they were
  // typing in; it is not a reason to move them.
  await expect(address).toBeFocused()

  // Saved from the keyboard, the button itself has nothing left to save and
  // becomes unavailable, so focus returns to the first field instead of the
  // page.
  await deviceField(detail, 'deploy.field.interface.keenetic').fill(
    'Wireguard1',
  )
  const save = detail.getByRole('button', { name: 'Save', exact: true })
  await save.focus()
  await page.keyboard.press('Enter')
  await expect(save).toBeDisabled()
  await expect(name).toBeFocused()

  // What the server answered is what the page now states.
  await expect(
    credential.getByText(copy('devices.credential.saved')),
  ).toHaveCount(0)
  await expect(
    delivery.getByRole('switch', { name: copy('devices.auto.switch') }),
  ).not.toBeChecked()
  await expect(detail.getByText(warning)).toHaveCount(0)
})

test('a refused save names the address when the address was what changed', async ({
  page,
  origin,
}) => {
  const { registry, detail } = await openConnection(
    page,
    origin,
    routerDevice(),
  )
  const address = deviceField(detail, 'deploy.field.address.keenetic')

  // The refusal carries no field, so it is attributed to the address only
  // when the address is the thing that changed.
  registry.refuseNextUpdate()
  await address.fill('http://192.168.1.78')
  await detail.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(detail.getByText(copy('devices.address.refused'))).toBeVisible()
  await expect(address).toBeFocused()

  registry.refuseNextUpdate()
  await address.fill('http://192.168.1.1')
  await deviceField(detail, 'devices.field.name').fill('Renamed router')
  await detail.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(detail.getByText(copy('devices.work.failed'))).toBeVisible()
  await expect(detail.getByText(copy('devices.address.refused'))).toHaveCount(0)
})

test('an address the service would refuse is answered beside the field before it is sent', async ({
  page,
  origin,
}) => {
  const writes: string[] = []
  page.on('request', (request) => {
    if (request.method() === 'POST' && request.url().endsWith('/v1/devices'))
      writes.push(request.url())
  })
  await page.goto(`${origin}/connections`)
  await page
    .getByRole('button', { name: 'Add a connection', exact: true })
    .click()
  await deviceField(page, 'devices.field.target').click()
  await page.getByRole('option', { name: 'Keenetic' }).click()
  await deviceField(page, 'devices.field.name').fill('Documentation range')
  await deviceField(page, 'deploy.field.account.keenetic').fill('admin')
  await deviceField(page, 'deploy.field.interface.keenetic').fill('Wireguard0')
  const address = deviceField(page, 'deploy.field.address.keenetic')
  const save = page.getByRole('button', { name: 'Save', exact: true })

  // Valid in shape, but not a local network address.
  for (const refused of [
    'http://192.0.2.1/',
    'http://router.local',
    'http://169.254.169.254',
  ]) {
    await address.fill(refused)
    await expect(save).toBeEnabled()
    await save.click()
    await expect(
      page.getByText(copy('devices.validation.address.local')),
    ).toBeVisible()
    await expect(address).toBeFocused()
  }
  expect(writes).toEqual([])

  await address.fill('http://192.168.1.201')
  await expect(
    page.getByText(copy('devices.validation.address.local')),
  ).toHaveCount(0)
})

test('the submit button stays enabled and a missing field is stated beside it on submit', async ({
  page,
  origin,
}) => {
  await page.goto(`${origin}/connections`)
  await page
    .getByRole('button', { name: 'Add a connection', exact: true })
    .click()
  await deviceField(page, 'devices.field.target').click()
  await page.getByRole('option', { name: 'Keenetic' }).click()
  const save = page.getByRole('button', { name: 'Save', exact: true })
  await expect(save).toBeEnabled()
  await save.click()
  await expect(page.getByText(copy('devices.validation.required'))).toHaveCount(
    4,
  )
  // Focus moves to the first field that needs attention.
  await expect(deviceField(page, 'devices.field.name')).toBeFocused()
  await expect(deviceField(page, 'devices.field.name')).toHaveAttribute(
    'aria-required',
    'true',
  )
})

test('a credential-free format shows no password section and its switch states the server fact', async ({
  page,
  origin,
}) => {
  const created = await page.request.post(`${origin}/v1/devices`, {
    data: {
      address: localConfigAddress('credential-free.json'),
      name: 'Credential-free client',
      target_id: 'singbox',
    },
    headers: { Origin: origin, 'X-Routevane-Request': '1' },
  })
  expect(created.ok()).toBe(true)
  await page.goto(`${origin}/connections`)
  const detail = page.getByRole('region', {
    name: 'Credential-free client',
    exact: true,
  })
  await expect(detail).toBeVisible()
  await expect(
    detail.getByRole('region', {
      name: copy('devices.credential.title'),
      exact: true,
    }),
  ).toHaveCount(0)
  const toggle = detail.getByRole('switch', {
    name: copy('devices.auto.switch'),
  })
  await expect(toggle).toBeEnabled()
  await toggle.click()
  await expect(toggle).toBeChecked()
  await page.reload()
  await expect(toggle).toBeChecked()
  await toggle.click()
  await expect(toggle).not.toBeChecked()
})
