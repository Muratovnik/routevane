/**
 * Settings and the recovery of a failed read: the language the surface speaks,
 * the refresh rule it saves, and what devices, settings and the library say
 * while a read of theirs is failing.
 *
 * The worker fixture in `support/served-product.ts` serves this suite
 * alone, over the data directory named below.
 */

import { expect } from '@playwright/test'

import { auditWidths } from './support/audits'
import { copyFor } from './support/copy'
import {
  documentLanguage,
  expectLibraryCategory,
  pressSegment,
} from './support/flows'
import { deviceField, refreshInterval, segmentOption } from './support/queries'
import { test as servedProduct } from './support/served-product'

/**
 * The categories a test commits to the product.
 *
 * Both language cases of this suite read the same library, so a category one of
 * them makes has to be gone before the other runs — whether the test that made
 * it passed or failed. A test hands the id it created here and states nothing
 * about removing it, which keeps the removal and its own answer out of a
 * `finally` block that would otherwise report cleanup instead of the failure.
 */
const test = servedProduct.extend<{ ownedCategories: string[] }>({
  ownedCategories: async ({ origin, page }, use) => {
    const owned: string[] = []
    await use(owned)
    for (const id of owned) {
      const removed = await page.request.post(
        `${origin}/v1/categories/${id}/remove`,
        {
          data: { lists: 'detach' },
          headers: { 'X-Routevane-Request': '1' },
        },
      )
      expect(removed.status()).toBe(204)
    }
  },
})

// The surface negotiates its language from the browser, and English is the
// product's primary language. `productData` is this suite's own data
// directory: it starts empty, no other suite writes to it, and it is gone
// again when the suite ends.
test.use({ locale: 'en-US', productData: 'settings-and-recovery' })

test('the surface speaks the language of the operator and declares which one', async ({
  page,
  origin,
  assertProductAlive,
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

test('the server refresh setting cannot race while a write is pending', async ({
  page,
  origin,
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
  await page.setViewportSize({ width: 1920, height: 900 })
  const settings = page.getByRole('region', { name: 'Settings', exact: true })
  const readyHeight = (await settings.boundingBox())!.height
  await pressSegment(page, 'Daily')
  await expect(page.getByText('Saving the rule…')).toBeVisible()
  expect((await settings.boundingBox())!.height).toBe(readyHeight)
  const refreshRadios = refreshInterval(page).getByRole('radio')
  await expect(refreshRadios).toHaveCount(3)
  for (let index = 0; index < 3; index++)
    await expect(refreshRadios.nth(index)).toBeDisabled()
  await expect.poll(() => writes).toBe(1)

  release()
  await expect(page.getByText('Saving the rule…')).toHaveCount(0)
  expect((await settings.boundingBox())!.height).toBe(readyHeight)
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
      origin,
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
          .getByRole('button', { name: copy('action.retry'), exact: true })
          .click()
        await page
          .getByRole('button', { name: copy('devices.add'), exact: true })
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
      origin,
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
      origin,
      ownedCategories,
      assertProductAlive,
    }) => {
      test.setTimeout(120000)
      const categoryTitle = `Stale browser ${Date.now()}`
      let failCatalogRead = false
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
        ownedCategories.push(
          ((await createdResponse.json()) as { category: { id: string } })
            .category.id,
        )

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
      }
    })
  })
}
