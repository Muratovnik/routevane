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
import {
  bodyRows,
  cardContents,
  cardRow,
  cardRows,
  categoryCell,
  dialogScrims,
  dragGhost,
  escapeRegExp,
  forecastStatus,
  infoPanel,
  libraryCategoryCell,
  libraryRow,
  libraryRowName,
  linkTo,
  listMembership,
  listRow,
  menuPanel,
  overlapsCell,
  pressableTargets,
  priorityHandle,
  profileOutputsCell,
  rowCellWidths,
  selectedListRows,
  titleField,
  type Scope,
} from './support/queries'

// The locale suites read their expected text from the shipped dictionary, so a
// message the product renames fails the test instead of passing silently.
const copyFor =
  (language: keyof typeof dictionaries): ((key: string) => string) =>
  (key) => {
    const value = dictionaries[language][key]
    if (typeof value !== 'string')
      throw new Error(`Not a string message: ${key}`)
    return value
  }

// The walkthroughs below run in English and name what they expect inline. The
// shared queries take a dictionary so the localized suites can hand them their
// own words, and default to this one so an English call site stays a call with
// one argument.
const englishCopy = copyFor('en')

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
  process.platform === 'win32' ? 'routevane.exe' : 'routevane',
)

let origin = ''
let port = 0
let managedProduct: SpawnedProduct | undefined
let expectedUIDigest = ''

// A read that stores nothing: the composer and the profile editor ask what a
// draft would weigh, and no flow these tests state is made of that question.
const FORECAST_PATH = '/v1/profiles/preview'

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

