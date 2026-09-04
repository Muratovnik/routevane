import { createHash } from 'node:crypto'
import { existsSync } from 'node:fs'
import {
  access,
  mkdir,
  readFile,
  readdir,
  rm,
  writeFile,
} from 'node:fs/promises'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import AxeBuilder from '@axe-core/playwright'
import { expect, test, type Locator, type Page } from '@playwright/test'

import { dictionaries } from '../../src/shared/i18n/messages'

import {
  assertPortBindable,
  delay,
  reserveLoopbackPort,
  spawnProduct,
  stopOwnedProduct,
  type SpawnedProduct,
} from './support/product'

const testDirectory = dirname(fileURLToPath(import.meta.url))
const repositoryRoot = resolve(testDirectory, '..', '..', '..')
const dataRoot = join(repositoryRoot, '.cache', 'browser-data')
const reviewRoot = join(
  repositoryRoot,
  'tmp',
  'test-screenshots',
  'browser-review',
)
const embeddedUIRoot = join(
  repositoryRoot,
  'internal',
  'infrastructure',
  'httpapi',
  'ui',
)
const binary = join(
  repositoryRoot,
  '.cache',
  'build',
  process.platform === 'win32' ? 'routing-agent.exe' : 'routing-agent',
)

let origin = ''
let port = 0
let managedProduct: SpawnedProduct | undefined
let expectedUIDigest = ''

// A read that stores nothing: the composer and the route editor ask what a
// draft would weigh, and no flow these tests state is made of that question.
const forecastPath = '/v1/lists/preview'

// The surface negotiates its language from the browser. English is the
// product's primary language, so the walkthrough runs against the English
// dictionary; the localization test below proves the Russian one.
test.use({ locale: 'en-US' })

test.beforeAll(async () => {
  await access(binary)
  expectedUIDigest = await embeddedUIDigest()
  port = await reserveLoopbackPort()
  origin = `http://127.0.0.1:${port}`
  await rm(dataRoot, { force: true, recursive: true })
  managedProduct = spawnProduct(
    binary,
    [
      'serve',
      '--port',
      String(port),
      '--catalog-dir',
      'testdata/expiry/browser-catalog',
      '--data-dir',
      dataRoot,
    ],
    { cwd: repositoryRoot, stdio: 'pipe', windowsHide: true },
  )
  await waitForAuthenticatedRoot()
})

test.afterAll(async () => {
  let cleanupError: unknown
  try {
    if (managedProduct !== undefined)
      await stopOwnedProduct(managedProduct.process)
    if (port !== 0) await assertPortBindable(port)
  } catch (error) {
    cleanupError = error
  }
  try {
    await rm(dataRoot, { force: true, recursive: true })
    if (existsSync(dataRoot))
      throw new Error('browser data cleanup did not complete')
  } catch (error) {
    cleanupError ??= error
  }
  if (cleanupError !== undefined) throw cleanupError
})

test('the library starts empty and shelves the route the composer creates and publishes', async ({
  page,
}) => {
  test.setTimeout(180000)
  const requestURLs: string[] = []
  const mutations: {
    body: string | null
    headers: Record<string, string>
    path: string
  }[] = []
  const consoleErrors: string[] = []
  const pageErrors: string[] = []
  page.on('request', (request) => {
    requestURLs.push(request.url())
    if (request.method() === 'POST') {
      mutations.push({
        body: request.postData(),
        headers: request.headers(),
        path: new URL(request.url()).pathname,
      })
    }
  })
  page.on('console', (message) => {
    // A forecast the server cannot answer yet — nothing has observed these
    // services on a fresh install — is reported by the browser itself. The
    // surface reads any refusal as "no forecast": it says nothing and blocks
    // nothing, which is what the rest of this walkthrough proves.
    if (message.location().url.includes(forecastPath)) return
    if (message.type() === 'error') {
      const location = message.location()
      consoleErrors.push(
        `${message.text()} @ ${location.url}:${location.lineNumber}:${location.columnNumber}`,
      )
    }
  })
  page.on('pageerror', (error) => pageErrors.push(error.message))

  const root = await page.goto(`${origin}/`)
  expect(root?.status()).toBe(200)
  expect(root?.headers()['x-routevane-ui-digest']).toBe(expectedUIDigest)
  expect(root?.headers()['content-security-policy']).toBe(
    "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self'; font-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'",
  )
  expect(root?.headers()['content-security-policy']).not.toContain('unsafe-')

  // The library is the home screen, and an empty shelf explains what a saved
  // route does before asking a first-time operator to create one.
  await expect(
    page.getByRole('heading', { level: 1, name: 'Routes' }),
  ).toBeVisible()
  await expect(page.getByText('No routes yet')).toBeVisible()
  await expect(
    page.getByText(
      'Choose lists and a device; Routevane will prepare the rules and show the available connection methods.',
    ),
  ).toBeVisible()

  await page
    .locator('.library__header')
    .getByRole('link', { name: 'Build a route' })
    .click()
  await page.waitForURL(`${origin}/lists/new`)
  await expect(
    page.getByRole('heading', { level: 1, name: 'New route' }),
  ).toBeVisible()
  // Nothing can be prepared before both the contents and their first
  // connection are chosen, so the primary action cannot create a
  // half-configured result.
  const submit = page.getByRole('button', { name: 'Create and prepare' })
  await expect(submit).toBeDisabled()

  // The list card is one table: every destination the list stands for with its
  // origin, read here rather than edited. Adding to the route happens from the
  // same card, before or after the checkbox in the column behind it.
  await page
    .getByRole('button', {
      name: 'Open the contents of list Discord',
    })
    .click()
  const serviceCard = page.getByRole('dialog', {
    exact: true,
    name: 'Discord',
  })
  await expect(
    serviceCard.getByRole('heading', { name: 'List contents' }),
  ).toBeVisible()
  await expect(
    serviceCard.locator('.service-card__value').first(),
  ).toBeVisible()
  await expect(serviceCard.getByText('catalog').first()).toBeVisible()
  // The catalog seeds an address as well as a domain. The value says what it
  // is; the caption says only where it came from.
  const seededAddress = cardRow(serviceCard, '192.0.2.10')
  await expect(seededAddress).toContainText('catalog')
  await expect(seededAddress).not.toContainText('IP address')

  // The card opens on the composition. Which feeds a list reads is the
  // library's subject (ADR 0029), so the composing card states how many there
  // are and offers nothing here that would change them.
  await expect(
    serviceCard.getByRole('heading', { name: 'Automatic sources' }),
  ).toHaveCount(0)
  await expect(serviceCard.getByText(/^Sources · \d+$/)).toBeVisible()
  for (const absent of [/^Sources · \d+$/, /^Refresh from sources$/])
    await expect(serviceCard.getByRole('button', { name: absent })).toHaveCount(
      0,
    )

  // A list can stand for hundreds of destinations, so the card narrows them in
  // place rather than asking the operator to scroll.
  const contentsFilter = serviceCard.getByRole('searchbox', {
    name: 'Search the contents',
  })
  await contentsFilter.fill('192.0.2.10')
  await expect(serviceCard.locator('.service-card__rows li')).toHaveCount(1)
  await contentsFilter.fill('no-such-entry')
  await expect(serviceCard.getByText('Nothing found.')).toBeVisible()
  await contentsFilter.fill('')
  // A modal dialog owns the scroll: the page behind it must not move.
  expect(
    await page.evaluate(
      () => getComputedStyle(document.body).overflow === 'hidden',
    ),
  ).toBe(true)
  await serviceCard.getByRole('button', { name: 'Add to route' }).click()
  await serviceCard.getByRole('button', { name: 'Close' }).last().click()
  await expect(page.locator('input[value="discord"]')).toBeChecked()
  await expect(page.getByText('1 list in the route')).toBeVisible()
  await expect(submit).toBeDisabled()

  // The search narrows the checkboxes without ever unpicking a list.
  const search = page.getByRole('searchbox', { name: 'Find a list' })
  await search.fill('youtu')
  await expect(page.locator('input[value="discord"]')).toHaveCount(0)
  await page.locator('input[value="youtube"]').check()
  await expect(page.getByText('2 lists in the route')).toBeVisible()

  // Priority is the route's overlap policy. The first row is dragged below the
  // second here, while the same handle also exposes arrow-key reordering.
  const priorityRows = page.locator('.priority-list__item')
  await expect(priorityRows).toHaveCount(2)
  await expect(priorityRows.nth(0)).toHaveAttribute('data-id', 'discord')
  const dragHandle = priorityRows.nth(0).locator('.priority-list__handle')
  await dragHandle.scrollIntoViewIfNeeded()
  const from = await dragHandle.boundingBox()
  const to = await priorityRows.nth(1).boundingBox()
  if (from === null || to === null)
    throw new Error('Priority rows have no geometry')
  await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2)
  await page.mouse.down()
  await page.mouse.move(from.x + from.width / 2 + 4, from.y + from.height, {
    steps: 4,
  })
  await page.waitForTimeout(50)
  await page.mouse.move(to.x + to.width / 2, to.y + to.height + 8, {
    steps: 16,
  })
  await page.waitForTimeout(100)
  await page.mouse.up()
  await expect(priorityRows.nth(0)).toHaveAttribute('data-id', 'youtube')

  // The name is proposed from what was picked.
  const nameInput = page.locator('#create-name')
  await expect(nameInput).toHaveValue('Discord, YouTube')

  // The proposed name is a starting point, never a gate.
  await nameInput.fill('Discord, YouTube')

  // The connection is chosen in the same flow. Its card states both the format
  // and whether Routevane has a direct adapter for it.
  await openFormats(page)

  // The list opens against the field it belongs to: the same left edge, and
  // never narrower than it. A panel centred under a field reads as a menu that
  // happens to be near it rather than as that field's own list.
  const targetField = page.locator('.rv-combobox__field')
  const targetPanel = page.locator('.rv-combobox__panel')
  await expect(targetPanel).toBeVisible()
  const fieldBox = await targetField.boundingBox()
  const panelBox = await targetPanel.boundingBox()
  expect(fieldBox).not.toBeNull()
  expect(panelBox).not.toBeNull()
  expect(Math.abs((panelBox?.x ?? 0) - (fieldBox?.x ?? 0))).toBeLessThanOrEqual(
    1,
  )
  expect(panelBox?.width ?? 0).toBeGreaterThanOrEqual(
    (fieldBox?.width ?? 0) - 1,
  )
  // A field drawn as a bordered wrapper around an input stands exactly as tall
  // as a plain input: the wrapper owns the height, not the input inside it.
  const nameBox = await page.locator('#create-name').boundingBox()
  expect(Math.round(fieldBox?.height ?? 0)).toBe(
    Math.round(nameBox?.height ?? -1),
  )

  await expect(
    page.getByRole('option', { name: /Keenetic.*\.bat.*from Routevane/ }),
  ).toBeVisible()
  await page.getByRole('option', { name: /Keenetic/ }).click()
  await expect(page.locator('#create-target')).toHaveValue('Keenetic')
  await expect(submit).toBeEnabled()
  const outputsResponse = page.waitForResponse(
    (candidate) =>
      candidate.request().method() === 'POST' &&
      /\/v1\/lists\/[a-f0-9]{32}\/outputs$/.test(
        new URL(candidate.url()).pathname,
      ),
  )
  await submit.click()
  await page.waitForURL(
    new RegExp(`^${escapeRegExp(origin)}/lists/[a-f0-9]{32}.*$`),
  )
  const listID = listIDFromURL(page.url())
  expect(listID).toMatch(/^[a-f0-9]{32}$/)
  await expect(
    page.getByRole('heading', { level: 1, name: 'Discord, YouTube' }),
  ).toBeVisible()

  // The chosen connection is published in the same visible flow, and the stored
  // route carries exactly what was picked. The list card reads its own sources
  // when it opens, so the route flow is read without the card's writes.
  expect([listFlow(mutations)[0]?.path, listFlow(mutations)[0]?.body]).toEqual([
    '/v1/lists',
    JSON.stringify({
      name: 'Discord, YouTube',
      services: ['discord', 'youtube'],
      categories: [],
      exclusions: [],
      service_domains: {},
      priority: ['youtube', 'discord'],
    }),
  ])

  const tablist = page.getByRole('tablist', { name: 'Route sections' })
  await expect(tablist.getByRole('tab')).toHaveText([
    'Contents',
    'Connection',
    'File',
    'Diagnostics',
  ])
  const outputPayload = (await (await outputsResponse).json()) as {
    output: { id: string }
  }
  const outputID = outputPayload.output.id
  expect(outputID).toMatch(/^[a-f0-9]{32}$/)

  // The one-time subscription block appears the moment the build finishes.
  await expect(
    page.getByRole('heading', { name: 'Subscription link · Keenetic' }),
  ).toBeVisible()
  const targetSelect = page.locator('#outputs-target')
  await expect(targetSelect).toBeEnabled()

  const published = listFlow(mutations)
  expect(published).toHaveLength(4)
  expect([published[1]?.path, published[1]?.body]).toEqual([
    `/v1/lists/${listID}/outputs`,
    JSON.stringify({ target_id: 'keenetic' }),
  ])
  expect([published[2]?.path, published[2]?.body]).toEqual([
    `/v1/lists/${listID}/refresh`,
    '{}',
  ])
  expect([published[3]?.path, published[3]?.body]).toEqual([
    `/v1/outputs/${outputID}/build`,
    '{}',
  ])
  for (const mutation of mutations) {
    expect(mutation.headers['content-type']).toBe('application/json')
    expect(mutation.headers['x-routevane-request']).toBe('1')
  }

  // The new output is a row: what it is, what it renders, when it was last
  // built, and where its own actions live.
  const outputRow = page.getByRole('row').filter({ hasText: 'Keenetic' })
  await expect(outputRow).toContainText('Router')
  await expect(outputRow).toContainText('.bat')
  const artifactLink = outputRow.getByRole('link', {
    name: 'Download the file for Keenetic',
  })
  await expect(artifactLink).toHaveAttribute(
    'href',
    /^\/v1\/artifacts\/[a-f0-9]{32}$/,
  )
  expect(await artifactLink.evaluate((element) => element.tagName)).toBe('A')
  const artifactPath = await artifactLink.getAttribute('href')
  expect(artifactPath).not.toBeNull()
  const directDownload = page.waitForEvent('download')
  await artifactLink.click()
  expect((await directDownload).suggestedFilename()).toMatch(/\.bat$/)

  await expect(outputRow.getByRole('link', { name: 'Send' })).toBeVisible()

  // The breadcrumb returns to the shelf, which now holds the new row.
  await page.getByRole('link', { name: 'Routes' }).first().click()
  await page.waitForURL(`${origin}/`)
  const row = page.getByRole('row').filter({ hasText: 'Discord, YouTube' })
  await expect(row).toHaveCount(1)
  await expect(
    row.getByRole('link', { exact: true, name: 'Discord, YouTube' }),
  ).toHaveAttribute('href', `/lists/${listID}`)
  // The route name keeps the original proposal, while the second line makes
  // the changed priority visible on the shelf.
  await expect(row.locator('.library__services')).toHaveText('YouTube, Discord')
  await expect(row.locator('.library__cell-outputs')).toHaveText('Keenetic')
  await expect(row.locator('.library__cell-outputs')).not.toContainText('BAT')
  await expect(row.getByRole('button')).toHaveCount(1)
  await row
    .getByRole('button', {
      name: 'Actions for route Discord, YouTube',
    })
    .click()
  const menu = page.locator('.rv-menu__panel:visible')
  // Everything in the panel is a menu item, so the keyboard traverses one list
  // rather than a mixture of buttons and links.
  await expect(menu.getByRole('menuitem', { name: 'Open route' })).toBeVisible()
  await expect(menu.getByRole('menuitem', { name: 'Download' })).toBeVisible()
  await expect(menu.getByRole('menuitem')).toHaveCount(6)
  await expect(
    menu.getByRole('menuitem', { name: 'Send to Keenetic' }),
  ).toHaveAttribute('href', `/lists/${listID}/send/${outputID}`)
  const exported = page.waitForResponse(
    (candidate) =>
      candidate.request().method() === 'POST' &&
      new URL(candidate.url()).pathname === `/v1/lists/${listID}/export`,
  )
  const download = page.waitForEvent('download')
  await menu.getByRole('menuitem', { name: 'Download' }).click()
  const formats = page.locator('.rv-menu__panel:visible').last()
  await expect(
    formats.getByRole('menuitem', { name: 'JSON · all rules' }),
  ).toBeVisible()
  await expect(
    formats.getByRole('menuitem', { name: 'BAT · routes' }),
  ).toHaveCount(1)
  await formats.getByRole('menuitem', { name: 'JSON · all rules' }).click()
  expect((await exported).status()).toBe(200)
  expect((await download).suggestedFilename()).toMatch(/\.json$/)

  await row
    .getByRole('button', { name: 'Actions for route Discord, YouTube' })
    .click()
  const reopenedMenu = page.locator('.rv-menu__panel:visible')
  await expect(
    reopenedMenu.getByRole('menuitem', { name: 'Copy contents' }),
  ).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(reopenedMenu).toBeHidden()

  await row.getByRole('link', { exact: true, name: 'Discord, YouTube' }).click()
  await page.waitForURL(`${origin}/lists/${listID}`)
  await expect(
    page.getByRole('heading', { level: 1, name: 'Discord, YouTube' }),
  ).toBeVisible()
  await expect(page.locator('#editor-name')).toHaveValue('Discord, YouTube')
  const listPage = page.locator('main')

  // The route states its own composition first. Each list is a row with the way
  // out beside it, and its card opens from that row rather than from a catalog
  // the operator has to search through again.
  const compositionRow = listPage
    .locator('.priority-list__item')
    .filter({ hasText: 'Discord' })
  await expect(
    compositionRow.getByRole('button', {
      name: 'Remove Discord from the route',
    }),
  ).toBeVisible()
  await expect(
    listPage.getByRole('button', { name: 'Remove YouTube from the route' }),
  ).toBeVisible()
  await compositionRow
    .getByRole('button', { name: 'Open the contents of list Discord' })
    .click()
  const serviceDialog = page.getByRole('dialog', {
    exact: true,
    name: 'Discord',
  })
  await expect(
    serviceDialog.getByRole('heading', { name: 'List contents' }),
  ).toBeVisible()
  await expect(serviceDialog.getByText('catalog').first()).toBeVisible()
  expect(await audit(page, 'service-detail')).toEqual([])
  await assertNoOverflow(page, 'service-detail')
  await serviceDialog.getByRole('button', { name: 'Close' }).click()

  // The catalog is a way to add, and it opens on request rather than sitting
  // under every route as a second copy of the composer.
  await expect(listPage.locator('.picker__workspace')).toBeHidden()
  await listPage.getByRole('button', { name: 'Add lists' }).click()
  const pickerWorkspace = listPage.locator('.picker__workspace')
  await expect(pickerWorkspace).toBeVisible()

  // A list no category claims is the last row of the same column, never a
  // second surface beside it.
  const unclaimed = listPage.locator('.picker__group').last()
  await expect(unclaimed).toContainText('Uncategorized')
  await expect(unclaimed).toContainText('Limit fixture')
  await expect(listPage.getByText('Other services')).toHaveCount(0)
  const membersButton = listPage.getByRole('button', {
    name: 'Show the contents of category Communication',
  })
  await expect(membersButton).toHaveText('')
  await expect(membersButton).toHaveAttribute('aria-current', 'true')

  // The workspace height follows the viewport, so the stability baseline is
  // measured after the overflow sweep put the viewport back. What must not
  // change it is the pane switch below.
  const workspaceBox = await pickerWorkspace.boundingBox()
  expect(workspaceBox).not.toBeNull()
  const videoButton = listPage.getByRole('button', {
    name: 'Show the contents of category Video',
  })
  await videoButton.click()
  await expect(
    listPage.getByRole('heading', { name: 'Video', exact: true }),
  ).toBeVisible()
  const switchedWorkspaceBox = await pickerWorkspace.boundingBox()
  expect(switchedWorkspaceBox).not.toBeNull()
  expect(switchedWorkspaceBox?.width).toBeCloseTo(workspaceBox?.width ?? 0)
  expect(switchedWorkspaceBox?.height).toBeCloseTo(workspaceBox?.height ?? 0)
  await membersButton.click()
  await expect(
    listPage.getByRole('heading', { name: 'Communication', exact: true }),
  ).toBeVisible()
  const discord = listPage.locator('input[value="discord"]')
  const discordRow = discord.locator('xpath=ancestor::label')
  const rowHeight = await discordRow.evaluate((element) => element.clientHeight)
  await discord.uncheck()
  await expect(discordRow).toHaveText('Discord')
  await expect(discordRow).not.toContainText('Selected manually')
  expect(await discordRow.evaluate((element) => element.clientHeight)).toBe(
    rowHeight,
  )
  await expect(listPage.getByText('1 list in the route')).toBeVisible()
  await expect(
    listPage.locator('input[value="communication"]'),
  ).toHaveJSProperty('indeterminate', false)

  expect(consoleErrors).toEqual([])
  expect(pageErrors).toEqual([])
  for (const requestURL of requestURLs)
    expect(new URL(requestURL).origin).toBe(origin)
  assertProductAlive()
})

