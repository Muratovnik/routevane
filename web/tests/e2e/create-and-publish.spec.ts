/**
 * The one walkthrough that starts with nothing: an empty shelf, the
 * composer, a published profile and the row it becomes.
 *
 * The worker fixture in `support/served-product.ts` serves this suite
 * alone, over the data directory named below.
 */

import { expect } from '@playwright/test'

import { assertNoOverflow, audit } from './support/audits'
import { englishCopy } from './support/copy'
import {
  FORECAST_PATH,
  openFormats,
  profileFlow,
  profileIDFromURL,
  settleAnimations,
} from './support/flows'
import {
  addConnectionField,
  bodyRows,
  cardMembership,
  cardRow,
  cardRows,
  choicePanel,
  deliveryField,
  dragGhost,
  escapeRegExp,
  listMembership,
  listRow,
  menuPanel,
  nameField,
  priorityHandle,
  profileOutputsCell,
  rowCellWidths,
  selectedListRows,
} from './support/queries'
import { test } from './support/served-product'

// The surface negotiates its language from the browser, and English is the
// product's primary language. `productData` is this suite's own data
// directory: it starts empty, no other suite writes to it, and it is gone
// again when the suite ends.
test.use({ locale: 'en-US', productData: 'create-and-publish' })

test('the library starts empty and shelves the profile the composer creates and publishes', async ({
  page,
  origin,
  expectedUIDigest,
  assertProductAlive,
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
      'Choose lists and a format; Routevane will prepare the rules and show how to get the file.',
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

  // The card opens on the composition. Which feeds a list reads, and when they
  // are read again, is the library's subject (ADR 0029), so the composing card
  // states how many there are and offers no control over them at all — neither
  // the panel that edits them nor the command that re-reads them.
  await expect(
    listCard.getByRole('heading', { name: 'Automatic sources' }),
  ).toHaveCount(0)
  await expect(listCard.getByText(/^Sources · \d+$/)).toBeVisible()
  for (const absent of [/^Sources · \d+$/, /^Refresh from sources/])
    await expect(listCard.getByRole('button', { name: absent })).toHaveCount(0)

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
  await cardMembership(listCard, englishCopy).click()
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
  // Both rows have been asserted visible and the handle scrolled into view, so
  // each has a box — read here the way the clone's own box is read below.
  const from = (await dragHandle.boundingBox())!
  const to = (await priorityRows.nth(1).boundingBox())!
  await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2)
  await page.mouse.down()
  await page.mouse.move(from.x + from.width / 2 + 4, from.y + from.height, {
    steps: 4,
  })
  // Sortable clones the chosen row once the pointer has moved, and the clone
  // is what the geometry below is measured against, so its own visibility is
  // what the measurement waits for.
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
  // Sortable moves the row itself while the pointer is still down, so the
  // release waits for the carried row to have taken its new place rather than
  // for the reorder animation to have run for some number of milliseconds.
  await expect(priorityRows.nth(0).getByRole('rowheader')).toContainText(
    'YouTube',
  )
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
  // The panel is named for the choice it offers, and it grows in on opening,
  // so it is measured once it has settled.
  const targetField = deliveryField(page)
  const targetPanel = choicePanel(page, englishCopy('create.target.toggle'))
  await expect(targetPanel).toBeVisible()
  await settleAnimations(page)
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
    'Publishing',
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
