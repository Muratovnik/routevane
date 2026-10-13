/**
 * The shelf of saved profiles: what a row says, where its cells lead, what
 * the archive holds, and how a slow or failed read of it reads.
 *
 * The worker fixture in `support/served-product.ts` serves this suite
 * alone, over the data directory named below.
 */

import { expect, type Page } from '@playwright/test'

import { assertNoOverflow, audit } from './support/audits'
import { englishCopy } from './support/copy'
import { buildProfile, pressSegment, statesFlow } from './support/flows'
import {
  addConnectionField,
  linkTo,
  menuPanel,
  nameField,
  profileOutputsCell,
  scheduleField,
  segmentOption,
} from './support/queries'
import { test } from './support/served-product'

// The surface negotiates its language from the browser, and English is the
// product's primary language. `productData` is this suite's own data
// directory: it starts empty, no other suite writes to it, and it is gone
// again when the suite ends.
test.use({ locale: 'en-US', productData: 'profile-shelf' })

const mutationHeaders = (origin: string): Record<string, string> => ({
  Origin: origin,
  'X-Routevane-Request': '1',
})

// The archive is counted by the server, so the suite asks the server what the
// count is instead of assuming how many profiles earlier cases left archived.
const archivedCount = async (page: Page, origin: string): Promise<number> => {
  const shelf = await page.request.get(`${origin}/v1/profile-shelf/archived`)
  return ((await shelf.json()) as { archived_count: number }).archived_count
}

type SeededProfile = { id: string; outputs: Record<string, string> }

// A profile with every named format published, made through the service the
// way the composer and the profile page do, so a case about the shelf does not
// have to walk the composer once per format.
const seedProfile = async (
  page: Page,
  origin: string,
  name: string,
  targets: string[],
): Promise<SeededProfile> => {
  const post = async <T>(path: string, data: object = {}): Promise<T> => {
    const response = await page.request.post(`${origin}${path}`, {
      data,
      headers: mutationHeaders(origin),
    })
    expect(response.ok(), path).toBe(true)
    return (await response.json()) as T
  }
  const created = await post<{ profile: { id: string } }>('/v1/profiles', {
    categories: [],
    exclusions: [],
    lists: ['youtube'],
    name,
  })
  const id = created.profile.id
  const outputs: Record<string, string> = {}
  for (const target of targets) {
    const added = await post<{ output: { id: string } }>(
      `/v1/profiles/${id}/outputs`,
      { target_id: target },
    )
    outputs[target] = added.output.id
  }
  await post(`/v1/profiles/${id}/refresh`)
  for (const outputID of Object.values(outputs))
    await post(`/v1/outputs/${outputID}/build`)
  return { id, outputs }
}