test('the service card takes domains, addresses and networks, typed or imported', async ({
  page,
}) => {
  test.setTimeout(180000)
  const importRoot = join(repositoryRoot, '.cache', 'browser-import')
  const routesFile = join(importRoot, 'keenetic-routes.bat')
  await rm(importRoot, { force: true, recursive: true })
  await mkdir(importRoot, { recursive: true })
  // What a router hands its owner: a label, one network, and one mask that is
  // not a prefix at all.
  await writeFile(
    routesFile,
    [
      ':: routes exported from the router',
      'route ADD 198.18.0.0 MASK 255.255.240.0 0.0.0.0',
      'route ADD 198.18.32.0 MASK 255.0.255.0 0.0.0.0',
      '',
    ].join('\r\n'),
    'utf8',
  )

  try {
    // Entries are the list's own material, so they are added where the list is
    // curated rather than where a route is composed (ADR 0029).
    await page.goto(`${origin}/library`)
    await openLibraryCategory(page, 'Communication')
    await page
      .getByRole('button', { name: 'Open the contents of list Discord' })
      .click()
    const card = page.getByRole('dialog', { exact: true, name: 'Discord' })
    await expect(
      card.getByRole('heading', { name: 'List contents' }),
    ).toBeVisible()
    // No route is in question here, so the card asks about none.
    await expect(card.locator('.service-card__membership')).toHaveCount(0)

    // The filter and the control beside it share one height: a toolbar is one
    // line, not two controls that happen to be near each other.
    const filterBox = await card.locator('.service-card__filter').boundingBox()
    const addBox = await card
      .getByRole('button', { name: 'Add entries' })
      .boundingBox()
    expect(Math.round(filterBox?.height ?? 0)).toBe(
      Math.round(addBox?.height ?? -1),
    )

    // One field for all three shapes, one submission for the whole draft. The
    // way in sits above the table it adds to, and opens a panel over it rather
    // than a form below everything the table says.
    await card.getByRole('button', { name: 'Add entries' }).click()
    const entries = page.getByRole('dialog', {
      exact: true,
      name: 'Add entries',
    })
    await expect(entries).toBeVisible()

    // One dimming for the stack: a panel opened over a sheet darkens the page
    // once, not twice.
    await expect(page.locator('.rv-dialog__scrim')).toHaveCount(2)
    await expect(
      page.locator('.rv-dialog__scrim:not(.rv-dialog__scrim--nested)'),
    ).toHaveCount(1)

    await entries
      .locator('#service-add-entries')
      .fill('corp.example\n203.0.113.7\n203.0.113.0/29')
    await entries.getByRole('button', { exact: true, name: 'Add' }).click()
    await expect(entries).toBeHidden()
    // All three shapes land as rows of the one table, each naming only where
    // it came from: the value already says what it is.
    await expect(cardRow(card, 'corp.example')).toContainText('by hand')
    await expect(cardRow(card, '203.0.113.7')).toContainText('by hand')
    await expect(cardRow(card, '203.0.113.0/29')).toContainText('by hand')

    // A routes file the operator already has is read in place, and the line it
    // could not read is stated rather than silently dropped — on the card,
    // beside the rows the file did land in.
    await card.getByRole('button', { name: 'Add entries' }).click()
    await expect(entries).toBeVisible()
    await entries.locator('input[type="file"]').setInputFiles(routesFile)
    await expect(entries).toBeHidden()
    await expect(cardRow(card, '198.18.0.0/20')).toContainText('by hand')
    await expect(card.getByText('1 line skipped')).toBeVisible()

    // Switching an added row off takes it back, which leaves the service as
    // this test found it for everything that reads Discord after it.
    for (const value of [
      'corp.example',
      '203.0.113.7',
      '203.0.113.0/29',
      '198.18.0.0/20',
    ]) {
      await cardRow(card, value).locator('input[type="checkbox"]').click()
      await expect(cardRow(card, value)).toHaveCount(0)
    }

    await card.getByRole('button', { name: 'Close' }).last().click()
    await expect(card).toBeHidden()
    assertProductAlive()
  } finally {
    await rm(importRoot, { force: true, recursive: true })
  }
})

/**
 * The card is a modal sheet, not part of the page it was opened from. This is
 * the failure it exists to prevent: a card opened after the page had been
 * scrolled used to be drawn where the page was, not where the screen is, and
 * the operator saw a cut-off panel over a page that kept scrolling underneath.
 */
test('the service card stays whole over a scrolled page and gives the scroll back', async ({
  page,
}) => {
  test.setTimeout(120000)
  const cspErrors: string[] = []
  page.on('console', (message) => {
    if (
      message.type() === 'error' &&
      /content security policy|refused to (?:apply|execute).*inline/i.test(
        message.text(),
      )
    )
      cspErrors.push(message.text())
  })
  await page.setViewportSize({ height: 640, width: 1280 })
  await page.goto(`${origin}/lists/new`)
  await expect(
    page.getByRole('heading', { level: 1, name: 'New route' }),
  ).toBeVisible()
  const logo = page.locator('.shell__product-mark')
  const logoBeforeScroll = await logo.boundingBox()
  expect(logoBeforeScroll).not.toBeNull()

  // The page is scrolled before the card opens. The click below would scroll
  // its own target into view, so the position the card must preserve is the
  // one read after that adjustment, not before it.
  await page.evaluate(() => window.scrollTo(0, document.body.scrollHeight))
  const opener = page.getByRole('button', {
    name: 'Open the contents of list Discord',
  })
  await opener.scrollIntoViewIfNeeded()
  const scrolled = await page.evaluate(() => Math.round(window.scrollY))
  expect(scrolled).toBeGreaterThan(0)
  const logoAfterScroll = await logo.boundingBox()
  expect(logoAfterScroll).not.toBeNull()
  expect(logoAfterScroll?.width).toBeCloseTo(logoBeforeScroll?.width ?? 0, 2)
  expect(logoAfterScroll?.height).toBeCloseTo(logoBeforeScroll?.height ?? 0, 2)

  await opener.focus()
  await expect(opener).toBeFocused()

  // Install the animation observer before the state change that creates the
  // portalled sheet. This makes the midpoint oracle independent of a 200 ms
  // polling race while still testing the browser's real CSS transition.
  await page.evaluate(() => {
    type Probe = {
      animation?: Animation
      promise: Promise<{ left: number; right: number; width: number }>
    }
    const probe = {} as Probe
    probe.promise = new Promise((resolve) => {
      const onStart = (event: Event) => {
        if (
          !(event instanceof AnimationEvent) ||
          event.animationName !== 'rv-dialog-sheet-in' ||
          !(event.target instanceof HTMLElement) ||
          !event.target.matches('.rv-dialog--sheet')
        )
          return
        document.removeEventListener('animationstart', onStart, true)
        const animation = event.target
          .getAnimations()
          .find((candidate) => candidate.constructor.name === 'CSSAnimation')
        const duration = animation?.effect?.getTiming().duration
        if (animation === undefined || typeof duration !== 'number')
          throw new Error('Sheet animation did not expose numeric timing')
        animation.pause()
        animation.currentTime = duration / 2
        probe.animation = animation
        requestAnimationFrame(() => {
          const box = event.target.getBoundingClientRect()
          resolve({ left: box.left, right: box.right, width: box.width })
        })
      }
      document.addEventListener('animationstart', onStart, true)
    })
    Reflect.set(window, '__routevaneSheetTransitionProbe', probe)
  })

  await opener.click()
  const card = page.getByRole('dialog', { exact: true, name: 'Discord' })
  await expect(card).toBeVisible()
  await expect(
    card.getByRole('searchbox', { name: 'Search the contents' }),
  ).toBeFocused()

  // USlideover supplies the real sheet. Its CSP-safe facade animation begins
  // beyond the right edge; pause it at its midpoint to prove that movement is
  // perceptible, then let it settle before asserting final geometry.
  const viewport = page.viewportSize()
  const midpoint = await page.evaluate(async () => {
    const probe = Reflect.get(window, '__routevaneSheetTransitionProbe') as {
      promise: Promise<{ left: number; right: number; width: number }>
    }
    return await probe.promise
  })
  expect(midpoint.left).toBeGreaterThan((viewport?.width ?? 0) - midpoint.width)
  expect(midpoint.right).toBeGreaterThan(viewport?.width ?? 0)
  await page.evaluate(() => {
    const probe = Reflect.get(window, '__routevaneSheetTransitionProbe') as {
      animation?: Animation
    }
    probe.animation?.play()
  })
  await expect
    .poll(async () => {
      const entering = await card.boundingBox()
      if (entering === null) return Number.POSITIVE_INFINITY
      return entering.x + entering.width
    })
    .toBeLessThanOrEqual((viewport?.width ?? 0) + 1)

  // Wholly inside the screen, whatever the page behind it is doing.
  const box = await card.boundingBox()
  expect(box).not.toBeNull()
  expect(box?.width ?? 0).toBeGreaterThanOrEqual(1000)
  expect(box?.x ?? -1).toBeGreaterThanOrEqual(0)
  expect(box?.y ?? -1).toBeGreaterThanOrEqual(0)
  expect((box?.x ?? 0) + (box?.width ?? 0)).toBeLessThanOrEqual(
    (viewport?.width ?? 0) + 1,
  )
  expect((box?.y ?? 0) + (box?.height ?? 0)).toBeLessThanOrEqual(
    (viewport?.height ?? 0) + 1,
  )

  // The page behind it owns nothing while it is open: it does not scroll, and
  // the context the card was opened from cannot drift away.
  expect(
    await page.evaluate(() => getComputedStyle(document.body).overflow),
  ).toBe('hidden')
  await page.mouse.move(8, 320)
  await page.mouse.wheel(0, -600)
  expect(await page.evaluate(() => Math.round(window.scrollY))).toBe(scrolled)

  // The library primitive owns a modal focus loop. Both ends wrap inside the
  // real USlideover, and close returns to the external opener.
  const focusable = card.locator(
    'button:not([disabled]), a[href], input:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])',
  )
  await focusable.first().focus()
  await page.keyboard.press('Shift+Tab')
  expect(
    await card.evaluate((element) => element.contains(document.activeElement)),
  ).toBe(true)
  await focusable.last().focus()
  await page.keyboard.press('Tab')
  expect(
    await card.evaluate((element) => element.contains(document.activeElement)),
  ).toBe(true)

  // Closing hands the page back exactly where it was left.
  await expect
    .poll(() =>
      card.evaluate((element) =>
        element
          .getAnimations()
          .some(
            (animation) =>
              animation.constructor.name === 'CSSAnimation' &&
              (animation.playState === 'running' ||
                animation.playState === 'paused'),
          ),
      ),
    )
    .toBe(false)
  const closeControl = card.getByRole('button', { name: 'Close' })
  await expect(closeControl).toBeEnabled()
  const settledClose = await page.evaluate(
    () =>
      new Promise<{ latency: number; removed: boolean }>((resolve) => {
        const sheet = document.querySelector<HTMLElement>('.rv-dialog--sheet')
        const close =
          sheet?.querySelector<HTMLButtonElement>('.rv-dialog__close')
        if (sheet === null || close === null)
          throw new Error('Settled sheet close controls are unavailable')

        let finished = false
        let clickStarted = Number.NaN
        const finish = (removed: boolean) => {
          if (finished) return
          finished = true
          observer.disconnect()
          window.clearTimeout(deadline)
          resolve({
            latency: performance.now() - clickStarted,
            removed,
          })
        }
        const observer = new MutationObserver(() => {
          if (!sheet.isConnected) finish(true)
        })
        observer.observe(document.body, { childList: true, subtree: true })
        const deadline = window.setTimeout(() => finish(false), 1_000)
        close.addEventListener(
          'click',
          () => {
            clickStarted = performance.now()
          },
          { capture: true, once: true },
        )
        close.click()
      }),
  )
  expect(settledClose.removed).toBe(true)
  expect(settledClose.latency).toBeLessThan(150)
  await expect(card).toBeHidden()
  await expect(opener).toBeFocused()
  await expect
    .poll(() => page.evaluate(() => getComputedStyle(document.body).overflow))
    .not.toBe('hidden')
  expect(await page.evaluate(() => Math.round(window.scrollY))).toBe(scrolled)

  // Reduced motion keeps the same final geometry but collapses the animation
  // to the global near-zero duration.
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await opener.click()
  await expect(card).toBeVisible()
  await expect
    .poll(async () => {
      const reduced = await card.boundingBox()
      if (reduced === null) return Number.POSITIVE_INFINITY
      return reduced.x + reduced.width
    })
    .toBeLessThanOrEqual((viewport?.width ?? 0) + 1)
  const reducedDuration = await card.evaluate((element) =>
    getComputedStyle(element)
      .animationDuration.split(',')
      .map((duration) =>
        duration.endsWith('ms')
          ? Number.parseFloat(duration)
          : Number.parseFloat(duration) * 1000,
      )
      .reduce((longest, duration) => Math.max(longest, duration), 0),
  )
  expect(reducedDuration).toBeLessThanOrEqual(1)
  await card.getByRole('button', { name: 'Close' }).click()
  await expect(card).toBeHidden()
  await expect(opener).toBeFocused()
  expect(await page.evaluate(() => Math.round(window.scrollY))).toBe(scrolled)
  await page.emulateMedia({ reducedMotion: 'no-preference' })

  // A quick dismissal during the entrance has no matching exit animation and
  // must not revive the CSP-unsafe runtime style writer.
  await page.evaluate(() => {
    type QuickCloseResult = {
      latency: number
      midpoint: { left: number; right: number; width: number }
      playStateBeforeClose: AnimationPlayState
      progressBeforeClose: number | null
      removed: boolean
    }
    const promise = new Promise<QuickCloseResult>((resolve, reject) => {
      const onStart = (event: Event) => {
        if (
          !(event instanceof AnimationEvent) ||
          event.animationName !== 'rv-dialog-sheet-in' ||
          !(event.target instanceof HTMLElement) ||
          !event.target.matches('.rv-dialog--sheet')
        )
          return
        document.removeEventListener('animationstart', onStart, true)

        const sheet = event.target
        const animation = sheet
          .getAnimations()
          .find((candidate) => candidate.constructor.name === 'CSSAnimation')
        const duration = animation?.effect?.getTiming().duration
        if (animation === undefined || typeof duration !== 'number') {
          reject(new Error('Quick-close animation has no numeric timing'))
          return
        }
        animation.pause()
        animation.currentTime = duration / 2

        requestAnimationFrame(() => {
          const close =
            sheet.querySelector<HTMLButtonElement>('.rv-dialog__close')
          if (close === null) {
            reject(new Error('Quick-close control is unavailable'))
            return
          }
          const box = sheet.getBoundingClientRect()
          const midpoint = {
            left: box.left,
            right: box.right,
            width: box.width,
          }
          const progressBeforeClose =
            animation.effect?.getComputedTiming().progress ?? null
          animation.play()
          const playStateBeforeClose = animation.playState

          let finished = false
          let clickStarted = Number.NaN
          const finish = (removed: boolean) => {
            if (finished) return
            finished = true
            observer.disconnect()
            window.clearTimeout(deadline)
            resolve({
              latency: performance.now() - clickStarted,
              midpoint,
              playStateBeforeClose,
              progressBeforeClose,
              removed,
            })
          }
          const observer = new MutationObserver(() => {
            if (!sheet.isConnected) finish(true)
          })
          observer.observe(document.body, { childList: true, subtree: true })
          const deadline = window.setTimeout(() => finish(false), 1_000)
          close.addEventListener(
            'click',
            () => {
              clickStarted = performance.now()
            },
            { capture: true, once: true },
          )
          close.click()
        })
      }
      document.addEventListener('animationstart', onStart, true)
    })
    Reflect.set(window, '__routevaneQuickCloseProbe', { promise })
  })
  await opener.click()
  const quickClose = await page.evaluate(async () => {
    const probe = Reflect.get(window, '__routevaneQuickCloseProbe') as {
      promise: Promise<{
        latency: number
        midpoint: { left: number; right: number; width: number }
        playStateBeforeClose: AnimationPlayState
        progressBeforeClose: number | null
        removed: boolean
      }>
    }
    return await probe.promise
  })
  expect(quickClose.midpoint.left).toBeGreaterThan(
    (viewport?.width ?? 0) - quickClose.midpoint.width,
  )
  expect(quickClose.midpoint.right).toBeGreaterThan(viewport?.width ?? 0)
  expect(quickClose.progressBeforeClose).toBeGreaterThan(0)
  expect(quickClose.progressBeforeClose).toBeLessThan(1)
  expect(quickClose.playStateBeforeClose).toBe('running')
  expect(quickClose.removed).toBe(true)
  expect(quickClose.latency).toBeLessThan(150)
  await expect(card).toBeHidden()
  await expect(opener).toBeFocused()
  expect(cspErrors).toEqual([])

  await page.setViewportSize({ height: 900, width: 1280 })
  assertProductAlive()
})

