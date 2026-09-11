/**
 * The list card under a filter, a held source read and a held rename, in both
 * languages — the geometry and the busy states that regressed before.
 *
 * The worker fixture in `support/served-product.ts` serves this suite
 * alone, over the data directory named below.
 */
import { join } from 'node:path'

import { expect } from '@playwright/test'

import { audit, auditWidths, fits, reviewRoot } from './support/audits'
import { copyFor } from './support/copy'
import { pressSegment } from './support/flows'
import {
  bodyRows,
  cardContents,
  cardMembership,
  cardRow,
  cardRows,
  dialogScrims,
  escapeRegExp,
  libraryRegion,
  libraryRowName,
  segmentOption,
  titleField,
} from './support/queries'
import { test } from './support/served-product'

// The surface negotiates its language from the browser, and English is the
// product's primary language. `productData` is this suite's own data
// directory: it starts empty, no other suite writes to it, and it is gone
// again when the suite ends.
test.use({ locale: 'en-US', productData: 'card-regression' })

// The two widths the card's geometry is measured at, each with the surface the
// card must have mounted by the time it is measured: a modal panel at the audit
// width, the workspace's docked sheet at the wide one. The expectation belongs
// to the width, so the case carries it instead of the body branching on it.
const CARD_WIDTHS = [
  { docked: false, width: 320 },
  { docked: true, width: 1919 },
]

for (const language of ['en', 'ru'] as const) {
  test.describe(`card regression ${language}`, () => {
    test.use({ locale: language === 'ru' ? 'ru-RU' : 'en-US' })
    const copy = copyFor(language)

    test('card audit: filtered rows stay dense in compose and library cards', async ({
      page,
      origin,
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
        // `color-scheme` is declared by a theme's own token block and by
        // nothing else, so its computed value is the observable end of a
        // switch: the audit below reads the contrast of the theme it names.
        const appliedTheme = () =>
          page.evaluate(
            () => getComputedStyle(document.documentElement).colorScheme,
          )
        try {
          for (const theme of ['dark', 'light']) {
            await page.evaluate(
              (value) =>
                document.documentElement.setAttribute('data-rv-theme', value),
              theme,
            )
            await expect.poll(appliedTheme).toBe(theme)
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
        for (const { docked, width } of CARD_WIDTHS) {
          await page.setViewportSize({ width, height: 900 })
          // ResizeObserver moves the card between modal and workspace surfaces.
          // Measure after the requested mode has mounted, not the outgoing tree.
          await expect
            .poll(async () =>
              ((await compose.getAttribute('class')) ?? '').includes(
                'rv-dialog--docked',
              ),
            )
            .toBe(docked)
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
          // The long value is deliberately wider than the card at either
          // width, the docked sheet included, so its row has to wrap past the
          // reference line rather than be clipped to it.
          expect(
            longRowBox!.height,
            JSON.stringify({ referenceHeight, width }),
          ).toBeGreaterThan(referenceHeight)

          await filter.fill('no-such-card-entry')
          await expect(cardRows(compose)).toHaveCount(0)
          await expect(
            compose.getByText(copy('listCard.filter.empty')),
          ).toBeVisible()
          await filter.fill('')
          await expect(cardRows(compose)).toHaveCount(domains.length)
        }
        // Composing keeps its membership switch, because membership is the one
        // act this flow owns.
        await expect(cardMembership(compose, copy)).toBeVisible()
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
          // The command states its own work: it is busy, its glyph is the
          // only mark of that, and the sentence lives in the status beside it.
          await expect(refresh).toHaveAttribute('aria-busy', 'true')
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

          // The card has a dismissal path per surface and the width decides
          // which surface it is on, so each is set here rather than probed:
          // both must refuse while the refresh has no result. Docked, the
          // workspace behind the sheet is inert, so pressing another list's
          // row cannot swap the card for that list's.
          await page.setViewportSize({ width: 1919, height: 900 })
          await expect(library).toHaveClass(/rv-dialog--docked/)
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
          await expect(library).toBeVisible()

          // As a modal panel, the dimming layer behind it is the other way out.
          await page.setViewportSize({ width: 320, height: 900 })
          await expect(library).not.toHaveClass(/rv-dialog--docked/)
          await dialogScrims(page).last().dispatchEvent('pointerdown')
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
        // The library flow owns the list rather than membership, so the control
        // that would change membership is not offered here at all.
        await expect(cardMembership(library, copy)).toHaveCount(0)
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
      origin,
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
      // One attempt at the weekly rule: the choice stays on the confirmed
      // value and refuses further presses while the write is held, and settles
      // on whichever value the answer leaves standing.
      const attempt = async (settled: 'daily' | 'weekly'): Promise<void> => {
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
        await expect(daily).toBeChecked({ checked: settled === 'daily' })
        await expect(weekly).toBeChecked({ checked: settled === 'weekly' })
      }

      // The refused write leaves the confirmed value in place and says so.
      await attempt('daily')
      await expect(
        page
          .getByRole('status')
          .filter({ hasText: copy('settings.refresh.failed') }),
      ).toBeVisible()
      // The retry runs against a second held answer, which is the one that
      // succeeds, so the new value is what the surface confirms.
      hold = new Promise<void>((resolve) => {
        release = resolve
      })
      await attempt('weekly')
      expect(writes).toBe(2)
    })

    test('card audit: compose source reads retry in place without a profile write', async ({
      page,
      origin,
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
