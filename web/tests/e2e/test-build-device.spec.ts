/**
 * A router connection on the real product, end to end, with nothing in the
 * page answered by the suite: register, save its password, use the saved
 * password for a manual send, remove it again (ADR 0050, ADR 0052).
 *
 * This runs against the test build (`support/test-build.ts`). That build is
 * what makes the walk possible without anyone's network: it accepts the RFC
 * 5737 documentation address used here as a device, keeps the saved password in
 * its own memory rather than in the workstation's credential store, and refuses
 * every device connection before a packet leaves. A preview of the route format
 * needs no device, so it completes; the apply reaches the device step and is
 * refused there. None of this is release-binary evidence: the release build
 * refuses the address itself, which the Go suite proves.
 *
 * The connection is registered through the service, because the connection
 * form holds the release rule in every build and refuses a documentation
 * address before it is sent (`connection-credential.spec.ts`).
 */
import { expect, type Page, type Response } from '@playwright/test'

import { englishCopy as copy } from './support/copy'
import { deviceField } from './support/queries'
import { test } from './support/served-product'

test.use({ locale: 'en-US', productData: 'test-build-device' })

const ADDRESS = 'http://192.0.2.10'
const NAME = 'Documentation router'
// A fresh value per run, so nothing on disk or in a log can match it by chance.
const PASSWORD = crypto.randomUUID()

const isDeploy =
  (confirm: boolean) =>
  (response: Response): boolean =>
    response.request().method() === 'POST' &&
    /\/v1\/artifacts\/[a-f0-9]{32}\/deploy$/.test(response.url()) &&
    (response.request().postDataJSON() as { confirm?: boolean }).confirm ===
      confirm

const service = (page: Page, origin: string) => ({
  post: async <T>(path: string, data: object = {}): Promise<T> => {
    const response = await page.request.post(`${origin}${path}`, {
      data,
      headers: { Origin: origin, 'X-Routevane-Request': '1' },
    })
    expect(response.ok(), `${path} ${response.status()}`).toBe(true)
    return (await response.json()) as T
  },
  device: async (id: string) => {
    const response = await page.request.get(`${origin}/v1/devices`)
    const text = await response.text()
    // The password never comes back, whatever the registry says about it.
    expect(text).not.toContain(PASSWORD)
    const registry = JSON.parse(text) as {
      devices: { id: string; credential_stored: boolean }[]
      secret_store_available: boolean
    }
    return {
      storeAvailable: registry.secret_store_available,
      device: registry.devices.find((device) => device.id === id),
    }
  },
})

