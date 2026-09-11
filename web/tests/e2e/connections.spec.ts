/**
 * The connections section: the formats this build can reach, the ones an
 * operator hides, and the devices a published output is delivered to.
 *
 * The worker fixture in `support/served-product.ts` serves this suite
 * alone, over the data directory named below.
 */

import { expect } from '@playwright/test'
import { join } from 'node:path'

import { audit, auditWidths, reviewRoot } from './support/audits'
import { englishCopy } from './support/copy'
import { buildProfile, openFormats } from './support/flows'
import {
  addConnectionField,
  catalogDisclosure,
  choiceSearch,
  deliveryField,
  deviceField,
  escapeRegExp,
  listMembership,
} from './support/queries'
import { test } from './support/served-product'

// The surface negotiates its language from the browser, and English is the
// product's primary language. `productData` is this suite's own data
// directory: it starts empty, no other suite writes to it, and it is gone
// again when the suite ends.
test.use({ locale: 'en-US', productData: 'connections' })

test('the searchable connection choice filters in its panel and reopens by keyboard', async ({
  page,
  origin,
  assertProductAlive,
}) => {
  await page.goto(`${origin}/profiles/new`)
  const field = deliveryField(page)
  await expect(field).toBeVisible()
  await field.press('Enter')
  const search = choiceSearch(page)
  await expect(search).toBeFocused()
  await search.fill('Keenetic')
  await expect(page.getByRole('option')).toHaveCount(1)
  await search.press('Escape')
  await expect(field).toHaveAttribute('aria-expanded', 'false')
  await expect(field).toBeFocused()
  await field.press('Enter')
  await expect(search).toHaveValue('')
  await search.fill('sing-box')
  await search.press('ArrowDown')
  await search.press('Enter')
  await expect(field).toHaveText('sing-box')
  await expect(field).toHaveAttribute('aria-expanded', 'false')
  expect(await audit(page, 'searchable connection keyboard cycle')).toEqual([])
  assertProductAlive()
})

test('hiding a format removes it from the connection picker and says where it went', async ({
  page,
  origin,
  assertProductAlive,
}) => {
  test.setTimeout(120000)
  // The section was renamed; the address it used to have still resolves, so
  // an old bookmark lands on it rather than on nothing.
  await page.goto(`${origin}/devices`)
  await page.waitForURL(`${origin}/connections`)
  await expect(
    page.getByRole('heading', { level: 1, name: 'Connections' }),
  ).toBeVisible()

  // What this build can talk to at all is reference material behind one
  // disclosure, under the connections the operator actually made.
  // A disclosure keeps its contents out of the page until it is opened, so
  // nothing of the catalog's table is on screen yet.
  await expect(page.getByRole('columnheader')).toHaveCount(0)
  await catalogDisclosure(page).click()
  await expect(page.getByRole('columnheader')).toHaveText([
    'Name',
    'Type',
    'Format',
    'Delivery from Routevane',
    'Offer when connecting',
  ])
  const keeneticRow = page.getByRole('row').filter({ hasText: 'Keenetic' })
  await expect(keeneticRow).toContainText('Router')
  await expect(keeneticRow).toContainText('.bat')
  await expect(keeneticRow).toContainText('Yes')

  // Hiding is a preference of this browser, kept under its own key.
  const toggle = page.getByRole('checkbox', {
    name: 'Offer Keenetic when connecting',
  })
  await toggle.uncheck()
  expect(
    await page.evaluate(() => localStorage.getItem('rv.hiddenTargets')),
  ).toBe('["keenetic"]')

  // Connection choices honor the same hidden-format preference in the first
  // setup flow and when another connection is added later.
  await page
    .getByRole('navigation', { name: 'Sections' })
    .getByRole('link', { name: 'Profiles' })
    .click()
  await page.getByRole('link', { name: 'Build a profile' }).first().click()
  await page.waitForURL(`${origin}/profiles/new`)
  await page.getByRole('searchbox', { name: 'Find a list' }).fill('discord')
  await listMembership(page, 'Discord').check()
  const offered = await openFormats(page)
  await expect(offered.getByRole('option', { name: /Keenetic/ })).toHaveCount(0)
  await expect(offered.getByRole('option', { name: /sing-box/ })).toHaveCount(1)
  await page.getByRole('option', { name: /sing-box/ }).click()
  await page.getByRole('button', { name: 'Create and prepare' }).click()
  await page.waitForURL(
    new RegExp(`^${escapeRegExp(origin)}/profiles/[a-f0-9]{32}.*$`),
  )
  await expect(
    page.getByRole('heading', { level: 1, name: 'Discord' }),
  ).toBeVisible()

  await expect(
    page.getByRole('heading', { name: 'Subscription link · sing-box' }),
  ).toBeVisible()
  await addConnectionField(page).click()
  const outputFormats = page.getByRole('listbox')
  await expect(
    outputFormats.getByRole('option', { name: /Keenetic/ }),
  ).toHaveCount(0)
  await expect(
    outputFormats.getByRole('option', { name: /sing-box/ }),
  ).toHaveCount(0)
  // What is left is one group, and the group says which one it is.
  await expect(outputFormats.getByRole('group')).toHaveCount(1)
  await expect(outputFormats.getByRole('group')).toHaveAccessibleName(
    'Applications',
  )
  await page.keyboard.press('Escape')

  // Unhiding brings it back, in both places it is offered.
  await page
    .getByRole('navigation', { name: 'Sections' })
    .getByRole('link', { name: 'Connections' })
    .click()
  await catalogDisclosure(page).click()
  await toggle.check()
  expect(
    await page.evaluate(() => localStorage.getItem('rv.hiddenTargets')),
  ).toBe('[]')
  assertProductAlive()
})