test('legacy profile names are explained as restored lists', async ({
  page,
}) => {
  await page.goto(`${origin}/`)
  const created = await page.request.post(`${origin}/v1/lists`, {
    data: {
      categories: [],
      exclusions: [],
      name: 'Imported 42 · youtube · keenetic',
      services: ['youtube'],
    },
    headers: {
      Origin: origin,
      'X-Routevane-Request': '1',
    },
  })
  expect(created.ok()).toBe(true)
  const payload = (await created.json()) as { list: { id: string } }

  await page.reload()
  await expect(
    page.getByText('1 route restored from the previous version'),
  ).toBeVisible()
  const restored = page.getByRole('link', {
    exact: true,
    name: 'Restored route 42',
  })
  await expect(restored).toHaveAttribute('href', `/lists/${payload.list.id}`)
  await restored.click()
  await expect(
    page.getByRole('heading', {
      level: 1,
      name: 'Restored route 42',
    }),
  ).toBeVisible()
  await expect(
    page.getByText('Restored from the previous version'),
  ).toBeVisible()
})

test('the route page guards the secret, shows the file and its diagnostics, and republishes on rename', async ({
  page,
}) => {
  test.setTimeout(180000)
  const requestURLs: string[] = []
  const mutations: { body: string | null; path: string }[] = []
  const pageErrors: string[] = []
  page.on('request', (request) => {
    requestURLs.push(request.url())
    const path = new URL(request.url()).pathname
    if (request.method() === 'POST' && statesFlow(path)) {
      mutations.push({ body: request.postData(), path })
    }
  })
  page.on('pageerror', (error) => pageErrors.push(error.message))

  const { listId, outputId } = await buildList(page)
  const listURL = `${origin}/lists/${listId}`

  await expect(page.locator('.list__header .rv-status__label')).toHaveText(
    'Published with notes',
  )
  await expect(page.getByText('Coverage is incomplete')).toBeVisible()
  await expect(page.locator('#editor-name')).toHaveValue('Discord, YouTube')
  // The contents tab states the composition itself, one row per list, and keeps
  // the catalog behind the control that adds to it.
  const routePriority = page.locator('main').locator('.priority-list__item')
  await expect(routePriority).toHaveCount(2)
  await expect(routePriority).toContainText(['Discord', 'YouTube'])

  // Refresh belongs to Connection. The inactive panel stays mounted for stable
  // state, but its control must not leak into the Contents tab.
  await expect(page.locator('#list-schedule-select')).toBeHidden()

  // The subscription link is shown masked: the raw secret is not in the DOM,
  // not in any request, until the operator asks for it.
  await expect(
    page.getByRole('heading', { name: 'Subscription link · Keenetic' }),
  ).toBeVisible()
  const secret = page.locator('.list__secret-value')
  await expect(secret).toContainText(`${origin}/v1/subscriptions/`)
  expect(await page.content()).not.toContain('rv1.')
  await page.getByRole('button', { name: 'Show', exact: true }).click()
  await expect(secret).toContainText(`${origin}/v1/subscriptions/rv1.`)
  expect(requestURLs.some((url) => url.includes('rv1.'))).toBe(false)

  // The tabs are one object's facets, announced as tabs. Connection sits between
  // the overview and the file, because a route may feed several formats.
  const tablist = page.getByRole('tablist', { name: 'Route sections' })
  await expect(tablist.getByRole('tab')).toHaveText([
    'Contents',
    'Connection',
    'File',
    'Diagnostics',
  ])

  // The file is read only when the operator opens it, and what is shown is
  // byte-for-byte what the device receives.
  await tablist.getByRole('tab', { name: 'Connection' }).click()
  const scheduleTrigger = page.locator('#list-schedule-select')
  await expect(scheduleTrigger).toBeVisible()
  const scheduleGround = async (): Promise<string> =>
    scheduleTrigger.evaluate((node) => getComputedStyle(node).backgroundColor)
  const atRest = await scheduleGround()
  await page.locator('#list-schedule').hover()
  expect(await scheduleGround()).toBe(atRest)
  await scheduleTrigger.hover()
  expect(await scheduleGround()).toBe(atRest)
  const artifactPath = new URL(
    (await page
      .getByRole('row')
      .filter({ hasText: 'Keenetic' })
      .getByRole('link', { name: 'Download the file for Keenetic' })
      .getAttribute('href')) ?? '',
    origin,
  ).pathname
  expect(artifactPath).toMatch(/^\/v1\/artifacts\/[a-f0-9]{32}$/)
  expect(
    requestURLs.filter((url) => new URL(url).pathname === artifactPath),
  ).toEqual([])
  const contentResponse = page.waitForResponse(
    (candidate) =>
      candidate.request().method() === 'GET' &&
      new URL(candidate.url()).pathname === artifactPath,
  )
  await tablist.getByRole('tab', { name: 'File' }).click()
  expect((await contentResponse).status()).toBe(200)
  const artifactBytes = await (await fetch(`${origin}${artifactPath}`)).text()
  // The fixture's catalog seeds a documentation-range address, and its DNS
  // source answers with loopback on purpose: the file proves that what reaches
  // a router is the routable destination, and that the loopback observation is
  // refused rather than installed.
  expect(artifactBytes).toContain(
    'route ADD 192.0.2.10 MASK 255.255.255.255 0.0.0.0',
  )
  expect(artifactBytes).not.toContain('127.0.0.1')
  const rendered = page.locator('pre.rv-code__body')
  await expect(rendered).toBeVisible()
  expect(await rendered.evaluate((element) => element.textContent)).toBe(
    artifactBytes,
  )
  await expect(page.locator('.rv-code__caption')).toContainText('1 line')

  // Diagnostics counts the published rules per service and translates the
  // format's stable reason code into operator language.
  const snapshotResponse = page.waitForResponse(
    (candidate) =>
      candidate.request().method() === 'GET' &&
      /^\/v1\/snapshots\/[a-f0-9]{32}$/.test(new URL(candidate.url()).pathname),
  )
  await tablist.getByRole('tab', { name: 'Diagnostics' }).click()
  expect((await snapshotResponse).status()).toBe(200)
  const counts = page.locator('.list__counts')
  await expect(counts).toContainText('Discord')
  await expect(counts).not.toContainText('YouTube')
  await expect(counts).toContainText('1 rule')
  const diagnosticsPanel = page.locator('#rv-panel-diagnostics')
  await expect(diagnosticsPanel).toContainText('Excluded')
  await expect(diagnosticsPanel).toContainText(
    'the rule belongs to a higher-priority list',
  )
  await expect(diagnosticsPanel).toContainText('not supported by this format')
  await expect(diagnosticsPanel).not.toContainText('unsupported_by_target')

  // A reload restores the route from the server. The one-time link does not
  // come back, and in the default detail mode nothing even mentions it.
  const requestsBeforeReload = requestURLs.length
  await page.reload()
  await expect(
    page.getByRole('heading', { level: 1, name: 'Discord, YouTube' }),
  ).toBeVisible()
  expect(
    requestURLs
      .slice(requestsBeforeReload)
      .some((url) => new URL(url).pathname === `/v1/lists/${listId}`),
  ).toBe(true)
  await expect(
    page.getByRole('heading', { name: /^Subscription link/ }),
  ).toHaveCount(0)
  await expect(
    page.getByRole('button', { name: 'Show', exact: true }),
  ).toHaveCount(0)
  await expect(
    page.getByText(
      'The subscription link was shown when the route was created.',
    ),
  ).toHaveCount(0)
  expect(await page.content()).not.toContain('rv1.')
  const storedAfterReload = await page.evaluate(() => ({
    local: { ...localStorage },
    session: { ...sessionStorage },
  }))
  expect(JSON.stringify(storedAfterReload)).not.toContain('rv1.')
  expect(requestURLs.some((url) => url.includes('rv1.'))).toBe(false)

  // The file is still this computer's record, readable after the reload.
  await tablist.getByRole('tab', { name: 'File' }).click()
  await expect(rendered).toBeVisible()
  expect(await rendered.evaluate((element) => element.textContent)).toBe(
    artifactBytes,
  )

  // Renaming and recomposing is an edit, not a new route: it republishes every
  // output the route already carries, and the name and the list composition
  // stay two independent facts.
  await tablist.getByRole('tab', { name: 'Contents' }).click()
  const editorNameInput = page.locator('#editor-name')
  await expect(editorNameInput).toHaveValue('Discord, YouTube')
  await editorNameInput.fill('Chat and video')
  await page.getByRole('button', { name: 'Save and rebuild' }).click()
  await expect(
    page.getByRole('heading', { level: 1, name: 'Chat and video' }),
  ).toBeVisible()
  expect(mutations.slice(-3)).toEqual([
    {
      body: JSON.stringify({
        name: 'Chat and video',
        services: ['discord', 'youtube'],
        categories: [],
        exclusions: [],
        service_domains: {},
        priority: ['discord', 'youtube'],
      }),
      path: `/v1/lists/${listId}/update`,
    },
    { body: '{}', path: `/v1/lists/${listId}/refresh` },
    { body: '{}', path: `/v1/outputs/${outputId}/build` },
  ])

  // The renamed route is still the same row, its list composition unmoved on
  // the second line.
  await page.getByRole('link', { name: 'Routes' }).first().click()
  await page.waitForURL(`${origin}/`)
  const renamedRow = page.getByRole('row').filter({ hasText: 'Chat and video' })
  await expect(renamedRow).toHaveCount(1)
  await expect(renamedRow.locator('.library__services')).toHaveText(
    'Discord, YouTube',
  )
  await renamedRow
    .getByRole('link', { exact: true, name: 'Chat and video' })
    .click()
  await page.waitForURL(listURL)

  // The expert mode is what states the link's fate in words, and it is also
  // the only mode that puts identifiers on the screen.
  await page
    .getByRole('navigation', { name: 'Sections' })
    .getByRole('link', { name: 'Settings' })
    .click()
  await segment(page, 'Full').click()
  await page
    .getByRole('navigation', { name: 'Sections' })
    .getByRole('link', { name: 'Routes' })
    .click()
  await page.getByRole('link', { exact: true, name: 'Chat and video' }).click()
  await expect(
    page.getByRole('heading', { level: 1, name: 'Chat and video' }),
  ).toBeVisible()
  await expect(
    page.getByText(
      'The subscription link was shown when the route was created.',
    ),
  ).toBeVisible()
  await expect(page.getByText('Technical details')).toBeVisible()

  expect(pageErrors).toEqual([])
  assertProductAlive()
})

test('a failed first build remains retryable and exposes no subscription', async ({
  page,
}) => {
  test.setTimeout(120000)
  // The composer no longer lets this pair be created: it forecasts the size
  // and refuses. The state under test is a route that already carries a format
  // it does not fit, so it is set up through the same API the composer calls.
  await page.goto(`${origin}/`)
  const mutationHeaders = { Origin: origin, 'X-Routevane-Request': '1' }
  const created = await page.request.post(`${origin}/v1/lists`, {
    data: {
      categories: [],
      exclusions: [],
      name: 'Over the limit',
      services: ['limit-fixture'],
    },
    headers: mutationHeaders,
  })
  expect(created.ok()).toBe(true)
  const failedListID = ((await created.json()) as { list: { id: string } }).list
    .id

  const added = await page.request.post(
    `${origin}/v1/lists/${failedListID}/outputs`,
    { data: { target_id: 'limited-fixture' }, headers: mutationHeaders },
  )
  expect(added.ok()).toBe(true)
  const addPayload = (await added.json()) as {
    output: { id: string }
  } & Record<string, unknown>
  expect(addPayload).not.toHaveProperty('subscription_url')

  const refreshed = await page.request.post(
    `${origin}/v1/lists/${failedListID}/refresh`,
    { data: {}, headers: mutationHeaders },
  )
  expect(refreshed.ok()).toBe(true)
  const failedResponse = await page.request.post(
    `${origin}/v1/outputs/${addPayload.output.id}/build`,
    { data: {}, headers: mutationHeaders },
  )
  expect(failedResponse.status()).toBe(422)
  await expect(failedResponse.json()).resolves.toMatchObject({
    code: 'rule_limit',
    maximum_rules: 1,
    projected_rules: 2,
  })

  await page.goto(`${origin}/lists/${failedListID}`)
  await expect(page.locator('.list__header .rv-status__label')).toHaveText(
    'Refresh failed',
  )
  // The editor says the same thing the build reported, before the operator
  // presses anything, and still lets the route be saved.
  await expect(
    page.getByText('Limited fixture: ≈ 2 of 1 — will not fit'),
  ).toBeVisible()
  await expect(page.getByText('Format not updated')).toBeVisible()
  await expect(
    page.getByText(
      'This needs 2 rules, but the format accepts at most 1. Remove some lists or choose another format.',
    ),
  ).toBeVisible()
  // The connection table lives on its own tab; a hidden row has no role.
  await page
    .getByRole('tablist', { name: 'Route sections' })
    .getByRole('tab', { name: 'Connection' })
    .click()
  const outputRow = page.getByRole('row').filter({ hasText: 'Limited fixture' })
  await expect(outputRow).toContainText('Last build failed')
  await expect(outputRow).toContainText('No published file')
  await expect(
    page.getByRole('heading', { name: /^Subscription link/ }),
  ).toHaveCount(0)
  expect(await page.content()).not.toContain('rv1.')

  await page.getByRole('link', { name: 'Routes' }).first().click()
  await page.waitForURL(`${origin}/`)
  await page.locator(`a.library__link[href="/lists/${failedListID}"]`).click()
  await expect(page.getByText('Format not updated')).toBeVisible()
  await page
    .getByRole('tablist', { name: 'Route sections' })
    .getByRole('tab', { name: 'Connection' })
    .click()
  await expect(outputRow).toContainText('Last build failed')
  await page
    .locator('main')
    .getByRole('button', { name: /Actions for route/ })
    .click()
  await page
    .locator('.rv-menu__panel:visible')
    .getByRole('menuitem', { name: 'Refresh and rebuild' })
    .click()
  await expect(page.getByText('Format not updated')).toBeVisible()
  await expect(
    page.getByRole('heading', { name: /^Subscription link/ }),
  ).toHaveCount(0)
})

