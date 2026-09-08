/**
 * The card that shows one list: the entries it takes, the sheet it is drawn
 * as, the height its table claims, and what it says about its sources.
 *
 * The worker fixture in `support/served-product.ts` serves this suite
 * alone, over the data directory named below.
 */
import { mkdir, rm, writeFile } from 'node:fs/promises'
import { join } from 'node:path'

import AxeBuilder from '@axe-core/playwright'
import { expect } from '@playwright/test'

import { dictionaries } from '../../src/shared/i18n/messages'

import { openLibraryCategory } from './support/flows'
import {
  cardContents,
  cardRow,
  cardRows,
  dialogScrims,
  menuPanel,
} from './support/queries'
import { repositoryRoot, test } from './support/served-product'

// The surface negotiates its language from the browser, and English is the
// product's primary language. `productData` is this suite's own data
// directory: it starts empty, no other suite writes to it, and it is gone
// again when the suite ends.
test.use({ locale: 'en-US', productData: 'list-card' })

test('the list card takes domains, addresses and networks, typed or imported', async ({
  page,
  origin,
  assertProductAlive,
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
  origin,
  assertProductAlive,
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
          sheet?.querySelector<HTMLButtonElement>('.rv-dialog__close') ?? null
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

/**
 * The sheet's height belongs to the table in it. This is the band of nothing
 * that used to sit between the last row and the footer: the contents were
 * capped at the composer's height while the sheet itself was the viewport.
 */
test('the list card fills the sheet rather than leaving a band above its footer', async ({
  page,
  origin,
  assertProductAlive,
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

test('source skips are visible without changing profiles, and clear after a clean refresh', async ({
  page,
  origin,
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
