/**
 * A connection's password is its own saved fact, and automatic delivery is a
 * separate switch that follows it (ADR 0050).
 *
 * The password and delivery routes here are answered by
 * `support/fakeDevices.ts` with the contract the real routes keep, so a case can
 * seed any registry state and inject a refusal. The same walk against the
 * product's own routes and its in-memory test-build store is
 * `test-build-device.spec.ts`. The credential-free walk at the end runs against
 * the product itself.
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
import { test as served } from './support/served-product'

served.use({ locale: 'en-US', productData: 'connection-credential' })

const MUTATION_HEADERS = { 'X-Routevane-Request': '1' }
const CREDENTIAL_FREE_NAME = 'Credential-free client'

// The product behind this suite is one process for every test in the file, so
// a connection one test writes into it is still there for the next. A test
// that registers one owns it and takes it away again, whether it passed or not.
const test = served.extend<{ credentialFreeClient: string }>({
  credentialFreeClient: async ({ origin, request }, use) => {
    const created = await request.post(`${origin}/v1/devices`, {
      data: {
        address: localConfigAddress('credential-free.json'),
        name: CREDENTIAL_FREE_NAME,
        target_id: 'singbox',
      },
      headers: { ...MUTATION_HEADERS, Origin: origin },
    })
    expect(created.ok()).toBe(true)
    const { device } = (await created.json()) as { device: { id: string } }
    try {
      await use(CREDENTIAL_FREE_NAME)
    } finally {
      const forgotten = await request.post(
        `${origin}/v1/devices/${device.id}/forget`,
        { data: {}, headers: { ...MUTATION_HEADERS, Origin: origin } },
      )
      expect(forgotten.ok()).toBe(true)
    }
  },
})

const TYPED = 'dummy-router-value'

// A page that starts from a registry of its own, empty: what the product behind
// the suite holds does not decide the layout under test. The empty registry
// states itself, so a page that is not in that state fails here, by name,
// instead of at a control it was never going to reach.
const openEmptyRegistry = async (page: Page, origin: string) => {
  await routeFakeDevices(page, [])
  await page.goto(`${origin}/connections`)
  await expect(
    page.getByRole('heading', { name: copy('devices.empty') }),
  ).toBeVisible()
}

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
    page.getByRole('button', { name: /^Home router / }),
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

  await address.fill('http://10.255.0.77')
  await expect(detail.getByText(warning)).toBeVisible()
  await expect(
    detail.getByText(copy('devices.consequence.credential')),
  ).toBeVisible()
  await expect(
    detail.getByText(copy('devices.consequence.autoDelivery')),
  ).toBeVisible()
  await expect(
    detail.getByText(copy('devices.consequence.routes.edit')),
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
  // Pressing Save with the pointer puts focus on Save, as it does for any
  // button, and the write ends with nothing left to save. Focus does not fall
  // to the page: it goes to the first field, the explicit fallback.
  await expect(name).toBeFocused()

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

test('a refused save names the address when the service named it', async ({
  page,
  origin,
}) => {
  const { registry, detail } = await openConnection(
    page,
    origin,
    routerDevice(),
  )
  const address = deviceField(detail, 'deploy.field.address.keenetic')

  // The service says which field it refused, so the words go beside it and the
  // generic notice stays out; a refusal that names no field is not pinned on
  // the address even though the address is what changed.
  registry.refuseNextUpdate({ error: 'address_rejected', field: 'address' })
  await address.fill('http://10.255.0.78')
  await detail.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(
    detail.getByText(copy('deploy.field.address.keenetic.rejected')),
  ).toBeVisible()
  await expect(address).toBeFocused()
  await expect(detail.getByText(copy('devices.work.failed'))).toHaveCount(0)

  registry.refuseNextUpdate()
  await address.fill('http://10.255.0.79')
  const save = detail.getByRole('button', { name: 'Save', exact: true })
  await save.click()
  await expect(detail.getByText(copy('devices.work.failed'))).toBeVisible()
  // The write ended and there is still something to save, so the button the
  // operator pressed is available again and holds focus.
  await expect(save).toBeEnabled()
  await expect(save).toBeFocused()
  await expect(
    detail.getByText(copy('deploy.field.address.keenetic.rejected')),
  ).toHaveCount(0)
})

test('an address the service refuses is answered beside the field', async ({
  page,
  origin,
}) => {
  const answers: number[] = []
  page.on('response', (response) => {
    if (
      response.request().method() === 'POST' &&
      response.url().endsWith('/v1/devices')
    )
      answers.push(response.status())
  })
  await openEmptyRegistry(page, origin)
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

  // Valid in shape, but not a local network address: the form sends it and the
  // service's own refusal is what the field states.
  const rejected = page.getByText(
    copy('deploy.field.address.keenetic.rejected'),
  )
  for (const refused of [
    'http://router.local',
    'http://8.8.8.8',
    'http://169.254.169.254',
  ]) {
    await address.fill(refused)
    await expect(save).toBeEnabled()
    await save.click()
    await expect(rejected).toBeVisible()
    await expect(address).toBeFocused()
  }
  expect(answers).toEqual([422, 422, 422])

  await address.fill('http://10.255.0.201')
  await expect(rejected).toHaveCount(0)
})

test('the submit button stays enabled and a missing field is stated beside it on submit', async ({
  page,
  origin,
}) => {
  await openEmptyRegistry(page, origin)
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
  credentialFreeClient,
  page,
  origin,
}) => {
  await page.goto(`${origin}/connections`)
  const detail = page.getByRole('region', {
    name: credentialFreeClient,
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