// The failure above is the one this forecast exists to prevent, which is why
// it runs against the same fixture right after it.
test('the composer sizes every format and refuses the pair that cannot hold the route', async ({
  page,
}) => {
  test.setTimeout(120000)
  // The forecast speaks the data a build would read; a service whose sources
  // were never observed has no forecast yet. Observing it first keeps this
  // test independent of which test refreshed the fixture before it.
  const observed = await page.request.post(
    `${origin}/v1/services/limit-fixture/refresh`,
    { data: {}, headers: { Origin: origin, 'X-Routevane-Request': '1' } },
  )
  expect(observed.ok()).toBe(true)
  await page.goto(`${origin}/lists/new`)
  // The fixture belongs to no category, so it lives in the last row of the
  // column and its checkbox appears once that row is opened.
  await page
    .getByRole('button', {
      name: 'Show the contents of category Uncategorized',
    })
    .click()
  await page.locator('input[value="limit-fixture"]').check()

  // Every format states what this draft would weigh in it, against its own
  // bound, in the same list the choice is made from.
  const formats = await openFormats(page)
  await expect(
    formats.getByRole('option', { name: /Limited fixture/ }),
  ).toContainText('≈ 2 of 1 rules')
  await expect(
    formats.getByRole('option', { name: /Limited fixture/ }),
  ).toContainText('Cannot hold this route')
  await expect(formats.getByRole('option', { name: /Keenetic/ })).toContainText(
    '≈ 2 of 1,024 rules',
  )

  // Choosing the format that cannot hold it stops the creation and names one
  // that can. The choice stays selectable: the refusal explains, it does not
  // hide it.
  const submit = page.getByRole('button', { name: 'Create and prepare' })
  await page.getByRole('option', { name: /Limited fixture/ }).click()
  await expect(page.locator('.create__forecast')).toHaveText('≈ 2 of 1 rules')
  await expect(submit).toBeDisabled()
  await expect(
    page.getByText('The route does not fit Limited fixture.'),
  ).toBeVisible()
  await expect(page.getByText('sing-box would fit.')).toBeVisible()

  await page.getByRole('button', { name: 'Choose sing-box' }).click()
  await expect(page.locator('#create-target')).toHaveValue('sing-box')
  await expect(submit).toBeEnabled()
  assertProductAlive()
})

/**
 * The composing card reads the list; the one act the route owns is its footer.
 *
 * A switch on every row used to promise what the product cannot do. The
 * per-list override a composition carries replaces the catalog's domain seeds
 * alone, and it accepts domains alone — so switching an address or an observed
 * rule off changed nothing the build would read, and had the forecast refused
 * outright the moment it was sent. This states the result against the running
 * product: the rows carry nothing to press, and the forecast keeps answering
 * while the card is open and membership is used.
 */
test('the composing card carries no row control and never spoils the forecast', async ({
  page,
}) => {
  test.setTimeout(120000)
  // Only an observed list can be weighed, and this fixture is observed from a
  // source of its own, so the test does not depend on what ran before it.
  const observed = await page.request.post(
    `${origin}/v1/services/limit-fixture/refresh`,
    { data: {}, headers: { Origin: origin, 'X-Routevane-Request': '1' } },
  )
  expect(observed.ok()).toBe(true)

  const forecastStatuses: number[] = []
  page.on('response', (response) => {
    if (new URL(response.url()).pathname === forecastPath)
      forecastStatuses.push(response.status())
  })
  const writes: string[] = []
  page.on('request', (request) => {
    const path = new URL(request.url()).pathname
    if (request.method() === 'POST' && path !== forecastPath) writes.push(path)
  })

  await page.goto(`${origin}/lists/new`)
  await page
    .getByRole('button', {
      name: 'Show the contents of category Uncategorized',
    })
    .click()
  await page
    .getByRole('button', { name: 'Open the contents of list Limit fixture' })
    .click()
  const card = page.getByRole('dialog', { exact: true, name: 'Limit fixture' })
  await expect(
    card.getByRole('heading', { name: 'List contents' }),
  ).toBeVisible()

  // The fixture stands for addresses and for no domain at all: exactly the
  // rows the removed switch could never have taken out of a route.
  await expect(cardRow(card, '192.0.2.20')).toContainText('catalog')
  await expect(cardRow(card, '198.51.100.20')).toContainText('catalog')
  await expect(
    card.locator('.service-card__rows input[type="checkbox"]'),
  ).toHaveCount(0)

  // The one control the card offers, used both ways with the card open.
  const add = card.getByRole('button', { name: 'Add to route' })
  const drop = card.getByRole('button', { name: 'Remove from route' })
  await add.click()
  await expect(drop).toBeVisible()
  await drop.click()
  await expect(add).toBeVisible()
  await add.click()
  await expect(drop).toBeVisible()

  // The composer asks what the draft weighs while the card is still open, and
  // is answered rather than refused: nothing the card did put a value into the
  // draft that the endpoint has to reject.
  await expect
    .poll(() => forecastStatuses.length, { timeout: 20000 })
    .toBeGreaterThan(0)
  expect(forecastStatuses.filter((status) => status >= 400)).toEqual([])
  // And the card itself wrote nothing at all while it was open.
  expect(writes).toEqual([])

  await card.getByRole('button', { name: 'Close' }).last().click()
  assertProductAlive()
})

/**
 * A category is the operator's as much as the catalog's (ADR 0028): they can
 * make one, fill it from the whole catalog, and every route naming it follows
 * what it holds. The one thing they cannot do is take it out from under a route
 * still built from it — and the refusal names that route, so the way forward is
 * the one the message states.
 */
test('an operator category carries its lists into a route and is kept while a route names it', async ({
  page,
}) => {
  test.setTimeout(240000)
  // Curating happens in its own section (ADR 0029), so the category exists
  // before the composer is opened at all.
  await page.goto(`${origin}/library`)
  await expect(
    page.getByRole('heading', { level: 1, name: 'Lists' }),
  ).toBeVisible()

  // The library is an operational workspace, not a short card surrounded by
  // unused page. Its rows stay dense while the two panes claim the available
  // viewport height.
  const viewport = page.viewportSize()
  const workspaceBox = await page.locator('.lists__workspace').boundingBox()
  const firstCategoryBox = await page
    .locator('.lists__category')
    .first()
    .boundingBox()
  expect(workspaceBox?.height ?? 0).toBeGreaterThanOrEqual(
    Math.floor((viewport?.height ?? 0) * 0.7),
  )
  expect(
    firstCategoryBox?.height ?? Number.POSITIVE_INFINITY,
  ).toBeLessThanOrEqual(56)

  for (const [footer, label] of [
    ['.lists__collections-footer', 'Custom category'],
    ['.lists__details-footer', 'Custom list'],
  ] as const) {
    const footerBox = await page.locator(footer).boundingBox()
    const buttonBox = await page
      .locator(footer)
      .getByRole('button', { name: label })
      .boundingBox()
    expect(buttonBox?.width ?? 0).toBeGreaterThan(
      (footerBox?.width ?? Number.POSITIVE_INFINITY) * 0.9,
    )
  }

  await page.getByRole('button', { name: 'Custom category' }).click()
  const categoryForm = page.getByRole('dialog', { name: 'New category' })
  // The panel opens with the keyboard in its field, so typing starts there
  // and is not undone by a focus that arrives late.
  await expect(categoryForm.getByLabel('Name')).toBeFocused()
  await categoryForm.getByLabel('Name').fill('Домашние')
  await categoryForm
    .getByRole('button', { exact: true, name: 'Create' })
    .click()
  await expect(categoryForm).toBeHidden()

  // The category just made is the one on screen, because it was made to be
  // filled.
  const details = page.locator('.lists__details')
  await expect(details.getByRole('heading', { name: 'Домашние' })).toBeVisible()

  await page
    .getByRole('button', { name: 'Actions for category Домашние' })
    .click()
  await page
    .locator('.rv-menu__panel:visible')
    .getByRole('menuitem', { name: 'Add a list' })
    .click()
  const addList = page.getByRole('dialog', { name: 'Add a list' })
  await expect(addList.getByRole('combobox')).toBeFocused()
  await addList.getByRole('combobox').fill('Discord')
  await page.getByRole('option', { name: 'Discord' }).click()
  await addList.getByRole('button', { exact: true, name: 'Add' }).click()
  await expect(addList).toBeHidden()
  await expect(details).toContainText('Discord')

  await page.goto(`${origin}/lists/new`)
  await expect(
    page.getByRole('heading', { level: 1, name: 'New route' }),
  ).toBeVisible()

  // Selecting a category selects the reference, so the route follows it rather
  // than freezing today's members.
  await page
    .locator('.picker__group')
    .filter({ hasText: 'Домашние' })
    .getByRole('checkbox')
    .check()
  await page
    .getByRole('button', { name: 'Show the contents of category Video' })
    .click()
  await page.locator('input[value="youtube"]').check()
  await expect(page.locator('#create-name')).toHaveValue('Домашние, YouTube')

  await chooseFormat(page, /Keenetic/)
  await page.getByRole('button', { name: 'Create and prepare' }).click()
  await page.waitForURL(
    new RegExp(`^${escapeRegExp(origin)}/lists/[a-f0-9]{32}.*$`),
  )
  await expect(
    page.getByRole('heading', { level: 1, name: 'Домашние, YouTube' }),
  ).toBeVisible()

  // The route states the category it follows, and what that category holds.
  await page
    .getByRole('tablist', { name: 'Route sections' })
    .getByRole('tab', { name: 'Contents' })
    .click()
  const carried = page.locator('.editor__row').filter({ hasText: 'Домашние' })
  await expect(carried).toHaveCount(1)
  await expect(carried).toContainText('1 list')
  await expect(
    page.locator('.priority-list__item').filter({ hasText: 'Discord' }),
  ).toHaveCount(1)

  const routeURL = page.url()

  // A route is built from this category, so the category stays where it is and
  // the refusal names the route standing in the way — beside the act it
  // refused, which is the one place the operator is standing.
  await page.goto(`${origin}/library`)
  await openCategoryActions(page, 'Домашние')
  await page
    .locator('.rv-menu__panel:visible')
    .getByRole('menuitem', { name: 'Delete the category' })
    .click()
  const removal = page.getByRole('dialog', { name: 'Delete the category' })
  await removal.getByRole('button', { exact: true, name: 'Delete' }).click()
  await expect(removal.getByText('The category was not deleted')).toBeVisible()
  await expect(
    removal.getByText(
      'It is part of these routes: Домашние, YouTube. Remove it there and try again.',
    ),
  ).toBeVisible()
  await removal.getByRole('button', { name: 'Cancel' }).click()
  await expect(page.locator('.lists__groups')).toContainText('Домашние')

  // Doing what the refusal asks is what makes the deletion possible, and the
  // route keeps everything it did not follow through the category.
  await page.goto(routeURL)
  await page
    .getByRole('tablist', { name: 'Route sections' })
    .getByRole('tab', { name: 'Contents' })
    .click()
  await page
    .getByRole('button', { name: 'Remove Домашние from the route' })
    .click()
  await page.getByRole('button', { name: 'Save and rebuild' }).click()
  await expect(
    page.locator('.editor__row').filter({ hasText: 'Домашние' }),
  ).toHaveCount(0)
  await expect(
    page.locator('.priority-list__item').filter({ hasText: 'YouTube' }),
  ).toHaveCount(1)

  // Nothing names it now, so it goes — and the list it held keeps the category
  // it already belonged to.
  await page.goto(`${origin}/library`)
  await openCategoryActions(page, 'Домашние')
  await page
    .locator('.rv-menu__panel:visible')
    .getByRole('menuitem', { name: 'Delete the category' })
    .click()
  await removal.getByRole('button', { exact: true, name: 'Delete' }).click()
  await expect(removal).toBeHidden()
  await expect(page.locator('.lists__groups')).not.toContainText('Домашние')
  await openLibraryCategory(page, 'Communication')
  await expect(page.locator('.lists__pane-body')).toContainText('Discord')
  assertProductAlive()
})

/**
 * The other answer to the one question deleting a category asks — and the
 * reason the question exists at all: a category made to hold one draft list
 * takes that list with it. Composing, meanwhile, offers none of this.
 */
test('the library deletes a category with its lists, and composing offers none of it', async ({
  page,
}) => {
  test.setTimeout(180000)
  await page.goto(`${origin}/library`)
  await expect(
    page.getByRole('heading', { level: 1, name: 'Lists' }),
  ).toBeVisible()

  await page.getByRole('button', { name: 'Custom category' }).click()
  const categoryForm = page.getByRole('dialog', { name: 'New category' })
  await categoryForm.getByLabel('Name').fill('Черновик')
  await categoryForm
    .getByRole('button', { exact: true, name: 'Create' })
    .click()
  await expect(categoryForm).toBeHidden()

  // A list made while a category is open joins that category.
  await page.getByRole('button', { name: 'Custom list' }).click()
  const newList = page.getByRole('dialog', { name: 'New list' })
  await newList.getByLabel('Name').fill('Draft fixture')
  await newList.getByLabel('Domains').fill('draft.example')
  await newList.getByRole('button', { name: 'Create and add' }).click()
  await expect(newList).toBeHidden()
  await expect(page.locator('.lists__details')).toContainText('Draft fixture')

  await openCategoryActions(page, 'Черновик')
  await page
    .locator('.rv-menu__panel:visible')
    .getByRole('menuitem', { name: 'Delete the category' })
    .click()
  const removal = page.getByRole('dialog', { name: 'Delete the category' })
  await removal.getByText('Delete them with it').click()
  await removal.getByRole('button', { exact: true, name: 'Delete' }).click()
  await expect(removal).toBeHidden()
  await expect(page.locator('.lists__groups')).not.toContainText('Черновик')
  await openLibraryCategory(page, 'Uncategorized')
  await expect(page.locator('.lists__pane-body')).not.toContainText(
    'Draft fixture',
  )

  // Selection is a checkbox and nothing else: the composer has no category
  // menu, no way to make or unmake anything, and no bin on a list row.
  await page.goto(`${origin}/lists/new`)
  await expect(page.locator('.picker__workspace')).toBeVisible()
  for (const gone of ['Custom category', 'Custom list'])
    await expect(page.getByRole('button', { name: gone })).toHaveCount(0)
  await expect(
    page.locator('.picker__workspace .rv-menu__trigger'),
  ).toHaveCount(0)
  await expect(page.locator('.picker__service-remove')).toHaveCount(0)
  assertProductAlive()
})

/**
 * The sheet's height belongs to the table in it. This is the band of nothing
 * that used to sit between the last row and the footer: the contents were
 * capped at the composer's height while the sheet itself was the viewport.
 */
test('the list card fills the sheet rather than leaving a band above its footer', async ({
  page,
}) => {
  test.setTimeout(180000)
  await page.setViewportSize({ height: 900, width: 1280 })

  // A list long enough to fill a tall sheet, made and removed by this test so
  // no shipped list is edited to prove a geometry.
  await page.goto(`${origin}/library`)
  await openLibraryCategory(page, 'Uncategorized')
  await page.getByRole('button', { name: 'Custom list' }).click()
  const newList = page.getByRole('dialog', { name: 'New list' })
  await newList.getByLabel('Name').fill('Tall fixture')
  await newList
    .getByLabel('Domains')
    .fill(
      Array.from({ length: 40 }, (_, index) => `row${index}.example`).join(
        '\n',
      ),
    )
  await newList.getByRole('button', { name: 'Create and add' }).click()
  await expect(newList).toBeHidden()

  await page.goto(`${origin}/lists/new`)
  await page
    .getByRole('searchbox', { name: 'Find a list' })
    .fill('Tall fixture')
  await page
    .getByRole('button', { name: 'Open the contents of list Tall fixture' })
    .click()
  const card = page.getByRole('dialog', { exact: true, name: 'Tall fixture' })
  await expect(
    card.getByRole('heading', { name: 'List contents' }),
  ).toBeVisible()

  // The table scrolls inside itself, and the sheet behind it does not grow.
  const rows = card.locator('.service-card__rows')
  expect(
    await rows.evaluate(
      (element) => element.scrollHeight - element.clientHeight,
    ),
  ).toBeGreaterThan(0)
  await rows.evaluate((element) => {
    element.scrollTop = element.scrollHeight
  })
  const rowBox = await rows.locator('li').last().boundingBox()
  const footerBox = await card.locator('.rv-dialog__footer').boundingBox()
  expect(rowBox).not.toBeNull()
  expect(footerBox).not.toBeNull()
  const band = (footerBox?.y ?? 0) - ((rowBox?.y ?? 0) + (rowBox?.height ?? 0))
  expect(band, 'empty band between the last row and the footer').toBeLessThan(
    48,
  )

  await card.getByRole('button', { name: 'Close' }).last().click()
  await page.goto(`${origin}/library`)
  await openLibraryCategory(page, 'Uncategorized')
  await page
    .getByRole('button', { name: 'Actions for list Tall fixture' })
    .click()
  await page
    .locator('.rv-menu__panel:visible')
    .getByRole('menuitem', { name: 'Delete the list' })
    .click()
  const removal = page.getByRole('dialog', { name: 'Delete the list' })
  await removal.getByRole('button', { exact: true, name: 'Delete' }).click()
  await expect(removal).toBeHidden()
  await expect(page.locator('.lists__pane-body')).not.toContainText(
    'Tall fixture',
  )
  assertProductAlive()
})

/**
 * Every overlay on this surface is portalled out of the component that owns it,
 * and a scoped rule that stops matching there takes the panel's ground and edge
 * with it. Nothing else here would notice: an unpainted panel still has a box,
 * still carries its text, and still passes the axe sweep. No unit test can see
 * it either, because jsdom applies no styles at all.
 */
test('every portalled overlay arrives with its own ground and edge', async ({
  page,
}) => {
  test.setTimeout(120000)
  const created = await page.request.post(`${origin}/v1/lists`, {
    data: {
      categories: [],
      exclusions: [],
      name: 'Overlay ground',
      services: ['youtube'],
    },
    headers: { Origin: origin, 'X-Routevane-Request': '1' },
  })
  expect(created.ok()).toBe(true)

  await page.goto(`${origin}/`)
  await page
    .getByRole('button', { name: 'Actions for route Overlay ground' })
    .click()
  await assertPainted(page.locator('.rv-menu__panel:visible'), 'menu')
  await page.keyboard.press('Escape')

  await page.goto(`${origin}/connections`)
  await page.locator('#device-target').click()
  await assertPainted(page.locator('.rv-select__panel'), 'select')
  await page.keyboard.press('Escape')

  await page.goto(`${origin}/lists/new`)
  await openFormats(page)
  await assertPainted(page.locator('.rv-combobox__panel'), 'combobox')
  await page.keyboard.press('Escape')

  await page
    .getByRole('button', { name: 'Open the contents of list Discord' })
    .click()
  const card = page.getByRole('dialog', { exact: true, name: 'Discord' })
  await card.locator('.rv-infotip__trigger').first().click()
  await assertPainted(page.locator('.rv-infotip__panel'), 'informer')
  assertProductAlive()
})

