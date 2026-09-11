/**
 * The shelf of saved profiles: what a row says, where its cells lead, what
 * the archive holds, and how a slow or failed read of it reads.
 *
 * The worker fixture in `support/served-product.ts` serves this suite
 * alone, over the data directory named below.
 */

import { expect } from '@playwright/test'

import { assertNoOverflow, audit } from './support/audits'
import { englishCopy } from './support/copy'
import { buildProfile, statesFlow } from './support/flows'
import {
  addConnectionField,
  linkTo,
  menuPanel,
  nameField,
  profileOutputsCell,
  scheduleField,
} from './support/queries'
import { test } from './support/served-product'

// The surface negotiates its language from the browser, and English is the
// product's primary language. `productData` is this suite's own data
// directory: it starts empty, no other suite writes to it, and it is gone
// again when the suite ends.
test.use({ locale: 'en-US', productData: 'profile-shelf' })

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