test('legacy profile names are explained as restored lists', async ({
  page,
  origin,
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

test('an archived profile leaves the shelf, keeps its file, and comes back whole', async ({
  page,
  origin,
  assertProductAlive,
}) => {
  test.setTimeout(180000)
  const pageErrors: string[] = []
  page.on('pageerror', (error) => pageErrors.push(error.message))
  // Earlier cases leave nothing archived, so the filter counts this profile alone.
  expect(await archivedCount(page, origin)).toBe(0)

  const { profileId } = await buildProfile(page, origin)
  // Earlier walkthroughs left their own lists on the shelf, so every row here
  // is addressed by this profile's identity rather than by its title.
  const shelfRow = page
    .getByRole('row')
    .filter({ has: linkTo(page, `/profiles/${profileId}`) })
  await page
    .getByRole('tablist', { name: 'Profile sections' })
    .getByRole('tab', { name: 'Publishing' })
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
    .getByRole('tab', { name: 'Publishing' })
    .click()
  await expect(addConnectionField(page)).toHaveCount(0)
  await expect(
    page.getByRole('main').getByRole('row').filter({ hasText: 'Keenetic' }),
  ).toBeVisible()

  // The archived profile has left the shelf without leaving the library: it is
  // one filter away, in the same table, and the filter states the server's count.
  await page.getByRole('link', { name: 'Profiles' }).first().click()
  await page.waitForURL(`${origin}/`)
  await expect(shelfRow).toHaveCount(0)
  await expect(segmentOption(page, 'Archive 1')).toHaveCount(1)
  await pressSegment(page, 'Archive 1')
  const archivedRow = page
    .getByRole('row')
    .filter({ has: linkTo(page, `/profiles/${profileId}`) })
  await expect(archivedRow).toHaveCount(1)
  // The same columns as the shelf, plus when the profile left it.
  for (const column of ['Profile', 'Formats', 'Content from', 'Archived since'])
    await expect(
      page.getByRole('columnheader', { name: column, exact: true }),
    ).toBeVisible()
  await expect(archivedRow).toContainText(/\b20\d\d\b/)
  await archivedRow
    .getByRole('button', {
      name: 'Actions for profile Discord, YouTube',
    })
    .click()
  await menuPanel(page)
    .getByRole('menuitem', { name: 'Export to file' })
    .click()
  await expect(
    menuPanel(page).last().getByRole('menuitem', { name: 'BAT · routes' }),
  ).toBeVisible()
  await page.keyboard.press('Escape')

  // The archive is part of the screen, so it is held to the same gates.
  expect(await audit(page, 'library-archive')).toEqual([])
  await assertNoOverflow(page, 'library-archive')

  // Restoring puts the profile back on the shelf with everything it had, and
  // publishes nothing by itself. The archive is empty again, so its filter goes
  // and the shelf is what is shown.
  mutations.length = 0
  await archivedRow
    .getByRole('button', { name: 'Actions for profile Discord, YouTube' })
    .click()
  await menuPanel(page).getByRole('menuitem', { name: 'Restore' }).click()
  await expect(shelfRow).toHaveCount(1)
  expect(mutations).toEqual([`/v1/profiles/${profileId}/restore`])
  await expect(page.getByRole('radio')).toHaveCount(0)
  expect(await archivedCount(page, origin)).toBe(0)

  expect(pageErrors).toEqual([])
  assertProductAlive()
})

// Archiving runs the moment it is chosen, so the way back is offered on the
// shelf itself: what moved is named and one action puts it back.
test('archiving from the shelf offers an undo that puts the profile back', async ({
  page,
  origin,
}) => {
  test.setTimeout(120000)
  expect(await archivedCount(page, origin)).toBe(0)
  const { profileId } = await buildProfile(page, origin)
  await page.goto(origin)
  const shelfRow = page
    .getByRole('row')
    .filter({ has: linkTo(page, `/profiles/${profileId}`) })
  const mutations: string[] = []
  page.on('request', (request) => {
    const path = new URL(request.url()).pathname
    if (request.method() === 'POST' && statesFlow(path)) mutations.push(path)
  })

  await shelfRow.getByRole('button', { name: /^Actions for profile/ }).click()
  await menuPanel(page).getByRole('menuitem', { name: 'Archive' }).click()

  const notice = page
    .getByRole('status')
    .filter({ hasText: 'moved to the archive' })
  await expect(notice).toContainText(
    'Profile “Discord, YouTube” moved to the archive',
  )
  await expect(shelfRow).toHaveCount(0)
  await expect(segmentOption(page, 'Archive 1')).toHaveCount(1)

  await notice.getByRole('button', { name: 'Undo' }).click()
  await expect(shelfRow).toHaveCount(1)
  await expect(notice).toHaveCount(0)
  await expect(page.getByRole('radio')).toHaveCount(0)
  expect(mutations).toEqual([
    `/v1/profiles/${profileId}/archive`,
    `/v1/profiles/${profileId}/restore`,
  ])
  expect(await archivedCount(page, origin)).toBe(0)
})

// Deletion is the step after archival and withdraws the profile's links. The
// case reads the real link before and after, so "stops working" is a status the
// server answered rather than a sentence the screen printed.
test('deleting an archived profile asks first, then its subscription link answers gone', async ({
  page,
  origin,
}) => {
  test.setTimeout(120000)
  expect(await archivedCount(page, origin)).toBe(0)
  const { profileId } = await buildProfile(page, origin)
  await page
    .getByRole('tablist', { name: 'Profile sections' })
    .getByRole('tab', { name: 'Publishing' })
    .click()
  await page.getByRole('button', { name: 'Show', exact: true }).click()
  const secret = page.getByRole('region', { name: /^Subscription link/ })
  const link =
    /http:\/\/127\.0\.0\.1:\d+\/v1\/subscriptions\/rv1\.[a-f0-9]{32}\.[\w-]{43}/.exec(
      (await secret.textContent()) ?? '',
    )?.[0]
  expect(link).toBeDefined()
  const subscription = link ?? ''
  expect((await page.request.get(subscription)).status()).toBe(200)

  await page.goto(origin)
  const shelfRow = page
    .getByRole('row')
    .filter({ has: linkTo(page, `/profiles/${profileId}`) })
  await shelfRow.getByRole('button', { name: /^Actions for profile/ }).click()
  await menuPanel(page).getByRole('menuitem', { name: 'Archive' }).click()
  await pressSegment(page, 'Archive 1')
  const archivedRow = page
    .getByRole('row')
    .filter({ has: linkTo(page, `/profiles/${profileId}`) })
  const mutations: string[] = []
  page.on('request', (request) => {
    const path = new URL(request.url()).pathname
    if (request.method() === 'POST' && statesFlow(path)) mutations.push(path)
  })

  // The permanent action is the last item of the archived menu. It asks, and
  // asking changes nothing.
  await archivedRow
    .getByRole('button', { name: /^Actions for profile/ })
    .click()
  const items = menuPanel(page).getByRole('menuitem')
  await expect(items.last()).toHaveText('Delete permanently…')
  await items.last().click()
  const dialog = page.getByRole('dialog', {
    name: 'Delete profile “Discord, YouTube” permanently?',
  })
  await expect(dialog).toContainText(
    'The subscription links of this profile will stop working',
  )
  await expect(dialog).toContainText(
    'Published files and the delivery log are kept',
  )
  expect(await audit(page, 'library-delete-dialog')).toEqual([])
  await dialog.getByRole('button', { name: 'Cancel' }).click()
  await expect(dialog).toHaveCount(0)
  expect(mutations).toEqual([])
  expect((await page.request.get(subscription)).status()).toBe(200)
  await expect(archivedRow).toHaveCount(1)

  await archivedRow
    .getByRole('button', { name: /^Actions for profile/ })
    .click()
  await menuPanel(page)
    .getByRole('menuitem', { name: 'Delete permanently…' })
    .click()
  await dialog
    .getByRole('button', { name: 'Delete permanently', exact: true })
    .click()

  // The row is gone, the archive is empty so its filter leaves, and the server
  // no longer knows the profile or honours its link.
  await expect(archivedRow).toHaveCount(0)
  await expect(
    page
      .getByRole('status')
      .filter({ hasText: 'Profile “Discord, YouTube” deleted' }),
  ).toBeVisible()
  await expect(page.getByRole('radio')).toHaveCount(0)
  expect(mutations).toEqual([`/v1/profiles/${profileId}/delete`])
  expect(
    (await page.request.get(`${origin}/v1/profiles/${profileId}`)).status(),
  ).toBe(404)
  expect((await page.request.get(subscription)).status()).toBe(410)
  expect(await archivedCount(page, origin)).toBe(0)
})

// A profile with two published formats has two files, so "copy the contents"
// has to be asked which one. The sing-box file is the second one; copying the
// first would have put the Keenetic file on the clipboard.
test('copying contents from the row copies the format that was chosen', async ({
  page,
  origin,
}) => {
  const seeded = await seedProfile(page, origin, 'Two formats', [
    'keenetic',
    'singbox',
  ])
  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: {
        writeText: (text: string) => {
          Object.assign(window, { copiedText: text })
          return Promise.resolve()
        },
      },
    })
  })
  const shelf = await page.request.get(`${origin}/v1/profile-shelf/active`)
  const card = (
    (await shelf.json()) as {
      profiles: {
        id: string
        outputs: { id: string; latest: { id: string } }[]
      }[]
    }
  ).profiles.find((candidate) => candidate.id === seeded.id)
  const singBoxArtifact = card?.outputs.find(
    (output) => output.id === seeded.outputs.singbox,
  )?.latest.id
  const keeneticArtifact = card?.outputs.find(
    (output) => output.id === seeded.outputs.keenetic,
  )?.latest.id
  const expected = await (
    await page.request.get(`${origin}/v1/artifacts/${singBoxArtifact}`)
  ).text()
  const other = await (
    await page.request.get(`${origin}/v1/artifacts/${keeneticArtifact}`)
  ).text()
  expect(expected).not.toBe(other)

  await page.goto(origin)
  const row = page
    .getByRole('row')
    .filter({ has: linkTo(page, `/profiles/${seeded.id}`) })
  await row.getByRole('button', { name: /^Actions for profile/ }).click()
  await menuPanel(page).getByRole('menuitem', { name: 'Copy contents' }).click()
  const formats = menuPanel(page).last()
  await expect(formats.getByRole('menuitem')).toHaveText([
    'Keenetic',
    'sing-box',
  ])
  await formats.getByRole('menuitem', { name: 'sing-box' }).click()

  await expect(page.getByText('Contents copied')).toBeVisible()
  expect(
    await page.evaluate(
      () => (window as unknown as { copiedText: string }).copiedText,
    ),
  ).toBe(expected)
})

