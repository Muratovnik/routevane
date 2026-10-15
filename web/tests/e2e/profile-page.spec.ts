/**
 * A published profile's own page: the one-time link, the file it publishes,
 * its diagnostics, a rename that republishes, and a build that failed.
 *
 * The worker fixture in `support/served-product.ts` serves this suite
 * alone, over the data directory named below.
 */

import { expect, type Page } from '@playwright/test'

import { copyFor, englishCopy } from './support/copy'
import {
  buildProfile,
  pressSegment,
  settleAnimations,
  statesFlow,
} from './support/flows'
import {
  forceStaleCatalog,
  PAGE_INSET,
  pageFoot,
  releaseCatalog,
} from './support/geometry'
import {
  cardMembership,
  chooseFormatAction,
  formatMenu,
  linkTo,
  menuPanel,
  nameField,
  openList,
  scheduleField,
  selectedListRows,
} from './support/queries'
import { test } from './support/served-product'

// The surface negotiates its language from the browser, and English is the
// product's primary language. `productData` is this suite's own data
// directory: it starts empty, no other suite writes to it, and it is gone
// again when the suite ends.
test.use({ locale: 'en-US', productData: 'profile-page' })

/** Enough lists that the composition table cannot fit the window it is in. */
const seedTallCatalog = async (page: Page, origin: string): Promise<void> => {
  for (let index = 0; index < 20; index += 1) {
    const created = await page.request.post(`${origin}/v1/lists`, {
      data: { title: `Tall catalog ${index}`, domains: [`t${index}.example`] },
      headers: { Origin: origin, 'X-Routevane-Request': '1' },
    })
    expect(created.ok()).toBe(true)
  }
}

test('the profile page guards the secret, shows the file and its diagnostics, and republishes on rename', async ({
  page,
  origin,
  assertProductAlive,
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

  const { profileId, outputId } = await buildProfile(page, origin)
  const profileURL = `${origin}/profiles/${profileId}`

  // The notice states the coverage; the header adds no second sentence for it.
  await expect(page.getByText('Coverage is incomplete')).toBeVisible()
  await expect(
    page.getByText('Published with notes', { exact: true }),
  ).toHaveCount(0)
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
  // not in any request, until the operator asks for it. The block opens by
  // itself for a link that was just issued, because this is the only moment it
  // can be read.
  await expect(
    page.getByRole('button', { name: 'Subscription link · Keenetic' }),
  ).toHaveAttribute('aria-expanded', 'true')
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
    'Publishing',
    'File',
    'Diagnostics',
  ])

  // The file is read only when the operator opens it, and what is shown is
  // byte-for-byte what the device receives.
  await tablist.getByRole('tab', { name: 'Publishing' }).click()
  const scheduleTrigger = scheduleField(page)
  await expect(scheduleTrigger).toBeVisible()
  const scheduleGround = async (): Promise<string> =>
    scheduleTrigger.evaluate((node) => getComputedStyle(node).backgroundColor)
  // The refresh rule is one field above the format table. Its caption is a
  // label, which hovers the field it names, so the neutral ground is the
  // table's own header.
  const scheduleHeading = page
    .getByRole('tabpanel', { name: 'Publishing' })
    .getByRole('columnheader')
    .first()
  await settleAnimations(page)
  const atRest = await scheduleGround()
  // A choice trigger is a field, and a field has no hover state (docs/UI.md):
  // neither the heading above it nor the pointer on the trigger itself changes
  // its ground. Read each settled state so an early transition frame cannot
  // satisfy it.
  await scheduleHeading.hover()
  await settleAnimations(page)
  expect(await scheduleGround()).toBe(atRest)
  await scheduleTrigger.hover()
  await settleAnimations(page)
  expect(await scheduleGround()).toBe(atRest)
  await scheduleHeading.hover()
  await settleAnimations(page)
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
  // The rules are one table; what each list contributed follows it.
  const counts = diagnosticsPanel
    .getByRole('region', { name: englishCopy('profile.diagnostics.perList') })
    .getByRole('list')
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
  // come back; the format's own tab says so, in every detail mode.
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
    page.getByRole('button', { name: /^Subscription link/ }),
  ).toHaveCount(0)
  await expect(
    page.getByRole('button', { name: 'Show', exact: true }),
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

  // The expert mode is the only mode that puts identifiers on the screen; the
  // link's fate is stated in it too.
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
  await expect(page.getByText('Technical details')).toBeVisible()
  await tablist.getByRole('tab', { name: 'Publishing' }).click()
  // The link is shown once. Every format's own row offers a new one; the tab
  // does not speak for one implicitly selected format.
  await formatMenu(page, 'Keenetic').click()
  await expect(
    menuPanel(page).getByRole('menuitem', { name: 'Issue a new link…' }),
  ).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(page.getByText(/shown once/)).toHaveCount(0)

  expect(pageErrors).toEqual([])
  assertProductAlive()
})