test('the connection combobox drops its highlight when closed and reopens by keyboard', async ({
  page,
}) => {
  await page.goto(`${origin}/lists/new`)
  const field = page.locator('#create-target')
  await expect(field).toBeVisible()

  await field.press('ArrowDown')
  await expect(field).toHaveAttribute('aria-expanded', 'true')
  expect(
    await field.evaluate((input) => {
      const id = input.getAttribute('aria-activedescendant')
      return id !== null && document.getElementById(id) !== null
    }),
  ).toBe(true)

  await field.press('Escape')
  await expect(field).toHaveAttribute('aria-expanded', 'false')
  await expect(field).not.toHaveAttribute('aria-activedescendant', /.+/)

  await field.press('ArrowDown')
  await expect(field).toHaveAttribute('aria-expanded', 'true')
  expect(
    await field.evaluate((input) => {
      const id = input.getAttribute('aria-activedescendant')
      return id !== null && document.getElementById(id) !== null
    }),
  ).toBe(true)
  await field.press('Enter')
  await expect(field).not.toHaveValue('')
  await expect(field).not.toHaveAttribute('aria-activedescendant', /.+/)

  expect(await audit(page, 'combobox keyboard cycle')).toEqual([])
  assertProductAlive()
})

test('hiding a format removes it from the connection picker and says where it went', async ({
  page,
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
  await expect(page.locator('.targets__table')).toBeHidden()
  await page.locator('.rv-disclosure__summary').click()
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
    .getByRole('link', { name: 'Routes' })
    .click()
  await page
    .locator('.library__header')
    .getByRole('link', { name: 'Build a route' })
    .click()
  await page.waitForURL(`${origin}/lists/new`)
  await page.getByRole('searchbox', { name: 'Find a list' }).fill('discord')
  await page.locator('input[value="discord"]').check()
  const offered = await openFormats(page)
  await expect(offered.getByRole('option', { name: /Keenetic/ })).toHaveCount(0)
  await expect(offered.getByRole('option', { name: /sing-box/ })).toHaveCount(1)
  await page.getByRole('option', { name: /sing-box/ }).click()
  await page.getByRole('button', { name: 'Create and prepare' }).click()
  await page.waitForURL(
    new RegExp(`^${escapeRegExp(origin)}/lists/[a-f0-9]{32}.*$`),
  )
  await expect(
    page.getByRole('heading', { level: 1, name: 'Discord' }),
  ).toBeVisible()

  await expect(
    page.getByRole('heading', { name: 'Subscription link · sing-box' }),
  ).toBeVisible()
  await page.locator('#outputs-target').click()
  const outputFormats = page.getByRole('listbox')
  await expect(
    outputFormats.getByRole('option', { name: /Keenetic/ }),
  ).toHaveCount(0)
  await expect(
    outputFormats.getByRole('option', { name: /sing-box/ }),
  ).toHaveCount(0)
  expect(
    await outputFormats
      .locator('.rv-select__group-label')
      .evaluateAll((nodes) => nodes.map((node) => node.textContent?.trim())),
  ).toEqual(['Applications'])
  await page.keyboard.press('Escape')

  // Unhiding brings it back, in both places it is offered.
  await page
    .getByRole('navigation', { name: 'Sections' })
    .getByRole('link', { name: 'Connections' })
    .click()
  await page.locator('.rv-disclosure__summary').click()
  await toggle.check()
  expect(
    await page.evaluate(() => localStorage.getItem('rv.hiddenTargets')),
  ).toBe('[]')
  assertProductAlive()
})

test('an archived route leaves the shelf, keeps its file, and comes back whole', async ({
  page,
}) => {
  test.setTimeout(180000)
  const pageErrors: string[] = []
  page.on('pageerror', (error) => pageErrors.push(error.message))

  const { listId } = await buildList(page)
  // Earlier walkthroughs left their own lists on the shelf, so every row here
  // is addressed by this route's identity rather than by its title.
  const shelfRow = page
    .getByRole('row')
    .filter({ has: page.locator(`a[href="/lists/${listId}"]`) })
  await page
    .getByRole('tablist', { name: 'Route sections' })
    .getByRole('tab', { name: 'Connection' })
    .click()
  const fileHref = await page
    .getByRole('link', { name: 'Download the file for Keenetic' })
    .first()
    .getAttribute('href')
  expect(fileHref).toMatch(/^\/v1\/artifacts\/[a-f0-9]{32}$/)

  // Archiving is one request and does not rebuild: the file subscribers are
  // receiving must not change because the route was shelved.
  const mutations: string[] = []
  page.on('request', (request) => {
    const path = new URL(request.url()).pathname
    if (request.method() === 'POST' && statesFlow(path)) {
      mutations.push(path)
    }
  })
  await page
    .locator('main')
    .getByRole('button', {
      name: 'Actions for route Discord, YouTube',
      exact: true,
    })
    .click()
  await page
    .locator('.rv-menu__panel:visible')
    .getByRole('menuitem', { name: 'Archive', exact: true })
    .click()
  await expect(page.getByText('This route is archived')).toBeVisible()
  expect(mutations).toEqual([`/v1/lists/${listId}/archive`])

  // What stops is change. The controls that would edit, rebuild, reschedule or
  // bind a new format are gone rather than disabled, and the file is still
  // offered from the same page.
  await expect(page.locator('.rv-status__label')).toHaveText('Archived')
  await expect(page.locator('#editor-name')).toHaveCount(0)
  await expect(page.locator('#list-schedule-select')).toHaveCount(0)
  await expect(
    page.getByRole('link', { name: 'Download the file for Keenetic' }).first(),
  ).toHaveAttribute('href', fileHref ?? '')
  await page
    .getByRole('tablist', { name: 'Route sections' })
    .getByRole('tab', { name: 'Connection' })
    .click()
  await expect(page.locator('#outputs-target')).toHaveCount(0)
  await expect(
    page.locator('main').getByRole('row').filter({ hasText: 'Keenetic' }),
  ).toBeVisible()

  // The archived route has left the shelf without leaving the library: the row
  // is behind one disclosure, still names when it was archived, and still
  // offers its published file.
  await page.getByRole('link', { name: 'Routes' }).first().click()
  await page.waitForURL(`${origin}/`)
  await expect(shelfRow).toHaveCount(0)
  const archive = page.locator('.library__archive')
  await expect(archive).toContainText('Archive')
  await archive.locator('.rv-disclosure__summary').click()
  const archivedRow = archive
    .locator('.library__archive-row')
    .filter({ has: page.locator(`a[href="/lists/${listId}"]`) })
  await expect(archivedRow).toHaveCount(1)
  await expect(archivedRow).toContainText('Archived since')
  await archivedRow
    .getByRole('button', {
      name: 'Actions for route Discord, YouTube',
    })
    .click()
  await page
    .locator('.rv-menu__panel:visible')
    .getByRole('menuitem', { name: 'Download' })
    .click()
  await expect(
    page
      .locator('.rv-menu__panel:visible')
      .last()
      .getByRole('menuitem', { name: 'BAT · routes' }),
  ).toBeVisible()
  await page.keyboard.press('Escape')

  // The archive is part of the screen, so it is held to the same gates.
  expect(await audit(page, 'library-archive')).toEqual([])
  await assertNoOverflow(page, 'library-archive')

  // Restoring puts the route back on the shelf with everything it had, and
  // publishes nothing by itself.
  mutations.length = 0
  await archivedRow
    .getByRole('button', { name: 'Actions for route Discord, YouTube' })
    .click()
  await page
    .locator('.rv-menu__panel:visible')
    .getByRole('menuitem', { name: 'Restore' })
    .click()
  await expect(shelfRow).toHaveCount(1)
  expect(mutations).toEqual([`/v1/lists/${listId}/restore`])
  await expect(page.locator('.library__archive')).toHaveCount(0)

  expect(pageErrors).toEqual([])
  assertProductAlive()
})

test('the surface speaks the language of the operator and declares which one', async ({
  page,
}) => {
  test.setTimeout(120000)
  const pageErrors: string[] = []
  page.on('pageerror', (error) => pageErrors.push(error.message))

  await page.goto(`${origin}/settings`)
  await expect(
    page.getByRole('heading', { level: 1, name: 'Settings' }),
  ).toBeVisible()
  const declared: Record<string, string> = {}
  declared.initial = await documentLanguage(page)

  await segment(page, 'Русский').click()
  await expect(
    page.getByRole('heading', { level: 1, name: 'Настройки' }),
  ).toBeVisible()
  const nav = page.getByRole('navigation', { name: 'Разделы' })
  await expect(nav).toBeVisible()
  await expect(nav.getByRole('link', { name: 'Подключения' })).toBeVisible()
  declared.russian = await documentLanguage(page)

  await nav.getByRole('link', { name: 'Маршруты' }).click()
  await expect(
    page.getByRole('heading', { level: 1, name: 'Маршруты' }),
  ).toBeVisible()
  await expect(
    page
      .locator('.library__header')
      .getByRole('link', { name: 'Собрать маршрут' }),
  ).toBeVisible()

  await nav.getByRole('link', { name: 'Настройки' }).click()
  await segment(page, 'English').click()
  await expect(
    page.getByRole('heading', { level: 1, name: 'Settings' }),
  ).toBeVisible()
  declared.english = await documentLanguage(page)

  // The words and the declared language are one statement: a screen reader
  // told 'en' pronounces Russian copy as English. Asserted last so a wrong
  // `lang` never hides a wrong translation.
  expect(declared).toEqual({ english: 'en', initial: 'en', russian: 'ru' })
  expect(pageErrors).toEqual([])
  assertProductAlive()
})

test('the device form asks only for fields the selected target needs', async ({
  page,
}) => {
  await page.goto(`${origin}/connections`)
  await expect(
    page.getByRole('heading', { level: 1, name: 'Connections' }),
  ).toBeVisible()
  const target = page.locator('#device-target')
  await expect(target).toBeEnabled()
  await expect(page.locator('#device-name')).toHaveCount(0)
  await expect(page.locator('#device-address')).toHaveCount(0)
  await expect(page.locator('#device-account')).toHaveCount(0)
  await expect(page.locator('#device-interface')).toHaveCount(0)

  await target.click()
  await page.getByRole('option', { name: 'Keenetic' }).click()
  await expect(page.locator('label[for="device-address"]')).toHaveText(
    'Device address',
  )
  await expect(page.locator('#device-address')).toHaveAttribute(
    'placeholder',
    'http://192.168.1.1',
  )
  await expect(page.locator('#device-account')).toBeVisible()
  await expect(page.locator('label[for="device-interface"]')).toHaveText(
    'Interface for routes',
  )
  await expect(page.locator('#device-interface')).toBeVisible()
  await expect(
    page.getByText(
      'For example, Wireguard0 — the Keenetic connection/interface ID.',
    ),
  ).toBeVisible()
  await page.locator('#device-address').fill('http://192.168.1.1')
  await page.locator('#device-account').fill('admin')

  await target.click()
  await page.getByRole('option', { name: 'sing-box' }).click()
  await expect(page.locator('label[for="device-address"]')).toHaveText(
    'Path to the sing-box configuration',
  )
  await expect(page.locator('#device-address')).toHaveAttribute(
    'placeholder',
    'file:///C:/sing-box/config.json',
  )
  await expect(page.locator('#device-address')).toHaveValue('')
  await expect(page.locator('#device-account')).toHaveCount(0)
  await expect(page.locator('#device-interface')).toHaveCount(0)
})

test('an output can be explicitly bound to and detached from a compatible device', async ({
  page,
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
  const { outputId } = await buildList(page)

  await page
    .getByRole('tablist', { name: 'Route sections' })
    .getByRole('tab', { name: 'Connection' })
    .click()
  const binding = page.locator(`#output-device-${outputId}`)
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

test('the server refresh setting cannot race while a write is pending', async ({
  page,
}) => {
  let release!: () => void
  const held = new Promise<void>((resolve) => {
    release = resolve
  })
  let writes = 0
  await page.route('**/v1/settings/update', async (route) => {
    if (route.request().method() !== 'POST') {
      await route.continue()
      return
    }
    writes++
    await held
    await route.continue()
  })

  await page.goto(`${origin}/settings`)
  await expect(segment(page, 'Daily')).toBeVisible()
  await segment(page, 'Daily').click()
  await expect(page.getByText('Saving the rule…')).toBeVisible()
  const refreshRadios = page.locator('input[name="rv-refresh-interval"]')
  await expect(refreshRadios).toHaveCount(3)
  for (let index = 0; index < 3; index++)
    await expect(refreshRadios.nth(index)).toBeDisabled()
  await expect.poll(() => writes).toBe(1)

  release()
  await expect(page.getByText('Saving the rule…')).toHaveCount(0)
  await page.unroute('**/v1/settings/update')
  await segment(page, 'Off').click()
  await expect(segment(page, 'Off').locator('input')).toBeChecked()
})

for (const language of ['en', 'ru'] as const) {
  test.describe(`failure recovery ${language}`, () => {
    test.use({ locale: language === 'ru' ? 'ru-RU' : 'en-US' })
    const copy = (key: string): string => {
      const value = dictionaries[language][key]
      if (typeof value !== 'string')
        throw new Error(`Not a string message: ${key}`)
      return value
    }
    test('prerequisite audit: devices remain readable while requirements retry', async ({
      page,
    }) => {
      test.setTimeout(120000)
      const headers = { Origin: origin, 'X-Routevane-Request': '1' }
      const created = await page.request.post(`${origin}/v1/devices`, {
        data: {
          account: 'admin',
          address: 'http://192.168.1.1',
          interface: 'Wireguard0',
          name: 'Requirements retry fixture',
          target_id: 'keenetic',
        },
        headers,
      })
      expect(created.ok()).toBe(true)
      const deviceID = ((await created.json()) as { device: { id: string } })
        .device.id
      let reads = 0
      let failRequirements = true
      await page.route('**/v1/deployments/targets', async (route) => {
        reads += 1
        if (failRequirements) {
          await route.fulfill({
            body: JSON.stringify({ error: 'temporarily unavailable' }),
            contentType: 'application/json',
            status: 503,
          })
          return
        }
        await route.continue()
      })

      try {
        await page.goto(`${origin}/connections`)
        await expect(
          page.getByText(copy('devices.requirements.failed')),
        ).toBeVisible()
        await expect(page.getByText('Requirements retry fixture')).toBeVisible()
        await expect(
          page.getByRole('button', { name: copy('devices.auto.enable') }),
        ).toHaveCount(0)

        expect(
          await auditWidths(page, `${language}-requirements-failed`),
        ).toEqual([])
        const beforeRetry = reads
        failRequirements = false
        await page
          .locator('.devices')
          .getByRole('button', { name: copy('action.retry') })
          .click()
        await expect(page.locator('#device-target')).toBeEnabled()
        await page.locator('#device-target').click()
        await page.getByRole('option', { name: 'Keenetic' }).click()
        await expect(page.locator('#device-account')).toBeVisible()
        await expect(page.locator('#device-interface')).toBeVisible()
        expect(reads).toBe(beforeRetry + 1)
      } finally {
        await page.unroute('**/v1/deployments/targets')
        const forgotten = await page.request.post(
          `${origin}/v1/devices/${deviceID}/forget`,
          { data: {}, headers },
        )
        expect(forgotten.ok()).toBe(true)
      }
    })

    test('prerequisite audit: settings keeps the schedule unknown until retry succeeds', async ({
      page,
    }) => {
      let reads = 0
      await page.route('**/v1/settings', async (route) => {
        if (route.request().method() !== 'GET') {
          await route.continue()
          return
        }
        reads += 1
        if (reads === 1) {
          await route.fulfill({
            body: JSON.stringify({ error: 'temporarily unavailable' }),
            contentType: 'application/json',
            status: 503,
          })
          return
        }
        await route.fulfill({
          body: JSON.stringify({ refresh_interval: 'daily' }),
          contentType: 'application/json',
          status: 200,
        })
      })

      try {
        await page.goto(`${origin}/settings`)
        await expect(
          page.getByText(copy('settings.refresh.read.failed')),
        ).toBeVisible()
        await expect(
          page.locator('input[name="rv-refresh-interval"]').nth(0),
        ).not.toBeChecked()
        await expect(
          page.locator('input[name="rv-locale"]').first(),
        ).toBeEnabled()
        await expect(
          page.locator('input[name="rv-appearance"]').first(),
        ).toBeEnabled()

        expect(await auditWidths(page, `${language}-settings-failed`)).toEqual(
          [],
        )
        await page.getByRole('button', { name: copy('action.retry') }).click()
        await expect(
          page.locator('input[name="rv-refresh-interval"]').nth(1),
        ).toBeChecked()
        expect(reads).toBe(2)
      } finally {
        await page.unroute('**/v1/settings')
      }
    })

    test('library audit: a committed category waits for GET recovery without a second write', async ({
      page,
    }) => {
      test.setTimeout(120000)
      const headers = { 'X-Routevane-Request': '1' }
      const categoryTitle = `Stale browser ${Date.now()}`
      let failCatalogRead = false
      let categoryID = ''
      const categoryWrites: string[] = []

      page.on('request', (request) => {
        if (
          request.method() === 'POST' &&
          new URL(request.url()).pathname === '/v1/categories'
        )
          categoryWrites.push(request.url())
      })
      await page.route('**/v1/services', async (route) => {
        if (route.request().method() === 'GET' && failCatalogRead) {
          failCatalogRead = false
          await route.fulfill({
            status: 503,
            contentType: 'application/json',
            body: JSON.stringify({ error: 'catalog temporarily unavailable' }),
          })
          return
        }
        await route.continue()
      })

      try {
        await page.goto(`${origin}/library`)
        await expect(
          page.getByRole('heading', { level: 1, name: copy('lists.title') }),
        ).toBeVisible()
        await page
          .getByRole('button', { name: copy('lists.addCategory') })
          .click()
        const form = page.getByRole('dialog', {
          name: copy('lists.category.new'),
        })
        await form.getByLabel(copy('lists.category.field')).fill(categoryTitle)

        const created = page.waitForResponse(
          (response) =>
            response.request().method() === 'POST' &&
            new URL(response.url()).pathname === '/v1/categories',
        )
        failCatalogRead = true
        await form
          .getByRole('button', {
            exact: true,
            name: copy('lists.category.create'),
          })
          .click()
        const createdResponse = await created
        expect(createdResponse.status()).toBe(201)
        categoryID = (
          (await createdResponse.json()) as { category: { id: string } }
        ).category.id

        await expect(form).toBeHidden()
        await expect(page.getByRole('status')).toContainText(
          copy('lists.stale'),
        )
        await expect(
          page.locator('.lists__details').getByRole('heading', {
            name: copy('category.communication'),
          }),
        ).toBeVisible()
        expect(categoryWrites).toHaveLength(1)

        expect(await auditWidths(page, `${language}-library-stale`)).toEqual([])
        await page.getByRole('button', { name: copy('lists.refresh') }).click()
        await expect(
          page.locator('.lists__details').getByRole('heading', {
            name: categoryTitle,
          }),
        ).toBeVisible()
        await expect(
          page.getByRole('status').filter({ hasText: copy('lists.stale') }),
        ).toHaveCount(0)
        expect(categoryWrites).toHaveLength(1)
        assertProductAlive()
      } finally {
        failCatalogRead = false
        await page.unroute('**/v1/services')
        if (categoryID !== '') {
          const removed = await page.request.post(
            `${origin}/v1/categories/${categoryID}/remove`,
            { headers, data: { lists: 'detach' } },
          )
          expect(removed.status()).toBe(204)
        }
      }
    })
  })
}

test('the accessibility helper detects undersized adjacent targets and accepts the valid control', async ({
  page,
}) => {
  await page.setContent(`<!doctype html><html lang="en"><head><title>Target size control</title>
    <style>button { width: 12px; height: 12px; padding: 0; margin: 0; border: 0; vertical-align: top; }</style>
    </head><body><main><h1>Target size control</h1><button aria-label="First"></button><button aria-label="Second"></button></main></body></html>`)
  expect(
    (await audit(page, 'invalid-target-control')).some(
      (finding) => finding.rule === 'target-size',
    ),
  ).toBe(true)
  await page.locator('button').evaluateAll((buttons) => {
    for (const button of buttons) {
      button.style.width = '24px'
      button.style.height = '24px'
    }
  })
  expect(await auditWidths(page, 'valid-target-control')).toEqual([])
})

for (const language of ['en', 'ru'] as const) {
  test.describe(`accessibility ${language}`, () => {
    test.use({ locale: language === 'ru' ? 'ru-RU' : 'en-US' })
    test('every section passes the accessibility gates', async ({ page }) => {
      test.setTimeout(300000)
      const copy = (key: string): string => {
        const value = dictionaries[language][key]
        if (typeof value !== 'string')
          throw new Error(`Missing text for ${key}`)
        return value
      }
      // Every screen is audited before anything is asserted, so one screen's
      // regression cannot hide another's.
      const violations: AuditFinding[] = []
      await mkdir(reviewRoot, { recursive: true })

      await page.goto(`${origin}/lists/new`)
      await expect(
        page.getByRole('heading', {
          level: 1,
          name: copy('create.title'),
        }),
      ).toBeVisible()
      await page
        .getByRole('button', {
          name: copy('serviceDetail.open.aria').replace('{service}', 'Discord'),
        })
        .click()
      const serviceDialog = page.getByRole('dialog', { name: 'Discord' })
      await expect(
        serviceDialog.getByRole('heading', {
          name: copy('serviceCard.domains'),
        }),
      ).toBeVisible()
      // The card reads its sources when it opens, so the audit is taken once the
      // observed rows have landed: the busy notice and the full table are both in
      // the same pass.
      await expect(serviceDialog.getByText('dns-client').first()).toBeVisible({
        timeout: 60000,
      })
      violations.push(...(await auditWidths(page, `${language}-service-card`)))
      await serviceDialog
        .getByRole('button', { name: copy('action.close') })
        .last()
        .click()
      await page
        .getByRole('searchbox', { name: copy('create.search') })
        .fill('youtube')
      await expect(page.locator('input[value="youtube"]')).toBeVisible()
      violations.push(...(await auditWidths(page, `${language}-builder`)))
      await page.screenshot({
        path: join(reviewRoot, `${language}-builder.png`),
        fullPage: true,
      })

      await page.locator('input[value="youtube"]').check()
      await chooseFormat(page, /Keenetic/)
      await page.getByRole('button', { name: copy('create.submit') }).click()
      await page.waitForURL(
        new RegExp(`^${escapeRegExp(origin)}/lists/[a-f0-9]{32}.*$`),
      )
      await expect(
        page.getByRole('heading', { level: 1, name: 'YouTube' }),
      ).toBeVisible()

      // The route audit is taken with an output bound and its subscription block
      // showing, so the outputs table and the secret disclosure are covered too.
      await expect(
        page.getByRole('heading', {
          name: copy('list.subscription').replace('{target}', 'Keenetic'),
        }),
      ).toBeVisible()
      const createdRoutePath = new URL(page.url()).pathname
      violations.push(...(await auditWidths(page, `${language}-list`)))
      await page.screenshot({
        path: join(reviewRoot, `${language}-list.png`),
        fullPage: true,
      })

      await page
        .getByRole('link', { name: copy('library.title') })
        .first()
        .click()
      await expect(page.locator(`a[href="${createdRoutePath}"]`)).toBeVisible()
      violations.push(...(await auditWidths(page, `${language}-library`)))
      await page.screenshot({
        path: join(reviewRoot, `${language}-library.png`),
        fullPage: true,
      })

      await page.goto(`${origin}/library`)
      await expect(
        page.getByRole('heading', { level: 1, name: copy('lists.title') }),
      ).toBeVisible()
      // The section is audited with a category open, because the pane is where its
      // rows and their menus live.
      await openLibraryCategory(page, copy('category.communication'))
      violations.push(...(await auditWidths(page, `${language}-lists`)))
      await page.screenshot({
        path: join(reviewRoot, `${language}-lists.png`),
        fullPage: true,
      })

      await page.goto(`${origin}/connections`)
      await expect(
        page.getByRole('heading', {
          level: 1,
          name: copy('connections.title'),
        }),
      ).toBeVisible()
      // The catalog is audited open: a disclosure hides its contents from the
      // sweep exactly as it hides them from the operator.
      await page.locator('.rv-disclosure__summary').click()
      await expect(
        page.getByRole('row').filter({ hasText: 'Keenetic' }),
      ).toBeVisible()
      violations.push(...(await auditWidths(page, `${language}-connections`)))
      await page.screenshot({
        path: join(reviewRoot, `${language}-connections.png`),
        fullPage: true,
      })

      await page.goto(`${origin}/settings`)
      await expect(
        page.getByRole('heading', { level: 1, name: copy('settings.title') }),
      ).toBeVisible()
      violations.push(...(await auditWidths(page, `${language}-settings`)))
      await page.screenshot({
        path: join(reviewRoot, `${language}-settings.png`),
        fullPage: true,
      })

      expect(violations).toEqual([])
      assertProductAlive()
    })
  })
}

// The hidden actions column header once escaped `.library__scroll` and
// stretched the document at 320px; the scroll box is its containing block now,
// and this test keeps it that way.
test('the populated library never scrolls sideways', async ({ page }) => {
  test.setTimeout(120000)
  await buildList(page)
  await page.getByRole('link', { name: 'Routes' }).first().click()
  await expect(
    page.getByRole('link', { exact: true, name: 'Discord, YouTube' }).first(),
  ).toBeVisible()
  await assertNoOverflow(page, 'library')

  // A menu triggered at the bottom-right edge is a viewport overlay. It flips
  // above the trigger, remains wholly visible and does not enlarge the table's
  // own scroll box.
  await page.setViewportSize({ height: 300, width: 320 })
  const bottomTrigger = page
    .getByRole('button', { name: /Actions for route/ })
    .last()
  await bottomTrigger.evaluate((element) =>
    element.scrollIntoView({ block: 'end', inline: 'end' }),
  )
  const triggerBox = await bottomTrigger.boundingBox()
  expect(triggerBox).not.toBeNull()
  const scrollBox = page.locator('.library__scroll')
  const scrollHeight = await scrollBox.evaluate((element) =>
    Math.round(element.scrollHeight),
  )
  await bottomTrigger.click()
  const panel = page.locator('.rv-menu__panel:visible')
  await expect(panel).toBeVisible()
  const panelBox = await panel.boundingBox()
  expect(panelBox).not.toBeNull()
  // The panel is not part of the table it was opened from, which is what keeps
  // it out of that table's scroll box and inside the viewport.
  expect(await panel.evaluate((element) => element.closest('main') === null)) //
    .toBe(true)
  expect(panelBox?.x ?? -1).toBeGreaterThanOrEqual(8)
  expect((panelBox?.x ?? 0) + (panelBox?.width ?? 0)).toBeLessThanOrEqual(312)
  expect(panelBox?.y ?? -1).toBeGreaterThanOrEqual(8)
  expect((panelBox?.y ?? 0) + (panelBox?.height ?? 0)).toBeLessThanOrEqual(292)
  expect(
    await scrollBox.evaluate((element) => Math.round(element.scrollHeight)),
  ).toBe(scrollHeight)
  await page.keyboard.press('Escape')
  await page.setViewportSize({ height: 900, width: 1280 })
})

test('the composition editor keeps its geometry across selections and shell breakpoints', async ({
  page,
}) => {
  await page.goto(`${origin}/lists/new`)
  const workspace = page.locator('.picker__workspace')
  await expect(workspace).toBeVisible()

  for (const width of [768, 769, 1024, 1025, 1440]) {
    await page.setViewportSize({ height: 900, width })
    const columns = await workspace.evaluate((element) =>
      getComputedStyle(element).gridTemplateColumns.split(' '),
    )
    expect(
      columns.length,
      `composition workspace collapsed at ${width}px`,
    ).toBeGreaterThanOrEqual(2)
  }

  const before = await workspace.boundingBox()
  expect(before).not.toBeNull()
  const configure = page.locator('.picker__configure')
  await configure.nth(1).click()
  const after = await workspace.boundingBox()
  expect(after).not.toBeNull()
  expect(after?.x).toBeCloseTo(before?.x ?? 0)
  expect(after?.y).toBeCloseTo(before?.y ?? 0)
  expect(after?.width).toBeCloseTo(before?.width ?? 0)
  expect(after?.height).toBeCloseTo(before?.height ?? 0)

  await page.setViewportSize({ height: 900, width: 320 })
  const mobileColumns = await workspace.evaluate((element) =>
    getComputedStyle(element).gridTemplateColumns.split(' '),
  )
  expect(mobileColumns).toHaveLength(1)
  await assertNoOverflow(page, 'composition-editor')
})

test('the composition editor remains operable with text enlarged to 200%', async ({
  page,
}) => {
  await page.goto(`${origin}/lists/new`)
  await page.evaluate(() => {
    document.documentElement.style.fontSize = '200%'
  })

  await expect(
    page.getByRole('heading', {
      level: 1,
      name: 'New route',
    }),
  ).toBeVisible()
  await expect(page.locator('.picker__workspace')).toBeVisible()
  expect(await fits(page)).toBe(true)

  const search = page.getByRole('searchbox', { name: 'Find a list' })
  await search.fill('Discord')
  await page
    .getByRole('button', {
      name: 'Show the contents of category Communication',
    })
    .click()
  await page.locator('input[value="discord"]').check()
  await expect(page.getByText('1 list in the route')).toBeVisible()
  expect(await fits(page)).toBe(true)
})

/**
 * buildList walks the surface the way an operator does — a route holding
 * Discord and YouTube, published as a Keenetic output — and leaves the page on
 * the published route. It answers with the route and output identities, since
 * every output-scoped mutation and address names them rather than the route
 * alone.
 */
async function buildList(
  page: Page,
): Promise<{ listId: string; outputId: string }> {
  await page.goto(`${origin}/lists/new`)
  await expect(
    page.getByRole('heading', {
      level: 1,
      name: 'New route',
    }),
  ).toBeVisible()
  const search = page.getByRole('searchbox', { name: 'Find a list' })
  await search.fill('discord')
  await page.locator('input[value="discord"]').check()
  await search.fill('youtube')
  await page.locator('input[value="youtube"]').check()
  await chooseFormat(page, /Keenetic/)
  const outputsResponse = page.waitForResponse(
    (candidate) =>
      candidate.request().method() === 'POST' &&
      /\/v1\/lists\/[a-f0-9]{32}\/outputs$/.test(
        new URL(candidate.url()).pathname,
      ),
  )
  await page.getByRole('button', { name: 'Create and prepare' }).click()
  await page.waitForURL(
    new RegExp(`^${escapeRegExp(origin)}/lists/[a-f0-9]{32}.*$`),
  )
  await expect(
    page.getByRole('heading', { level: 1, name: 'Discord, YouTube' }),
  ).toBeVisible()
  const listId = listIDFromURL(page.url())
  const outputPayload = (await (await outputsResponse).json()) as {
    output: { id: string }
  }
  await expect(
    page.getByRole('heading', { name: 'Subscription link · Keenetic' }),
  ).toBeVisible()
  await expect(page.locator('#outputs-target')).toBeEnabled()
  await page
    .getByRole('tablist', { name: 'Route sections' })
    .getByRole('tab', { name: 'Contents' })
    .click()

  return { listId, outputId: outputPayload.output.id }
}

/** One category's pane in «Списки», opened the way the column opens it. */
async function openLibraryCategory(
  page: Page,
  category: string,
): Promise<void> {
  await page
    .locator('.lists__category')
    .filter({ hasText: category })
    .first()
    .click()
  await expect(
    page.locator('.lists__details').getByRole('heading', { name: category }),
  ).toBeVisible()
}

/**
 * The action menu of one category. Every act it offers is global, so it lives
 * in «Списки» and nowhere else (ADR 0029).
 */
async function openCategoryActions(
  page: Page,
  category: string,
): Promise<void> {
  await openLibraryCategory(page, category)
  await page
    .getByRole('button', { name: `Actions for category ${category}` })
    .click()
}

/**
 * Whether a panel was drawn by its own component or merely placed. Both facts
 * come from one read, so a panel that lost its stylesheet fails on the ground
 * it should have had rather than on a coincidence of geometry.
 */
async function assertPainted(panel: Locator, name: string): Promise<void> {
  await expect(panel).toBeVisible()
  const painted = await panel.evaluate((element) => {
    const style = getComputedStyle(element)
    return { edge: style.borderTopWidth, ground: style.backgroundColor }
  })
  expect(painted.ground, `${name} panel has no ground`).not.toBe(
    'rgba(0, 0, 0, 0)',
  )
  expect(painted.edge, `${name} panel has no edge`).not.toBe('0px')
}

// One row of the service card's contents table, addressed by what it names.
function cardRow(card: Locator, value: string): Locator {
  return card.locator('.service-card__rows li').filter({ hasText: value })
}

/**
 * The connection format is one searchable list, so a test chooses it the way an
 * operator does: open the list, take the option that names the format. Every
 * format's projected size is read from the same list, because that is where it
 * is stated.
 */
test('the forecast explains overlaps in create and edit without rewriting the library', async ({
  page,
}) => {
  test.setTimeout(90000)
  const headers = { 'X-Routevane-Request': '1' }
  const ids: string[] = []
  for (const [title, domains, values] of [
    ['Overlap Alpha', ['shared.example', 'alpha.example'], ['192.0.2.0/24']],
    ['Overlap Beta', ['shared.example', 'beta.example'], ['192.0.2.1']],
    ['Overlap Gamma', ['gamma.example'], ['198.51.100.9']],
  ] as const) {
    const created = await page.request.post(`${origin}/v1/services`, {
      headers,
      data: { title, domains },
    })
    expect(created.status()).toBe(201)
    const { service } = (await created.json()) as { service: { id: string } }
    ids.push(service.id)
    if (values.length > 0)
      expect(
        (
          await page.request.post(
            `${origin}/v1/services/${service.id}/domains`,
            { headers, data: { values, verdict: 'include' } },
          )
        ).ok(),
      ).toBe(true)
    expect(
      (
        await page.request.post(`${origin}/v1/services/${service.id}/refresh`, {
          headers,
          data: {},
        })
      ).ok(),
    ).toBe(true)
  }
  const beforeRoutes = await (
    await page.request.get(`${origin}/v1/lists`)
  ).text()
  const beforeContents = await Promise.all(
    ids.map(async (id) =>
      (await page.request.get(`${origin}/v1/services/${id}/contents`)).text(),
    ),
  )
  let inspecting = true
  const writes: string[] = []
  const errors: string[] = []
  page.on('pageerror', (error) => errors.push(error.message))
  page.on('request', (request) => {
    const path = new URL(request.url()).pathname
    if (inspecting && request.method() === 'POST' && path !== forecastPath)
      writes.push(path)
  })
  await page.goto(`${origin}/lists/new`)
  async function select(index: number, title: string, checked: boolean) {
    await page.getByRole('searchbox', { name: 'Find a list' }).fill(title)
    await page.locator(`input[value="${ids[index]}"]`).setChecked(checked)
  }
  await select(0, 'Overlap Alpha', true)
  await select(1, 'Overlap Beta', true)
  await chooseFormat(page, /sing-box/)
  const disclosure = page.locator('.rv-disclosure').filter({
    has: page.getByRole('button', { name: 'How overlaps are resolved' }),
  })
  await expect(disclosure).toBeVisible()
  await expect(
    disclosure.getByText('No manual cleanup is required'),
  ).not.toBeVisible()
  await disclosure
    .getByRole('button', { name: 'How overlaps are resolved' })
    .focus()
  await page.keyboard.press('Enter')
  await expect(disclosure).toContainText('file forecast — 4 entries')
  await expect(disclosure).toContainText(
    'Overlaps were found and will be resolved automatically.',
  )
  await expect(
    disclosure.getByText('No manual cleanup is required'),
  ).toBeVisible()
  await expect(disclosure.locator('.overlaps__item')).toHaveCount(0)
  for (const width of [320, 768, 1024, 1440]) {
    await page.setViewportSize({ width, height: 1000 })
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true)
    const axe = await new AxeBuilder({ page }).analyze()
    expect(
      axe.violations.filter(
        (item) => item.impact === 'serious' || item.impact === 'critical',
      ),
    ).toEqual([])
    await page.screenshot({
      path: join(reviewRoot, `overlaps-create-${width}.png`),
      fullPage: true,
    })
  }
  let fail = false
  let release = () => {}
  let held: Promise<void> | undefined
  await page.route(`**${forecastPath}`, async (route) => {
    if (held !== undefined) await held
    if (fail)
      await route.fulfill({
        status: 503,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'unavailable' }),
      })
    else await route.continue()
  })
  try {
    held = new Promise<void>((resolve) => {
      release = resolve
    })
    await select(2, 'Overlap Gamma', true)
    await expect(disclosure).toContainText('previous result')
    await expect(disclosure).toContainText('file forecast — 4 entries')
    const recalculated = page.waitForResponse(
      (response) => new URL(response.url()).pathname === forecastPath,
    )
    release()
    held = undefined
    const recalculatedResponse = await recalculated
    expect(
      recalculatedResponse.status(),
      await recalculatedResponse.text(),
    ).toBe(200)
    await expect(disclosure).toContainText('file forecast — 6 entries')
    fail = true
    await select(2, 'Overlap Gamma', false)
    await expect(disclosure).toContainText('Overlaps are not known yet')
    await expect(disclosure.locator('.overlaps__item')).toHaveCount(0)
    fail = false
    await disclosure.getByRole('button', { name: 'Retry', exact: true }).click()
    await expect(disclosure).toContainText('file forecast — 4 entries')
    await select(2, 'Overlap Gamma', true)
    await select(1, 'Overlap Beta', false)
    await expect(disclosure).toContainText('No identical rules or containment')
    await select(1, 'Overlap Beta', true)
    await select(2, 'Overlap Gamma', false)
    await expect(disclosure).toContainText('file forecast — 4 entries')
  } finally {
    release()
  }
  expect(writes).toEqual([])
  expect(await (await page.request.get(`${origin}/v1/lists`)).text()).toBe(
    beforeRoutes,
  )
  for (const [index, id] of ids.entries())
    expect(
      await (
        await page.request.get(`${origin}/v1/services/${id}/contents`)
      ).text(),
    ).toBe(beforeContents[index])
  inspecting = false
  const built = page.waitForResponse(
    (response) =>
      response.request().method() === 'POST' &&
      /\/v1\/outputs\/[a-f0-9]{32}\/build$/.test(
        new URL(response.url()).pathname,
      ),
  )
  await page.getByRole('button', { name: 'Create and prepare' }).click()
  expect((await built).ok()).toBe(true)
  await page.getByRole('tab', { name: 'Contents', exact: true }).click()
  const editor = page.locator('.editor')
  const editorDisclosure = editor.locator('.rv-disclosure').filter({
    has: page.getByRole('button', { name: 'How overlaps are resolved' }),
  })
  await editorDisclosure
    .getByRole('button', { name: 'How overlaps are resolved' })
    .click()
  await expect(editorDisclosure).toContainText('file forecast — 4 entries')
  await expect(editorDisclosure).toContainText('No manual cleanup is required')
  await expect(editorDisclosure.locator('.overlaps__item')).toHaveCount(0)
  await expect(
    editor.getByRole('button', { name: 'Save and rebuild', exact: true }),
  ).toBeDisabled()
  expect(errors).toEqual([])
  assertProductAlive()
})

function formatList(page: Page): Locator {
  return page.getByRole('listbox')
}

test('source skips are visible without changing routes, and clear after a clean refresh', async ({
  page,
}) => {
  const headers = { 'X-Routevane-Request': '1' }
  const created = await page.request.post(`${origin}/v1/services`, {
    headers,
    data: { title: 'Source diagnostic fixture', domains: ['notice.example'] },
  })
  expect(created.ok()).toBe(true)
  const { service } = (await created.json()) as { service: { id: string } }
  const before = await (await page.request.get(`${origin}/v1/lists`)).text()
  let skipped = 1
  let failed = false
  await page.route(`**/v1/services/${service.id}/refresh`, async (route) => {
    await route.fulfill({
      status: failed ? 422 : 200,
      contentType: 'application/json',
      body: JSON.stringify(
        failed
          ? { error: 'source unavailable' }
          : { refresh: { skipped_entries: skipped } },
      ),
    })
  })
  try {
    await page.goto(`${origin}/library#list=${service.id}`)
    const card = page.getByRole('dialog', {
      name: 'Source diagnostic fixture',
      exact: true,
    })
    const refresh = card.getByRole('button', { name: 'Refresh from sources' })
    await refresh.focus()
    await page.keyboard.press('Enter')
    const status = card
      .getByRole('status')
      .filter({ hasText: '1 source entry skipped' })
    await expect(status).toBeVisible()
    for (const width of [320, 768, 1024, 1440]) {
      await page.setViewportSize({ width, height: 900 })
      await expect(status).toBeVisible()
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
      ).toBe(true)
      const accessibility = await new AxeBuilder({ page }).analyze()
      expect(
        accessibility.violations.filter(
          (item) => item.impact === 'serious' || item.impact === 'critical',
        ),
      ).toEqual([])
    }
    failed = true
    await refresh.click()
    await expect(card.getByRole('alert')).toContainText(
      dictionaries.en['serviceCard.refresh.failed.generic'] as string,
    )
    await expect(
      card.getByText('notice.example', { exact: true }),
    ).toBeVisible()
    await expect(status).toHaveCount(0)
    failed = false
    skipped = 0
    await refresh.click()
    await expect(card.getByRole('alert')).toHaveCount(0)
    await expect(status).toHaveCount(0)
    expect(await (await page.request.get(`${origin}/v1/lists`)).text()).toBe(
      before,
    )
    await expect(
      card.getByRole('button', { name: 'Close' }).last(),
    ).toBeEnabled()
    await page.keyboard.press('Escape')
    await expect(card).toBeHidden()
  } finally {
    const removed = await page.request.post(
      `${origin}/v1/services/${service.id}/remove`,
      { headers, data: {} },
    )
    expect(removed.status()).toBe(204)
  }
})

