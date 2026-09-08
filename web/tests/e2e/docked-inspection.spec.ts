/**
 * Inspection on a wide screen, where the card is docked beside the table it
 * was opened from rather than over it.
 *
 * The worker fixture in `support/served-product.ts` serves this suite
 * alone, over the data directory named below.
 */
import { join } from 'node:path'

import { expect, type Locator } from '@playwright/test'

import { reviewRoot } from './support/audits'
import { englishCopy } from './support/copy'
import { chooseFormat, profileIDFromURL } from './support/flows'
import {
  bodyRows,
  cardContents,
  categoryCell,
  composerSettings,
  dialogScrims,
  libraryCategoryCell,
  libraryRow,
  libraryRowName,
  listMembership,
  nameField,
  openList,
} from './support/queries'
import { test } from './support/served-product'

// The surface negotiates its language from the browser, and English is the
// product's primary language. `productData` is this suite's own data
// directory: it starts empty, no other suite writes to it, and it is gone
// again when the suite ends.
test.use({ locale: 'en-US', productData: 'docked-inspection' })

test('library rows disclose inspection and the card keeps its controls while switching lists', async ({
  page,
  origin,
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
  origin,
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
  // The card takes the keyboard when it opens and hands it back when it
  // closes, so Escape is pressed once it holds it: a dismissal that arrives
  // first is answered by a card whose own focus lands afterwards, and the
  // keyboard ends up on neither.
  await expect(filter).toBeFocused()
  await expect(
    card.getByRole('button', { name: 'Close', exact: true }),
  ).toBeEnabled({ timeout: 60000 })
  await page.keyboard.press('Escape')
  await expect(card).toBeHidden()
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
  origin,
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

test('docked inspection transitions the form and table together on opening and closing', async ({
  page,
  origin,
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
  // Each opening is dismissed once the card holds the keyboard, which is what
  // makes the dismissal the card's own rather than a race with its focus.
  const cardFilter = page
    .getByRole('dialog')
    .getByRole('searchbox', { name: englishCopy('listCard.filter') })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await openList(page, 'Discord').click()
  await expect(page.getByRole('dialog')).toBeVisible()
  await expect(cardFilter).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(page.getByRole('dialog')).toBeHidden()
  await page.emulateMedia({ reducedMotion: 'no-preference' })
  await page.evaluate(() => {
    Object.defineProperty(document, 'startViewTransition', {
      value: undefined,
      configurable: true,
    })
  })
  await openList(page, 'Discord').click()
  await expect(page.getByRole('dialog')).toBeVisible()
  await expect(cardFilter).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(page.getByRole('dialog')).toBeHidden()
})