test('the device form asks only for fields the selected target needs', async ({
  page,
  origin,
}) => {
  await page.emulateMedia({ colorScheme: 'dark' })
  await page.goto(`${origin}/connections`)
  await expect(
    page.getByRole('heading', { level: 1, name: 'Connections' }),
  ).toBeVisible()
  // Every field is addressed by the caption the chosen target's own
  // requirements give it, which is the same fact the label assertions used to
  // state separately.
  await expect(
    page.getByRole('region', { name: 'Add a connection', exact: true }),
  ).toBeVisible()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  // With nothing saved the form is the whole work area: no header action
  // opens it and there is nothing a cancel could return to.
  await expect(
    page.getByRole('button', { name: 'Add a connection', exact: true }),
  ).toHaveCount(0)
  await expect(
    page.getByRole('button', { name: 'Cancel', exact: true }),
  ).toHaveCount(0)
  const target = deviceField(page, 'devices.field.target')
  await expect(target).toBeEnabled()
  for (const absent of [
    'devices.field.name',
    'deploy.field.address.keenetic',
    'deploy.field.account.keenetic',
    'deploy.field.interface.keenetic',
  ])
    await expect(deviceField(page, absent)).toHaveCount(0)

  await target.click()
  await page.getByRole('option', { name: 'Keenetic' }).click()
  const keeneticAddress = deviceField(page, 'deploy.field.address.keenetic')
  await expect(keeneticAddress).toBeVisible()
  await expect(keeneticAddress).toHaveAttribute(
    'placeholder',
    'http://192.168.1.1',
  )
  await expect(deviceField(page, 'deploy.field.account.keenetic')).toBeVisible()
  await expect(
    deviceField(page, 'deploy.field.interface.keenetic'),
  ).toBeVisible()
  await expect(
    page.getByText(
      'For example, Wireguard0 — the Keenetic connection/interface ID.',
    ),
  ).toBeVisible()
  await keeneticAddress.fill('http://192.168.1.1')
  await deviceField(page, 'deploy.field.account.keenetic').fill('admin')
  await page
    .getByRole('button', { name: 'Save', exact: true })
    .scrollIntoViewIfNeeded()
  await expect(
    page.getByRole('button', { name: 'Save', exact: true }),
  ).toBeInViewport({ ratio: 1 })
  await page.screenshot({
    path: join(reviewRoot, 'connection-create-final.png'),
  })

  await target.click()
  await page.getByRole('option', { name: 'sing-box' }).click()
  const singBoxAddress = deviceField(page, 'deploy.field.address.singbox')
  await expect(singBoxAddress).toBeVisible()
  await expect(singBoxAddress).toHaveAttribute(
    'placeholder',
    'file:///C:/sing-box/config.json',
  )
  await expect(singBoxAddress).toHaveValue('')
  await expect(deviceField(page, 'deploy.field.account.keenetic')).toHaveCount(
    0,
  )
  await expect(
    deviceField(page, 'deploy.field.interface.keenetic'),
  ).toHaveCount(0)
  await deviceField(page, 'devices.field.name').fill('Local client')
  await singBoxAddress.fill('file:///C:/sing-box/config.json')
  await page.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(
    page.getByRole('heading', { name: 'Local client', exact: true }),
  ).toBeFocused()
  await expect(
    page.getByRole('region', { name: 'Add a connection', exact: true }),
  ).toHaveCount(0)
  const addConnection = page.getByRole('button', {
    name: 'Add a connection',
    exact: true,
  })
  await addConnection.click()
  // The header action keeps its place while the form is open beside the
  // saved connection.
  await expect(addConnection).toBeDisabled()
  await target.click()
  await page.getByRole('option', { name: 'sing-box' }).click()
  await expect(deviceField(page, 'devices.field.name')).toHaveValue('')
  await expect(
    page.getByText('Fill in this field', { exact: true }),
  ).toHaveCount(0)
  await deviceField(page, 'devices.field.name').fill('Abandoned draft')
  await page.screenshot({
    path: join(reviewRoot, 'connection-create-beside-list.png'),
  })
  // Cancelling discards the draft and puts the connection the form replaced
  // back into the work area: the list is still one connection long, the
  // header action is available again, and nothing collapsed into a button.
  await page.getByRole('button', { name: 'Cancel', exact: true }).click()
  await expect(
    page.getByRole('heading', { name: 'Local client', exact: true }),
  ).toBeFocused()
  await expect(
    page.getByRole('region', { name: 'Add a connection', exact: true }),
  ).toHaveCount(0)
  await expect(
    page.getByRole('button', { name: /^Configure connection / }),
  ).toHaveCount(1)
  await expect(addConnection).toBeEnabled()
  await addConnection.click()
  await target.click()
  await page.getByRole('option', { name: 'sing-box' }).click()
  await expect(deviceField(page, 'devices.field.name')).toHaveValue('')
  await page
    .getByRole('button', {
      name: 'Configure connection Local client',
      exact: true,
    })
    .click()
  const details = page.getByRole('region', {
    name: 'Local client',
    exact: true,
  })
  await expect(
    details.getByText('file:///C:/sing-box/config.json', { exact: true }),
  ).toBeVisible()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  expect(await auditWidths(page, 'connection-management')).toEqual([])
  await page.screenshot({
    path: join(reviewRoot, 'connection-management-graphite.png'),
  })

  // A saved connection's own parameters are edited in place. The name that
  // comes back after a reload is the one the service stored, not the one this
  // tab typed.
  await deviceField(details, 'devices.field.name').fill('Local client v2')
  await details.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(page.getByText('Parameters saved.')).toBeVisible()
  await page.reload()
  const renamed = page.getByRole('region', {
    name: 'Local client v2',
    exact: true,
  })
  await expect(renamed).toBeVisible()
  await expect(
    page.getByRole('button', {
      name: 'Configure connection Local client v2',
      exact: true,
    }),
  ).toBeVisible()

  // Forgetting is the connection's own secondary action, in its action menu,
  // and it is confirmed before it runs.
  await renamed
    .getByRole('button', { name: 'Actions for connection Local client v2' })
    .click()
  await page.getByRole('menuitem', { name: 'Forget this connection' }).click()
  const confirmation = page.getByRole('dialog', {
    name: 'Forget connection Local client v2?',
  })
  await expect(confirmation).toBeVisible()
  await confirmation
    .getByRole('button', { name: 'Forget this connection', exact: true })
    .click()
  await expect(renamed).toHaveCount(0)
  await expect(
    page.getByRole('region', { name: 'Add a connection', exact: true }),
  ).toBeVisible()
  await expect(
    page.getByRole('button', { name: /^Configure connection / }),
  ).toHaveCount(0)
})

