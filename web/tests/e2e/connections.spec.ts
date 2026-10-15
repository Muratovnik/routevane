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
import { localConfigAddress } from './support/environment'
import { buildProfile, openFormats } from './support/flows'
import {
  addConnectionField,
  chooseFormatAction,
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

test('hiding a format in Settings removes it from the connection picker', async ({
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

  // The page carries no reference of formats: no disclosure, no table. Which
  // formats exist and which the composer offers is Settings' to say, and
  // whether a format can be delivered to automatically is the note of its
  // option in the form that registers a connection.
  await expect(
    page.getByRole('button', { name: 'Supported formats' }),
  ).toHaveCount(0)
  await expect(page.getByRole('columnheader')).toHaveCount(0)
  await expect(page.getByRole('checkbox')).toHaveCount(0)
  await page
    .getByRole('button', { name: 'Add a connection', exact: true })
    .click()
  await deviceField(page, 'devices.field.target').click()
  await expect(page.getByRole('option', { name: /^Keenetic/ })).toContainText(
    'Can be delivered automatically',
  )
  await page.keyboard.press('Escape')
  await expect(page.getByRole('listbox')).toHaveCount(0)

  // Hiding is a preference of this browser, kept under its own key.
  await page
    .getByRole('navigation', { name: 'Sections' })
    .getByRole('link', { name: 'Settings' })
    .click()
  const formats = page.getByRole('group', {
    name: 'Offer when adding a format',
  })
  const toggle = formats.getByRole('switch', { name: 'Keenetic', exact: true })
  await expect(toggle).toBeChecked()
  await toggle.click()
  await expect(toggle).not.toBeChecked()
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
    page.getByRole('button', { name: 'Subscription link · sing-box' }),
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

  // Showing it again brings it back, in both places it is offered.
  await page
    .getByRole('navigation', { name: 'Sections' })
    .getByRole('link', { name: 'Settings' })
    .click()
  await toggle.click()
  await expect(toggle).toBeChecked()
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
  // An empty registry states that it holds nothing and offers the one act that
  // changes it; the form is what that act opens.
  await expect(
    page.getByRole('heading', { name: 'No connections yet' }),
  ).toBeVisible()
  await expect(
    page.getByRole('region', { name: 'Add a connection', exact: true }),
  ).toHaveCount(0)
  await page
    .getByRole('button', { name: 'Add a connection', exact: true })
    .click()
  // Every field is addressed by the caption the chosen target's own
  // requirements give it, which is the same fact the label assertions used to
  // state separately.
  await expect(
    page.getByRole('region', { name: 'Add a connection', exact: true }),
  ).toBeVisible()
  await expect(page.getByRole('dialog')).toHaveCount(0)
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
  // The library's panel animates out after a choice; the next field is used
  // once it has gone, as an operator's next act follows the closed list.
  await expect(page.getByRole('listbox')).toHaveCount(0)
  const keeneticAddress = deviceField(page, 'deploy.field.address.keenetic')
  await expect(keeneticAddress).toBeVisible()
  await expect(keeneticAddress).toHaveAttribute(
    'placeholder',
    'http://192.168.1.1',
  )
  // Creating a connection uses the same two-column grid as editing one: the
  // name and the address stand side by side.
  const nameBox = await deviceField(page, 'devices.field.name').boundingBox()
  const addressBox = await keeneticAddress.boundingBox()
  expect(addressBox!.x).toBeGreaterThan(nameBox!.x + nameBox!.width - 1)
  expect(Math.abs(addressBox!.y - nameBox!.y)).toBeLessThan(2)
  await expect(deviceField(page, 'deploy.field.account.keenetic')).toBeVisible()
  await expect(
    deviceField(page, 'deploy.field.interface.keenetic'),
  ).toBeVisible()
  // The example is the placeholder; the explanation is behind the informer at
  // the label, and nothing stands under the field.
  await expect(
    deviceField(page, 'deploy.field.interface.keenetic'),
  ).toHaveAttribute('placeholder', 'Wireguard0')
  await page
    .getByRole('button', { name: 'What the interface field takes' })
    .click()
  await expect(
    page.getByRole('dialog', { name: 'What the interface field takes' }),
  ).toHaveText('The ID of the Keenetic connection or interface.')
  await page.keyboard.press('Escape')
  await keeneticAddress.fill('http://10.255.0.1')
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
  await expect(page.getByRole('listbox')).toHaveCount(0)
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
  await singBoxAddress.fill(localConfigAddress())
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
    page.getByRole('button', { name: /^Local client / }),
  ).toHaveCount(1)
  await expect(addConnection).toBeEnabled()
  await addConnection.click()
  await target.click()
  await page.getByRole('option', { name: 'sing-box' }).click()
  await expect(deviceField(page, 'devices.field.name')).toHaveValue('')
  await page
    .getByRole('button', {
      name: /^Local client /,
    })
    .click()
  const details = page.getByRole('region', {
    name: 'Local client',
    exact: true,
  })
  // The address is the first parameter, shown in its own field.
  await expect(
    deviceField(details, 'deploy.field.address.singbox'),
  ).toHaveValue(localConfigAddress())
  await expect(page.getByRole('dialog')).toHaveCount(0)
  // The list's own heading is not the page's: one heading says «Connections».
  await expect(
    page.getByRole('heading', { name: 'Connections', exact: true }),
  ).toHaveCount(1)
  await expect(
    page.getByRole('heading', { name: 'Saved connections', exact: true }),
  ).toBeVisible()
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
      name: /^Local client v2 /,
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
  // Forgetting the last connection returns the registry to stating that it
  // holds nothing, with the act that changes that beside it.
  await expect(
    page.getByRole('heading', { name: 'No connections yet' }),
  ).toBeVisible()
  await expect(
    page.getByRole('button', { name: 'Add a connection', exact: true }),
  ).toBeEnabled()
  await expect(
    page.getByRole('button', { name: /^Local client / }),
  ).toHaveCount(0)
})

test('an output can be explicitly bound to and detached from a compatible device', async ({
  page,
  origin,
}) => {
  const headers = { Origin: origin, 'X-Routevane-Request': '1' }
  const deviceName = 'Prerelease Keenetic'
  const created = await page.request.post(`${origin}/v1/devices`, {
    data: {
      account: 'admin',
      address: 'http://192.0.2.1',
      interface: 'Wireguard0',
      name: deviceName,
      target_id: 'keenetic',
    },
    headers,
  })
  expect(created.ok()).toBe(true)
  const deviceID = ((await created.json()) as { device: { id: string } }).device
    .id
  // Read the running product's capability, rather than assuming the test
  // host's OS can retain a router password. These reads grant no consent.
  const [registryResponse, targetsResponse] = await Promise.all([
    page.request.get(`${origin}/v1/devices`),
    page.request.get(`${origin}/v1/deployments/targets`),
  ])
  expect(registryResponse.ok()).toBe(true)
  expect(targetsResponse.ok()).toBe(true)
  const registry = (await registryResponse.json()) as {
    secret_store_available: boolean
    devices: { id: string; auto_deliver: boolean; deployable: boolean }[]
  }
  const targets = (await targetsResponse.json()) as {
    targets: {
      target_id: string
      requirements: { needs_credential: boolean }
    }[]
  }
  expect(typeof registry.secret_store_available).toBe('boolean')
  expect(
    registry.devices.find((device) => device.id === deviceID),
  ).toMatchObject({
    auto_deliver: false,
    deployable: true,
  })
  expect(
    targets.targets.find((target) => target.target_id === 'keenetic'),
  ).toMatchObject({ requirements: { needs_credential: true } })
  const copyByStore = {
    false: {
      device: 'outputs.device.unavailable',
    },
    true: {
      device: 'outputs.device.disabled',
    },
  } as const
  const expectedCopy =
    copyByStore[
      String(registry.secret_store_available) as keyof typeof copyByStore
    ]
  // The option is named by the connection; its state is the option's note.
  const deviceOption = page.getByRole('option', {
    name: new RegExp(`^${escapeRegExp(deviceName)}`),
  })
  const none = englishCopy('outputs.device.none')
  const { profileId, outputId } = await buildProfile(page, origin)

  const publishingTab = page
    .getByRole('tablist', { name: 'Profile sections' })
    .getByRole('tab', { name: 'Publishing' })
  await publishingTab.click()
  const publishing = page.getByRole('tabpanel', { name: 'Publishing' })
  // The row's device choice is captioned with the connection it belongs to,
  // which is what identifies it whatever it currently holds.
  const binding = publishing.getByLabel(
    englishCopy('outputs.device.label').replace('{target}', 'Keenetic'),
    { exact: true },
  )
  await expect(binding).toHaveText(none)

  await binding.click()
  await expect(deviceOption).toContainText(englishCopy(expectedCopy.device))
  const [boundResponse] = await Promise.all([
    page.waitForResponse(
      (response) =>
        response.request().method() === 'POST' &&
        new URL(response.url()).pathname === `/v1/outputs/${outputId}/device`,
    ),
    deviceOption.click(),
  ])
  expect(boundResponse.ok()).toBe(true)
  expect(boundResponse.request().postDataJSON()).toEqual({
    device_id: deviceID,
  })
  await expect(binding).toHaveText(deviceName)
  await page.reload()
  await publishingTab.click()
  await expect(binding).toHaveText(deviceName)
  // The connection cell holds the choice alone; the way to the connection is
  // the row menu's item, and Send is the row's named icon action.
  await chooseFormatAction(page, 'Keenetic', 'outputs.menu.open', englishCopy)
  await page.waitForURL(`${origin}/connections#connection=${deviceID}`)
  await page.goBack()
  await publishingTab.click()
  await expect(
    publishing.getByRole('link', { name: 'Send Keenetic', exact: true }),
  ).toHaveAttribute('href', `/profiles/${profileId}/send/${outputId}`)

  const [detachedResponse] = await Promise.all([
    page.waitForResponse(
      (response) =>
        response.request().method() === 'POST' &&
        new URL(response.url()).pathname === `/v1/outputs/${outputId}/device`,
    ),
    (async () => {
      await binding.click()
      await page.getByRole('option', { name: none, exact: true }).click()
    })(),
  ])
  expect(detachedResponse.ok()).toBe(true)
  expect(detachedResponse.request().postDataJSON()).toEqual({ device_id: '' })
  await expect(binding).toHaveText(none)
  await page.reload()
  await publishingTab.click()
  await expect(binding).toHaveText(none)

  const forgotten = await page.request.post(
    `${origin}/v1/devices/${deviceID}/forget`,
    { data: {}, headers },
  )
  expect(forgotten.ok()).toBe(true)
})

// Every write on the profile page reads the profile again. That read keeps the
// page mounted, so an unsaved rename on Contents survives a device binding made
// on Publishing instead of being reset by a reloaded editor.
test('binding a device on Publishing keeps an unsaved rename on Contents', async ({
  page,
  origin,
}) => {
  const headers = { Origin: origin, 'X-Routevane-Request': '1' }
  const deviceName = 'Draft-keeping Keenetic'
  const created = await page.request.post(`${origin}/v1/devices`, {
    data: {
      account: 'admin',
      address: 'http://192.0.2.2',
      interface: 'Wireguard0',
      name: deviceName,
      target_id: 'keenetic',
    },
    headers,
  })
  expect(created.ok()).toBe(true)
  const deviceID = ((await created.json()) as { device: { id: string } }).device
    .id
  const { outputId } = await buildProfile(page, origin)

  const sections = page.getByRole('tablist', { name: 'Profile sections' })
  await sections.getByRole('tab', { name: 'Contents' }).click()
  const contents = page.getByRole('tabpanel', { name: 'Contents' })
  const name = contents.getByLabel('Name', { exact: true })
  await name.fill('Unsaved rename')

  await sections.getByRole('tab', { name: 'Publishing' }).click()
  const publishing = page.getByRole('tabpanel', { name: 'Publishing' })
  const binding = publishing.getByLabel(
    englishCopy('outputs.device.label').replace('{target}', 'Keenetic'),
    { exact: true },
  )
  await binding.click()
  const [boundResponse] = await Promise.all([
    page.waitForResponse(
      (response) =>
        response.request().method() === 'POST' &&
        new URL(response.url()).pathname === `/v1/outputs/${outputId}/device`,
    ),
    page
      .getByRole('option', {
        name: new RegExp(`^${escapeRegExp(deviceName)}`),
      })
      .click(),
  ])
  expect(boundResponse.ok()).toBe(true)
  await expect(binding).toHaveText(deviceName)
  await expect(binding).toBeEnabled()

  await sections.getByRole('tab', { name: 'Contents' }).click()
  await expect(name).toHaveValue('Unsaved rename')
  await expect(
    contents.getByRole('button', { name: 'Discard changes' }),
  ).toBeVisible()

  const forgotten = await page.request.post(
    `${origin}/v1/devices/${deviceID}/forget`,
    { data: {}, headers },
  )
  expect(forgotten.ok()).toBe(true)
})