test('the library starts empty and shelves the profile the composer creates and publishes', async ({
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
    // lists on a fresh install — is reported by the browser itself. The
    // surface reads any refusal as "no forecast": it says nothing and blocks
    // nothing, which is what the rest of this walkthrough proves.
    if (message.location().url.includes(FORECAST_PATH)) return
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
  // profile does before asking a first-time operator to create one.
  await expect(
    page.getByRole('heading', { level: 1, name: 'Profiles' }),
  ).toBeVisible()
  await expect(page.getByText('No profiles yet')).toBeVisible()
  await expect(
    page.getByText(
      'Choose lists and a device; Routevane will prepare the rules and show the available connection methods.',
    ),
  ).toBeVisible()

  // The shelf's own header offers the action, above the empty-state copy that
  // repeats it, so the first of the two is the one on the header.
  await page.getByRole('link', { name: 'Build a profile' }).first().click()
  await page.waitForURL(`${origin}/profiles/new`)
  await expect(
    page.getByRole('heading', { level: 1, name: 'New profile' }),
  ).toBeVisible()
  // Nothing can be prepared before both the contents and their first
  // connection are chosen, so the primary action cannot create a
  // half-configured result.
  const submit = page.getByRole('button', { name: 'Create and prepare' })
  await expect(submit).toBeDisabled()

  // The list card is one table: every destination the list stands for with its
  // origin, read here rather than edited. Adding to the profile happens from the
  // same card, before or after the checkbox in the column behind it.
  await page
    .getByRole('button', {
      name: 'Open the contents of list Discord',
    })
    .click()
  const listCard = page.getByRole('dialog', {
    exact: true,
    name: 'Discord',
  })
  await expect(
    listCard.getByRole('heading', { name: 'List contents' }),
  ).toBeVisible()
  await expect(cardRows(listCard).first()).toBeVisible()
  await expect(listCard.getByText('catalog').first()).toBeVisible()
  // The catalog seeds an address as well as a domain. The value says what it
  // is; the caption says only where it came from.
  const seededAddress = cardRow(listCard, '192.0.2.10')
  await expect(seededAddress).toContainText('catalog')
  await expect(seededAddress).not.toContainText('IP address')

  // The card opens on the composition. Which feeds a list reads is the
  // library's subject (ADR 0029), so the composing card states how many there
  // are and offers nothing here that would change them.
  await expect(
    listCard.getByRole('heading', { name: 'Automatic sources' }),
  ).toHaveCount(0)
  await expect(listCard.getByText(/^Sources · \d+$/)).toBeVisible()
  for (const absent of [/^Sources · \d+$/])
    await expect(listCard.getByRole('button', { name: absent })).toHaveCount(0)

  const refresh = listCard.getByRole('button', {
    name: /^Refresh from sources: Sources · \d+$/,
  })
  await expect(refresh).toBeEnabled()
  const refreshed = page.waitForResponse(
    (response) =>
      response.url().endsWith('/v1/lists/discord/refresh') &&
      response.request().method() === 'POST',
  )
  await refresh.click()
  expect((await refreshed).ok()).toBe(true)

  // A list can stand for hundreds of destinations, so the card narrows them in
  // place rather than asking the operator to scroll.
  const contentsFilter = listCard.getByRole('searchbox', {
    name: 'Search the contents',
  })
  await contentsFilter.fill('192.0.2.10')
  await expect(cardRows(listCard)).toHaveCount(1)
  await contentsFilter.fill('no-such-entry')
  await expect(listCard.getByText('Nothing found.')).toBeVisible()
  await contentsFilter.fill('')
  // A modal dialog owns the scroll: the page behind it must not move.
  expect(
    await page.evaluate(
      () => getComputedStyle(document.body).overflow === 'hidden',
    ),
  ).toBe(true)
  await listCard.getByRole('button', { name: 'Add to profile' }).click()
  await listCard.getByRole('button', { name: 'Close' }).last().click()
  await expect(listMembership(page, 'Discord')).toBeChecked()
  await expect(page.getByText('1 list in the profile')).toBeVisible()
  await expect(submit).toBeDisabled()

  // The search narrows the checkboxes without ever unpicking a list.
  const search = page.getByRole('searchbox', { name: 'Find a list' })
  await search.fill('youtu')
  await expect(listMembership(page, 'Discord')).toHaveCount(0)
  await listMembership(page, 'YouTube').check()
  await expect(page.getByText('2 lists in the profile')).toBeVisible()

  // Priority is the profile's overlap policy. The first row is dragged below the
  // second here, while the same handle also exposes arrow-key reordering.
  await search.fill('')
  const priorityRows = selectedListRows(page)
  await expect(priorityRows).toHaveCount(2)
  await expect(priorityRows.nth(0).getByRole('rowheader')).toContainText(
    'Discord',
  )
  const dragHandle = priorityHandle(priorityRows.nth(0))
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
  const ghost = dragGhost(page)
  await expect(ghost).toBeVisible()
  const ghostBox = (await ghost.boundingBox())!
  const originalBox = (await priorityRows.nth(0).boundingBox())!
  expect(Math.abs(ghostBox.width - originalBox.width)).toBeLessThanOrEqual(1)
  expect(Math.abs(ghostBox.height - originalBox.height)).toBeLessThanOrEqual(1)
  const originalCells = await rowCellWidths(priorityRows.nth(0))
  const ghostCells = await rowCellWidths(ghost)
  expect(ghostCells).toHaveLength(originalCells.length)
  ghostCells.forEach((width, index) =>
    expect(Math.abs(width - originalCells[index]!)).toBeLessThanOrEqual(1),
  )
  await page.mouse.move(to.x + to.width / 2, to.y + to.height + 8, {
    steps: 16,
  })
  await page.waitForTimeout(100)
  await page.mouse.up()
  await expect(priorityRows.nth(0).getByRole('rowheader')).toContainText(
    'YouTube',
  )

  // The name is proposed from what was picked.
  const nameInput = nameField(page)
  await expect(nameInput).toHaveValue('Discord, YouTube')

  // The proposed name is a starting point, never a gate.
  await nameInput.fill('Discord, YouTube')

  // The connection is chosen in the same flow. Its card states both the format
  // and whether Routevane has a direct adapter for it.
  await openFormats(page)

  // The list opens against the field it belongs to: the same left edge, and
  // never narrower than it. A panel centred under a field reads as a menu that
  // happens to be near it rather than as that field's own list.
  const targetField = deliveryField(page)
  const targetPanel = page.getByRole('dialog', {
    name: englishCopy('create.target.toggle'),
  })
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
  const nameBox = await nameField(page).boundingBox()
  expect(Math.round(fieldBox?.height ?? 0)).toBe(
    Math.round(nameBox?.height ?? -1),
  )

  await expect(
    page.getByRole('option', { name: /Keenetic.*\.bat.*from Routevane/ }),
  ).toBeVisible()
  await page.getByRole('option', { name: /Keenetic/ }).click()
  await expect(targetField).toHaveText('Keenetic')
  await expect(submit).toBeEnabled()
  const outputsResponse = page.waitForResponse(
    (candidate) =>
      candidate.request().method() === 'POST' &&
      /\/v1\/profiles\/[a-f0-9]{32}\/outputs$/.test(
        new URL(candidate.url()).pathname,
      ),
  )
  await submit.click()
  await page.waitForURL(
    new RegExp(`^${escapeRegExp(origin)}/profiles/[a-f0-9]{32}.*$`),
  )
  const profileID = profileIDFromURL(page.url())
  expect(profileID).toMatch(/^[a-f0-9]{32}$/)
  await expect(
    page.getByRole('heading', { level: 1, name: 'Discord, YouTube' }),
  ).toBeVisible()

  // The chosen connection is published in the same visible flow, and the stored
  // profile carries exactly what was picked. The list card reads its own sources
  // when it opens, so the profile flow is read without the card's writes.
  expect([
    profileFlow(mutations)[0]?.path,
    profileFlow(mutations)[0]?.body,
  ]).toEqual([
    '/v1/profiles',
    JSON.stringify({
      name: 'Discord, YouTube',
      lists: ['discord', 'youtube'],
      categories: [],
      exclusions: [],
      list_domains: {},
      priority: ['youtube', 'discord'],
    }),
  ])

  const tablist = page.getByRole('tablist', { name: 'Profile sections' })
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
  const targetSelect = addConnectionField(page)
  await expect(targetSelect).toBeEnabled()

  const published = profileFlow(mutations)
  expect(published).toHaveLength(4)
  expect([published[1]?.path, published[1]?.body]).toEqual([
    `/v1/profiles/${profileID}/outputs`,
    JSON.stringify({ target_id: 'keenetic' }),
  ])
  expect([published[2]?.path, published[2]?.body]).toEqual([
    `/v1/profiles/${profileID}/refresh`,
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
  await page.getByRole('link', { name: 'Profiles' }).first().click()
  await page.waitForURL(`${origin}/`)
  const row = page.getByRole('row').filter({ hasText: 'Discord, YouTube' })
  await expect(row).toHaveCount(1)
  await expect(
    row.getByRole('link', { exact: true, name: 'Discord, YouTube' }),
  ).toHaveAttribute('href', `/profiles/${profileID}`)
  // The profile name keeps the original proposal, while the second line makes
  // the changed priority visible on the shelf.
  await expect(row).toContainText('YouTube, Discord')
  await expect(profileOutputsCell(row)).toHaveText('Keenetic')
  await expect(profileOutputsCell(row)).not.toContainText('BAT')
  await expect(row.getByRole('button')).toHaveCount(1)
  await row
    .getByRole('button', {
      name: 'Actions for profile Discord, YouTube',
    })
    .click()
  const menu = menuPanel(page)
  // Everything in the panel is a menu item, so the keyboard traverses one list
  // rather than a mixture of buttons and links.
  await expect(
    menu.getByRole('menuitem', { name: 'Open profile' }),
  ).toBeVisible()
  await expect(menu.getByRole('menuitem', { name: 'Download' })).toBeVisible()
  await expect(menu.getByRole('menuitem')).toHaveCount(6)
  await expect(
    menu.getByRole('menuitem', { name: 'Send to Keenetic' }),
  ).toHaveAttribute('href', `/profiles/${profileID}/send/${outputID}`)
  const exported = page.waitForResponse(
    (candidate) =>
      candidate.request().method() === 'POST' &&
      new URL(candidate.url()).pathname === `/v1/profiles/${profileID}/export`,
  )
  const download = page.waitForEvent('download')
  await menu.getByRole('menuitem', { name: 'Download' }).click()
  // A grouped choice opens its own menu over the one that offered it, so the
  // formats are in the last panel the stack holds.
  const formats = menuPanel(page).last()
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
    .getByRole('button', { name: 'Actions for profile Discord, YouTube' })
    .click()
  const reopenedMenu = menuPanel(page)
  await expect(
    reopenedMenu.getByRole('menuitem', { name: 'Copy contents' }),
  ).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(reopenedMenu).toBeHidden()

  await row.getByRole('link', { exact: true, name: 'Discord, YouTube' }).click()
  await page.waitForURL(`${origin}/profiles/${profileID}`)
  await expect(
    page.getByRole('heading', { level: 1, name: 'Discord, YouTube' }),
  ).toBeVisible()
  await expect(nameField(page)).toHaveValue('Discord, YouTube')
  const profilePage = page.getByRole('main')

  // Editing uses the same stable rows and in-table membership controls.
  const compositionRow = selectedListRows(profilePage).filter({
    hasText: 'Discord',
  })
  await expect(
    compositionRow.getByRole('checkbox', {
      name: 'Remove Discord from the profile',
    }),
  ).toBeVisible()
  await expect(
    profilePage.getByRole('checkbox', {
      name: 'Remove YouTube from the profile',
    }),
  ).toBeVisible()
  const editorTable = profilePage.getByRole('table')
  await expect(editorTable).toBeVisible()
  await expect(selectedListRows(editorTable)).toHaveCount(2)
  await listRow(editorTable, 'Discord')
    .getByRole('button', { name: 'Open the contents of list Discord' })
    .click()
  const listDialog = page.getByRole('dialog', {
    exact: true,
    name: 'Discord',
  })
  await expect(
    listDialog.getByRole('heading', { name: 'List contents' }),
  ).toBeVisible()
  await expect(listDialog.getByText('catalog').first()).toBeVisible()
  expect(await audit(page, 'list-detail')).toEqual([])
  await assertNoOverflow(page, 'list-detail')
  await listDialog.getByRole('button', { name: 'Close' }).click()

  // Filtering narrows the table without changing composition, and an
  // uncategorized list remains available in the same surface.
  const editorSearch = profilePage.getByRole('searchbox', {
    name: 'Find a list',
  })
  await editorSearch.fill('Limit fixture')
  await expect(
    bodyRows(editorTable).filter({ hasText: 'Limit fixture' }),
  ).toHaveCount(1)
  await editorSearch.fill('')

  const discord = listMembership(profilePage, 'Discord')
  const discordRow = listRow(profilePage, 'Discord')
  const rowHeight = await discordRow.evaluate((element) => element.clientHeight)
  await discord.uncheck()
  await expect(discordRow).toContainText('Discord')
  expect(await discordRow.evaluate((element) => element.clientHeight)).toBe(
    rowHeight,
  )
  await expect(
    profilePage.getByText('1 list in the profile', { exact: true }),
  ).toBeVisible()
  await expect(compositionRow).toHaveCount(0)

  expect(consoleErrors).toEqual([])
  expect(pageErrors).toEqual([])
  for (const requestURL of requestURLs)
    expect(new URL(requestURL).origin).toBe(origin)
  assertProductAlive()
})

test('the list card takes domains, addresses and networks, typed or imported', async ({
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
      ':: profiles exported from the router',
      'route ADD 198.18.0.0 MASK 255.255.240.0 0.0.0.0',
      'route ADD 198.18.32.0 MASK 255.0.255.0 0.0.0.0',
      '',
    ].join('\r\n'),
    'utf8',
  )

  try {
    // Entries are the list's own material, so they are added where the list is
    // curated rather than where a profile is composed (ADR 0029).
    await page.goto(`${origin}/lists`)
    await openLibraryCategory(page, 'Communication')
    await page
      .getByRole('button', { name: 'Open the contents of list Discord' })
      .click()
    const card = page.getByRole('dialog', { exact: true, name: 'Discord' })
    await expect(
      card.getByRole('heading', { name: 'List contents' }),
    ).toBeVisible()
    // No profile is in question here, so the card asks about none: the act its
    // footer would carry is not offered at all.
    await expect(
      card.getByRole('button', {
        name: /^(Add to profile|Remove from profile)$/,
      }),
    ).toHaveCount(0)

    // The filter and the control beside it share one line: the field's frame
    // owns its height, so the input inside it stands a hairline border shorter
    // than the control it sits next to, and both are centred on the same line.
    const filterBox = await card
      .getByRole('searchbox', { name: 'Search the contents' })
      .boundingBox()
    const addBox = await card
      .getByRole('button', { name: 'Add entries' })
      .boundingBox()
    expect(filterBox).not.toBeNull()
    expect(addBox).not.toBeNull()
    expect(
      Math.round((addBox?.height ?? 0) - (filterBox?.height ?? 0)),
    ).toBeLessThanOrEqual(2)
    expect(
      Math.abs(
        (filterBox?.y ?? 0) +
          (filterBox?.height ?? 0) / 2 -
          (addBox?.y ?? 0) -
          (addBox?.height ?? 0) / 2,
      ),
    ).toBeLessThanOrEqual(1)

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
    // once, not twice. Both layers stay — a pointer landing outside the panel
    // still closes it — so what must hold is that exactly one of them is
    // painted.
    await expect(dialogScrims(page)).toHaveCount(2)
    const grounds = await dialogScrims(page).evaluateAll((layers) =>
      layers.map((layer) => getComputedStyle(layer).backgroundColor),
    )
    expect(
      grounds.filter((ground) => ground !== 'rgba(0, 0, 0, 0)'),
    ).toHaveLength(1)

    await entries
      .getByLabel('Entries')
      .fill('corp.example\n203.0.113.7\n203.0.113.0/29')
    await entries.getByRole('button', { exact: true, name: 'Add' }).click()
    await expect(entries).toBeHidden()
    // All three shapes land as rows of the one table, each naming only where
    // it came from: the value already says what it is.
    await expect(cardRow(card, 'corp.example')).toContainText('by hand')
    await expect(cardRow(card, '203.0.113.7')).toContainText('by hand')
    await expect(cardRow(card, '203.0.113.0/29')).toContainText('by hand')

    // A profiles file the operator already has is read in place, and the line it
    // could not read is stated rather than silently dropped — on the card,
    // beside the rows the file did land in.
    await card.getByRole('button', { name: 'Add entries' }).click()
    await expect(entries).toBeVisible()
    // The picker's own caption names both the input that takes the file and
    // the surface that opens the chooser; the input is the first of the two.
    await entries.getByLabel('Import a file').first().setInputFiles(routesFile)
    await expect(entries).toBeHidden()
    await expect(cardRow(card, '198.18.0.0/20')).toContainText('by hand')
    await expect(card.getByText('1 line skipped')).toBeVisible()

    // Switching an added row off takes it back, which leaves the list as
    // this test found it for everything that reads Discord after it.
    for (const value of [
      'corp.example',
      '203.0.113.7',
      '203.0.113.0/29',
      '198.18.0.0/20',
    ]) {
      await cardRow(card, value).getByRole('checkbox').click()
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
test('the list card stays whole over a scrolled page and gives the scroll back', async ({
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
  await page.setViewportSize({ height: 500, width: 1280 })
  await page.goto(`${origin}/profiles/new`)
  await expect(
    page.getByRole('heading', { level: 1, name: 'New profile' }),
  ).toBeVisible()
  const logo = page.getByTestId('rv-shell-product-mark')
  const logoBeforeScroll = await logo.boundingBox()
  expect(logoBeforeScroll).not.toBeNull()

  // The page is scrolled before the card opens. The click below would scroll
  // its own target into view, so the position the card must preserve is the
  // one read after that adjustment, not before it.
  await page.getByRole('main').evaluate((element) => {
    element.scrollTop = element.scrollHeight
  })
  const opener = page.getByRole('button', {
    name: 'Open the contents of list Discord',
  })
  await opener.scrollIntoViewIfNeeded()
  const scrolled = await page
    .getByRole('main')
    .evaluate((element) => Math.round(element.scrollTop))
  expect(scrolled).toBeGreaterThan(0)
  const logoAfterScroll = await logo.boundingBox()
  expect(logoAfterScroll).not.toBeNull()
  expect(logoAfterScroll?.width).toBeCloseTo(logoBeforeScroll?.width ?? 0, 2)
  expect(logoAfterScroll?.height).toBeCloseTo(logoBeforeScroll?.height ?? 0, 2)
  expect(logoAfterScroll?.y).toBe(logoBeforeScroll?.y)

  await opener.focus()
  await expect(opener).toBeFocused()

  // Pause the actual snapshot animation, rather than accepting final geometry
  // while an empty portal host is the only thing that was animated.
  await page.evaluate(() => {
    const native = document.startViewTransition.bind(document)
    const probe: {
      animations: Animation[]
      promise: Promise<{ opacity: number; x: number }>
      native: typeof native
    } = {
      animations: [],
      promise: Promise.resolve({ opacity: 0, x: 0 }),
      native,
    }
    probe.promise = new Promise((resolve) => {
      document.startViewTransition = (update) => {
        const current = native(update)
        void current.ready.then(async () => {
          probe.animations = document
            .getAnimations()
            .filter((a) => (a.effect as KeyframeEffect)?.pseudoElement)
          for (const animation of probe.animations) {
            animation.pause()
            animation.currentTime =
              Number(animation.effect!.getTiming().duration) / 2
          }
          await new Promise(requestAnimationFrame)
          const style = getComputedStyle(
            document.documentElement,
            '::view-transition-new(workspace-detail)',
          )
          resolve({
            opacity: Number(style.opacity),
            x: new DOMMatrix(style.transform).e,
          })
        })
        return current
      }
    })
    Reflect.set(window, '__routevaneSheetTransitionProbe', probe)
  })
  await opener.click()
  const card = page.getByRole('dialog', { exact: true, name: 'Discord' })
  await expect(card).toBeVisible()
  await expect(
    card.getByRole('searchbox', { name: 'Search the contents' }),
  ).toBeFocused()
  const viewport = page.viewportSize()
  const midpoint = await page.evaluate(
    async () =>
      Reflect.get(window, '__routevaneSheetTransitionProbe')
        .promise as Promise<{ opacity: number; x: number }>,
  )
  expect(midpoint.opacity).toBeGreaterThan(0)
  expect(midpoint.opacity).toBeLessThan(1)
  expect(midpoint.x).toBeGreaterThan(0)
  await page.evaluate(async () => {
    const probe = Reflect.get(window, '__routevaneSheetTransitionProbe') as {
      animations: Animation[]
      native: typeof document.startViewTransition
    }
    probe.animations.forEach((animation) => animation.play())
    await Promise.all(probe.animations.map((animation) => animation.finished))
    document.startViewTransition = probe.native
  })

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
  // The card owns the screen, so the surface behind it is hidden from the
  // accessibility tree — which is what a reader's software must be told. The
  // landmark is therefore read with hidden elements included rather than
  // through a markup selector.
  expect(
    await page
      .getByRole('main', { includeHidden: true })
      .evaluate((element) => Math.round(element.scrollTop)),
  ).toBe(scrolled)

  // The library primitive owns a modal focus loop. Both ends wrap inside the
  // real USlideover, and close returns to the external opener. The ends are
  // named rather than counted: the first thing the keyboard reaches is the
  // link in the card's header, and the last is the act in its footer.
  await card.getByRole('link', { name: 'Open in the library' }).focus()
  await page.keyboard.press('Shift+Tab')
  expect(
    await card.evaluate((element) => element.contains(document.activeElement)),
  ).toBe(true)
  await card.getByRole('button', { name: 'Add to profile' }).focus()
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
  expect(
    await page
      .getByRole('main')
      .evaluate((element) => Math.round(element.scrollTop)),
  ).toBe(scrolled)

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
  expect(
    await page
      .getByRole('main')
      .evaluate((element) => Math.round(element.scrollTop)),
  ).toBe(scrolled)
  await page.emulateMedia({ reducedMotion: 'no-preference' })

  // Dismissing midway through the captured entrance must finish with the
  // sheet gone and focus restored, without reviving an obsolete transition.
  await page.evaluate(() => {
    const native = document.startViewTransition.bind(document)
    let opening = true
    const promise = new Promise<{ opacity: number; populated: boolean }>(
      (resolve, reject) => {
        document.startViewTransition = (update) => {
          const current = native(update)
          if (opening) {
            opening = false
            void current.ready
              .then(async () => {
                const animations = document
                  .getAnimations()
                  .filter((a) => (a.effect as KeyframeEffect)?.pseudoElement)
                animations.forEach((a) => {
                  a.pause()
                  a.currentTime = Number(a.effect!.getTiming().duration) / 2
                })
                await new Promise(requestAnimationFrame)
                const opacity = Number(
                  getComputedStyle(
                    document.documentElement,
                    '::view-transition-new(workspace-detail)',
                  ).opacity,
                )
                const sheet = document.querySelector('.rv-dialog--inspection')!
                const populated = sheet.textContent!.includes('Discord')
                sheet
                  .querySelector<HTMLButtonElement>('.rv-dialog__close')!
                  .click()
                resolve({ opacity, populated })
              })
              .catch(reject)
          } else {
            void current.finished.finally(() => {
              document.startViewTransition = native
            })
          }
          return current
        }
      },
    )
    Reflect.set(window, '__routevaneQuickCloseProbe', promise)
  })
  await opener.click()
  const quickClose = await page.evaluate(
    async () =>
      Reflect.get(window, '__routevaneQuickCloseProbe') as Promise<{
        opacity: number
        populated: boolean
      }>,
  )
  expect(quickClose.opacity).toBeGreaterThan(0)
  expect(quickClose.opacity).toBeLessThan(1)
  expect(quickClose.populated).toBe(true)
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
  const created = await page.request.post(`${origin}/v1/profiles`, {
    data: {
      categories: [],
      exclusions: [],
      name: 'Imported 42 · youtube · keenetic',
      lists: ['youtube'],
    },
    headers: {
      Origin: origin,
      'X-Routevane-Request': '1',
    },
  })
  expect(created.ok()).toBe(true)
  const payload = (await created.json()) as { profile: { id: string } }

  await page.reload()
  await expect(
    page.getByText('1 profile restored from the previous version'),
  ).toBeVisible()
  const restored = page.getByRole('link', {
    exact: true,
    name: 'Restored profile 42',
  })
  await expect(restored).toHaveAttribute(
    'href',
    `/profiles/${payload.profile.id}`,
  )
  await restored.click()
  await expect(
    page.getByRole('heading', {
      level: 1,
      name: 'Restored profile 42',
    }),
  ).toBeVisible()
  await expect(
    page.getByText('Restored from the previous version'),
  ).toBeVisible()
})

test('the profile page guards the secret, shows the file and its diagnostics, and republishes on rename', async ({
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

  const { profileId, outputId } = await buildProfile(page)
  const profileURL = `${origin}/profiles/${profileId}`

  await expect(
    page.getByText('Published with notes', { exact: true }),
  ).toBeVisible()
  await expect(page.getByText('Coverage is incomplete')).toBeVisible()
  await expect(nameField(page)).toHaveValue('Discord, YouTube')
  // The contents tab states the composition itself, one row per list, and keeps
  // the catalog behind the control that adds to it.
  const routePriority = selectedListRows(page.getByRole('main'))
  await expect(routePriority).toHaveCount(2)
  await expect(routePriority).toContainText(['Discord', 'YouTube'])

  // Refresh belongs to Connection. The inactive panel stays mounted for stable
  // state, but its control must not leak into the Contents tab.
  await expect(scheduleField(page)).toBeHidden()

  // The subscription link is shown masked: the raw secret is not in the DOM,
  // not in any request, until the operator asks for it.
  await expect(
    page.getByRole('heading', { name: 'Subscription link · Keenetic' }),
  ).toBeVisible()
  const secret = page.getByRole('region', { name: /^Subscription link/ })
  await expect(secret).toContainText(`${origin}/v1/subscriptions/`)
  expect(await page.content()).not.toContain('rv1.')
  await page.getByRole('button', { name: 'Show', exact: true }).click()
  await expect(secret).toContainText(`${origin}/v1/subscriptions/rv1.`)
  expect(requestURLs.some((url) => url.includes('rv1.'))).toBe(false)

  // The tabs are one object's facets, announced as tabs. Connection sits between
  // the overview and the file, because a profile may feed several formats.
  const tablist = page.getByRole('tablist', { name: 'Profile sections' })
  await expect(tablist.getByRole('tab')).toHaveText([
    'Contents',
    'Connection',
    'File',
    'Diagnostics',
  ])

  // The file is read only when the operator opens it, and what is shown is
  // byte-for-byte what the device receives.
  await tablist.getByRole('tab', { name: 'Connection' }).click()
  const scheduleTrigger = scheduleField(page)
  await expect(scheduleTrigger).toBeVisible()
  const scheduleGround = async (): Promise<string> =>
    scheduleTrigger.evaluate((node) => getComputedStyle(node).backgroundColor)
  const atRest = await scheduleGround()
  await page
    .getByRole('heading', {
      name: englishCopy('profile.schedule'),
      exact: true,
    })
    .hover()
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
  const rendered = page.getByRole('code')
  await expect(rendered).toBeVisible()
  expect(await rendered.evaluate((element) => element.textContent)).toBe(
    artifactBytes,
  )
  await expect(page.getByRole('figure')).toContainText('1 line')
  await page.reload()
  await expect(rendered).toHaveText(artifactBytes)
  await expect(tablist.getByRole('tab', { name: 'File' })).toHaveAttribute(
    'aria-selected',
    'true',
  )

  // Diagnostics counts the published rules per list and translates the
  // format's stable reason code into operator language.
  const snapshotResponse = page.waitForResponse(
    (candidate) =>
      candidate.request().method() === 'GET' &&
      /^\/v1\/snapshots\/[a-f0-9]{32}$/.test(new URL(candidate.url()).pathname),
  )
  await tablist.getByRole('tab', { name: 'Diagnostics' }).click()
  expect((await snapshotResponse).status()).toBe(200)
  const diagnosticsPanel = page.getByRole('tabpanel', { name: 'Diagnostics' })
  // The panel states the per-list counts first and the rules themselves after,
  // so the first list it holds is what each list contributed.
  const counts = diagnosticsPanel.getByRole('list').first()
  await expect(counts).toContainText('Discord')
  await expect(counts).not.toContainText('YouTube')
  await expect(counts).toContainText('1 rule')
  await expect(diagnosticsPanel).toContainText('Excluded')
  await expect(diagnosticsPanel).toContainText(
    'the rule belongs to a higher-priority list',
  )
  await expect(diagnosticsPanel).toContainText('not supported by this format')
  await expect(diagnosticsPanel).not.toContainText('unsupported_by_target')

  // A reload restores the profile from the server. The one-time link does not
  // come back, and in the default detail mode nothing even mentions it.
  const requestsBeforeReload = requestURLs.length
  await page.reload()
  await expect(
    page.getByRole('heading', { level: 1, name: 'Discord, YouTube' }),
  ).toBeVisible()
  expect(
    requestURLs
      .slice(requestsBeforeReload)
      .some((url) => new URL(url).pathname === `/v1/profiles/${profileId}`),
  ).toBe(true)
  await expect(
    page.getByRole('heading', { name: /^Subscription link/ }),
  ).toHaveCount(0)
  await expect(
    page.getByRole('button', { name: 'Show', exact: true }),
  ).toHaveCount(0)
  await expect(
    page.getByText(
      'The subscription link was shown when the profile was created.',
    ),
  ).toHaveCount(0)
  expect(await page.content()).not.toContain('rv1.')
  const storedAfterReload = await page.evaluate(() => ({
    local: { ...localStorage },
    session: { ...sessionStorage },
  }))
  expect(JSON.stringify(storedAfterReload)).not.toContain('rv1.')
  expect(requestURLs.some((url) => url.includes('rv1.'))).toBe(false)

  // A restored lazy tab loads without needing a detour through another tab.
  await expect(diagnosticsPanel).toContainText('Excluded')

  // The file is still this computer's record, readable after the reload.
  await tablist.getByRole('tab', { name: 'File' }).click()
  await expect(rendered).toBeVisible()
  expect(await rendered.evaluate((element) => element.textContent)).toBe(
    artifactBytes,
  )

  // Renaming and recomposing is an edit, not a new profile: it republishes every
  // output the profile already carries, and the name and the list composition
  // stay two independent facts.
  await tablist.getByRole('tab', { name: 'Contents' }).click()
  const editorNameInput = nameField(page)
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
        lists: ['discord', 'youtube'],
        categories: [],
        exclusions: [],
        list_domains: {},
        priority: ['discord', 'youtube'],
      }),
      path: `/v1/profiles/${profileId}/update`,
    },
    { body: '{}', path: `/v1/profiles/${profileId}/refresh` },
    { body: '{}', path: `/v1/outputs/${outputId}/build` },
  ])

  // The renamed profile is still the same row, its list composition unmoved on
  // the second line.
  await page.getByRole('link', { name: 'Profiles' }).first().click()
  await page.waitForURL(`${origin}/`)
  const renamedRow = page.getByRole('row').filter({ hasText: 'Chat and video' })
  await expect(renamedRow).toHaveCount(1)
  await expect(renamedRow).toContainText('Discord, YouTube')
  await renamedRow
    .getByRole('link', { exact: true, name: 'Chat and video' })
    .click()
  await page.waitForURL(profileURL)

  // The expert mode is what states the link's fate in words, and it is also
  // the only mode that puts identifiers on the screen.
  await page
    .getByRole('navigation', { name: 'Sections' })
    .getByRole('link', { name: 'Settings' })
    .click()
  await pressSegment(page, 'Full')
  await page
    .getByRole('navigation', { name: 'Sections' })
    .getByRole('link', { name: 'Profiles' })
    .click()
  await page.getByRole('link', { exact: true, name: 'Chat and video' }).click()
  await expect(
    page.getByRole('heading', { level: 1, name: 'Chat and video' }),
  ).toBeVisible()
  await expect(
    page.getByText(
      'The subscription link was shown when the profile was created.',
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
  // and refuses. The state under test is a profile that already carries a format
  // it does not fit, so it is set up through the same API the composer calls.
  await page.goto(`${origin}/`)
  const mutationHeaders = { Origin: origin, 'X-Routevane-Request': '1' }
  const created = await page.request.post(`${origin}/v1/profiles`, {
    data: {
      categories: [],
      exclusions: [],
      name: 'Over the limit',
      lists: ['limit-fixture'],
    },
    headers: mutationHeaders,
  })
  expect(created.ok()).toBe(true)
  const failedProfileID = (
    (await created.json()) as { profile: { id: string } }
  ).profile.id

  const added = await page.request.post(
    `${origin}/v1/profiles/${failedProfileID}/outputs`,
    { data: { target_id: 'limited-fixture' }, headers: mutationHeaders },
  )
  expect(added.ok()).toBe(true)
  const addPayload = (await added.json()) as {
    output: { id: string }
  } & Record<string, unknown>
  expect(addPayload).not.toHaveProperty('subscription_url')

  const refreshed = await page.request.post(
    `${origin}/v1/profiles/${failedProfileID}/refresh`,
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

  await page.goto(`${origin}/profiles/${failedProfileID}`)
  await expect(page.getByText('Refresh failed', { exact: true })).toBeVisible()
  // The editor says the same thing the build reported, before the operator
  // presses anything, and still lets the profile be saved.
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
    .getByRole('tablist', { name: 'Profile sections' })
    .getByRole('tab', { name: 'Connection' })
    .click()
  const outputRow = page.getByRole('row').filter({ hasText: 'Limited fixture' })
  await expect(outputRow).toContainText('Last build failed')
  await expect(outputRow).toContainText('No published file')
  await expect(
    page.getByRole('heading', { name: /^Subscription link/ }),
  ).toHaveCount(0)
  expect(await page.content()).not.toContain('rv1.')

  await page.getByRole('link', { name: 'Profiles' }).first().click()
  await page.waitForURL(`${origin}/`)
  await linkTo(page, `/profiles/${failedProfileID}`).click()
  await expect(page.getByText('Format not updated')).toBeVisible()
  await page
    .getByRole('tablist', { name: 'Profile sections' })
    .getByRole('tab', { name: 'Connection' })
    .click()
  await expect(outputRow).toContainText('Last build failed')
  await page
    .getByRole('main')
    .getByRole('button', { name: /Actions for profile/ })
    .click()
  await menuPanel(page)
    .getByRole('menuitem', { name: 'Refresh and rebuild' })
    .click()
  await expect(page.getByText('Format not updated')).toBeVisible()
  await expect(
    page.getByRole('heading', { name: /^Subscription link/ }),
  ).toHaveCount(0)
})

// The failure above is the one this forecast exists to prevent, which is why
// it runs against the same fixture right after it.
test('the composer sizes every format and refuses the pair that cannot hold the profile', async ({
  page,
}) => {
  test.setTimeout(120000)
  // The forecast speaks the data a build would read; a list whose sources
  // were never observed has no forecast yet. Observing it first keeps this
  // test independent of which test refreshed the fixture before it.
  const observed = await page.request.post(
    `${origin}/v1/lists/limit-fixture/refresh`,
    { data: {}, headers: { Origin: origin, 'X-Routevane-Request': '1' } },
  )
  expect(observed.ok()).toBe(true)
  await page.goto(`${origin}/profiles/new`)
  // The fixture belongs to no category, but still appears in the one table.
  await listMembership(page, 'Limit fixture').check()

  // Every format states what this draft would weigh in it, against its own
  // bound, in the same list the choice is made from.
  const formats = await openFormats(page)
  await expect(
    formats.getByRole('option', { name: /Limited fixture/ }),
  ).toContainText('≈ 2 of 1 rules')
  await expect(
    formats.getByRole('option', { name: /Limited fixture/ }),
  ).toContainText('Cannot hold this profile')
  await expect(formats.getByRole('option', { name: /Keenetic/ })).toContainText(
    '≈ 2 of 1,024 rules',
  )

  // Choosing the format that cannot hold it stops the creation and names one
  // that can. The choice stays selectable: the refusal explains, it does not
  // hide it.
  const submit = page.getByRole('button', { name: 'Create and prepare' })
  await page.getByRole('option', { name: /Limited fixture/ }).click()
  await expect(page.getByText('≈ 2 of 1 rules', { exact: true })).toBeVisible()
  await expect(submit).toBeDisabled()
  await expect(
    page.getByText('The profile does not fit Limited fixture.'),
  ).toBeVisible()
  await expect(page.getByText('sing-box would fit.')).toBeVisible()

  await page.getByRole('button', { name: 'Choose sing-box' }).click()
  await expect(deliveryField(page)).toHaveText('sing-box')
  await expect(submit).toBeEnabled()
  assertProductAlive()
})

/**
 * The composing card reads the list; the one act the profile owns is its footer.
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
    `${origin}/v1/lists/limit-fixture/refresh`,
    { data: {}, headers: { Origin: origin, 'X-Routevane-Request': '1' } },
  )
  expect(observed.ok()).toBe(true)

  const forecastStatuses: number[] = []
  page.on('response', (response) => {
    if (new URL(response.url()).pathname === FORECAST_PATH)
      forecastStatuses.push(response.status())
  })
  const writes: string[] = []
  page.on('request', (request) => {
    const path = new URL(request.url()).pathname
    if (request.method() === 'POST' && path !== FORECAST_PATH) writes.push(path)
  })

  await page.goto(`${origin}/profiles/new`)
  await page
    .getByRole('button', { name: 'Open the contents of list Limit fixture' })
    .click()
  const card = page.getByRole('dialog', { exact: true, name: 'Limit fixture' })
  await expect(
    card.getByRole('heading', { name: 'List contents' }),
  ).toBeVisible()

  // The fixture stands for addresses and for no domain at all: exactly the
  // rows the removed switch could never have taken out of a profile.
  await expect(cardRow(card, '192.0.2.20')).toContainText('catalog')
  await expect(cardRow(card, '198.51.100.20')).toContainText('catalog')
  await expect(card.getByRole('checkbox')).toHaveCount(0)

  // The one control the card offers, used both ways with the card open.
  const add = card.getByRole('button', { name: 'Add to profile' })
  const drop = card.getByRole('button', { name: 'Remove from profile' })
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

test('the visible library order seeds new profiles without rewriting saved profiles', async ({
  page,
}) => {
  const headers = { 'X-Routevane-Request': '1' }
  const catalog = (await (
    await page.request.get(`${origin}/v1/lists`)
  ).json()) as {
    default_priority: string[]
    list_details: { id: string; title: string }[]
  }
  const original = [...catalog.default_priority]
  // The order is stored as identifiers and read on screen as titles, so the
  // expectations below name the list the same way the row does.
  const titles = new Map(
    catalog.list_details.map((list) => [list.id, list.title]),
  )
  const rowTitle = (listID: string): string => titles.get(listID) ?? listID

  try {
    await page.goto(`${origin}/lists`)
    const sheet = libraryRegion(page)
    await expect(sheet).not.toContainText(
      'New profiles start with this order. Existing saved profiles do not change.',
    )

    const first = bodyRows(sheet).first()
    const firstHandle = await priorityHandle(first).boundingBox()
    const secondRow = await bodyRows(sheet).nth(1).boundingBox()
    await page.mouse.move(firstHandle!.x + 8, firstHandle!.y + 8)
    await page.mouse.down()
    await page.mouse.move(
      firstHandle!.x + 8,
      secondRow!.y + secondRow!.height - 4,
      { steps: 12 },
    )
    await page.mouse.up()
    await expect(first.getByRole('rowheader')).toHaveText(
      rowTitle(original[1]!),
    )
    await sheet.getByRole('button', { name: 'Reset order' }).click()
    await expect(first.getByRole('rowheader')).toHaveText(
      rowTitle(original[0]!),
    )

    const handle = priorityHandle(libraryRow(sheet, 'YouTube'))
    for (let index = original.indexOf('youtube'); index > 0; index -= 1)
      await handle.press('ArrowUp')
    await expect(first.getByRole('rowheader')).toHaveText('YouTube')
    await sheet.getByRole('button', { name: 'Save order' }).click()
    await expect(sheet.getByRole('button', { name: 'Save order' })).toBeHidden()

    await page.goto(`${origin}/profiles/new`)
    const search = page.getByRole('searchbox', { name: 'Find a list' })
    await search.fill('Discord')
    await listMembership(page, 'Discord').check()
    await search.fill('YouTube')
    await listMembership(page, 'YouTube').check()
    await search.fill('')
    // A row states its own priority on the handle that changes it, which is
    // where an operator reads the position too.
    await expect(priorityHandle(listRow(page, 'YouTube'))).toHaveAccessibleName(
      'Change priority of list YouTube, position 1',
    )
    await expect(priorityHandle(listRow(page, 'Discord'))).toHaveAccessibleName(
      'Change priority of list Discord, position 2',
    )
  } finally {
    const restored = await page.request.post(`${origin}/v1/lists/priority`, {
      data: { default_priority: original },
      headers,
    })
    expect(restored.ok(), await restored.text()).toBe(true)
  }
})

/**
 * A category is the operator's as much as the catalog's (ADR 0028): they can
 * make one, fill it from the whole catalog, and every profile naming it follows
 * what it holds. The one thing they cannot do is take it out from under a profile
 * still built from it — and the refusal names that profile, so the way forward is
 * the one the message states.
 */
test('an operator category carries its lists into a profile and is kept while a profile names it', async ({
  page,
}) => {
  test.setTimeout(60000)
  // Curating happens in its own section (ADR 0029), so the category exists
  // before the composer is opened at all.
  await page.goto(`${origin}/lists`)
  await expect(
    page.getByRole('heading', { level: 1, name: 'Lists' }),
  ).toBeVisible()

  // The library is an operational workspace, not a short card surrounded by
  // unused page. Its rows stay dense while the two panes claim the available
  // viewport height.
  const viewport = page.viewportSize()
  const workspaceBox = await page
    .getByTestId('rv-lists-workspace')
    .boundingBox()
  const firstListBox = await bodyRows(libraryRegion(page)).first().boundingBox()
  expect(workspaceBox?.height ?? 0).toBeGreaterThanOrEqual(
    Math.floor((viewport?.height ?? 0) * 0.55),
  )
  expect(firstListBox?.height ?? Number.POSITIVE_INFINITY).toBeLessThanOrEqual(
    48,
  )

  await page.getByRole('button', { name: 'Categories', exact: true }).click()
  await page.getByRole('button', { name: 'New category' }).click()
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
  const details = libraryRegion(page)
  await expectLibraryCategory(page, 'Домашние')

  await page.getByRole('button', { name: 'New list' }).click()
  const addList = page.getByRole('dialog', { name: 'New list' })
  await addList.getByText('Existing list', { exact: true }).click()
  await addList
    .getByRole('button', {
      name: englishCopy('lists.category.pick.placeholder'),
    })
    .click()
  await choiceSearch(page).fill('Discord')
  await page.getByRole('option', { name: 'Discord' }).click()
  await addList.getByRole('button', { exact: true, name: 'Add' }).click()
  await expect(addList).toBeHidden()
  await expect(details).toContainText('Discord')

  await page.goto(`${origin}/profiles/new`)
  await expect(
    page.getByRole('heading', { level: 1, name: 'New profile' }),
  ).toBeVisible()

  // A filtered category exposes an explicit live-reference choice, so the
  // profile follows it rather than freezing today's members.
  const categoryFilter = categoryMore(page)
  await categoryFilter.click()
  await page.getByRole('option', { name: 'Домашние' }).click()
  await page.getByRole('button', { name: 'Follow “Домашние”' }).click()
  await page
    .getByRole('menuitem', {
      name: 'Automatically include new lists in this category',
    })
    .click()
  await categoryFilter.click()
  await page.getByRole('option', { name: 'Video' }).click()
  await listMembership(page, 'YouTube').check()
  await expect(nameField(page)).toHaveValue('Домашние, YouTube')

  await chooseFormat(page, /Keenetic/)
  await page.getByRole('button', { name: 'Create and prepare' }).click()
  await page.waitForURL(
    new RegExp(`^${escapeRegExp(origin)}/profiles/[a-f0-9]{32}.*$`),
  )
  await expect(
    page.getByRole('heading', { level: 1, name: 'Домашние, YouTube' }),
  ).toBeVisible()

  // The saved profile still states the category reference through the filter,
  // while the ordered rail shows the concrete list it currently contributes.
  await page
    .getByRole('tablist', { name: 'Profile sections' })
    .getByRole('tab', { name: 'Contents' })
    .click()
  const editorFilter = categoryMore(page)
  await editorFilter.click()
  await page.getByRole('option', { name: 'Домашние' }).click()
  await expect(
    page.getByRole('button', { name: 'Follow “Домашние”' }),
  ).toContainText('New lists: automatic')
  await expect(
    selectedListRows(page).filter({ hasText: 'Discord' }),
  ).toHaveCount(1)

  const routeURL = page.url()

  // A profile is built from this category, so the category stays where it is and
  // the refusal names the profile standing in the way — beside the act it
  // refused, which is the one place the operator is standing.
  await page.goto(`${origin}/lists`)
  await openCategoryActions(page, 'Домашние')
  await menuPanel(page)
    .getByRole('menuitem', { name: 'Delete the category' })
    .click()
  const removal = page.getByRole('dialog', { name: 'Delete the category' })
  await removal.getByRole('button', { exact: true, name: 'Delete' }).click()
  await expect(removal.getByText('The category was not deleted')).toBeVisible()
  await expect(
    removal.getByText(
      'It is part of these profiles: Домашние, YouTube. Remove it there and try again.',
    ),
  ).toBeVisible()
  await removal.getByRole('button', { name: 'Cancel' }).click()
  await page.getByRole('button', { name: 'Categories', exact: true }).click()
  const categories = page.getByRole('dialog', {
    name: 'Categories',
    exact: true,
  })
  await expect(categories).toContainText('Домашние')
  await categories.getByRole('button', { name: 'Close', exact: true }).click()

  // Doing what the refusal asks is what makes the deletion possible, and the
  // profile keeps everything it did not follow through the category.
  await page.goto(routeURL)
  await page
    .getByRole('tablist', { name: 'Profile sections' })
    .getByRole('tab', { name: 'Contents' })
    .click()
  const routeEditorFilter = categoryMore(page)
  await routeEditorFilter.click()
  await page.getByRole('option', { name: 'Домашние' }).click()
  await page.getByRole('button', { name: 'Follow “Домашние”' }).click()
  await page
    .getByRole('menuitem', { name: 'Select new lists manually' })
    .click()
  await page.getByRole('button', { name: 'Save and rebuild' }).click()
  await expect(
    page.getByRole('button', { name: 'Follow “Домашние”' }),
  ).toContainText('New lists: manual')
  await page.getByRole('searchbox', { name: 'Find a list' }).fill('')
  await page
    .getByRole('button', { name: 'All categories', exact: true })
    .click()
  await expect(
    selectedListRows(page).filter({ hasText: 'YouTube' }),
  ).toHaveCount(1)

  // Nothing names it now, so it goes — and the list it held keeps the category
  // it already belonged to.
  await page.goto(`${origin}/lists`)
  await openCategoryActions(page, 'Домашние')
  await menuPanel(page)
    .getByRole('menuitem', { name: 'Delete the category' })
    .click()
  await removal.getByRole('button', { exact: true, name: 'Delete' }).click()
  await expect(removal).toBeHidden()
  await page.getByRole('button', { name: 'Categories', exact: true }).click()
  await expect(categories).not.toContainText('Домашние')
  await categories.getByRole('button', { name: 'Close', exact: true }).click()
  await openLibraryCategory(page, 'Communication')
  await expect(page.getByRole('table')).toContainText('Discord')
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
  await page.goto(`${origin}/lists`)
  await expect(
    page.getByRole('heading', { level: 1, name: 'Lists' }),
  ).toBeVisible()

  await page.getByRole('button', { name: 'Categories', exact: true }).click()
  await page.getByRole('button', { name: 'New category' }).click()
  const categoryForm = page.getByRole('dialog', { name: 'New category' })
  await categoryForm.getByLabel('Name').fill('Черновик')
  await categoryForm
    .getByRole('button', { exact: true, name: 'Create' })
    .click()
  await expect(categoryForm).toBeHidden()

  // A list made while a category is open joins that category.
  await page.getByRole('button', { name: 'New list' }).click()
  const newProfile = page.getByRole('dialog', { name: 'New list' })
  await newProfile.getByLabel('Name').fill('Draft fixture')
  await newProfile.getByLabel('Domains').fill('draft.example')
  await newProfile.getByRole('button', { exact: true, name: 'Create' }).click()
  await expect(newProfile).toBeHidden()
  await expect(libraryRegion(page)).toContainText('Draft fixture')

  await openCategoryActions(page, 'Черновик')
  await menuPanel(page)
    .getByRole('menuitem', { name: 'Delete the category' })
    .click()
  const removal = page.getByRole('dialog', { name: 'Delete the category' })
  await removal.getByText('Delete them with it').click()
  await removal.getByRole('button', { exact: true, name: 'Delete' }).click()
  await expect(removal).toBeHidden()
  await page.getByRole('button', { name: 'Categories', exact: true }).click()
  const categories = page.getByRole('dialog', {
    name: 'Categories',
    exact: true,
  })
  await expect(categories).not.toContainText('Черновик')
  await categories.getByRole('button', { name: 'Close', exact: true }).click()
  await openLibraryCategory(page, 'Uncategorized')
  await expect(page.getByRole('table')).not.toContainText('Draft fixture')

  // Selection is a checkbox and nothing else: the composer has no category
  // menu, no way to make or unmake anything, and no bin on a list row.
  await page.goto(`${origin}/profiles/new`)
  await expect(page.getByRole('table')).toBeVisible()
  for (const gone of ['New category', 'New list'])
    await expect(page.getByRole('button', { name: gone })).toHaveCount(0)
  await expect(
    page.getByRole('button', { name: /^Actions for list/ }),
  ).toHaveCount(0)
  await expect(
    page.getByRole('button', { name: 'Delete the list', exact: true }),
  ).toHaveCount(0)
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
  await page.goto(`${origin}/lists`)
  await openLibraryCategory(page, 'Uncategorized')
  await page.getByRole('button', { name: 'New list' }).click()
  const newProfile = page.getByRole('dialog', { name: 'New list' })
  await newProfile.getByLabel('Name').fill('Tall fixture')
  await newProfile
    .getByLabel('Domains')
    .fill(
      Array.from({ length: 40 }, (_, index) => `row${index}.example`).join(
        '\n',
      ),
    )
  await newProfile.getByRole('button', { exact: true, name: 'Create' }).click()
  await expect(newProfile).toBeHidden()

  await page.goto(`${origin}/profiles/new`)
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
  const rows = cardContents(card)
  expect(
    await rows.evaluate(
      (element) => element.scrollHeight - element.clientHeight,
    ),
  ).toBeGreaterThan(0)
  await rows.evaluate((element) => {
    element.scrollTop = element.scrollHeight
  })
  // The band is read against the act the footer holds, which is the first
  // thing under the table and the only part of that band a reader can name.
  const rowBox = await cardRows(card).last().boundingBox()
  const footerBox = await card
    .getByRole('button', { name: 'Add to profile' })
    .boundingBox()
  expect(rowBox).not.toBeNull()
  expect(footerBox).not.toBeNull()
  const band = (footerBox?.y ?? 0) - ((rowBox?.y ?? 0) + (rowBox?.height ?? 0))
  expect(band, 'empty band between the last row and the footer').toBeLessThan(
    48,
  )

  await card.getByRole('button', { name: 'Close' }).last().click()
  await page.goto(`${origin}/lists`)
  await openLibraryCategory(page, 'Uncategorized')
  await page
    .getByRole('button', { name: 'Actions for list Tall fixture' })
    .click()
  await menuPanel(page)
    .getByRole('menuitem', { name: 'Delete the list' })
    .click()
  const removal = page.getByRole('dialog', { name: 'Delete the list' })
  await removal.getByRole('button', { exact: true, name: 'Delete' }).click()
  await expect(removal).toBeHidden()
  await expect(page.getByRole('table')).not.toContainText('Tall fixture')
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
  const created = await page.request.post(`${origin}/v1/profiles`, {
    data: {
      categories: [],
      exclusions: [],
      name: 'Overlay ground',
      lists: ['youtube'],
    },
    headers: { Origin: origin, 'X-Routevane-Request': '1' },
  })
  expect(created.ok()).toBe(true)

  await page.goto(`${origin}/`)
  await page
    .getByRole('button', { name: 'Actions for profile Overlay ground' })
    .click()
  await assertPainted(menuPanel(page), 'menu')
  await page.keyboard.press('Escape')

  await page.goto(`${origin}/connections`)
  await deviceField(page, 'devices.field.target').click()
  await assertPainted(
    choicePanel(page, englishCopy('devices.field.target.pick')),
    'connection choice',
  )
  await page.keyboard.press('Escape')

  await page.goto(`${origin}/profiles/new`)
  await openFormats(page)
  await assertPainted(
    choicePanel(page, englishCopy('create.target.toggle')),
    'combobox',
  )
  await page.keyboard.press('Escape')

  await page
    .getByRole('button', { name: 'Open the contents of list Discord' })
    .click()
  const card = page.getByRole('dialog', { exact: true, name: 'Discord' })
  await card
    .getByRole('button', { name: englishCopy('listCard.domains.info') })
    .click()
  // An informer's panel is a dialog of its own, opened over the card that
  // offers it, so it is addressed by the name of the control it belongs to.
  await assertPainted(
    infoPanel(page, englishCopy('listCard.domains.info')),
    'informer',
  )
  assertProductAlive()
})

test('the composition table blocks unselected drags and bulk-selects only its category', async ({
  page,
}) => {
  await page.goto(`${origin}/profiles/new`)
  const rows = bodyRows(page)
  await expect(rows.first()).toBeVisible()
  // The order the table is in, read the way it is displayed: the name each row
  // states in its own header.
  const order = (): Promise<string[]> =>
    rows.getByRole('rowheader').allInnerTexts()
  const before = await order()
  const handle = priorityHandle(rows.first())
  await expect(handle).toBeDisabled()
  const from = (await handle.boundingBox())!
  const to = (await rows.nth(1).boundingBox())!
  await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2)
  await page.mouse.down()
  await page.mouse.move(to.x + to.width / 2, to.y + to.height, { steps: 12 })
  await expect(dragGhost(page)).toHaveCount(0)
  await page.mouse.up()
  expect(await order()).toEqual(before)
  await listMembership(page, 'YouTube').check()
  await categoryChip(page, 'Communication').click()
  const bulk = page.getByRole('checkbox', {
    name: 'Select or clear all visible lists',
  })
  await bulk.check()
  await expect(listMembership(page, 'Discord')).toBeChecked()
  await bulk.uncheck()
  await categoryChip(page, englishCopy('listPicker.filter.all')).click()
  await expect(listMembership(page, 'YouTube')).toBeChecked()
  await expect(listMembership(page, 'Discord')).not.toBeChecked()
  const text = nameField(page)
  const choice = deliveryField(page)
  expect(
    await text.evaluate((element) => getComputedStyle(element).backgroundColor),
  ).toBe(
    await choice.evaluate(
      (element) => getComputedStyle(element).backgroundColor,
    ),
  )
  const refresh = compositionRefresh(page)
  await expect(refresh).toBeEnabled()
  const read = page.waitForResponse(
    (response) =>
      response.url().endsWith('/v1/lists/youtube/refresh') &&
      response.request().method() === 'POST',
  )
  const forecast = page.waitForResponse(
    (response) =>
      response.url().endsWith(FORECAST_PATH) &&
      response.request().method() === 'POST',
  )
  await refresh.click()
  expect((await read).ok()).toBe(true)
  expect((await forecast).ok()).toBe(true)
  assertProductAlive()
})

test('the searchable connection choice filters in its panel and reopens by keyboard', async ({
  page,
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

test('an archived profile leaves the shelf, keeps its file, and comes back whole', async ({
  page,
}) => {
  test.setTimeout(180000)
  const pageErrors: string[] = []
  page.on('pageerror', (error) => pageErrors.push(error.message))

  const { profileId } = await buildProfile(page)
  // Earlier walkthroughs left their own lists on the shelf, so every row here
  // is addressed by this profile's identity rather than by its title.
  const shelfRow = page
    .getByRole('row')
    .filter({ has: linkTo(page, `/profiles/${profileId}`) })
  await page
    .getByRole('tablist', { name: 'Profile sections' })
    .getByRole('tab', { name: 'Connection' })
    .click()
  const fileHref = await page
    .getByRole('link', { name: 'Download the file for Keenetic' })
    .first()
    .getAttribute('href')
  expect(fileHref).toMatch(/^\/v1\/artifacts\/[a-f0-9]{32}$/)

  // Archiving is one request and does not rebuild: the file subscribers are
  // receiving must not change because the profile was shelved.
  const mutations: string[] = []
  page.on('request', (request) => {
    const path = new URL(request.url()).pathname
    if (request.method() === 'POST' && statesFlow(path)) {
      mutations.push(path)
    }
  })
  await page
    .getByRole('main')
    .getByRole('button', {
      name: 'Actions for profile Discord, YouTube',
      exact: true,
    })
    .click()
  await menuPanel(page)
    .getByRole('menuitem', { name: 'Archive', exact: true })
    .click()
  await expect(page.getByText('This profile is archived')).toBeVisible()
  expect(mutations).toEqual([`/v1/profiles/${profileId}/archive`])

  // What stops is change. The controls that would edit, rebuild, reschedule or
  // bind a new format are gone rather than disabled, and the file is still
  // offered from the same page.
  await expect(page.getByText('Archived', { exact: true })).toBeVisible()
  await expect(nameField(page)).toHaveCount(0)
  await expect(scheduleField(page)).toHaveCount(0)
  await expect(
    page.getByRole('link', { name: 'Download the file for Keenetic' }).first(),
  ).toHaveAttribute('href', fileHref ?? '')
  await page
    .getByRole('tablist', { name: 'Profile sections' })
    .getByRole('tab', { name: 'Connection' })
    .click()
  await expect(addConnectionField(page)).toHaveCount(0)
  await expect(
    page.getByRole('main').getByRole('row').filter({ hasText: 'Keenetic' }),
  ).toBeVisible()

  // The archived profile has left the shelf without leaving the library: the row
  // is behind one disclosure, still names when it was archived, and still
  // offers its published file.
  await page.getByRole('link', { name: 'Profiles' }).first().click()
  await page.waitForURL(`${origin}/`)
  await expect(shelfRow).toHaveCount(0)
  // The archive is one disclosure, named by what it holds and how much.
  const archive = page.getByRole('button', { name: /^Archive/ })
  await expect(archive).toBeVisible()
  await archive.click()
  const archivedRow = page
    .getByRole('region', { name: /^Archive/ })
    .getByRole('listitem')
    .filter({ has: linkTo(page, `/profiles/${profileId}`) })
  await expect(archivedRow).toHaveCount(1)
  await expect(archivedRow).toContainText('Archived since')
  await archivedRow
    .getByRole('button', {
      name: 'Actions for profile Discord, YouTube',
    })
    .click()
  await menuPanel(page).getByRole('menuitem', { name: 'Download' }).click()
  await expect(
    menuPanel(page).last().getByRole('menuitem', { name: 'BAT · routes' }),
  ).toBeVisible()
  await page.keyboard.press('Escape')

  // The archive is part of the screen, so it is held to the same gates.
  expect(await audit(page, 'library-archive')).toEqual([])
  await assertNoOverflow(page, 'library-archive')

  // Restoring puts the profile back on the shelf with everything it had, and
  // publishes nothing by itself.
  mutations.length = 0
  await archivedRow
    .getByRole('button', { name: 'Actions for profile Discord, YouTube' })
    .click()
  await menuPanel(page).getByRole('menuitem', { name: 'Restore' }).click()
  await expect(shelfRow).toHaveCount(1)
  expect(mutations).toEqual([`/v1/profiles/${profileId}/restore`])
  await expect(page.getByRole('button', { name: /^Archive/ })).toHaveCount(0)

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

  await pressSegment(page, 'Русский')
  await expect(
    page.getByRole('heading', { level: 1, name: 'Настройки' }),
  ).toBeVisible()
  const nav = page.getByRole('navigation', { name: 'Разделы' })
  await expect(nav).toBeVisible()
  await expect(nav.getByRole('link', { name: 'Подключения' })).toBeVisible()
  declared.russian = await documentLanguage(page)

  await nav.getByRole('link', { name: 'Профили' }).click()
  await expect(
    page.getByRole('heading', { level: 1, name: 'Профили' }),
  ).toBeVisible()
  await expect(
    page.getByRole('link', { name: 'Собрать профиль' }).first(),
  ).toBeVisible()

  await nav.getByRole('link', { name: 'Настройки' }).click()
  await pressSegment(page, 'English')
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
  // Every field is addressed by the caption the chosen target's own
  // requirements give it, which is the same fact the label assertions used to
  // state separately.
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
  const { outputId } = await buildProfile(page)

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
  await expect(segmentOption(page, 'Daily')).toBeEnabled()
  await pressSegment(page, 'Daily')
  await expect(page.getByText('Saving the rule…')).toBeVisible()
  const refreshRadios = refreshInterval(page).getByRole('radio')
  await expect(refreshRadios).toHaveCount(3)
  for (let index = 0; index < 3; index++)
    await expect(refreshRadios.nth(index)).toBeDisabled()
  await expect.poll(() => writes).toBe(1)

  release()
  await expect(page.getByText('Saving the rule…')).toHaveCount(0)
  await page.unroute('**/v1/settings/update')
  await pressSegment(page, 'Off')
  await expect(segmentOption(page, 'Off')).toBeChecked()
})

for (const language of ['en', 'ru'] as const) {
  test.describe(`failure recovery ${language}`, () => {
    test.use({ locale: language === 'ru' ? 'ru-RU' : 'en-US' })
    const copy = copyFor(language)

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
          .getByRole('region', { name: copy('connections.title') })
          .getByRole('button', { name: copy('action.retry') })
          .click()
        const target = deviceField(page, 'devices.field.target', copy)
        await expect(target).toBeEnabled()
        await target.click()
        await page.getByRole('option', { name: 'Keenetic' }).click()
        await expect(
          deviceField(page, 'deploy.field.account.keenetic', copy),
        ).toBeVisible()
        await expect(
          deviceField(page, 'deploy.field.interface.keenetic', copy),
        ).toBeVisible()
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
          refreshInterval(page, copy).getByRole('radio').nth(0),
        ).not.toBeChecked()
        for (const settled of ['settings.locale', 'settings.theme'])
          await expect(
            page
              .getByRole('group', { name: copy(settled) })
              .getByRole('radio')
              .first(),
          ).toBeEnabled()

        expect(await auditWidths(page, `${language}-settings-failed`)).toEqual(
          [],
        )
        await page.getByRole('button', { name: copy('action.retry') }).click()
        await expect(
          refreshInterval(page, copy).getByRole('radio').nth(1),
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
      await page.route('**/v1/lists', async (route) => {
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
        await page.goto(`${origin}/lists`)
        await expect(
          page.getByRole('heading', { level: 1, name: copy('lists.title') }),
        ).toBeVisible()
        await page
          .getByRole('button', {
            name: copy('lists.manageCategories'),
            exact: true,
          })
          .click()
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
        await expectLibraryCategory(page, copy('listPicker.filter.all'), copy)
        expect(categoryWrites).toHaveLength(1)

        expect(await auditWidths(page, `${language}-library-stale`)).toEqual([])
        await page.getByRole('button', { name: copy('lists.refresh') }).click()
        await expectLibraryCategory(page, categoryTitle, copy)
        await expect(
          page.getByRole('status').filter({ hasText: copy('lists.stale') }),
        ).toHaveCount(0)
        expect(categoryWrites).toHaveLength(1)
        assertProductAlive()
      } finally {
        failCatalogRead = false
        await page.unroute('**/v1/lists')
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
  await page.getByRole('button').evaluateAll((buttons) => {
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

      await page.goto(`${origin}/profiles/new`)
      await expect(
        page.getByRole('heading', {
          level: 1,
          name: copy('create.title'),
        }),
      ).toBeVisible()
      await page
        .getByRole('button', {
          name: copy('listDetail.open.aria').replace('{list}', 'Discord'),
        })
        .click()
      const listDialog = page.getByRole('dialog', { name: 'Discord' })
      await expect(
        listDialog.getByRole('heading', {
          name: copy('listCard.domains'),
        }),
      ).toBeVisible()
      // The card reads its sources when it opens, so the audit is taken once the
      // observed rows have landed: the busy notice and the full table are both in
      // the same pass.
      await expect(listDialog.getByText('dns-client').first()).toBeVisible({
        timeout: 60000,
      })
      violations.push(...(await auditWidths(page, `${language}-list-card`)))
      await listDialog
        .getByRole('button', { name: copy('action.close') })
        .last()
        .click()
      await page
        .getByRole('searchbox', { name: copy('create.search') })
        .fill('youtube')
      await expect(listMembership(page, 'YouTube')).toBeVisible()
      violations.push(...(await auditWidths(page, `${language}-builder`)))
      await page.screenshot({
        path: join(reviewRoot, `${language}-builder.png`),
        fullPage: true,
      })

      await listMembership(page, 'YouTube').check()
      await chooseFormat(page, /Keenetic/, copy)
      await page.getByRole('button', { name: copy('create.submit') }).click()
      await page.waitForURL(
        new RegExp(`^${escapeRegExp(origin)}/profiles/[a-f0-9]{32}.*$`),
      )
      await expect(
        page.getByRole('heading', { level: 1, name: 'YouTube' }),
      ).toBeVisible()

      // The profile audit is taken with an output bound and its subscription block
      // showing, so the outputs table and the secret disclosure are covered too.
      await expect(
        page.getByRole('heading', {
          name: copy('profile.subscription').replace('{target}', 'Keenetic'),
        }),
      ).toBeVisible()
      const createdRoutePath = new URL(page.url()).pathname
      violations.push(...(await auditWidths(page, `${language}-list`)))
      await page.screenshot({
        path: join(reviewRoot, `${language}-list.png`),
        fullPage: true,
      })

      await page
        .getByRole('link', { name: copy('profiles.title') })
        .first()
        .click()
      await expect(linkTo(page, createdRoutePath)).toBeVisible()
      violations.push(...(await auditWidths(page, `${language}-library`)))
      await page.screenshot({
        path: join(reviewRoot, `${language}-library.png`),
        fullPage: true,
      })

      await page.goto(`${origin}/lists`)
      await expect(
        page.getByRole('heading', { level: 1, name: copy('lists.title') }),
      ).toBeVisible()
      // The section is audited with a category open, because the pane is where its
      // rows and their menus live.
      await openLibraryCategory(page, copy('category.communication'), copy)
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
      await catalogDisclosure(page, copy).click()
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

// The hidden actions column header once escaped `.profiles__scroll` and
// stretched the document at 320px; the scroll box is its containing block now,
// and this test keeps it that way.
test('the populated library never scrolls sideways', async ({ page }) => {
  test.setTimeout(120000)
  await buildProfile(page)
  await page.getByRole('link', { name: 'Profiles' }).first().click()
  await expect(
    page.getByRole('link', { exact: true, name: 'Discord, YouTube' }).first(),
  ).toBeVisible()
  await assertNoOverflow(page, 'library')

  // A menu triggered at the bottom-right edge is a viewport overlay. It flips
  // above the trigger, remains wholly visible and does not enlarge the table's
  // own scroll box.
  await page.setViewportSize({ height: 300, width: 320 })
  const bottomTrigger = page
    .getByRole('button', { name: /Actions for profile/ })
    .last()
  await bottomTrigger.evaluate((element) =>
    element.scrollIntoView({ block: 'end', inline: 'end' }),
  )
  const triggerBox = await bottomTrigger.boundingBox()
  expect(triggerBox).not.toBeNull()
  const scrollBox = page.getByTestId('rv-profiles-scroll')
  const scrollHeight = await scrollBox.evaluate((element) =>
    Math.round(element.scrollHeight),
  )
  await bottomTrigger.click()
  const panel = menuPanel(page)
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

test('workspace pages share geometry and the category panel supports keyboard search', async ({
  page,
}) => {
  for (const width of [1366, 1920]) {
    await page.setViewportSize({ width, height: 900 })
    let reference: { x: number; width: number; background: string } | undefined
    for (const path of [
      '/profiles/new',
      '/lists',
      '/connections',
      '/settings',
      '/',
    ]) {
      await page.goto(`${origin}${path}`)
      await expect(page.getByRole('heading', { level: 1 })).toBeVisible()
      // The content column is the page's own landmark, and it is the box the
      // shell aligns; every section must place it identically.
      const geometry = await page.getByRole('main').evaluate((element) => ({
        x: element.getBoundingClientRect().x,
        width: element.getBoundingClientRect().width,
        background: getComputedStyle(document.documentElement).backgroundColor,
      }))
      reference ??= geometry
      expect(geometry).toEqual(reference)
      if (path === '/profiles/new' || path === '/lists') {
        const frame = page.getByTestId(
          path === '/profiles/new'
            ? 'rv-list-picker-frame'
            : 'rv-lists-workspace',
        )
        await expect(frame).toBeVisible()
        expect(
          await frame.evaluate(
            (element) => element.scrollWidth - element.clientWidth,
          ),
        ).toBe(0)
        expect(
          await page.evaluate(
            () => document.documentElement.scrollHeight - innerHeight,
          ),
        ).toBeLessThanOrEqual(1)
      }
    }
  }
  await page.goto(`${origin}/profiles/new`)
  const collapse = page.getByRole('button', { name: 'Collapse sidebar' })
  await collapse.click()
  // The control states the collapsed state itself, which is also what the
  // reload has to bring back.
  await expect(
    page.getByRole('button', { name: 'Expand sidebar' }),
  ).toBeVisible()
  await page.reload()
  await expect(
    page.getByRole('button', { name: 'Expand sidebar' }),
  ).toBeVisible()
  const nav = page.getByRole('link', { name: 'Lists', exact: true })
  await nav.focus()
  const tooltip = page.getByTestId('rv-tooltip')
  await expect(tooltip).toBeVisible()
  await expect(nav).toHaveAccessibleDescription('Lists')
  expect(
    await tooltip.evaluate((element) => {
      const rect = element.getBoundingClientRect()
      return element.contains(
        document.elementFromPoint(
          rect.x + rect.width / 2,
          rect.y + rect.height / 2,
        ),
      )
    }),
  ).toBe(true)
  await page.getByRole('button', { name: 'Expand sidebar' }).click()
  const more = categoryMore(page)
  await more.click()
  const search = page.getByRole('textbox', { name: 'Find a category' })
  await expect(search).toBeFocused()
  const panel = choicePanel(page, englishCopy('listPicker.collections'))
  await expect(panel).not.toHaveCSS('background-color', 'rgba(0, 0, 0, 0)')
  await expect(panel).toHaveCSS('border-top-width', '1px')
  await search.fill('no category matches this')
  await expect(page.getByText('No matching categories')).toBeVisible()
  await search.fill('Video')
  await search.press('ArrowDown')
  await search.press('Enter')
  await expect(search).toBeVisible()
  await search.press('Escape')
  await expect(search).toBeHidden()
  await expect(more).toBeFocused()
  await expect(bodyRows(page)).toHaveCount(1)
  await expect(bodyRows(page)).toContainText('YouTube')
  await more.click()
  await expect(page.getByRole('option', { name: 'Video' })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await search.fill('Communication')
  await search.press('Escape')
  await expect(search).toBeHidden()
  await expect(more).toBeFocused()
  await expect(bodyRows(page)).toContainText('YouTube')
})

test('the composition editor keeps its geometry across selections and shell breakpoints', async ({
  page,
}) => {
  await page.goto(`${origin}/profiles/new`)
  const table = page.getByTestId('rv-list-picker-frame')
  const rail = composerSettings(page)
  await expect(table).toBeVisible()

  await page.setViewportSize({ height: 900, width: 1440 })
  const tableBox = await table.boundingBox()
  const railBox = await rail.boundingBox()
  expect(railBox!.x).toBeGreaterThanOrEqual(tableBox!.x + tableBox!.width)
  await expect(
    page.getByRole('columnheader', { name: 'Category', exact: true }),
  ).toBeVisible()
  await expect(
    page.getByRole('columnheader', { name: 'Rules', exact: true }),
  ).toBeVisible()
  await expect(
    page.getByRole('button', { name: 'Create and prepare', exact: true }),
  ).toBeInViewport()
  // The reference is a dense table: a normal desktop row must not become
  // a 70px two-line card when the priority rail is beside it.
  const row = await bodyRows(page).first().boundingBox()
  expect(row!.height).toBeLessThanOrEqual(48)
  const before = await table.boundingBox()
  expect(before).not.toBeNull()
  // Which lists the table holds and where each row sits: membership must move
  // neither.
  const layout = async (): Promise<{ lists: string[]; tops: number[] }> => ({
    lists: await bodyRows(page).getByRole('rowheader').allInnerTexts(),
    tops: await bodyRows(page).evaluateAll((rows) =>
      rows.map((element) => element.getBoundingClientRect().y),
    ),
  })
  const settled = await layout()
  const checkbox = listMembership(page, 'Discord')
  await checkbox.check()
  await expect(checkbox).toBeFocused()
  expect(await layout()).toEqual(settled)
  const chips = await categoryChip(
    page,
    englishCopy('listPicker.filter.all'),
  ).boundingBox()
  const more = await categoryMore(page).boundingBox()
  expect(more!.height).toBe(chips!.height)
  const after = await table.boundingBox()
  expect(after).not.toBeNull()
  expect(after?.x).toBeCloseTo(before?.x ?? 0)
  expect(after?.y).toBeCloseTo(before?.y ?? 0)
  expect(after?.width).toBeCloseTo(before?.width ?? 0)
  expect(after?.height).toBeCloseTo(before?.height ?? 0)

  for (const width of [1280, 1024, 768, 320]) {
    await page.setViewportSize({ height: 900, width })
    if (width <= 768) {
      // Resizing also updates the measured quick-filter count. Compare both
      // boxes in one layout snapshot after that reactive update settles.
      await expect
        .poll(async () => {
          const stacked = await table.boundingBox()
          const settings = await rail.boundingBox()
          return (
            (settings?.y ?? 0) - ((stacked?.y ?? 0) + (stacked?.height ?? 0))
          )
        })
        .toBeGreaterThanOrEqual(0)
    }
    expect(
      await table.evaluate(
        (element) => element.scrollWidth - element.clientWidth,
      ),
    ).toBeLessThanOrEqual(1)
    await assertNoOverflow(page, `composition-editor-${width}`)
  }
})

test('the composition editor remains operable with text enlarged to 200%', async ({
  page,
}) => {
  await page.goto(`${origin}/profiles/new`)
  await page.evaluate(() => {
    document.documentElement.style.fontSize = '200%'
  })

  await expect(
    page.getByRole('heading', {
      level: 1,
      name: 'New profile',
    }),
  ).toBeVisible()
  await expect(page.getByRole('table')).toBeVisible()
  expect(await fits(page)).toBe(true)

  const search = page.getByRole('searchbox', { name: 'Find a list' })
  await search.fill('Discord')
  await listMembership(page, 'Discord').check()
  await expect(page.getByText('1 list in the profile')).toBeVisible()
  expect(await fits(page)).toBe(true)
})

/**
 * buildList walks the surface the way an operator does — a profile holding
 * Discord and YouTube, published as a Keenetic output — and leaves the page on
 * the published profile. It answers with the profile and output identities, since
 * every output-scoped mutation and address names them rather than the profile
 * alone.
 */
const buildProfile = async (
  page: Page,
): Promise<{ profileId: string; outputId: string }> => {
  await page.goto(`${origin}/profiles/new`)
  await expect(
    page.getByRole('heading', {
      level: 1,
      name: 'New profile',
    }),
  ).toBeVisible()
  const search = page.getByRole('searchbox', { name: 'Find a list' })
  await search.fill('discord')
  await listMembership(page, 'Discord').check()
  await search.fill('youtube')
  await listMembership(page, 'YouTube').check()
  await chooseFormat(page, /Keenetic/)
  const outputsResponse = page.waitForResponse(
    (candidate) =>
      candidate.request().method() === 'POST' &&
      /\/v1\/profiles\/[a-f0-9]{32}\/outputs$/.test(
        new URL(candidate.url()).pathname,
      ),
  )
  await page.getByRole('button', { name: 'Create and prepare' }).click()
  await page.waitForURL(
    new RegExp(`^${escapeRegExp(origin)}/profiles/[a-f0-9]{32}.*$`),
  )
  await expect(
    page.getByRole('heading', { level: 1, name: 'Discord, YouTube' }),
  ).toBeVisible()
  const profileId = profileIDFromURL(page.url())
  const outputPayload = (await (await outputsResponse).json()) as {
    output: { id: string }
  }
  await expect(
    page.getByRole('heading', { name: 'Subscription link · Keenetic' }),
  ).toBeVisible()
  await expect(addConnectionField(page)).toBeEnabled()
  await page
    .getByRole('tablist', { name: 'Profile sections' })
    .getByRole('tab', { name: 'Contents' })
    .click()

  return { profileId, outputId: outputPayload.output.id }
}

/**
 * What the filter row says it is filtered by.
 *
 * The row reports its choice as a pressed chip, and the chip states the
 * category and then its size — so the category is what its name starts with. A
 * category the row has no room to show as a chip is stated by the More control
 * instead, whose whole name is the collection heading and that category.
 */
const expectLibraryCategory = async (
  page: Page,
  category: string,
  copy: (key: string) => string = englishCopy,
): Promise<void> => {
  await expect(
    categoryFilters(page, copy)
      .getByRole('button', {
        name: new RegExp(`^${escapeRegExp(category)}`),
        pressed: true,
      })
      .or(
        page.getByRole('button', {
          name: new RegExp(
            `^${escapeRegExp(copy('listPicker.collections'))}: ${escapeRegExp(category)}$`,
          ),
        }),
      ),
  ).toBeVisible()
}

/** One category's pane in «Списки», opened the way the column opens it. */
const openLibraryCategory = async (
  page: Page,
  category: string,
  copy: (key: string) => string = englishCopy,
): Promise<void> => {
  await categoryChip(page, copy('listPicker.filter.all'), copy).click()
  await categoryMore(page, copy).click()
  await page.getByRole('option', { name: category }).click()
  await page.keyboard.press('Escape')
  await expectLibraryCategory(page, category, copy)
}

/**
 * The action menu of one category. Every act it offers is global, so it lives
 * in «Списки» and nowhere else (ADR 0029).
 */
const openCategoryActions = async (
  page: Page,
  category: string,
): Promise<void> => {
  await openLibraryCategory(page, category)
  await page.getByRole('button', { name: 'Categories', exact: true }).click()
  await page
    .getByRole('button', { name: `Actions for category ${category}` })
    .click()
}

/**
 * Whether a panel was drawn by its own component or merely placed. Both facts
 * come from one read, so a panel that lost its stylesheet fails on the ground
 * it should have had rather than on a coincidence of geometry.
 */
const assertPainted = async (panel: Locator, name: string): Promise<void> => {
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

/**
 * The connection format is one searchable list, so a test chooses it the way an
 * operator does: open the list, take the option that names the format. Every
 * format's projected size is read from the same list, because that is where it
 * is stated. The field is captioned in the operator's own language, so a
 * localized suite hands its dictionary in.
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
    const created = await page.request.post(`${origin}/v1/lists`, {
      headers,
      data: { title, domains },
    })
    expect(created.status()).toBe(201)
    const { list: list } = (await created.json()) as {
      list: { id: string }
    }
    ids.push(list.id)
    if (values.length > 0)
      expect(
        (
          await page.request.post(`${origin}/v1/lists/${list.id}/domains`, {
            headers,
            data: { values, verdict: 'include' },
          })
        ).ok(),
      ).toBe(true)
    expect(
      (
        await page.request.post(`${origin}/v1/lists/${list.id}/refresh`, {
          headers,
          data: {},
        })
      ).ok(),
    ).toBe(true)
  }
  const beforeRoutes = await (
    await page.request.get(`${origin}/v1/profiles`)
  ).text()
  const beforeContents = await Promise.all(
    ids.map(async (id) =>
      (await page.request.get(`${origin}/v1/lists/${id}/contents`)).text(),
    ),
  )
  let inspecting = true
  const writes: string[] = []
  const errors: string[] = []
  page.on('pageerror', (error) => errors.push(error.message))
  page.on('request', (request) => {
    const path = new URL(request.url()).pathname
    if (inspecting && request.method() === 'POST' && path !== FORECAST_PATH)
      writes.push(path)
  })
  await page.goto(`${origin}/profiles/new`)
  const select = async (index: number, title: string, checked: boolean) => {
    await page.getByRole('searchbox', { name: 'Find a list' }).fill(title)
    await listMembership(page, title).setChecked(checked)
  }
  await select(0, 'Overlap Alpha', true)
  await select(1, 'Overlap Beta', true)
  await page.getByRole('searchbox', { name: 'Find a list' }).fill('')
  await chooseFormat(page, /sing-box/)
  const alphaRow = listRow(page, 'Overlap Alpha')
  const betaRow = listRow(page, 'Overlap Beta')
  await expect(alphaRow).toContainText('Overlap: Overlap Beta')
  // The count is the control that lists the overlaps, and it starts at the
  // leading edge of the cell it sits in rather than at an inherited indent.
  const countInset = await alphaRow
    .getByRole('button', { name: 'Overlap: Overlap Beta', exact: true })
    .evaluate((count) => {
      const cell = count.closest('td')!
      const range = document.createRange()
      range.selectNodeContents(count)
      return (
        range.getBoundingClientRect().left -
        cell.getBoundingClientRect().left -
        Number.parseFloat(getComputedStyle(cell).paddingLeft)
      )
    })
  expect(Math.abs(countInset)).toBeLessThanOrEqual(1)
  await expect(betaRow).toContainText('Overlap: Overlap Alpha')
  await alphaRow
    .getByRole('button', { name: 'Overlap: Overlap Beta', exact: true })
    .click()
  await expect(page.getByRole('dialog')).toContainText('Overlaps with lists')
  await expect(page.getByRole('dialog').getByRole('listitem')).toHaveText(
    'Overlap Beta',
  )
  await page.keyboard.press('Escape')
  await expect(
    page.getByRole('button', { name: 'How overlaps are resolved' }),
  ).toHaveCount(0)
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
      path: join(reviewRoot, `overlap-tags-create-${width}.png`),
      fullPage: true,
    })
  }
  let fail = false
  let release = () => {}
  let held: Promise<void> | undefined
  await page.route(`**${FORECAST_PATH}`, async (route) => {
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
    await expect(forecastStatus(page, 'Calculating overlaps…')).toBeVisible()
    const pendingTableY = (await page
      .getByTestId('rv-list-picker-frame')
      .boundingBox())!.y
    const recalculated = page.waitForResponse(
      (response) => new URL(response.url()).pathname === FORECAST_PATH,
    )
    release()
    held = undefined
    const recalculatedResponse = await recalculated
    expect(
      recalculatedResponse.status(),
      await recalculatedResponse.text(),
    ).toBe(200)
    await expect(forecastStatus(page, 'Calculating overlaps…')).toBeHidden()
    expect(
      (await page.getByTestId('rv-list-picker-frame').boundingBox())!.y,
    ).toBeCloseTo(pendingTableY, 0)
    fail = true
    const failed = page.waitForResponse(
      (response) => new URL(response.url()).pathname === FORECAST_PATH,
    )
    await select(2, 'Overlap Gamma', false)
    expect((await failed).status()).toBe(503)
    await expect(
      page.getByText('Calculation unavailable. Try again.'),
    ).toBeVisible()
    const retry = page.getByRole('button', {
      name: 'Recalculate',
      exact: true,
    })
    await expect(retry).toBeVisible()
    fail = false
    await retry.click()
    await page.getByRole('searchbox', { name: 'Find a list' }).fill('')
    await expect(
      alphaRow.getByRole('button', {
        name: 'Overlap: Overlap Beta',
        exact: true,
      }),
    ).toHaveCount(1)
    await select(2, 'Overlap Gamma', true)
    await select(1, 'Overlap Beta', false)
    await page.getByRole('searchbox', { name: 'Find a list' }).fill('')
    await expect(
      alphaRow.getByRole('button', {
        name: 'Overlap: Overlap Beta',
        exact: true,
      }),
    ).toHaveCount(0)
    await select(1, 'Overlap Beta', true)
    await select(2, 'Overlap Gamma', false)
    await page.getByRole('searchbox', { name: 'Find a list' }).fill('')
    await expect(
      alphaRow.getByRole('button', {
        name: 'Overlap: Overlap Beta',
        exact: true,
      }),
    ).toHaveCount(1)
  } finally {
    release()
  }
  expect(writes).toEqual([])
  expect(await (await page.request.get(`${origin}/v1/profiles`)).text()).toBe(
    beforeRoutes,
  )
  for (const [index, id] of ids.entries())
    expect(
      await (
        await page.request.get(`${origin}/v1/lists/${id}/contents`)
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
  // The saved profile's own composition tab holds the editor.
  const editor = page.getByRole('tabpanel', { name: 'Contents' })
  await expect(listRow(editor, 'Overlap Alpha')).toContainText(
    'Overlap: Overlap Beta',
  )
  await expect(
    editor.getByRole('button', { name: 'How overlaps are resolved' }),
  ).toHaveCount(0)
  await expect(
    editor.getByRole('button', { name: 'Save and rebuild', exact: true }),
  ).toBeDisabled()
  expect(errors).toEqual([])
  assertProductAlive()
})

const formatList = (page: Page): Locator => page.getByRole('listbox')

test('source skips are visible without changing profiles, and clear after a clean refresh', async ({
  page,
}) => {
  const headers = { 'X-Routevane-Request': '1' }
  const created = await page.request.post(`${origin}/v1/lists`, {
    headers,
    data: { title: 'Source diagnostic fixture', domains: ['notice.example'] },
  })
  expect(created.ok()).toBe(true)
  const { list: list } = (await created.json()) as {
    list: { id: string }
  }
  const before = await (await page.request.get(`${origin}/v1/profiles`)).text()
  let skipped = 1
  let failed = false
  await page.route(`**/v1/lists/${list.id}/refresh`, async (route) => {
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
    await page.goto(`${origin}/lists#list=${list.id}`)
    const card = page.getByRole('dialog', {
      name: 'Source diagnostic fixture',
      exact: true,
    })
    const refresh = card.getByRole('button', { name: 'Refresh from sources' })
    const firstRefresh = page.waitForResponse(
      (response) =>
        response.request().method() === 'POST' &&
        new URL(response.url()).pathname === `/v1/lists/${list.id}/refresh`,
    )
    await refresh.click()
    expect((await firstRefresh).ok()).toBe(true)
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
      dictionaries.en['listCard.refresh.failed.generic'] as string,
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
    expect(await (await page.request.get(`${origin}/v1/profiles`)).text()).toBe(
      before,
    )
    await expect(
      card.getByRole('button', { name: 'Close' }).last(),
    ).toBeEnabled()
    await page.keyboard.press('Escape')
    await expect(card).toBeHidden()
  } finally {
    const removed = await page.request.post(
      `${origin}/v1/lists/${list.id}/remove`,
      { headers, data: {} },
    )
    expect(removed.status()).toBe(204)
  }
})

for (const language of ['en', 'ru'] as const) {
  test.describe(`card regression ${language}`, () => {
    test.use({ locale: language === 'ru' ? 'ru-RU' : 'en-US' })
    const copy = copyFor(language)

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
      const created = await page.request.post(`${origin}/v1/lists`, {
        headers,
        data: { domains, title },
      })
      expect(created.status()).toBe(201)
      const { list: list } = (await created.json()) as {
        list: { id: string }
      }
      const disabled = await page.request.post(
        `${origin}/v1/lists/${list.id}/domains`,
        {
          headers,
          data: { values: ['row-36.example'], verdict: 'exclude' },
        },
      )
      expect(disabled.status()).toBe(200)

      try {
        await page.goto(`${origin}/profiles/new`)
        await page
          .getByRole('searchbox', { name: copy('create.search') })
          .fill(title)
        await page
          .getByRole('button', {
            name: copy('listDetail.open.aria').replace('{list}', title),
          })
          .click()
        const compose = page.getByRole('dialog', { name: title, exact: true })
        await expect(
          compose.getByRole('heading', { name: copy('listCard.domains') }),
        ).toBeVisible()
        const rows = cardContents(compose)
        await expect(cardRows(compose)).toHaveCount(domains.length)
        const disabledLibraryRow = cardRow(compose, 'row-36.example')
        await expect(disabledLibraryRow).toHaveClass(/list-card__row--disabled/)
        await expect(
          disabledLibraryRow.getByText(
            copy('listCard.domains.disabledInLibrary'),
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
          name: copy('listCard.filter'),
        })
        for (const width of [320, 1919]) {
          await page.setViewportSize({ width, height: 900 })
          // ResizeObserver moves the card between modal and workspace surfaces.
          // Measure after the requested mode has mounted, not the outgoing tree.
          if (width === 1919)
            await expect(compose).toHaveClass(/rv-dialog--docked/)
          else await expect(compose).not.toHaveClass(/rv-dialog--docked/)
          await filter.fill('')
          await expect(cardRows(compose)).toHaveCount(domains.length)
          const normalRowBox = await cardRow(
            compose,
            'match-one.example',
          ).boundingBox()
          expect(normalRowBox).not.toBeNull()
          const referenceHeight = normalRowBox!.height
          await filter.fill('match-one.example')
          await expect(cardRows(compose)).toHaveCount(1)
          const oneRowsBox = await rows.boundingBox()
          const oneRowBox = await cardRows(compose).first().boundingBox()
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
          await expect(cardRows(compose)).toHaveCount(2)
          for (const row of await cardRows(compose).all()) {
            expect(
              Math.abs((await row.boundingBox())!.height - referenceHeight),
            ).toBeLessThanOrEqual(1)
          }
          await filter.fill('long')
          await expect(cardRows(compose)).toHaveCount(1)
          const longRowBox = await cardRows(compose).first().boundingBox()
          if (width === 320)
            expect(longRowBox?.height ?? 0).toBeGreaterThan(referenceHeight)

          await filter.fill('no-such-card-entry')
          await expect(cardRows(compose)).toHaveCount(0)
          await expect(
            compose.getByText(copy('listCard.filter.empty')),
          ).toBeVisible()
          await filter.fill('')
          await expect(cardRows(compose)).toHaveCount(domains.length)
        }
        // Composing keeps its footer, because membership is the one act this
        // flow owns.
        await expect(
          compose.getByRole('button', { name: copy('listDetail.add') }),
        ).toBeVisible()
        await compose
          .getByRole('button', { name: copy('action.close') })
          .last()
          .click()

        // The same dense table and long-value wrapping apply in the library flow;
        // it has no profile footer because it owns the list rather than membership.
        await page.goto(`${origin}/lists#list=${list.id}`)
        const library = page.getByRole('dialog', { name: title, exact: true })
        await expect(
          library.getByRole('heading', { name: copy('listCard.domains') }),
        ).toBeVisible()
        const libraryFilter = library.getByRole('searchbox', {
          name: copy('listCard.filter'),
        })
        const libraryRows = cardContents(library)
        await expect(cardRows(library)).toHaveCount(domains.length)

        // The action remains recognizable while one separate status line owns
        // the pending message. Every dismissal path stays unavailable until
        // the refresh has a result.
        let releaseRefresh!: () => void
        const refreshHeld = new Promise<void>((resolve) => {
          releaseRefresh = resolve
        })
        let refreshRequests = 0
        const refreshPath = `**/v1/lists/${list.id}/refresh`
        await page.route(refreshPath, async (route) => {
          refreshRequests += 1
          await refreshHeld
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({ refresh: { skipped_entries: 0 } }),
          })
        })
        // The action names both the act and how many sources it reads, which
        // is what keeps it recognizable while it works.
        const refresh = library.getByRole('button', {
          name: new RegExp(`^${escapeRegExp(copy('listCard.refresh'))}: `),
        })
        await expect(refresh).toHaveAccessibleName(
          new RegExp(`^${escapeRegExp(copy('listCard.refresh'))}:`),
        )
        const libraryClose = library
          .getByRole('button', { name: copy('action.close') })
          .last()
        try {
          await refresh.click()
          await expect(refresh).not.toHaveAttribute('aria-busy', 'true')
          await expect(refresh).toBeDisabled()
          await expect(refresh.getByTestId('rv-button-spinner')).toHaveCount(0)
          await expect(
            library.getByText(copy('listCard.refresh.busy'), {
              exact: true,
            }),
          ).toHaveCount(1)
          await expect(refresh.getByTestId('rv-icon')).toHaveCount(1)
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
          if (
            (await library.getAttribute('class'))?.includes('rv-dialog--docked')
          ) {
            await expect(page.getByTestId('rv-workspace-main')).toHaveAttribute(
              'inert',
            )
            const other = await libraryRowName(
              bodyRows(libraryRegion(page, copy)).first(),
            ).boundingBox()
            await page.mouse.click(
              other!.x + other!.width / 2,
              other!.y + other!.height / 2,
            )
            await expect(library).toHaveAccessibleName(title)
          } else {
            await dialogScrims(page).last().dispatchEvent('pointerdown')
          }
          await expect(library).toBeVisible()
        } finally {
          releaseRefresh()
        }
        await expect(refresh).not.toHaveAttribute('aria-busy', 'true')
        await expect(refresh).toBeEnabled()
        await expect(page.getByTestId('rv-workspace-main')).not.toHaveAttribute(
          'inert',
        )
        await page.unroute(refreshPath)

        await page.evaluate(() => document.fonts.ready.then(() => true))
        const normalLibraryRow = await cardRow(
          library,
          'match-two.example',
        ).boundingBox()
        await libraryFilter.fill('match-two.example')
        await expect(cardRows(library)).toHaveCount(1)
        const libraryRowsBox = await libraryRows.boundingBox()
        const libraryRowBox = await cardRows(library).first().boundingBox()
        expect(libraryRowsBox).not.toBeNull()
        expect(libraryRowBox).not.toBeNull()
        expect(
          Math.abs(libraryRowBox!.height - normalLibraryRow!.height),
        ).toBeLessThanOrEqual(1)
        expect(
          (libraryRowBox?.y ?? 0) - (libraryRowsBox?.y ?? 0),
        ).toBeLessThanOrEqual(2)
        // The library flow owns the list rather than membership, so the act
        // that would change membership is not offered here at all.
        await expect(
          library.getByRole('button', {
            name: new RegExp(
              `^(${escapeRegExp(copy('listDetail.add'))}|${escapeRegExp(copy('listDetail.remove'))})$`,
            ),
          }),
        ).toHaveCount(0)
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
          await expect(cardRows(library)).toHaveCount(1)
          const row = cardRows(library).first()
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
            name: copy('listCard.remove'),
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
        const savePath = `**/v1/lists/${list.id}/update`
        const renamedTitle = `${title} renamed`
        await page.route(savePath, async (route) => {
          await saveHeld
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({
              list: { id: list.id, title: renamedTitle, domains },
            }),
          })
        })
        const save = library.getByRole('button', {
          name: copy('listCard.title.save'),
        })
        try {
          // The section that holds the field is announced by the same caption,
          // so the field is addressed as the control it is.
          await titleField(library, copy).fill(renamedTitle)
          await save.click()
          await expect(save).toHaveAttribute('aria-busy', 'true')
          await expect(save.getByTestId('rv-button-spinner')).toHaveCount(1)
          await expect(libraryClose).toBeDisabled()
          await page.keyboard.press('Escape')
          await expect(library).toBeVisible()
          await dialogScrims(page).last().dispatchEvent('pointerdown')
          await expect(library).toBeVisible()
        } finally {
          releaseSave()
        }
        // A successful rename changes the dialog's accessible name in place.
        // Resolve it again by its committed identity instead of keeping the
        // locator rooted in the obsolete title.
        const renamedLibrary = page.getByRole('dialog', {
          name: renamedTitle,
          exact: true,
        })
        await expect(renamedLibrary).toBeVisible()
        await expect(
          renamedLibrary.getByRole('button', {
            name: copy('listCard.title.save'),
          }),
        ).not.toHaveAttribute('aria-busy', 'true')
        await expect(titleField(renamedLibrary, copy)).toHaveValue(renamedTitle)
        await page.unroute(savePath)
      } finally {
        const removed = await page.request.post(
          `${origin}/v1/lists/${list.id}/remove`,
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
      const daily = segmentOption(page, copy('settings.refresh.daily'))
      const weekly = segmentOption(page, copy('settings.refresh.weekly'))
      await expect(daily).toBeChecked()
      for (const success of [false, true]) {
        await pressSegment(page, copy('settings.refresh.weekly'))
        await expect(
          page
            .getByRole('status')
            .filter({ hasText: copy('settings.refresh.saving') }),
        ).toBeVisible()
        await expect(daily).toBeChecked()
        await expect(weekly).not.toBeChecked()
        await expect(daily).toBeDisabled()
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

    test('card audit: compose source reads retry in place without a profile write', async ({
      page,
    }) => {
      test.setTimeout(60000)
      page.setDefaultTimeout(15000)
      const headers = { 'X-Routevane-Request': '1' }
      const title = 'Card source retry fixture'
      const created = await page.request.post(`${origin}/v1/lists`, {
        headers,
        data: { domains: ['retry.example'], title },
      })
      expect(created.status()).toBe(201)
      const { list: list } = (await created.json()) as {
        list: { id: string }
      }
      const source = await page.request.post(
        `${origin}/v1/lists/${list.id}/sources`,
        {
          headers,
          data: { format: 'text', url: 'https://feed.example/retry.txt' },
        },
      )
      expect(source.ok()).toBe(true)
      const contentsResponse = await page.request.get(
        `${origin}/v1/lists/${list.id}/contents`,
      )
      expect(contentsResponse.ok()).toBe(true)
      const contents = (await contentsResponse.json()) as {
        observed: boolean
        rows: unknown[]
        list_id: string
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
      await page.route(`**/v1/lists/${list.id}/contents`, async (route) => {
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
      })
      await page.route(`**/v1/lists/${list.id}/refresh`, async (route) => {
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
      })

      try {
        await page.setViewportSize({ width: 320, height: 900 })
        const beforeRoutes = await (
          await page.request.get(`${origin}/v1/profiles`)
        ).text()
        await page.goto(`${origin}/profiles/new`)
        await page
          .getByRole('searchbox', { name: copy('create.search') })
          .fill(title)
        await page
          .getByRole('button', {
            name: copy('listDetail.open.aria').replace('{list}', title),
          })
          .click()
        const card = page.getByRole('dialog', { name: title, exact: true })
        const close = card
          .getByRole('button', { name: copy('action.close') })
          .last()
        await expect(
          card.getByText(copy('listCard.loading'), { exact: true }),
        ).toBeVisible()
        // Initial reading can be dismissed; actual source work below stays guarded.
        await expect(close).toBeEnabled()
        releaseInitialContents()
        await expect(
          card.getByRole('heading', { name: copy('listCard.domains') }),
        ).toBeVisible()
        await expect(card.getByText(copy('listCard.observing'))).toBeVisible()
        await expect(close).toBeDisabled()
        await page.keyboard.press('Escape')
        await expect(card).toBeVisible()
        await dialogScrims(page).last().dispatchEvent('pointerdown')
        await expect(card).toBeVisible()
        const pendingFilterBox = await card
          .getByRole('searchbox', { name: copy('listCard.filter') })
          .boundingBox()
        const pendingRefreshStatusBox = await card
          .getByTestId('rv-list-card-status')
          .boundingBox()
        expect(pendingFilterBox).not.toBeNull()
        expect(pendingRefreshStatusBox).not.toBeNull()
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
          copy('listCard.refresh.failed.generic'),
        )
        const failedFilterBox = await card
          .getByRole('searchbox', { name: copy('listCard.filter') })
          .boundingBox()
        const failedRefreshStatusBox = await card
          .getByTestId('rv-list-card-status')
          .boundingBox()
        expect(failedFilterBox).not.toBeNull()
        expect(failedRefreshStatusBox).not.toBeNull()
        const naturalStatusGrowth = Math.max(
          0,
          (failedRefreshStatusBox?.height ?? 0) -
            (pendingRefreshStatusBox?.height ?? 0),
        )
        expect(
          Math.abs(
            (failedFilterBox?.y ?? 0) -
              (pendingFilterBox?.y ?? 0) -
              naturalStatusGrowth,
          ),
        ).toBeLessThanOrEqual(1)
        await expect(
          card.getByRole('button', { name: copy('action.retry'), exact: true }),
        ).toBeVisible()
        expect(await auditWidths(page, `${language}-card-failed`)).toEqual([])
        await page.screenshot({
          path: join(reviewRoot, `${language}-card-failed-320.png`),
        })
        const filter = card.getByRole('searchbox', {
          name: copy('listCard.filter'),
        })
        await filter.fill('retry.example')
        await expect(cardRows(card)).toHaveCount(1)
        await card
          .getByRole('button', { name: copy('action.retry'), exact: true })
          .click()
        await expect(card.getByRole('alert')).toHaveCount(0)
        await expect(
          card.getByRole('img', { name: copy('listCard.refresh.ready') }),
        ).toBeVisible()
        const recoveredFilterBox = await card
          .getByRole('searchbox', { name: copy('listCard.filter') })
          .boundingBox()
        const recoveredRefreshStatusBox = await card
          .getByTestId('rv-list-card-status')
          .boundingBox()
        expect(recoveredFilterBox).not.toBeNull()
        expect(recoveredRefreshStatusBox).not.toBeNull()
        // Recovery replaces the localized reading status with the compact ready
        // icon, so the reserved row's own height changes with the translation
        // and the platform's wrapping. The filter must follow that change
        // exactly, which is what proves nothing else moved it.
        const recoveredStatusChange =
          (recoveredRefreshStatusBox?.height ?? 0) -
          (pendingRefreshStatusBox?.height ?? 0)
        expect(
          Math.abs(
            (recoveredFilterBox?.y ?? 0) -
              (pendingFilterBox?.y ?? 0) -
              recoveredStatusChange,
          ),
        ).toBeLessThanOrEqual(1)
        await expect(filter).toHaveValue('retry.example')
        await expect(cardRows(card)).toHaveCount(1)
        expect(refreshCalls).toBe(2)
        expect(
          await (await page.request.get(`${origin}/v1/profiles`)).text(),
        ).toBe(beforeRoutes)
      } finally {
        releaseInitialContents()
        releaseFirstRefresh()
        await page.unroute(`**/v1/lists/${list.id}/contents`)
        await page.unroute(`**/v1/lists/${list.id}/refresh`)
        const removed = await page.request.post(
          `${origin}/v1/lists/${list.id}/remove`,
          { headers, data: {} },
        )
        expect(removed.status()).toBe(204)
      }
    })
  })
}

const openFormats = async (
  page: Page,
  copy: (key: string) => string = englishCopy,
): Promise<Locator> => {
  await deliveryField(page, copy).click()
  await expect(formatList(page)).toBeVisible()
  return formatList(page)
}

const chooseFormat = async (
  page: Page,
  name: RegExp,
  copy: (key: string) => string = englishCopy,
): Promise<void> => {
  await openFormats(page, copy)
  await page.getByRole('option', { name }).click()
  await expect(deliveryField(page, copy)).not.toHaveText(
    copy('create.target.placeholder'),
  )
}

/**
 * Whether a mutation is part of a flow these tests state.
 *
 * Two reads are not: the list card observes a list's sources as soon as it
 * opens, and a forecast observes an unread draft's lists once so it can be
 * weighed. Both change what a list knows, neither changes a profile, and neither
 * is anything the operator asked for by name.
 */
const statesFlow = (path: string): boolean =>
  !path.startsWith('/v1/lists/') && path !== FORECAST_PATH

const profileFlow = (
  mutations: { body: string | null; path: string }[],
): { body: string | null; path: string }[] =>
  mutations.filter((mutation) => statesFlow(mutation.path))

/**
 * One option of a segmented choice, by the words it is announced with. The
 * radio carries the name and the state.
 */
const segmentOption = (scope: Scope, label: string): Locator =>
  scope.getByRole('radio', { name: label, exact: true })

/**
 * Presses one segmented option. The radio itself is a clipped pixel behind its
 * own label, so what an operator presses — and what the browser turns into a
 * change on the radio — is the label's words.
 */
const pressSegment = async (scope: Scope, label: string): Promise<void> => {
  await scope.getByText(label, { exact: true }).click()
}

/** The interval a profile without a rule of its own follows. */
const refreshInterval = (
  page: Page,
  copy: (key: string) => string = englishCopy,
): Locator => page.getByRole('group', { name: copy('settings.refresh.label') })

/** The composer's name field, which the editor captions the same way. */
const nameField = (scope: Scope): Locator =>
  scope.getByLabel(englishCopy('create.name'), { exact: true })

/** The field that states where a profile is delivered. */
const deliveryField = (
  scope: Scope,
  copy: (key: string) => string = englishCopy,
): Locator => scope.getByLabel(copy('create.target'))

/** The field a published profile adds another connection with. */
const addConnectionField = (
  scope: Scope,
  copy: (key: string) => string = englishCopy,
): Locator => scope.getByLabel(copy('outputs.add'), { exact: true })

/** The choice that says how often a published profile is refreshed. */
const scheduleField = (
  page: Page,
  copy: (key: string) => string = englishCopy,
): Locator =>
  page.getByRole('combobox', { name: copy('profile.schedule'), exact: true })

/** One field of the connection form, by the caption its target asks for. */
const deviceField = (
  scope: Scope,
  key: string,
  copy: (key: string) => string = englishCopy,
): Locator => scope.getByLabel(copy(key), { exact: true })

/** The panel a searchable choice opens, by the heading it announces. */
const choicePanel = (page: Page, heading: string): Locator =>
  page.getByRole('dialog', { name: heading })

/** The field that narrows a searchable choice to what was typed. */
const choiceSearch = (
  page: Page,
  copy: (key: string) => string = englishCopy,
): Locator =>
  page.getByRole('textbox', { name: copy('choice.search'), exact: true })

/** The library section of «Списки», by the heading it is announced with. */
const libraryRegion = (
  page: Page,
  copy: (key: string) => string = englishCopy,
): Locator =>
  page.getByRole('region', { name: copy('lists.title'), exact: true })

/** The filter row the library and the composer share. */
const categoryFilters = (
  page: Page,
  copy: (key: string) => string = englishCopy,
): Locator => page.getByRole('group', { name: copy('listPicker.filter.label') })

/**
 * The control that offers the categories the filter row has no room to show as
 * chips. It announces the collection heading and then its own choice.
 */
const categoryMore = (
  page: Page,
  copy: (key: string) => string = englishCopy,
): Locator =>
  categoryFilters(page, copy).getByRole('button', {
    name: new RegExp(`^${escapeRegExp(copy('listPicker.collections'))}: `),
  })

/** One quick-filter chip, which states its category and then its size. */
const categoryChip = (
  page: Page,
  category: string,
  copy: (key: string) => string = englishCopy,
): Locator =>
  categoryFilters(page, copy).getByRole('button', { name: category })

/** The refresh the composer offers for the lists a draft already holds. */
const compositionRefresh = (
  page: Page,
  copy: (key: string) => string = englishCopy,
): Locator =>
  page.getByRole('button', { name: copy('listCard.refresh'), exact: true })

/** The settings rail beside the composition table. */
const composerSettings = (
  page: Page,
  copy: (key: string) => string = englishCopy,
): Locator => page.getByRole('complementary', { name: copy('create.settings') })

/** The control on a row that opens the list it stands for. */
const openList = (
  scope: Scope,
  list: string,
  copy: (key: string) => string = englishCopy,
): Locator =>
  scope.getByRole('button', {
    name: copy('listDetail.open.aria').replace('{list}', list),
  })

/** The disclosure that holds the catalog of everything this build can reach. */
const catalogDisclosure = (
  page: Page,
  copy: (key: string) => string = englishCopy,
): Locator => page.getByRole('button', { name: copy('targets.title') })

const documentLanguage = (page: Page): Promise<string> =>
  page.evaluate(() => document.documentElement.lang)

type AuditFinding = {
  detail: string
  rule: string
  screen: string
  targets: string[]
}

const audit = async (page: Page, screen: string): Promise<AuditFinding[]> => {
  // Measure settled contrast, not the intermediate opacity of an opening panel.
  // Loading indicators may run indefinitely; they are still audited as drawn.
  await page.evaluate(async () => {
    await Promise.all(
      document
        .getAnimations()
        .filter(
          (animation) =>
            animation.effect?.getComputedTiming().iterations !== Infinity,
        )
        .map((animation) => animation.finished.catch(() => {})),
    )
  })
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

const auditWidths = async (
  page: Page,
  screen: string,
): Promise<AuditFinding[]> => {
  const findings: AuditFinding[] = []
  const previous = page.viewportSize()
  for (const width of [320, 768, 1024, 1440]) {
    await page.setViewportSize({ height: 900, width })
    if (!(await fits(page))) {
      await page.screenshot({
        path: join(reviewRoot, `${screen}-${width}-overflow.png`),
        fullPage: true,
      })
      // A diagnostic dump rather than an assertion: every descendant of the
      // content column is measured in the page, so this reads the document
      // directly instead of addressing elements one at a time.
      const bounds = await page.evaluate(() =>
        [...document.querySelectorAll('main *')].flatMap((element) => {
          const rect = element.getBoundingClientRect()
          return rect.right > document.documentElement.clientWidth &&
            rect.width > 0
            ? [
                {
                  tag: element.tagName,
                  className: String(element.className),
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
    const smallControls = await pressableTargets(page).evaluateAll((controls) =>
      controls.flatMap((control) => {
        // A native input drawn as one clipped pixel is not the target: the
        // label around it is, and a file input behind its own button has none,
        // so nothing there is pressed directly.
        const target =
          control instanceof HTMLInputElement
            ? control.closest('label')
            : control
        if (target === null) return []
        const bounds = target.getBoundingClientRect()
        const style = getComputedStyle(target)
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
                  target.getAttribute('aria-label') ??
                  target.textContent?.trim(),
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

const assertNoOverflow = async (page: Page, screen: string): Promise<void> => {
  for (const width of [320, 768, 1024, 1440]) {
    await page.setViewportSize({ height: 900, width })
    expect(await fits(page), `${screen} overflows at ${width}px`).toBe(true)
  }
  await page.setViewportSize({ height: 900, width: 1280 })
}

const fits = (page: Page): Promise<boolean> =>
  page.evaluate(
    () =>
      document.documentElement.scrollWidth <=
      document.documentElement.clientWidth,
  )

const profileIDFromURL = (value: string): string =>
  /\/profiles\/([a-f0-9]{32})/.exec(new URL(value).pathname)?.[1] ?? ''

const assertProductAlive = (): void => {
  if (managedProduct === undefined)
    throw new Error('Routevane browser server exited early: not started')
  managedProduct.assertAlive()
}

const waitForAuthenticatedRoot = async (): Promise<void> => {
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

const embeddedUIDigest = async (): Promise<string> => {
  const files = await embeddedFiles(embeddedUIRoot)
  const rows: string[] = []
  for (const relativePath of files.sort()) {
    if (relativePath === 'routevane-ui.marker') continue
    const bytes = await readFile(join(embeddedUIRoot, relativePath))
    rows.push(`${sha256(bytes)}  ${relativePath}\n`)
  }
  return `sha256-${sha256(rows.join(''))}`
}

const embeddedFiles = async (root: string, prefix = ''): Promise<string[]> => {
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

const sha256 = (value: string | Uint8Array): string =>
  createHash('sha256').update(value).digest('hex')

test('source refresh stays available during a forecast and reports its own busy state', async ({
  page,
}) => {
  let releaseForecast!: () => void
  let releaseRefresh!: () => void
  const forecastHeld = new Promise<void>((resolve) => {
    releaseForecast = resolve
  })
  const refreshHeld = new Promise<void>((resolve) => {
    releaseRefresh = resolve
  })
  let forecasts = 0
  let refreshes = 0
  await page.route('**/v1/profiles/preview', async (route) => {
    forecasts += 1
    if (forecasts === 1) await forecastHeld
    await route.fulfill({ json: { targets: [] } })
  })
  await page.route('**/v1/lists/discord/refresh', async (route) => {
    refreshes += 1
    await refreshHeld
    await route.fulfill({ json: { refresh: {} } })
  })
  try {
    await page.goto(`${origin}/profiles/new`)
    await listMembership(page, 'Discord').check()
    await expect.poll(() => forecasts).toBe(1)
    const refresh = compositionRefresh(page)
    await expect(refresh).toBeEnabled()
    await refresh.click()
    await expect.poll(() => refreshes).toBe(1)
    await expect(refresh).toBeDisabled()
    await expect(refresh).toHaveAttribute('aria-busy', 'true')
    releaseRefresh()
    await expect.poll(() => forecasts).toBeGreaterThan(1)
    await expect(refresh).toBeEnabled()
    releaseForecast()
    await expect(
      page.getByText('List order sets the priority.', { exact: true }),
    ).toHaveCount(0)
    const search = (await page
      .getByRole('searchbox', { name: 'Find a list' })
      .boundingBox())!
    const categories = (await categoryFilters(page).boundingBox())!
    expect(search.y).toBeGreaterThanOrEqual(categories.y + categories.height)
  } finally {
    releaseForecast()
    releaseRefresh()
    await page.unrouteAll({ behavior: 'wait' })
  }
})

test('library rows disclose inspection and the card keeps its controls while switching lists', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1920, height: 960 })
  const first = await (
    await page.request.get(`${origin}/v1/lists/discord/contents`)
  ).json()
  const second = await (
    await page.request.get(`${origin}/v1/lists/youtube/contents`)
  ).json()
  let release!: () => void
  const held = new Promise<void>((resolve) => {
    release = resolve
  })
  await page.route('**/v1/lists/discord/contents', (route) =>
    route.fulfill({ json: { ...first, observed: true } }),
  )
  await page.route('**/v1/lists/youtube/contents', async (route) => {
    await held
    await route.fulfill({ json: { ...second, observed: true } })
  })
  try {
    await page.goto(`${origin}/lists`)
    // A row opens from its own cells, so pressing what a row says about its
    // categories is one of the ways in.
    await libraryCategoryCell(libraryRow(page, 'Discord')).click()
    // The docked card is the only dialog the workspace holds.
    const card = page.getByRole('dialog')
    await expect(card).toHaveAccessibleName('Discord')
    const filter = card.getByRole('searchbox', {
      name: englishCopy('listCard.filter'),
    })
    await expect(filter).toBeEnabled()
    const before = (await filter.boundingBox())!
    const rowsBefore = (await cardContents(card).boundingBox())!
    await libraryCategoryCell(libraryRow(page, 'YouTube')).click()
    await expect(card).toHaveAccessibleName('YouTube')
    await expect(filter).toBeDisabled()
    await expect(
      card.getByText('Loading the list contents…', { exact: true }),
    ).toBeVisible()
    const loading = (await filter.boundingBox())!
    const rowsLoading = (await cardContents(card).boundingBox())!
    expect(Math.abs(loading.y - before.y)).toBeLessThanOrEqual(1)
    expect(Math.abs(rowsLoading.y - rowsBefore.y)).toBeLessThanOrEqual(1)
    // The count claims no stale number while it does not know one.
    await expect(card.getByText('—', { exact: true })).toBeVisible()
    release()
    await expect(filter).toBeEnabled()
    expect(
      Math.abs((await filter.boundingBox())!.y - before.y),
    ).toBeLessThanOrEqual(1)
    await page.keyboard.press('Escape')
    await libraryRow(page, 'Discord')
      .getByRole('button', {
        name: englishCopy('listDetail.open.aria').replace('{list}', 'Discord'),
      })
      .click()
    await expect(card).toHaveAccessibleName('Discord')
  } finally {
    release()
    await page.unrouteAll({ behavior: 'wait' })
  }
})

test('composition keeps its context in docked and overlaid cards and isolates navigation scroll', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1920, height: 960 })
  await page.goto(`${origin}/profiles/new`)
  const frame = page.getByTestId('rv-list-picker-frame')
  await expect(frame).toBeVisible()
  const closedBounds = (await frame.boundingBox())!
  const closedWidth = closedBounds.width
  expect(closedWidth).toBeLessThan(1150)
  const opener = openList(page, 'Discord')
  await opener.click()
  const card = page.getByRole('dialog', { name: 'Discord', exact: true })
  await expect(card).toHaveClass(/rv-dialog--docked/)
  await expect(dialogScrims(page)).toHaveCount(0)
  const tableBox = (await frame.boundingBox())!
  const cardBox = (await card.boundingBox())!
  expect(Math.abs(tableBox.x - closedBounds.x)).toBeLessThanOrEqual(1)
  expect(tableBox.x + tableBox.width).toBeLessThan(cardBox.x)
  expect(cardBox.y + cardBox.height).toBeLessThanOrEqual(960)
  expect(
    await frame.evaluate((e) => e.scrollWidth - e.clientWidth),
  ).toBeLessThanOrEqual(1)
  const filter = card.getByRole('searchbox', {
    name: englishCopy('listCard.filter'),
  })
  await filter.fill('discord')
  await filter.press('Enter')
  await expect(page).toHaveURL(`${origin}/profiles/new`)
  expect(
    await filter.evaluate((element) => (element as HTMLInputElement).form),
  ).toBeNull()
  await listMembership(page, 'Discord').check()
  await expect(card).toBeVisible()
  await page.screenshot({ path: join(reviewRoot, 'composition-docked.png') })
  await page.setViewportSize({ width: 1100, height: 850 })
  await expect(card).not.toHaveClass(/rv-dialog--docked/)
  await expect(dialogScrims(page)).toBeVisible()
  await expect(filter).toHaveValue('discord')
  await expect(
    card.getByRole('button', { name: 'Close', exact: true }),
  ).toBeEnabled({ timeout: 60000 })
  await page.keyboard.press('Escape')
  await expect(card).toBeHidden()
  await expect(opener).toBeFocused()
  await expect(listMembership(page, 'Discord')).toBeChecked()
  await page.setViewportSize({ width: 1920, height: 960 })
  await opener.click()
  await expect(card).toHaveClass(/rv-dialog--docked/)
  await expect(
    card.getByRole('button', { name: 'Close', exact: true }),
  ).toBeEnabled({ timeout: 60000 })
  await page.keyboard.press('Escape')
  await expect(opener).toBeFocused()
  await page.getByRole('button', { name: 'Collapse sidebar' }).click()
  await expect(
    page.getByRole('button', { name: 'Expand sidebar' }),
  ).toBeVisible()
  expect(
    await page
      .getByTestId('rv-shell-grid')
      .evaluate((e) => getComputedStyle(e).transitionProperty),
  ).toContain('grid-template-columns')
  await page.screenshot({ path: join(reviewRoot, 'composition-compact.png') })
  // A long existing profile must scroll its own content without moving navigation.
  await page.goto(`${origin}/settings`)
  const logo = page.getByTestId('rv-shell-product-mark')
  const logoY = (await logo.boundingBox())!.y
  await page.getByRole('main').evaluate((e) => {
    e.scrollTop = e.scrollHeight
  })
  expect((await logo.boundingBox())!.y).toBe(logoY)
  expect(
    await page.evaluate(
      () => document.documentElement.scrollHeight - innerHeight,
    ),
  ).toBe(0)
  expect(
    await page
      .getByRole('banner')
      .evaluate((e) => e.scrollHeight - e.clientHeight),
  ).toBeLessThanOrEqual(1)
})

test('page inspection preserves primary actions and full-height geometry in every list workflow', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1920, height: 960 })
  await page.goto(`${origin}/profiles/new`)
  await listMembership(page, 'Discord').check()
  await chooseFormat(page, /sing-box/)
  await nameField(page).fill('Inspection workflow')
  await openList(page, 'Discord').click()
  const card = page.getByRole('dialog', { name: 'Discord', exact: true })
  const aligned = async (frame: Locator) => {
    await expect(card).toHaveClass(/rv-dialog--docked/)
    await expect
      .poll(async () => {
        const a = (await frame.boundingBox())!
        const b = (await card.boundingBox())!
        return Math.abs(a.y + a.height - b.y - b.height)
      })
      .toBeLessThanOrEqual(2)
    const bounds = (await card.boundingBox())!
    expect(bounds.y).toBeLessThanOrEqual(24)
    expect(bounds.y + bounds.height).toBeGreaterThanOrEqual(936)
    expect(
      await frame.evaluate((e) => e.scrollWidth - e.clientWidth),
    ).toBeLessThanOrEqual(1)
    await expect(dialogScrims(page)).toHaveCount(0)
  }
  await aligned(page.getByTestId('rv-list-picker-frame'))
  // The name column and the category column of the composition table, whose
  // widths the library's own table has to match below.
  const routeNameWidth = (await page
    .getByRole('columnheader', {
      exact: true,
      name: englishCopy('listPicker.column.list'),
    })
    .boundingBox())!.width
  const routeCategoryWidth = (await categoryCell(
    bodyRows(page).first(),
  ).boundingBox())!.width
  // The two fields of the settings form sit on one line, and the act it
  // carries sits under both. A field is measured from its own caption, which
  // is where the field starts in both the composer and the editor.
  const fieldTop = async (caption: string): Promise<number> =>
    (await composerSettings(page)
      .getByText(caption, { exact: true })
      .boundingBox())!.y
  const firstField = await fieldTop(englishCopy('create.name'))
  const secondField = await fieldTop(englishCopy('create.target'))
  expect(Math.abs(firstField - secondField)).toBeLessThanOrEqual(1)
  const actionRow = (await page
    .getByRole('button', { name: 'Create and prepare', exact: true })
    .boundingBox())!
  expect(actionRow.y).toBeGreaterThanOrEqual(Math.max(firstField, secondField))
  const libraryLink = card.getByRole('link', { name: 'Open in the library' })
  await expect(libraryLink).toBeVisible()
  expect((await libraryLink.boundingBox())!.x).toBeGreaterThan(
    (await card
      .getByRole('heading', { name: 'Discord', exact: true })
      .boundingBox())!.x,
  )

  const create = page.getByRole('button', {
    name: 'Create and prepare',
    exact: true,
  })
  await expect(create).toBeInViewport()
  await expect(create).toBeEnabled()
  const firstOutput = page.waitForResponse(
    (response) =>
      response.request().method() === 'POST' &&
      /\/v1\/profiles\/[a-f0-9]{32}\/outputs$/.test(
        new URL(response.url()).pathname,
      ),
  )
  await create.click()
  await page.waitForURL(/\/profiles\/[a-f0-9]{32}/)
  expect((await firstOutput).ok()).toBe(true)
  const id = profileIDFromURL(page.url())
  await page.goto(`${origin}/profiles/${id}`)
  await nameField(page).fill('Inspection workflow saved')
  await openList(page, 'Discord').click()
  await aligned(page.getByTestId('rv-list-picker-frame'))
  expect(
    Math.abs(
      (await fieldTop(englishCopy('create.name'))) -
        (await fieldTop(englishCopy('create.target'))),
    ),
  ).toBeLessThanOrEqual(1)
  const save = page.getByRole('button', {
    name: 'Save and rebuild',
    exact: true,
  })
  await expect(save).toBeInViewport()
  await save.click()
  await expect(
    page.getByRole('heading', {
      name: 'Inspection workflow saved',
      exact: true,
    }),
  ).toBeVisible()
  await page.reload()
  await expect(nameField(page)).toHaveValue('Inspection workflow saved')
  await page.goto(`${origin}/lists`)
  const workspace = page.getByTestId('rv-lists-workspace')
  const libraryLeadingEdge = (await workspace.boundingBox())!.x
  await libraryRowName(libraryRow(page, 'Discord')).click()
  await aligned(workspace)
  expect(
    Math.abs(
      (await page
        .getByRole('columnheader', {
          exact: true,
          name: englishCopy('listPicker.column.list'),
        })
        .boundingBox())!.width - routeNameWidth,
    ),
  ).toBeLessThanOrEqual(8)
  expect(
    Math.abs(
      (await libraryCategoryCell(bodyRows(page).first()).boundingBox())!.width -
        routeCategoryWidth,
    ),
  ).toBeLessThanOrEqual(8)
  expect(
    Math.abs((await workspace.boundingBox())!.x - libraryLeadingEdge),
  ).toBeLessThanOrEqual(1)
  await expect(
    page.getByRole('button', { name: 'New list', exact: true }),
  ).toBeInViewport()
  await page.screenshot({ path: join(reviewRoot, 'library-docked.png') })
  await page.setViewportSize({ width: 1100, height: 850 })
  await expect(card).not.toHaveClass(/rv-dialog--docked/)
  await expect(dialogScrims(page)).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(card).toBeHidden()
})

test('sidebar labels and buttons retain their geometry throughout expansion and collapse', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto(`${origin}/`)
  await expect(
    page.getByRole('button', { name: 'Collapse sidebar' }),
  ).toBeVisible()
  for (const direction of ['collapse', 'expand']) {
    const frames = await page.evaluate(async () => {
      const button =
        document.querySelector<HTMLButtonElement>('.shell__collapse')!
      const side = document.querySelector<HTMLElement>('.shell__side')!
      const labels = [
        ...side.querySelectorAll<HTMLElement>('.shell__nav-label'),
      ]
      button.click()
      const frames: {
        height: number
        overflow: number
        labels: number[]
        opacity: number
      }[] = []
      const start = performance.now()
      do {
        await new Promise(requestAnimationFrame)
        frames.push({
          height: button.getBoundingClientRect().height,
          overflow: side.scrollWidth - side.clientWidth,
          labels: labels.map((e) => e.getBoundingClientRect().height),
          opacity: Number(getComputedStyle(labels[0]!).opacity),
        })
      } while (performance.now() - start < 350)
      return frames
    })
    expect(frames.length, direction).toBeGreaterThan(2)
    expect(
      Math.max(...frames.map((f) => f.height)) -
        Math.min(...frames.map((f) => f.height)),
      direction,
    ).toBeLessThanOrEqual(1)
    expect(
      Math.max(...frames.map((f) => f.overflow)),
      direction,
    ).toBeLessThanOrEqual(1)
    for (let i = 0; i < frames[0]!.labels.length; i++) {
      const heights = frames.map((f) => f.labels[i]!)
      expect(
        Math.max(...heights) - Math.min(...heights),
        direction,
      ).toBeLessThanOrEqual(1)
    }
    expect(
      frames.some((f) => f.opacity > 0 && f.opacity < 1),
      direction,
    ).toBe(true)
  }
})

test('docked inspection transitions the form and table together on opening and closing', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1920, height: 960 })
  await page.goto(`${origin}/profiles/new`)
  await expect(openList(page, 'Discord')).toBeVisible()
  const transitionFrames = async (selector: string) =>
    page.evaluate(async (selector) => {
      const rect = (name: string) => {
        const r = document.querySelector(name)!.getBoundingClientRect()
        return { x: r.x, y: r.y }
      }
      const before = {
        form: rect('.rv-composer__settings'),
        table: rect('.rv-composer__main'),
      }
      const native = document.startViewTransition.bind(document)
      let current: ViewTransition | undefined
      document.startViewTransition = (update) => {
        current = native(update)
        void current.ready.then(() =>
          document
            .getAnimations()
            .filter((a) => (a.effect as KeyframeEffect)?.pseudoElement)
            .forEach((a) => a.pause()),
        )
        return current
      }
      document.querySelector<HTMLButtonElement>(selector)!.click()
      if (!current)
        throw new Error(
          'Inspection did not capture the layout before changing it',
        )
      await current.ready
      const animations = document
        .getAnimations()
        .filter((a) => (a.effect as KeyframeEffect)?.pseudoElement)
      const frames = []
      for (const progress of [0, 0.5, 1]) {
        for (const a of animations)
          a.currentTime = Number(a.effect!.getTiming().duration) * progress
        await new Promise(requestAnimationFrame)
        const position = (name: string) => {
          const matrix = new DOMMatrix(
            getComputedStyle(
              document.documentElement,
              `::view-transition-group(${name})`,
            ).transform,
          )
          return { x: matrix.e, y: matrix.f }
        }
        frames.push({
          form: position('composer-settings'),
          table: position('composer-content'),
        })
      }
      const after = {
        form: rect('.rv-composer__settings'),
        table: rect('.rv-composer__main'),
      }
      const card = document.querySelector('.rv-dialog--docked')
      const nestedSlide = card ? getComputedStyle(card).animationName : 'none'
      animations.forEach((a) => a.finish())
      await current.finished
      document.startViewTransition = native
      return { before, after, frames, nestedSlide }
    }, selector)
  const check = (result: Awaited<ReturnType<typeof transitionFrames>>) => {
    for (const part of ['form', 'table'] as const) {
      for (const axis of ['x', 'y'] as const) {
        expect(
          Math.abs(result.frames[0]![part][axis] - result.before[part][axis]),
        ).toBeLessThan(2)
        expect(
          Math.abs(result.frames[2]![part][axis] - result.after[part][axis]),
        ).toBeLessThan(2)
      }
    }
    const positions = result.frames.map((f) => f.form.x)
    expect(positions[1]).toBeGreaterThan(Math.min(positions[0]!, positions[2]!))
    expect(positions[1]).toBeLessThan(Math.max(positions[0]!, positions[2]!))
    expect(result.nestedSlide).toBe('none')
  }
  // Start at the keyboard opener; native click() below does not focus it.
  await openList(page, 'Discord').focus()
  check(await transitionFrames('[data-id="discord"] .picker__open'))
  await expect(
    page
      .getByRole('dialog')
      .getByRole('button', { name: 'Close', exact: true }),
  ).toBeEnabled({ timeout: 60000 })
  check(await transitionFrames('.rv-dialog__close'))
  await expect(page.getByRole('dialog')).toBeHidden()
  await expect(openList(page, 'Discord')).toBeFocused()
  const collapse = await page
    .getByRole('button', { name: englishCopy('shell.collapse'), exact: true })
    .boundingBox()
  expect(Math.abs(collapse!.y + collapse!.height - 960)).toBeLessThanOrEqual(1)
  // The same operation stays usable when animation is disabled or unavailable.
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await openList(page, 'Discord').click()
  await expect(page.getByRole('dialog')).toBeVisible()
  await page.keyboard.press('Escape')
  await page.emulateMedia({ reducedMotion: 'no-preference' })
  await page.evaluate(() => {
    Object.defineProperty(document, 'startViewTransition', {
      value: undefined,
      configurable: true,
    })
  })
  await openList(page, 'Discord').click()
  await expect(page.getByRole('dialog')).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(page.getByRole('dialog')).toBeHidden()
})

test('one list without IP coverage preserves other lists and domain-format forecasts', async ({
  page,
}) => {
  const headers = { 'X-Routevane-Request': '1' }
  const ids: string[] = []
  const fixtures = [
    ['Partial Alpha', '192.0.2.1'],
    ['Partial Beta', '192.0.2.1'],
    ['Partial Missing', ''],
    ['Partial Separate', '192.0.2.2'],
  ] as const
  try {
    for (const [title, address] of fixtures) {
      const created = await page.request.post(`${origin}/v1/lists`, {
        headers,
        data: { title, domains: ['forecast.invalid'] },
      })
      expect(created.status()).toBe(201)
      const { list: list } = (await created.json()) as {
        list: { id: string }
      }
      ids.push(list.id)
      if (address)
        expect(
          (
            await page.request.post(`${origin}/v1/lists/${list.id}/domains`, {
              headers,
              data: { values: [address], verdict: 'include' },
            })
          ).ok(),
        ).toBe(true)
      expect(
        (
          await page.request.post(`${origin}/v1/lists/${list.id}/refresh`, {
            headers,
            data: {},
          })
        ).ok(),
      ).toBe(true)
    }
    const response = await page.request.post(`${origin}/v1/profiles/preview`, {
      headers,
      data: { lists: ids, priority: ids },
    })
    expect(response.status()).toBe(200)
    const { targets } = (await response.json()) as {
      targets: {
        target_id: string
        incomplete_lists?: string[]
        projected_rules: number
        fits: boolean
      }[]
    }
    expect(
      targets.find((x) => x.target_id === 'keenetic')?.incomplete_lists,
    ).toEqual([ids[2]])
    expect(targets.find((x) => x.target_id === 'keenetic')?.fits).toBe(false)
    expect(
      targets.find((x) => x.target_id === 'singbox')?.incomplete_lists ?? [],
    ).toEqual([])
    expect(targets.find((x) => x.target_id === 'singbox')?.fits).toBe(true)
    await page.setViewportSize({ width: 1440, height: 900 })
    await page.goto(`${origin}/profiles/new`)
    for (const [title] of fixtures) await listMembership(page, title).check()
    await chooseFormat(page, /Keenetic/)
    await expect(
      forecastStatus(page, 'Some lists could not be calculated'),
    ).toBeVisible({ timeout: 60000 })
    // Each of the two lists that share an address credits the other, and the
    // count is the control that names it.
    for (const [title] of fixtures.slice(0, 2))
      await expect(
        listRow(page, title).getByRole('button', { name: /^Overlap: / }),
      ).toHaveText('1')
    // The separate list overlaps nothing, so its cell states a dash and offers
    // nothing to open.
    await expect(overlapsCell(listRow(page, 'Partial Separate'))).toHaveText(
      '—',
    )
    await page
      .getByRole('button', { name: englishCopy('forecast.partial.missing') })
      .click()
    await expect(
      page.getByText(
        'These lists have no data usable by the selected connection. Refresh sources or choose another connection:',
      ),
    ).toBeVisible()
    await expect(page.getByRole('dialog')).toContainText('Partial Missing')
    await page.keyboard.press('Escape')
    // The list without usable data has no forecast at all, which is the same
    // thing its cell is named after.
    await expect(
      listRow(page, 'Partial Missing').getByRole('cell', {
        name: englishCopy('listPicker.rules.unknown'),
        exact: true,
      }),
    ).toHaveText('—')
    await expect(
      page.getByRole('button', { name: 'Create and prepare', exact: true }),
    ).toBeEnabled()
  } finally {
    for (const id of ids)
      expect(
        (
          await page.request.post(`${origin}/v1/lists/${id}/remove`, {
            headers,
            data: {},
          })
        ).ok(),
      ).toBe(true)
  }
})

test('quick profile reads stay quiet while slow reads and failures remain visible', async ({
  page,
}) => {
  const payload = await (await page.request.get(`${origin}/v1/profiles`)).json()
  let mode: 'fast' | 'slow' | 'failure' = 'fast'
  let release!: () => void
  let held = new Promise<void>((resolve) => {
    release = resolve
  })
  await page.addInitScript(() => {
    const feedback = { samples: [] as string[] }
    Object.assign(window, { routeFeedback: feedback })
    const sample = () => {
      const notice = document.querySelector('.rv-notice--busy')
      if (notice) feedback.samples.push(getComputedStyle(notice).visibility)
      requestAnimationFrame(sample)
    }
    requestAnimationFrame(sample)
  })
  await page.route('**/v1/profiles', async (route) => {
    if (mode !== 'fast') await held
    await route.fulfill({
      status: mode === 'failure' ? 503 : 200,
      json: mode === 'failure' ? { error: 'unavailable' } : payload,
    })
  })
  const samples = () =>
    page.evaluate(
      () =>
        (window as unknown as { routeFeedback: { samples: string[] } })
          .routeFeedback.samples,
    )
  try {
    // The shelf has arrived when it either holds the table or says it is
    // empty.
    const shelf = page
      .getByRole('table')
      .or(page.getByText(englishCopy('profiles.empty'), { exact: true }))
    const reading = page
      .getByRole('status')
      .filter({ hasText: englishCopy('profiles.loading') })
    const unavailable = page
      .getByRole('status')
      .filter({ hasText: englishCopy('profiles.failed') })
    await page.goto(origin)
    await expect(shelf).toBeVisible()
    expect(await samples()).not.toContain('visible')
    mode = 'slow'
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.goto(origin)
    await expect(reading).toBeVisible()
    expect((await samples())[0]).toBe('hidden')
    release()
    await expect(shelf).toBeVisible()
    await expect(reading).toHaveCount(0)
    mode = 'failure'
    held = new Promise<void>((resolve) => {
      release = resolve
    })
    await page.goto(origin)
    await expect(reading).toBeVisible()
    release()
    await expect(unavailable).toBeVisible()
    await expect(
      page.getByRole('button', { name: 'Retry', exact: true }),
    ).toBeEnabled()
    expect(
      await unavailable.evaluate((e) => getComputedStyle(e).animationDelay),
    ).toBe('0s')
  } finally {
    release()
    await page.unrouteAll({ behavior: 'wait' })
  }
})

test('category tabs and More toggle a union that bulk selection can add to a profile', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1920, height: 960 })
  for (const path of ['/lists', '/profiles/new']) {
    await page.goto(origin + path)
    await categoryChip(page, 'Communication').click()
    await categoryChip(page, 'Video').click()
    // Both surfaces answer with rows of their own table, each naming the list
    // it stands for.
    const rows = bodyRows(page)
    await expect
      .poll(async () =>
        (await rows.getByRole('rowheader').allInnerTexts())
          .map((name) => name.split('\n')[0])
          .sort(),
      )
      .toEqual(['Discord', 'YouTube'])
    if (path === '/lists') {
      // Reload the committed bookmark, not an in-flight router replacement.
      await expect
        .poll(() =>
          new URLSearchParams(new URL(page.url()).hash.slice(1))
            .getAll('category')
            .sort(),
        )
        .toEqual(['communication', 'video'])
      await page.reload()
      await expect.poll(() => rows.count()).toBe(2)
      for (const category of ['Communication', 'Video'])
        await expect(categoryChip(page, category)).toHaveAttribute(
          'aria-pressed',
          'true',
        )
    } else {
      await page
        .getByRole('checkbox', {
          name: englishCopy('listPicker.selectVisible'),
        })
        .check()
      for (const title of ['Discord', 'YouTube'])
        await expect(listMembership(page, title)).toBeChecked()
      await categoryChip(page, englishCopy('listPicker.filter.all')).click()
      await expect(listMembership(page, 'Limit fixture')).not.toBeChecked()
      await categoryChip(page, 'Communication').click()
      await categoryChip(page, 'Video').click()
    }
    await categoryMore(page).click()
    const video = page.getByRole('option', { name: /^Video/ })
    await expect(video).toHaveAttribute('aria-selected', 'true')
    await video.click()
    await expect(video).toHaveAttribute('aria-selected', 'false')
    await expect(
      page.getByRole('option', { name: /^Communication/ }),
    ).toHaveAttribute('aria-selected', 'true')
    await page.keyboard.press('Escape')
    await expect.poll(() => rows.count()).toBe(1)
    await categoryChip(page, 'Communication').click()
    await expect(
      categoryChip(page, englishCopy('listPicker.filter.all')),
    ).toHaveAttribute('aria-pressed', 'true')
    await expect.poll(() => rows.count()).toBeGreaterThan(2)
  }
})

test('profile rows navigate from their cells while menus and links keep their actions', async ({
  page,
}) => {
  const { profileId } = await buildProfile(page)
  await page.goto(origin)
  const row = page
    .getByRole('row')
    .filter({ has: linkTo(page, `/profiles/${profileId}`) })
  await profileOutputsCell(row).click()
  await expect(page).toHaveURL(origin + '/profiles/' + profileId)
  await page.goto(origin)
  await row.getByRole('button').click()
  await expect(page.getByRole('menu')).toBeVisible()
  await expect(page).toHaveURL(origin + '/')
  await page.keyboard.press('Escape')
  await linkTo(page, `/profiles/${profileId}`).focus()
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(origin + '/profiles/' + profileId)
})