// A first build that failed leaves no file and one failed attempt. The row has
// to say the build failed, not only that nothing was published. The service has
// no way to be made to fail a build on demand, so the shelf read is answered
// with the one fact this case is about changed.
test('a profile whose build failed is marked in its row', async ({
  page,
  origin,
}) => {
  const seeded = await seedProfile(page, origin, 'Failing build', ['keenetic'])
  await page.route('**/v1/profile-shelf/active', async (route) => {
    const response = await route.fetch()
    const payload = (await response.json()) as {
      profiles: { id: string; outputs: Record<string, unknown>[] }[]
    }
    for (const profile of payload.profiles) {
      if (profile.id !== seeded.id) continue
      profile.outputs = profile.outputs.map((output) => ({
        ...output,
        last_attempt: {
          code: 'source_unavailable',
          completed_at: '2026-10-13T12:00:00Z',
          status: 'failed',
        },
        latest: null,
      }))
    }
    await route.fulfill({ json: payload, response })
  })
  await page.goto(origin)
  const row = page
    .getByRole('row')
    .filter({ has: linkTo(page, `/profiles/${seeded.id}`) })

  await expect(row.getByText('Build failed', { exact: true })).toBeVisible()
  await expect(row.getByText('No published file')).toHaveCount(0)
  expect(await audit(page, 'library-failed-build')).toEqual([])
})