test('a failed first build remains retryable and exposes no subscription', async ({
  page,
  origin,
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
  // Nothing was ever published: the header and the notice both say the build
  // failed, and the notice carries the rebuild itself.
  const buildFailed = page.getByText('Build failed', { exact: true })
  await expect(buildFailed).toHaveCount(2)
  const failureNotice = page
    .getByRole('status')
    .filter({ hasText: 'Build failed' })
  await expect(
    failureNotice.getByRole('button', { name: 'Rebuild files', exact: true }),
  ).toBeVisible()
  // The editor says the same thing the build reported, before the operator
  // presses anything, and still lets the profile be saved.
  await expect(
    page.getByText('Limited fixture: ≈ 2 of 1 — will not fit'),
  ).toBeVisible()
  await expect(
    page.getByText(
      'This needs 2 rules, but the format accepts at most 1. Remove some lists or choose another format.',
    ),
  ).toBeVisible()
  // The connection table lives on its own tab; a hidden row has no role.
  await page
    .getByRole('tablist', { name: 'Profile sections' })
    .getByRole('tab', { name: 'Publishing' })
    .click()
  const outputRow = page.getByRole('row').filter({ hasText: 'Limited fixture' })
  await expect(outputRow).toContainText('Last build failed')
  await expect(outputRow).toContainText('No published file')
  await expect(
    page.getByRole('button', { name: /^Subscription link/ }),
  ).toHaveCount(0)
  expect(await page.content()).not.toContain('rv1.')

  await page.getByRole('link', { name: 'Profiles' }).first().click()
  await page.waitForURL(`${origin}/`)
  await linkTo(page, `/profiles/${failedProfileID}`).click()
  await expect(buildFailed).toHaveCount(2)
  await page
    .getByRole('tablist', { name: 'Profile sections' })
    .getByRole('tab', { name: 'Publishing' })
    .click()
  await expect(outputRow).toContainText('Last build failed')
  await page
    .getByRole('main')
    .getByRole('button', { name: /Actions for profile/ })
    .click()
  await menuPanel(page).getByRole('menuitem', { name: 'Rebuild files' }).click()
  await expect(buildFailed).toHaveCount(2)
  await expect(
    page.getByRole('button', { name: /^Subscription link/ }),
  ).toHaveCount(0)
})

test('send links navigate through the router for one and multiple published outputs', async ({
  page,
  origin,
}) => {
  const { profileId, outputId } = await buildProfile(page, origin)
  const menu = page.getByRole('button', {
    name: 'Actions for profile Discord, YouTube',
  })
  // Send is one submenu of formats, even for one.
  await menu.click()
  await page.getByRole('menuitem', { name: 'Send', exact: true }).click()
  await page.getByRole('menuitem', { name: 'Keenetic', exact: true }).click()
  await expect(page).toHaveURL(
    `${origin}/profiles/${profileId}/send/${outputId}`,
  )
  const headers = { Origin: origin, 'X-Routevane-Request': '1' }
  const added = await page.request.post(
    `${origin}/v1/profiles/${profileId}/outputs`,
    { headers, data: { target_id: 'singbox' } },
  )
  expect(added.ok()).toBe(true)
  const { output } = (await added.json()) as { output: { id: string } }
  const built = await page.request.post(
    `${origin}/v1/outputs/${output.id}/build`,
    { headers, data: {} },
  )
  expect(built.ok()).toBe(true)
  await page.goto(`${origin}/profiles/${profileId}`)
  await menu.click()
  await page.getByRole('menuitem', { name: 'Send', exact: true }).click()
  await page.getByRole('menuitem', { name: 'sing-box', exact: true }).click()
  await expect(page).toHaveURL(
    `${origin}/profiles/${profileId}/send/${output.id}`,
  )
})

test('Back retraces the tabs the operator opened and then leaves the profile', async ({
  page,
  origin,
}) => {
  const { profileId } = await buildProfile(page, origin)
  // Arrive from the shelf, so that leaving the profile has a known place to go.
  await page.goto(`${origin}/`)
  await linkTo(page, `/profiles/${profileId}`).click()
  await page.waitForURL(`${origin}/profiles/${profileId}`)
  const tablist = page.getByRole('tablist', { name: 'Profile sections' })
  const selected = (name: string) =>
    tablist.getByRole('tab', { name, selected: true })

  await expect(selected('Contents')).toBeVisible()
  await tablist.getByRole('tab', { name: 'Publishing' }).click()
  await expect(page).toHaveURL(`${origin}/profiles/${profileId}#tab=outputs`)
  await tablist.getByRole('tab', { name: 'File' }).click()
  await expect(page).toHaveURL(`${origin}/profiles/${profileId}#tab=file`)
  await expect(selected('File')).toBeVisible()

  await page.goBack()
  await expect(page).toHaveURL(`${origin}/profiles/${profileId}#tab=outputs`)
  await expect(selected('Publishing')).toBeVisible()
  await page.goBack()
  await expect(page).toHaveURL(`${origin}/profiles/${profileId}`)
  await expect(selected('Contents')).toBeVisible()
  // The tab Back returned to did not push an entry of its own: one more Back
  // is the shelf, not the tab it just left.
  await page.goBack()
  await expect(page).toHaveURL(`${origin}/`)
  await expect(
    page.getByRole('heading', { level: 1, name: 'Profiles' }),
  ).toBeVisible()
  // And Forward walks the same way back in.
  await page.goForward()
  await expect(selected('Contents')).toBeVisible()
  await page.goForward()
  await expect(selected('Publishing')).toBeVisible()
})

test('the header menu names each act in the words of the shelf row menu', async ({
  page,
  origin,
}) => {
  await buildProfile(page, origin)
  await page
    .getByRole('button', { name: 'Actions for profile Discord, YouTube' })
    .click()
  // One wording for one act wherever the act is offered: the shelf row owns
  // these keys, and the page header draws them rather than keeping its own.
  await expect(menuPanel(page).getByRole('menuitem')).toHaveText([
    englishCopy('profiles.menu.export'),
    englishCopy('profiles.menu.send'),
    englishCopy('profiles.addFormat'),
    englishCopy('profiles.rebuild'),
    englishCopy('profiles.archive'),
  ])
})

/**
 * A notice is an addition to the page, not a subtraction from the table it
 * appears above, and the page's closing inset stays under its last row.
 *
 * Nothing else would catch either regression: the notice is readable whether
 * or not the table shrank under it, and a page that has lost its bottom inset
 * still shows everything it holds.
 */
test('a notice lengthens the profile page instead of shortening its table', async ({
  page,
  origin,
}) => {
  await seedTallCatalog(page, origin)
  await page.setViewportSize({ width: 1440, height: 900 })
  const { profileId } = await buildProfile(page, origin)
  // The freshly published profile still shows its one-time link and stands on
  // the tab the publication left it on; the page as an operator returns to it,
  // opened on its contents, is the one whose height is the subject here.
  await page.goto(`${origin}/profiles/${profileId}`)
  const frame = page.getByTestId('rv-list-picker-frame')
  await expect(frame).toBeVisible()
  const standing = (await frame.boundingBox())!.height

  // With nothing added, the section fills the window exactly and the catalog
  // scrolls inside its own frame.
  const resting = await pageFoot(page)
  expect(resting.overflow).toBe(0)
  expect(resting.inset).toBeGreaterThanOrEqual(PAGE_INSET)
  expect(
    await frame.evaluate(
      (element) => element.scrollHeight - element.clientHeight,
    ),
  ).toBeGreaterThan(0)

  try {
    const notice = await forceStaleCatalog(page)
    const added = (await notice.boundingBox())!.height
    expect((await frame.boundingBox())!.height).toBeCloseTo(standing, 0)
    const noticed = await pageFoot(page)
    expect(noticed.overflow).toBeGreaterThanOrEqual(added)
    expect(noticed.inset).toBeGreaterThanOrEqual(PAGE_INSET)
  } finally {
    await releaseCatalog(page)
  }
})

/**
 * The same rule on a tab the window cannot hold: the page scrolls and its
 * closing inset travels with the content instead of staying at the window's
 * edge. The window is the constrained desktop case from the UI contract, which
 * is where this tab and one notice no longer fit together. The tab has no
 * region that fills the window, so what it has to show is only that the page
 * scrolls and where it ends.
 */
test('a tab that outgrows the window keeps the page inset under its last row', async ({
  page,
  origin,
}) => {
  await page.setViewportSize({ width: 1280, height: 600 })
  const { profileId } = await buildProfile(page, origin)
  const added = await page.request.post(
    `${origin}/v1/profiles/${profileId}/outputs`,
    {
      data: { target_id: 'singbox' },
      headers: { Origin: origin, 'X-Routevane-Request': '1' },
    },
  )
  expect(added.ok()).toBe(true)

  await page.goto(`${origin}/profiles/${profileId}`)
  await page
    .getByRole('tablist', { name: 'Profile sections' })
    .getByRole('tab', { name: 'Publishing' })
    .click()
  await expect(
    page.getByRole('row').filter({ hasText: 'sing-box' }),
  ).toBeVisible()

  try {
    await forceStaleCatalog(page)
    const foot = await pageFoot(page)
    expect(foot.overflow).toBeGreaterThan(0)
    expect(foot.inset).toBeGreaterThanOrEqual(PAGE_INSET)
  } finally {
    await releaseCatalog(page)
  }
})

test('the membership switch states its result without a save caption', async ({
  page,
  origin,
}) => {
  const { profileId } = await buildProfile(page, origin)
  await page.goto(`${origin}/profiles/${profileId}`)
  await expect(page.getByTestId('rv-list-picker-frame')).toBeVisible()

  await openList(page, 'Discord').click()
  const card = page.getByRole('dialog', { exact: true, name: 'Discord' })
  const membership = cardMembership(card, englishCopy)
  await expect(membership).toBeVisible()

  const caption = card.getByText('applies when the profile is saved')
  await expect(caption).toHaveCount(0)
  const contents = card.getByRole('heading', { name: 'List contents' })
  const before = (await contents.boundingBox())!.y
  const checkedBefore = await membership.getAttribute('aria-checked')
  await expect(membership).toHaveAccessibleName(
    checkedBefore === 'true' ? 'Active' : 'Inactive',
  )

  await membership.click()

  await expect(membership).toHaveAttribute(
    'aria-checked',
    checkedBefore === 'true' ? 'false' : 'true',
  )
  await expect(cardMembership(card, englishCopy)).toHaveAccessibleName(
    checkedBefore === 'true' ? 'Inactive' : 'Active',
  )
  await expect(caption).toHaveCount(0)
  expect((await contents.boundingBox())!.y).toBe(before)
})

/**
 * The docked card is pinned at the page inset, where it rests when the page
 * fits the window. A page that grew under a notice must not carry the card off
 * the top of the window with it, nor press it against the window's edge, and
 * the act the card owns stays reachable without scrolling back.
 */
test('a docked list card stays pinned while the page scrolls under a notice', async ({
  page,
  origin,
}) => {
  await page.setViewportSize({ width: 1920, height: 906 })
  const { profileId } = await buildProfile(page, origin)
  await page.goto(`${origin}/profiles/${profileId}`)
  await expect(page.getByTestId('rv-list-picker-frame')).toBeVisible()

  try {
    const notice = await forceStaleCatalog(page)
    const height = (await notice.boundingBox())!.height
    await openList(page, 'Discord').click()
    const card = page.getByRole('dialog', { exact: true, name: 'Discord' })
    await expect(card).toHaveClass(/rv-dialog--docked/)
    const resting = (await card.boundingBox())!.y

    await page.getByRole('main').evaluate((main, by) => {
      main.scrollTop = by
    }, height)
    const scrolled = (await card.boundingBox())!
    expect(Math.abs(scrolled.y - resting)).toBeLessThanOrEqual(1)
    expect(scrolled.y).toBeGreaterThanOrEqual(PAGE_INSET - 1)
    // The one act this card owns is still on screen, not scrolled past.
    await expect(cardMembership(card, englishCopy)).toBeInViewport()
  } finally {
    await releaseCatalog(page)
  }
})

test('a refused schedule change keeps the confirmed value and retries visibly', async ({
  page,
  origin,
}) => {
  const { profileId } = await buildProfile(page, origin)
  await page.getByRole('tab', { name: 'Publishing', exact: true }).click()
  await page.route(
    `**/v1/profiles/${profileId}/schedule`,
    (route) => route.fulfill({ status: 503, json: { error: 'unavailable' } }),
    { times: 1 },
  )
  await scheduleField(page).click()
  await page.getByRole('option', { name: 'Daily', exact: true }).click()
  await expect(page.getByText('The schedule was not changed')).toBeVisible()
  await expect(scheduleField(page)).toContainText('As in settings')
  await page.getByRole('button', { name: 'Retry', exact: true }).click()
  await expect(scheduleField(page)).toContainText('Daily')
  const response = await page.request.get(`${origin}/v1/profiles/${profileId}`)
  const detail = (await response.json()) as { schedule: { interval: string } }
  expect(detail.schedule.interval).toBe('daily')
})

const publishingTab = (page: Page) =>
  page
    .getByRole('tablist', { name: 'Profile sections' })
    .getByRole('tab', { name: 'Publishing' })

/** The one-time link on screen, revealed and read as the operator copies it. */
const revealedLink = async (page: Page, origin: string): Promise<string> => {
  const secret = page.getByRole('region', { name: /^Subscription link/ })
  await secret.getByRole('button', { name: 'Show', exact: true }).click()
  const value = secret.getByText(`${origin}/v1/subscriptions/rv1.`)
  await expect(value).toBeVisible()
  return ((await value.textContent()) ?? '').trim()
}

test('the row menu issues a new link that withdraws the old one and shows it once', async ({
  page,
  origin,
}) => {
  const { outputId } = await buildProfile(page, origin)
  const original = await revealedLink(page, origin)
  expect((await page.request.get(original)).status()).toBe(200)

  // Every act on a format is in its row's menu, in one order; a format that
  // has published offers a new link.
  await publishingTab(page).click()
  await formatMenu(page, 'Keenetic').click()
  await expect(menuPanel(page).getByRole('menuitem')).toHaveText([
    'Rebuild',
    'Issue a new link…',
    'Remove format…',
  ])
  await page.keyboard.press('Escape')

  await chooseFormatAction(page, 'Keenetic', 'outputs.menu.reissue')
  const confirm = page.getByRole('dialog', {
    name: 'Issue a new link for Keenetic?',
  })
  await expect(confirm).toContainText(
    englishCopy('outputs.reissue.confirm.body'),
  )
  const [reissued] = await Promise.all([
    page.waitForResponse(
      (response) =>
        response.request().method() === 'POST' &&
        new URL(response.url()).pathname ===
          `/v1/outputs/${outputId}/subscription/reissue`,
    ),
    confirm
      .getByRole('button', { name: 'Issue a new link', exact: true })
      .click(),
  ])
  expect(reissued.ok()).toBe(true)
  await expect(confirm).toBeHidden()

  const replacement = await revealedLink(page, origin)
  expect(replacement).not.toBe(original)
  // The old link stops working at that moment; the new one answers.
  const withdrawn = await page.request.get(original)
  expect(withdrawn.status()).toBe(410)
  await expect(withdrawn.json()).resolves.toMatchObject({
    code: 'subscription_revoked',
  })
  expect((await page.request.get(replacement)).status()).toBe(200)

  // Shown once: after a reload the format's own row offers a new one.
  const token = replacement.slice(replacement.indexOf('rv1.'))
  await page.reload()
  await expect(
    page.getByRole('button', { name: /^Subscription link/ }),
  ).toHaveCount(0)
  await publishingTab(page).click()
  const publishing = page.getByRole('tabpanel', { name: 'Publishing' })
  await expect(publishing.getByText(/shown once/)).toHaveCount(0)
  await formatMenu(page, 'Keenetic').click()
  await expect(
    menuPanel(page).getByRole('menuitem', {
      name: 'Issue a new link…',
      exact: true,
    }),
  ).toBeVisible()
  await page.keyboard.press('Escape')
  expect(await page.content()).not.toContain(token)
  const stored = await page.evaluate(() =>
    JSON.stringify({
      local: { ...localStorage },
      session: { ...sessionStorage },
    }),
  )
  expect(stored).not.toContain(token)
})

test('the row menu removes a format after stating what removal does', async ({
  page,
  origin,
}) => {
  const { profileId } = await buildProfile(page, origin)
  const headers = { Origin: origin, 'X-Routevane-Request': '1' }
  const added = await page.request.post(
    `${origin}/v1/profiles/${profileId}/outputs`,
    { headers, data: { target_id: 'singbox' } },
  )
  expect(added.ok()).toBe(true)
  const { output } = (await added.json()) as { output: { id: string } }
  await page.reload()
  await publishingTab(page).click()
  const publishing = page.getByRole('tabpanel', { name: 'Publishing' })
  const row = publishing.getByRole('row').filter({ hasText: 'sing-box' })
  await expect(row).toHaveCount(1)

  await chooseFormatAction(page, 'sing-box', 'outputs.menu.remove')
  const confirm = page.getByRole('dialog', {
    name: 'Remove the sing-box format?',
  })
  await expect(confirm).toContainText(
    englishCopy('outputs.remove.confirm.body'),
  )
  // Cancelling changes nothing.
  await confirm.getByRole('button', { name: 'Cancel', exact: true }).click()
  await expect(confirm).toBeHidden()
  await expect(row).toHaveCount(1)

  await chooseFormatAction(page, 'sing-box', 'outputs.menu.remove')
  const [removed] = await Promise.all([
    page.waitForResponse(
      (response) =>
        response.request().method() === 'POST' &&
        new URL(response.url()).pathname === `/v1/outputs/${output.id}/remove`,
    ),
    confirm.getByRole('button', { name: 'Remove format', exact: true }).click(),
  ])
  expect(removed.ok()).toBe(true)
  await expect(row).toHaveCount(0)
  await expect(
    publishing.getByRole('row').filter({ hasText: 'Keenetic' }),
  ).toHaveCount(1)
  const detail = (await (
    await page.request.get(`${origin}/v1/profiles/${profileId}`)
  ).json()) as { outputs: { id: string }[] }
  expect(detail.outputs.map((candidate) => candidate.id)).not.toContain(
    output.id,
  )
})

test('diagnostics is one table of rules with a list filter and a search', async ({
  page,
  origin,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await buildProfile(page, origin)
  await page
    .getByRole('tablist', { name: 'Profile sections' })
    .getByRole('tab', { name: 'Diagnostics' })
    .click()
  const rules = page
    .getByRole('tabpanel', { name: 'Diagnostics' })
    .getByRole('region', {
      name: englishCopy('profile.diagnostics.rules'),
      exact: true,
    })
  const table = rules.getByRole('table')
  // The file excluded something, so each rule states whether it entered.
  await expect(table.getByRole('columnheader')).toHaveText([
    'State',
    'List',
    'Value',
    'Reason',
  ])
  // Body rows are the rows that hold cells; the header row holds column
  // headers only.
  const rows = table.getByRole('row').filter({ has: page.getByRole('cell') })
  const total = await rows.count()
  expect(total).toBeGreaterThan(1)
  await expect(rows.filter({ hasText: 'Excluded' }).first()).toBeVisible()
  await expect(table).toContainText('not supported by this format')

  // The list filter keeps the rules of one list.
  const listFilter = rules.getByRole('combobox', { name: 'List', exact: true })
  await listFilter.click()
  await page.getByRole('option', { name: 'YouTube', exact: true }).click()
  await expect(listFilter).toHaveText('YouTube')
  await expect(rows.first()).toBeVisible()
  const youtube = await rows.count()
  expect(youtube).toBeGreaterThan(0)
  expect(youtube).toBeLessThan(total)
  for (const row of await rows.all())
    await expect(row.getByRole('cell').nth(1)).toHaveText('YouTube')

  // The search narrows by value, and says when nothing is left.
  await listFilter.click()
  await page.getByRole('option', { name: 'All lists', exact: true }).click()
  await expect(rows).toHaveCount(total)
  const search = rules.getByRole('searchbox', { name: 'Find a rule' })
  // The fixture's loopback observation was refused in both lists.
  await search.fill('127.0.0.1')
  await expect(rows).toHaveCount(2)
  await expect(rows).toContainText(['127.0.0.1', '127.0.0.1'])
  await search.fill('no-such-rule.invalid')
  await expect(rows).toHaveCount(1)
  await expect(rows.first()).toHaveText(englishCopy('choice.empty'))

  // The table fills the window below its heading, as the composition does.
  const height = (await rules.boundingBox())!.height
  expect(height).toBeGreaterThan(900 * 0.6)
})

// An address or a network is digits and dots, or holds a colon; a domain is
// neither, because its last label is never all digits.
const isAddress = (value: string): boolean =>
  value.includes(':') || /^[\d./]+$/.test(value)

for (const { language, choice } of [
  { language: 'en', choice: 'English' },
  { language: 'ru', choice: 'Русский' },
] as const) {
  test(`diagnostics reads addresses before names and shows only the excluded rules on request (${language})`, async ({
    page,
    origin,
  }) => {
    const copy = copyFor(language)
    const { profileId } = await buildProfile(page, origin)
    await page.goto(`${origin}/settings`)
    await pressSegment(page, choice)
    await page.goto(`${origin}/profiles/${profileId}`)
    await page
      .getByRole('tablist', { name: copy('profile.tabs') })
      .getByRole('tab', { name: copy('profile.tab.diagnostics') })
      .click()
    const rules = page.getByRole('region', {
      name: copy('profile.diagnostics.rules'),
      exact: true,
    })
    const rows = rules.getByRole('row').filter({ has: page.getByRole('cell') })
    const excludedRows = rows.filter({
      has: page.getByRole('cell', {
        name: copy('profile.diagnostics.excluded'),
        exact: true,
      }),
    })
    await expect(excludedRows.first()).toBeVisible()
    const total = await rows.count()
    const excluded = await excludedRows.count()
    expect(excluded).toBeLessThan(total)

    // Every address the fixture yields — its documentation-range seed and the
    // refused loopback answer — stands before every name.
    const values = await Promise.all(
      (await rows.all()).map((row) => row.getByRole('cell').nth(2).innerText()),
    )
    const kinds = values.map((value) =>
      isAddress(value.trim()) ? 'address' : 'name',
    )
    expect(kinds).toContain('address')
    expect(kinds).toContain('name')
    expect(kinds).toEqual(kinds.toSorted())

    const onlyExcluded = copy('profile.diagnostics.filter.excluded').replace(
      '{count}',
      String(excluded),
    )
    await pressSegment(rules, onlyExcluded)
    await expect(rules.getByRole('radio', { name: onlyExcluded })).toBeChecked()
    await expect(rows).toHaveCount(excluded)
    for (const row of await rows.all())
      await expect(row.getByRole('cell').first()).toHaveText(
        copy('profile.diagnostics.excluded'),
      )

    await pressSegment(rules, copy('profile.diagnostics.filter.all'))
    await expect(rows).toHaveCount(total)
  })
}

test('publishing cells stay on one line at 1024px and archived facts keep their height', async ({
  page,
  origin,
}) => {
  await page.setViewportSize({ width: 1024, height: 700 })
  const { profileId } = await buildProfile(page, origin)
  await publishingTab(page).click()
  const publishing = page.getByRole('tabpanel', { name: 'Publishing' })
  const table = publishing.getByRole('table')
  await expect(table).toBeVisible()
  // Every piece of text in the format and date cells is one line box: a long
  // name is shortened, never wrapped.
  const wrapped = await table.evaluate((node) => {
    const found: string[] = []
    for (const cell of node.querySelectorAll(
      'tbody td:nth-child(1), tbody td:nth-child(3)',
    )) {
      const walker = document.createTreeWalker(cell, NodeFilter.SHOW_TEXT)
      for (let text = walker.nextNode(); text; text = walker.nextNode()) {
        if ((text.textContent ?? '').trim() === '') continue
        const range = document.createRange()
        range.selectNodeContents(text)
        const tops = new Set(
          [...range.getClientRects()].map((rect) => Math.round(rect.top)),
        )
        if (tops.size > 1) found.push(text.textContent!.trim())
      }
    }
    return found
  })
  expect(wrapped).toEqual([])
  await expect(table).toContainText('.bat')
  // Download and Send stay in the row at this width as icon buttons named for
  // the format, and the menu holds the rest with its trigger at full target.
  const row = table.getByRole('row').filter({ hasText: 'Keenetic' })
  await expect(
    row.getByRole('link', { name: 'Download the file for Keenetic' }),
  ).toBeVisible()
  await expect(row.getByRole('link', { name: 'Send Keenetic' })).toBeVisible()
  const menu = formatMenu(page, 'Keenetic')
  const trigger = (await menu.boundingBox())!
  expect(Math.min(trigger.width, trigger.height)).toBeGreaterThanOrEqual(24)
  await menu.click()
  await expect(menuPanel(page).getByRole('menuitem')).toHaveText([
    'Rebuild',
    'Issue a new link…',
    'Remove format…',
  ])
  await page.keyboard.press('Escape')

  // At a desktop width the table fits its frame: nothing scrolls sideways.
  await page.setViewportSize({ width: 1440, height: 900 })
  const overflow = await table.evaluate(
    (node) => node.parentElement!.scrollWidth - node.parentElement!.clientWidth,
  )
  expect(overflow).toBeLessThanOrEqual(0)

  // An archived profile states its facts at their own height instead of
  // stretching them to the window the composition would fill.
  const archived = await page.request.post(
    `${origin}/v1/profiles/${profileId}/archive`,
    { headers: { Origin: origin, 'X-Routevane-Request': '1' }, data: {} },
  )
  expect(archived.ok()).toBe(true)
  await page.goto(`${origin}/profiles/${profileId}`)
  const terms = page
    .getByRole('tabpanel', { name: 'Contents' })
    .getByRole('term')
  await expect(terms.first()).toBeVisible()
  // Each fact is a term and its value on one row of the ledger.
  const rowHeights = await terms.evaluateAll((items) =>
    items.map((term) => term.parentElement!.getBoundingClientRect().height),
  )
  expect(rowHeights.length).toBeGreaterThan(1)
  for (const height of rowHeights) expect(height).toBeLessThan(64)
  // The archived row offers no change to its formats. What it still offers is
  // reading the published file; sending it is a change of the connection's
  // device and is withdrawn with the rest. With no connection bound there is
  // nothing left for its menu to hold.
  await publishingTab(page).click()
  const archivedRow = page
    .getByRole('tabpanel', { name: 'Publishing' })
    .getByRole('row')
    .filter({ hasText: 'Keenetic' })
  await expect(
    archivedRow.getByRole('link', { name: 'Download the file for Keenetic' }),
  ).toBeVisible()
  await expect(
    archivedRow.getByRole('link', { name: 'Send Keenetic' }),
  ).toHaveCount(0)
  await expect(formatMenu(page, 'Keenetic')).toHaveCount(0)
})