for (const language of ['en', 'ru'] as const) {
  test.describe(`card regression ${language}`, () => {
    test.use({ locale: language === 'ru' ? 'ru-RU' : 'en-US' })
    const copy = (key: string): string => {
      const value = dictionaries[language][key]
      if (typeof value !== 'string')
        throw new Error(`Not a string message: ${key}`)
      return value
    }
    test('card audit: filtered rows stay dense in compose and library cards', async ({
      page,
    }) => {
      test.setTimeout(180000)
      const headers = { 'X-Routevane-Request': '1' }
      const title = 'Card geometry fixture'
      const longValue = `long.${Array.from({ length: 18 }, () => 'value').join('.')}.example`
      const domains = [
        'match-one.example',
        'match-two.example',
        longValue,
        ...Array.from({ length: 37 }, (_, index) => `row-${index}.example`),
      ]
      const created = await page.request.post(`${origin}/v1/services`, {
        headers,
        data: { domains, title },
      })
      expect(created.status()).toBe(201)
      const { service } = (await created.json()) as { service: { id: string } }
      const disabled = await page.request.post(
        `${origin}/v1/services/${service.id}/domains`,
        {
          headers,
          data: { values: ['row-36.example'], verdict: 'exclude' },
        },
      )
      expect(disabled.status()).toBe(200)

      try {
        await page.goto(`${origin}/lists/new`)
        await page
          .getByRole('searchbox', { name: copy('create.search') })
          .fill(title)
        await page
          .getByRole('button', {
            name: copy('serviceDetail.open.aria').replace('{service}', title),
          })
          .click()
        const compose = page.getByRole('dialog', { name: title, exact: true })
        await expect(
          compose.getByRole('heading', { name: copy('serviceCard.domains') }),
        ).toBeVisible()
        const rows = compose.locator('.service-card__rows')
        await expect(rows.locator('li')).toHaveCount(domains.length)
        const disabledLibraryRow = rows
          .locator('li')
          .filter({ hasText: 'row-36.example' })
        await expect(disabledLibraryRow).toHaveClass(
          /service-card__row--disabled/,
        )
        await expect(
          disabledLibraryRow.getByText(
            copy('serviceCard.domains.disabledInLibrary'),
          ),
        ).toBeVisible()
        await expect(disabledLibraryRow).toHaveCSS('opacity', '1')
        const previousTheme = await page.evaluate(() =>
          document.documentElement.getAttribute('data-rv-theme'),
        )
        try {
          for (const theme of ['dark', 'light']) {
            await page.evaluate(
              (value) =>
                document.documentElement.setAttribute('data-rv-theme', value),
              theme,
            )
            await page.waitForTimeout(150)
            expect(
              await audit(page, `${language}-disabled-library-row-${theme}`),
            ).toEqual([])
          }
        } finally {
          await page.evaluate((value) => {
            if (value === null)
              document.documentElement.removeAttribute('data-rv-theme')
            else document.documentElement.setAttribute('data-rv-theme', value)
          }, previousTheme)
        }
        await page.evaluate(() => document.fonts.ready.then(() => true))

        // At both the narrow audit width and a wide sheet, filtered rows stay at
        // the table's leading edge. The short row is the reference for the long
        // value, so the assertion follows the natural typography at that width.
        const filter = compose.getByRole('searchbox', {
          name: copy('serviceCard.filter'),
        })
        for (const width of [320, 1919]) {
          await page.setViewportSize({ width, height: 900 })
          await filter.fill('')
          await expect(rows.locator('li')).toHaveCount(domains.length)
          const normalRowBox = await rows
            .locator('li')
            .filter({ hasText: 'match-one.example' })
            .boundingBox()
          expect(normalRowBox).not.toBeNull()
          const referenceHeight = normalRowBox!.height
          await filter.fill('match-one.example')
          await expect(rows.locator('li')).toHaveCount(1)
          const oneRowsBox = await rows.boundingBox()
          const oneRowBox = await rows.locator('li').first().boundingBox()
          expect(oneRowsBox).not.toBeNull()
          expect(oneRowBox).not.toBeNull()
          expect(
            (oneRowBox?.y ?? 0) - (oneRowsBox?.y ?? 0),
          ).toBeLessThanOrEqual(2)
          expect(
            Math.abs(oneRowBox!.height - referenceHeight),
            JSON.stringify({
              width,
              referenceHeight,
              filteredHeight: oneRowBox!.height,
            }),
          ).toBeLessThanOrEqual(1)
          await page.screenshot({
            path: join(reviewRoot, `${language}-card-one-${width}.png`),
          })

          await filter.fill('match-')
          await expect(rows.locator('li')).toHaveCount(2)
          for (const row of await rows.locator('li').all()) {
            expect(
              Math.abs((await row.boundingBox())!.height - referenceHeight),
            ).toBeLessThanOrEqual(1)
          }
          await filter.fill('long')
          await expect(rows.locator('li')).toHaveCount(1)
          const longRowBox = await rows.locator('li').first().boundingBox()
          if (width === 320)
            expect(longRowBox?.height ?? 0).toBeGreaterThan(referenceHeight)

          await filter.fill('no-such-card-entry')
          await expect(rows.locator('li')).toHaveCount(0)
          await expect(
            compose.getByText(copy('serviceCard.filter.empty')),
          ).toBeVisible()
          await filter.fill('')
          await expect(rows.locator('li')).toHaveCount(domains.length)
        }
        await expect(compose.locator('.rv-dialog__footer')).toBeVisible()
        await compose
          .getByRole('button', { name: copy('action.close') })
          .last()
          .click()

        // The same dense table and long-value wrapping apply in the library flow;
        // it has no route footer because it owns the list rather than membership.
        await page.goto(`${origin}/library#list=${service.id}`)
        const library = page.getByRole('dialog', { name: title, exact: true })
        await expect(
          library.getByRole('heading', { name: copy('serviceCard.domains') }),
        ).toBeVisible()
        const libraryFilter = library.getByRole('searchbox', {
          name: copy('serviceCard.filter'),
        })
        const libraryRows = library.locator('.service-card__rows')
        await expect(libraryRows.locator('li')).toHaveCount(domains.length)

        // The action remains recognizable while one separate status line owns
        // the pending message. Every dismissal path stays unavailable until
        // the refresh has a result.
        let releaseRefresh!: () => void
        const refreshHeld = new Promise<void>((resolve) => {
          releaseRefresh = resolve
        })
        let refreshRequests = 0
        const refreshPath = `**/v1/services/${service.id}/refresh`
        await page.route(refreshPath, async (route) => {
          refreshRequests += 1
          await refreshHeld
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({ refresh: { skipped_entries: 0 } }),
          })
        })
        const refresh = library
          .locator('.service-card__heading > .rv-button')
          .first()
        await expect(refresh).toHaveAccessibleName(copy('serviceCard.refresh'))
        const libraryClose = library
          .getByRole('button', { name: copy('action.close') })
          .last()
        try {
          await refresh.click()
          await expect(refresh).not.toHaveAttribute('aria-busy', 'true')
          await expect(refresh).toBeDisabled()
          await expect(refresh.locator('.rv-button__spinner')).toHaveCount(0)
          await expect(
            library.getByText(copy('serviceCard.refresh.busy'), {
              exact: true,
            }),
          ).toHaveCount(1)
          await expect(refresh.locator('.rv-icon')).toHaveCount(1)
          await refresh.click({ force: true })
          await page.evaluate(
            () =>
              new Promise<void>((resolve) =>
                requestAnimationFrame(() => resolve()),
              ),
          )
          expect(refreshRequests).toBe(1)
          await expect(libraryClose).toBeDisabled()
          await page.keyboard.press('Escape')
          await expect(library).toBeVisible()
          await page
            .locator('.rv-dialog__scrim')
            .last()
            .dispatchEvent('pointerdown')
          await expect(library).toBeVisible()
        } finally {
          releaseRefresh()
        }
        await expect(refresh).not.toHaveAttribute('aria-busy', 'true')
        await expect(refresh).toBeEnabled()
        await page.unroute(refreshPath)

        await page.evaluate(() => document.fonts.ready.then(() => true))
        const normalLibraryRow = await libraryRows
          .locator('li')
          .filter({ hasText: 'match-two.example' })
          .boundingBox()
        await libraryFilter.fill('match-two.example')
        await expect(libraryRows.locator('li')).toHaveCount(1)
        const libraryRowsBox = await libraryRows.boundingBox()
        const libraryRowBox = await libraryRows
          .locator('li')
          .first()
          .boundingBox()
        expect(libraryRowsBox).not.toBeNull()
        expect(libraryRowBox).not.toBeNull()
        expect(
          Math.abs(libraryRowBox!.height - normalLibraryRow!.height),
        ).toBeLessThanOrEqual(1)
        expect(
          (libraryRowBox?.y ?? 0) - (libraryRowsBox?.y ?? 0),
        ).toBeLessThanOrEqual(2)
        await expect(library.locator('.rv-dialog__footer')).toHaveCount(0)
        // Text-only resizing is independent of pixel density. Double the
        // computed root font, keeping the viewport fixed, then restore it.
        await page.setViewportSize({ width: 768, height: 900 })
        const originalFontSize = await page.evaluate(() => {
          const root = document.documentElement
          const previous = root.style.fontSize
          root.style.fontSize = `${Number.parseFloat(getComputedStyle(root).fontSize) * 2}px`
          return previous
        })
        try {
          expect(await fits(page)).toBe(true)
          await expect(libraryFilter).toBeVisible()
          await expect(libraryRows.locator('li')).toHaveCount(1)
          const row = libraryRows.locator('li').first()
          expect(
            await libraryRows.evaluate((element) => element.clientHeight),
          ).toBeGreaterThanOrEqual((await row.boundingBox())!.height)
          // A body scroll is the fallback when enlarged controls leave too
          // little room. DOM presence is not enough: reach the row by wheel,
          // then by the normal tab sequence, including the final delete action.
          await page.mouse.move(600, 700)
          await page.mouse.wheel(0, 1200)
          await expect(row).toBeInViewport({ ratio: 1 })
          await libraryFilter.press('Tab')
          await page.keyboard.press('Tab')
          await expect(row.getByRole('checkbox')).toBeFocused()
          await expect(row).toBeInViewport({ ratio: 1 })
          await page.keyboard.press('Tab')
          const remove = library.getByRole('button', {
            name: copy('serviceCard.remove'),
            exact: true,
          })
          await expect(remove).toBeFocused()
          await expect(remove).toBeInViewport({ ratio: 1 })
          await remove.click({ trial: true })
          await page.screenshot({
            path: join(reviewRoot, `${language}-card-text-200.png`),
          })
          expect(await audit(page, `${language}-card-text-200`)).toEqual([])
        } finally {
          await page.evaluate((size) => {
            document.documentElement.style.fontSize = size
          }, originalFontSize)
        }

        // Saving the custom title is the third owner of the same busy
        // invariant (after initial read and refresh). Keep the server write
        // pending and prove close, Escape and scrim cannot abandon it.
        let releaseSave!: () => void
        const saveHeld = new Promise<void>((resolve) => {
          releaseSave = resolve
        })
        const savePath = `**/v1/services/${service.id}/update`
        const renamedTitle = `${title} renamed`
        await page.route(savePath, async (route) => {
          await saveHeld
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({
              service: { id: service.id, title: renamedTitle, domains },
            }),
          })
        })
        const save = library.getByRole('button', {
          name: copy('serviceCard.title.save'),
        })
        try {
          await library.locator('#service-title').fill(renamedTitle)
          await save.click()
          await expect(save).toHaveAttribute('aria-busy', 'true')
          await expect(save.locator('.rv-button__spinner')).toHaveCount(1)
          await expect(libraryClose).toBeDisabled()
          await page.keyboard.press('Escape')
          await expect(page.locator('.rv-dialog--sheet')).toBeVisible()
          await page
            .locator('.rv-dialog__scrim')
            .last()
            .dispatchEvent('pointerdown')
          await expect(page.locator('.rv-dialog--sheet')).toBeVisible()
        } finally {
          releaseSave()
        }
        await expect(save).not.toHaveAttribute('aria-busy', 'true')
        await page.unroute(savePath)
      } finally {
        const removed = await page.request.post(
          `${origin}/v1/services/${service.id}/remove`,
          { headers, data: {} },
        )
        expect(removed.status()).toBe(204)
      }
    })

    test('settings audit: native selection stays confirmed through failed and successful writes', async ({
      page,
    }) => {
      let release!: () => void
      let hold = new Promise<void>((resolve) => {
        release = resolve
      })
      let writes = 0
      await page.route('**/v1/settings', (route) =>
        route.fulfill({ json: { refresh_interval: 'daily' } }),
      )
      await page.route('**/v1/settings/update', async (route) => {
        writes += 1
        expect(route.request().postDataJSON()).toEqual({
          refresh_interval: 'weekly',
        })
        await hold
        await route.fulfill(
          writes === 1
            ? { status: 503, json: { error: 'unavailable' } }
            : { json: { refresh_interval: 'weekly' } },
        )
      })
      await page.goto(`${origin}/settings`)
      const daily = page.locator(
        'input[name="rv-refresh-interval"][value="daily"]',
      )
      const weekly = page.locator(
        'input[name="rv-refresh-interval"][value="weekly"]',
      )
      const weeklyLabel = page.locator('label').filter({ has: weekly })
      await expect(daily).toBeChecked()
      for (const success of [false, true]) {
        await weeklyLabel.click()
        await expect(
          page
            .getByRole('status')
            .filter({ hasText: copy('settings.refresh.saving') }),
        ).toBeVisible()
        await expect(daily).toBeChecked()
        await expect(weekly).not.toBeChecked()
        await expect(daily).toBeDisabled()
        await expect(
          page.locator('.rv-segmented__option--active').filter({ has: daily }),
        ).toBeVisible()
        release()
        await expect(daily).toBeEnabled()
        await expect(daily).toBeChecked({ checked: !success })
        await expect(weekly).toBeChecked({ checked: success })
        if (!success) {
          await expect(
            page
              .getByRole('status')
              .filter({ hasText: copy('settings.refresh.failed') }),
          ).toBeVisible()
          hold = new Promise<void>((resolve) => {
            release = resolve
          })
        }
      }
      expect(writes).toBe(2)
    })

    test('card audit: compose source reads retry in place without a route write', async ({
      page,
    }) => {
      test.setTimeout(60000)
      page.setDefaultTimeout(15000)
      const headers = { 'X-Routevane-Request': '1' }
      const title = 'Card source retry fixture'
      const created = await page.request.post(`${origin}/v1/services`, {
        headers,
        data: { domains: ['retry.example'], title },
      })
      expect(created.status()).toBe(201)
      const { service } = (await created.json()) as { service: { id: string } }
      const source = await page.request.post(
        `${origin}/v1/services/${service.id}/sources`,
        {
          headers,
          data: { format: 'text', url: 'https://feed.example/retry.txt' },
        },
      )
      expect(source.ok()).toBe(true)
      const contentsResponse = await page.request.get(
        `${origin}/v1/services/${service.id}/contents`,
      )
      expect(contentsResponse.ok()).toBe(true)
      const contents = (await contentsResponse.json()) as {
        observed: boolean
        rows: unknown[]
        service_id: string
        sources: { enabled: boolean }[]
      }
      expect(contents.sources.some((entry) => entry.enabled)).toBe(true)

      let refreshCalls = 0
      let contentsCalls = 0
      let releaseInitialContents!: () => void
      const initialContentsHeld = new Promise<void>((resolve) => {
        releaseInitialContents = resolve
      })
      let releaseFirstRefresh!: () => void
      const firstRefreshHeld = new Promise<void>((resolve) => {
        releaseFirstRefresh = resolve
      })
      await page.route(
        `**/v1/services/${service.id}/contents`,
        async (route) => {
          contentsCalls += 1
          if (contentsCalls === 1) await initialContentsHeld
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({
              ...contents,
              observed: refreshCalls > 1,
            }),
          })
        },
      )
      await page.route(
        `**/v1/services/${service.id}/refresh`,
        async (route) => {
          refreshCalls += 1
          if (refreshCalls === 1) {
            await firstRefreshHeld
            await route.fulfill({
              status: 503,
              contentType: 'application/json',
              body: JSON.stringify({ error: 'source unavailable' }),
            })
            return
          }
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({ refresh: { skipped_entries: 0 } }),
          })
        },
      )

      try {
        await page.setViewportSize({ width: 320, height: 900 })
        const beforeRoutes = await (
          await page.request.get(`${origin}/v1/lists`)
        ).text()
        await page.goto(`${origin}/lists/new`)
        await page
          .getByRole('searchbox', { name: copy('create.search') })
          .fill(title)
        await page
          .getByRole('button', {
            name: copy('servicePicker.configure.aria').replace(
              '{category}',
              copy('servicePicker.other'),
            ),
          })
          .click()
        await page
          .getByRole('button', {
            name: copy('serviceDetail.open.aria').replace('{service}', title),
          })
          .click()
        const card = page.getByRole('dialog', { name: title, exact: true })
        const close = card
          .getByRole('button', { name: copy('action.close') })
          .last()
        await expect(
          card.getByText(copy('serviceCard.loading'), { exact: true }),
        ).toBeVisible()
        await expect(close).toBeDisabled()
        await page.keyboard.press('Escape')
        await expect(card).toBeVisible()
        await page
          .locator('.rv-dialog__scrim')
          .last()
          .dispatchEvent('pointerdown')
        await expect(card).toBeVisible()
        releaseInitialContents()
        await expect(
          card.getByRole('heading', { name: copy('serviceCard.domains') }),
        ).toBeVisible()
        await expect(
          card.getByText(copy('serviceCard.observing')),
        ).toBeVisible()
        await expect(close).toBeDisabled()
        await page.keyboard.press('Escape')
        await expect(card).toBeVisible()
        await page
          .locator('.rv-dialog__scrim')
          .last()
          .dispatchEvent('pointerdown')
        await expect(card).toBeVisible()
        const pendingFilterBox = await card
          .getByRole('searchbox', { name: copy('serviceCard.filter') })
          .boundingBox()
        expect(pendingFilterBox).not.toBeNull()
        await page.screenshot({
          path: join(reviewRoot, `${language}-card-pending-320.png`),
        })
        await test.step('audit held source read at every width', async () => {
          expect(await auditWidths(page, `${language}-card-pending`)).toEqual(
            [],
          )
        })
        releaseFirstRefresh()
        await expect(card.getByRole('alert')).toContainText(
          copy('serviceCard.refresh.failed.generic'),
        )
        const failedFilterBox = await card
          .getByRole('searchbox', { name: copy('serviceCard.filter') })
          .boundingBox()
        expect(failedFilterBox).not.toBeNull()
        expect(
          Math.abs((failedFilterBox?.y ?? 0) - (pendingFilterBox?.y ?? 0)),
        ).toBeLessThanOrEqual(1)
        await expect(
          card.getByRole('button', { name: copy('action.retry'), exact: true }),
        ).toBeVisible()
        expect(await auditWidths(page, `${language}-card-failed`)).toEqual([])
        await page.screenshot({
          path: join(reviewRoot, `${language}-card-failed-320.png`),
        })
        const filter = card.getByRole('searchbox', {
          name: copy('serviceCard.filter'),
        })
        await filter.fill('retry.example')
        await expect(card.locator('.service-card__rows li')).toHaveCount(1)
        await card
          .getByRole('button', { name: copy('action.retry'), exact: true })
          .click()
        await expect(card.getByRole('alert')).toHaveCount(0)
        await expect(
          card.getByText(copy('serviceCard.refresh.ready')),
        ).toBeVisible()
        const recoveredFilterBox = await card
          .getByRole('searchbox', { name: copy('serviceCard.filter') })
          .boundingBox()
        expect(recoveredFilterBox).not.toBeNull()
        expect(
          Math.abs((recoveredFilterBox?.y ?? 0) - (pendingFilterBox?.y ?? 0)),
        ).toBeLessThanOrEqual(1)
        await expect(filter).toHaveValue('retry.example')
        await expect(card.locator('.service-card__rows li')).toHaveCount(1)
        expect(refreshCalls).toBe(2)
        expect(
          await (await page.request.get(`${origin}/v1/lists`)).text(),
        ).toBe(beforeRoutes)
      } finally {
        releaseInitialContents()
        releaseFirstRefresh()
        await page.unroute(`**/v1/services/${service.id}/contents`)
        await page.unroute(`**/v1/services/${service.id}/refresh`)
        const removed = await page.request.post(
          `${origin}/v1/services/${service.id}/remove`,
          { headers, data: {} },
        )
        expect(removed.status()).toBe(204)
      }
    })
  })
}