test('a documentation-range router keeps a saved password in the test build and sends with it', async ({
  page,
  origin,
  assertProductAlive,
}) => {
  test.setTimeout(120_000)
  const api = service(page, origin)

  // The test build stores this address; a release build refuses it.
  const { device } = await api.post<{ device: { id: string } }>('/v1/devices', {
    target_id: 'keenetic',
    name: NAME,
    address: ADDRESS,
    account: 'admin',
    interface: 'Wireguard0',
  })
  const { profile } = await api.post<{ profile: { id: string } }>(
    '/v1/profiles',
    { categories: [], exclusions: [], lists: ['youtube'], name: 'Routes' },
  )
  const { output } = await api.post<{ output: { id: string } }>(
    `/v1/profiles/${profile.id}/outputs`,
    { target_id: 'keenetic' },
  )
  await api.post(`/v1/profiles/${profile.id}/refresh`)
  await api.post(`/v1/outputs/${output.id}/build`)
  await api.post(`/v1/outputs/${output.id}/device`, { device_id: device.id })
  expect(await api.device(device.id)).toMatchObject({
    storeAvailable: true,
    device: { credential_stored: false },
  })

  const sendPage = `${origin}/profiles/${profile.id}/send/${output.id}`
  const sendPassword = page.getByLabel(copy('deploy.field.password.keenetic'), {
    exact: true,
  })
  // With nothing saved, the send asks for the password.
  await page.goto(sendPage)
  await expect(
    page.getByRole('combobox', { name: copy('send.connection.label') }),
  ).toHaveText(NAME)
  await expect(sendPassword).toBeVisible()

  // Saving the password is the connection's own act, kept by the server.
  await page.goto(`${origin}/connections`)
  const detail = page.getByRole('region', { name: NAME, exact: true })
  const credential = detail.getByRole('region', {
    name: copy('devices.credential.title'),
    exact: true,
  })
  await deviceField(credential, 'deploy.field.password.keenetic').fill(PASSWORD)
  await credential
    .getByRole('button', { name: copy('devices.credential.save'), exact: true })
    .click()
  await expect(
    credential.getByText(copy('devices.credential.saved')),
  ).toBeVisible()
  expect((await api.device(device.id)).device).toMatchObject({
    credential_stored: true,
  })
  // Saving signs in once (ADR 0059). The test build refuses to dial a
  // documentation address, so the notice says the password was kept without a
  // check, and why.
  await expect(
    page.getByText(copy('devices.credential.check.unverified'), {
      exact: true,
    }),
  ).toBeVisible()
  await expect(
    page.getByText(copy('error.device_unreachable'), { exact: true }),
  ).toBeVisible()

  // A send now takes the saved password without asking, and the preview the
  // server answers is built with it: the route format needs no device for that.
  await page.goto(sendPage)
  await expect(
    page.getByText(copy('devices.credential.saved'), { exact: true }),
  ).toBeVisible()
  await expect(sendPassword).toHaveCount(0)
  const previewed = page.waitForResponse(isDeploy(false))
  await page
    .getByRole('button', { name: copy('send.review'), exact: true })
    .click()
  const preview = await previewed
  expect(preview.status()).toBe(200)
  expect(preview.request().postDataJSON()).toMatchObject({
    device_id: device.id,
    expected_connection: {
      device: ADDRESS,
      username: 'admin',
      interface: 'Wireguard0',
    },
    password: '',
  })
  expect(await preview.text()).not.toContain(PASSWORD)
  const apply = page.getByRole('button', {
    name: copy('send.apply'),
    exact: true,
  })
  await expect(apply).toBeVisible()

  // Applying reaches the device step, where the test build refuses the
  // connection: no packet leaves for the documentation address.
  const applied = page.waitForResponse(isDeploy(true))
  await apply.click()
  const attempt = await applied
  const outcome = await attempt.text()
  expect(outcome).not.toContain(PASSWORD)
  expect(attempt.ok()).toBe(false)
  // The saved password got the send as far as the device probe, the first
  // step that needs the network, and nothing was applied. The refused dial
  // reads as a router that does not answer.
  expect(JSON.parse(outcome)).toMatchObject({
    status: 'failed',
    error: 'device_unreachable',
    result: {
      applied: false,
      events: [{ step: 'probe', outcome: 'failed' }],
    },
  })
  await expect(page.getByText(copy('send.applied'))).toHaveCount(0)

  // Removing the password ends it on the server, and the send asks again.
  await page.goto(`${origin}/connections`)
  await credential
    .getByRole('button', {
      name: copy('devices.credential.remove'),
      exact: true,
    })
    .click()
  await page
    .getByRole('dialog', {
      name: copy('devices.credential.remove.title').replace(
        '{name}',
        'Documentation router',
      ),
    })
    .getByRole('button', {
      name: copy('devices.credential.remove'),
      exact: true,
    })
    .click()
  await expect(
    credential.getByRole('button', {
      name: copy('devices.credential.save'),
      exact: true,
    }),
  ).toBeVisible()
  expect((await api.device(device.id)).device).toMatchObject({
    credential_stored: false,
  })
  await page.goto(sendPage)
  await expect(sendPassword).toBeVisible()
  assertProductAlive()
})