test('an output can be explicitly bound to and detached from a compatible device', async ({
  page,
  origin,
}) => {
  const headers = { Origin: origin, 'X-Routevane-Request': '1' }
  const created = await page.request.post(`${origin}/v1/devices`, {
    data: {
      account: 'admin',
      address: 'http://192.168.1.1',
      interface: 'Wireguard0',
      name: 'Prerelease Keenetic',
      target_id: 'keenetic',
    },
    headers,
  })
  expect(created.ok()).toBe(true)
  const deviceID = ((await created.json()) as { device: { id: string } }).device
    .id
  const { outputId } = await buildProfile(page, origin)

  await page
    .getByRole('tablist', { name: 'Profile sections' })
    .getByRole('tab', { name: 'Connection' })
    .click()
  // The row's device choice is captioned with the connection it belongs to,
  // which is what identifies it whatever it currently holds.
  const binding = page.getByLabel(
    englishCopy('outputs.device.label').replace('{target}', 'Keenetic'),
  )
  await expect(binding).toHaveText('No automatic delivery')

  const bound = page.waitForRequest(
    (request) =>
      request.method() === 'POST' &&
      new URL(request.url()).pathname === `/v1/outputs/${outputId}/device`,
  )
  await binding.click()
  await page
    .getByRole('option', {
      name: 'Prerelease Keenetic · turn on automatic delivery in Connections',
    })
    .click()
  expect((await bound).postDataJSON()).toEqual({ device_id: deviceID })
  await expect(binding).toContainText('Prerelease Keenetic')

  const detached = page.waitForRequest(
    (request) =>
      request.method() === 'POST' &&
      new URL(request.url()).pathname === `/v1/outputs/${outputId}/device`,
  )
  await page.getByRole('button', { name: 'Detach' }).click()
  expect((await detached).postDataJSON()).toEqual({ device_id: '' })
  await expect(binding).toHaveText('No automatic delivery')

  const forgotten = await page.request.post(
    `${origin}/v1/devices/${deviceID}/forget`,
    { data: {}, headers },
  )
  expect(forgotten.ok()).toBe(true)
})
