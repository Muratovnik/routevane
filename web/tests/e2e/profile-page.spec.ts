/**
 * A published profile's own page: the one-time link, the file it publishes,
 * its diagnostics, a rename that republishes, and a build that failed.
 *
 * The worker fixture in `support/served-product.ts` serves this suite
 * alone, over the data directory named below.
 */

import { expect } from '@playwright/test'

import { englishCopy } from './support/copy'
import { buildProfile, pressSegment, statesFlow } from './support/flows'
import {
  linkTo,
  menuPanel,
  nameField,
  scheduleField,
  selectedListRows,
} from './support/queries'
import { test } from './support/served-product'

// The surface negotiates its language from the browser, and English is the
// product's primary language. `productData` is this suite's own data
// directory: it starts empty, no other suite writes to it, and it is gone
// again when the suite ends.
test.use({ locale: 'en-US', productData: 'profile-page' })

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