async function openFormats(page: Page): Promise<Locator> {
  await page.locator('.rv-combobox__toggle').click()
  await expect(formatList(page)).toBeVisible()
  return formatList(page)
}

async function chooseFormat(page: Page, name: RegExp): Promise<void> {
  await openFormats(page)
  await page.getByRole('option', { name }).click()
  await expect(page.locator('#create-target')).not.toHaveValue('')
}

/**
 * Whether a mutation is part of a flow these tests state.
 *
 * Two reads are not: the list card observes a list's sources as soon as it
 * opens, and a forecast observes an unread draft's lists once so it can be
 * weighed. Both change what a list knows, neither changes a route, and neither
 * is anything the operator asked for by name.
 */
function statesFlow(path: string): boolean {
  return !path.startsWith('/v1/services/') && path !== forecastPath
}

function listFlow(
  mutations: { body: string | null; path: string }[],
): { body: string | null; path: string }[] {
  return mutations.filter((mutation) => statesFlow(mutation.path))
}

function segment(page: Page, label: string) {
  // The radio itself is visually hidden behind its own label, which is exactly
  // what an operator clicks.
  return page.locator(`label.rv-segmented__option:has-text("${label}")`)
}

function documentLanguage(page: Page): Promise<string> {
  return page.evaluate(() => document.documentElement.lang)
}