// The hidden actions column header once escaped `.profiles__scroll` and
// stretched the document at 320px; the scroll box is its containing block now,
// and this test keeps it that way.
test('the populated library never scrolls sideways', async ({
  page,
  origin,
}) => {
  test.setTimeout(120000)
  await buildProfile(page, origin)
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

test('quick profile reads stay quiet while slow reads and failures remain visible', async ({
  page,
  origin,
}) => {
  const profiles = await page.request.get(`${origin}/v1/profile-shelf/active`)
  expect(profiles.ok()).toBe(true)
  const payload = await profiles.json()
  // The shelf waits for these decorations as well as its profiles. Keep all
  // initial reads fast; an uncontrolled catalog read can outlast the feedback
  // delay even when the profiles response is immediate.
  const decorations = await Promise.all(
    [
      '/v1/lists',
      '/v1/targets',
      '/v1/deployments/targets',
      '/v1/export-formats',
    ].map(async (path) => {
      const response = await page.request.get(`${origin}${path}`)
      expect(response.ok()).toBe(true)
      return { path, payload: await response.json() }
    }),
  )
  for (const decoration of decorations) {
    await page.route(`**${decoration.path}`, (route) =>
      route.fulfill({ status: 200, json: decoration.payload }),
    )
  }
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
  await page.route('**/v1/profile-shelf/active', async (route) => {
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

test('profile rows navigate from their cells while menus and links keep their actions', async ({
  page,
  origin,
}) => {
  const { profileId } = await buildProfile(page, origin)
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
  await expect(page.getByRole('menu')).toBeHidden()
  await expect(row.getByRole('button')).toBeFocused()
  await linkTo(page, `/profiles/${profileId}`).focus()
  await expect(linkTo(page, `/profiles/${profileId}`)).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(origin + '/profiles/' + profileId)
})