type AuditFinding = {
  detail: string
  rule: string
  screen: string
  targets: string[]
}

async function audit(page: Page, screen: string): Promise<AuditFinding[]> {
  const result = await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa'])
    .analyze()
  return result.violations.map((violation) => ({
    detail:
      violation.nodes[0]?.failureSummary?.replaceAll(/\s+/g, ' ').trim() ?? '',
    rule: violation.id,
    screen,
    targets: violation.nodes.flatMap((node) => node.target.map(String)),
  }))
}

async function auditWidths(
  page: Page,
  screen: string,
): Promise<AuditFinding[]> {
  const findings: AuditFinding[] = []
  const previous = page.viewportSize()
  for (const width of [320, 768, 1024, 1440]) {
    await page.setViewportSize({ height: 900, width })
    if (!(await fits(page))) {
      await page.screenshot({
        path: join(reviewRoot, `${screen}-${width}-overflow.png`),
        fullPage: true,
      })
      const bounds = await page.locator('main *').evaluateAll((elements) =>
        elements.flatMap((element) => {
          const rect = element.getBoundingClientRect()
          return rect.right > document.documentElement.clientWidth &&
            rect.width > 0
            ? [
                {
                  tag: element.tagName,
                  className: element.className,
                  left: rect.left,
                  right: rect.right,
                  width: rect.width,
                },
              ]
            : []
        }),
      )
      await test.info().attach(`${screen}-${width}-overflow`, {
        body: JSON.stringify(bounds),
        contentType: 'application/json',
      })
      await writeFile(
        join(reviewRoot, `${screen}-${width}-overflow.json`),
        JSON.stringify(bounds, null, 2),
      )
    }
    expect(await fits(page), `${screen} overflows at ${width}px`).toBe(true)
    findings.push(...(await audit(page, `${screen}-${width}`)))
    const smallControls = await page
      .locator('button:not(:disabled), a.rv-button, .rv-segmented__option')
      .evaluateAll((controls) =>
        controls.flatMap((control) => {
          const bounds = control.getBoundingClientRect()
          const style = getComputedStyle(control)
          if (
            bounds.width === 0 ||
            bounds.height === 0 ||
            style.visibility === 'hidden'
          )
            return []
          return bounds.width < 24 || bounds.height < 24
            ? [
                {
                  label:
                    control.getAttribute('aria-label') ??
                    control.textContent?.trim(),
                  width: bounds.width,
                  height: bounds.height,
                },
              ]
            : []
        }),
      )
    expect(
      smallControls,
      `${screen} actionable targets below 24px at ${width}px`,
    ).toEqual([])
  }
  if (previous !== null) await page.setViewportSize(previous)
  return findings
}

async function assertNoOverflow(page: Page, screen: string): Promise<void> {
  for (const width of [320, 768, 1024, 1440]) {
    await page.setViewportSize({ height: 900, width })
    expect(await fits(page), `${screen} overflows at ${width}px`).toBe(true)
  }
  await page.setViewportSize({ height: 900, width: 1280 })
}

function fits(page: Page): Promise<boolean> {
  return page.evaluate(
    () =>
      document.documentElement.scrollWidth <=
      document.documentElement.clientWidth,
  )
}

function escapeRegExp(value: string): string {
  return value.replaceAll(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

function listIDFromURL(value: string): string {
  return /\/lists\/([a-f0-9]{32})/.exec(new URL(value).pathname)?.[1] ?? ''
}

function assertProductAlive(): void {
  if (managedProduct === undefined)
    throw new Error('Routevane browser server exited early: not started')
  managedProduct.assertAlive()
}

async function waitForAuthenticatedRoot(): Promise<void> {
  const deadline = Date.now() + 10000
  let lastFailure = 'listener did not accept a request'
  while (Date.now() < deadline) {
    assertProductAlive()
    try {
      const response = await fetch(`${origin}/`)
      if (response.status === 200) {
        const digest = response.headers.get('x-routevane-ui-digest')
        if (digest !== expectedUIDigest) {
          throw new Error(
            `listener identity mismatch: got ${digest ?? 'missing'}, want ${expectedUIDigest}`,
          )
        }
        // Nuxt UI adds its isolation class to the application root. The root
        // identity is the id; optional framework-owned attributes are not part
        // of Routevane's server handshake.
        if (!/<div\s+id="__nuxt"(?:\s|>)/.test(await response.text())) {
          throw new Error('listener did not return the generated Routevane UI')
        }
        return
      }
      lastFailure = `root returned ${response.status}`
    } catch (error) {
      if (
        error instanceof Error &&
        error.message.startsWith('listener identity')
      ) {
        throw error
      }
      lastFailure = error instanceof Error ? error.message : String(error)
    }
    await delay(25)
  }
  assertProductAlive()
  throw new Error(
    `Routevane browser server did not become ready: ${lastFailure}`,
  )
}

async function embeddedUIDigest(): Promise<string> {
  const files = await embeddedFiles(embeddedUIRoot)
  const rows: string[] = []
  for (const relativePath of files.sort()) {
    if (relativePath === 'routevane-ui.marker') continue
    const bytes = await readFile(join(embeddedUIRoot, relativePath))
    rows.push(`${sha256(bytes)}  ${relativePath}\n`)
  }
  return `sha256-${sha256(rows.join(''))}`
}

async function embeddedFiles(root: string, prefix = ''): Promise<string[]> {
  const files: string[] = []
  for (const entry of await readdir(join(root, prefix), {
    withFileTypes: true,
  })) {
    const relativePath = prefix === '' ? entry.name : `${prefix}/${entry.name}`
    if (entry.isDirectory()) {
      files.push(...(await embeddedFiles(root, relativePath)))
    } else if (entry.isFile()) {
      files.push(relativePath)
    } else {
      throw new Error(`embedded UI has a non-regular entry: ${relativePath}`)
    }
  }
  return files
}

function sha256(value: string | Uint8Array): string {
  return createHash('sha256').update(value).digest('hex')
}
