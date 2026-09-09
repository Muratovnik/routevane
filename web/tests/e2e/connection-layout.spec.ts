import { mkdir } from 'node:fs/promises'
import { join } from 'node:path'
import { expect } from '@playwright/test'
import { audit, auditWidths, reviewRoot } from './support/audits'
import { copyFor } from './support/copy'
import { catalogDisclosure } from './support/queries'
import { test } from './support/served-product'

test.use({ productData: 'connection-layout', productCatalog: 'catalog' })

for (const locale of ['en', 'ru'] as const) {
  test.describe(`connection layout (${locale})`, () => {
    test.use({
      locale,
      colorScheme: 'dark',
    })

    test('keeps DNS configuration and actions reachable across window sizes', async ({
      page,
      origin,
      assertProductAlive,
    }) => {
      test.setTimeout(180000)
      const copy = copyFor(locale)
      const headers = { Origin: origin, 'X-Routevane-Request': '1' }
      const listResponse = await page.request.post(`${origin}/v1/lists`, {
        headers,
        data: { title: `Layout example ${locale}`, domains: ['example.com'] },
      })
      expect(listResponse.ok()).toBe(true)
      const { list } = (await listResponse.json()) as { list: { id: string } }
      const entries = await page.request.post(
        `${origin}/v1/lists/${list.id}/domains`,
        {
          headers,
          data: { values: ['192.0.2.10'], verdict: 'include' },
        },
      )
      expect(entries.ok()).toBe(true)
      const profileResponse = await page.request.post(`${origin}/v1/profiles`, {
        headers,
        data: {
          name: `Home VPN ${locale}`,
          lists: [list.id],
          categories: [],
          exclusions: [],
          list_domains: {},
        },
      })
      expect(profileResponse.ok()).toBe(true)
      const { profile } = (await profileResponse.json()) as {
        profile: { id: string }
      }
      const refreshed = await page.request.post(
        `${origin}/v1/profiles/${profile.id}/refresh`,
        { headers, data: {} },
      )
      expect(refreshed.ok()).toBe(true)
      for (const target_id of ['keenetic', 'keenetic-dns']) {
        const output = await page.request.post(
          `${origin}/v1/profiles/${profile.id}/outputs`,
          { headers, data: { target_id } },
        )
        expect(output.ok()).toBe(true)
        const created = (await output.json()) as { output: { id: string } }
        const built = await page.request.post(
          `${origin}/v1/outputs/${created.output.id}/build`,
          { headers, data: {} },
        )
        expect(
          built.ok(),
          ((await built.json()) as { error?: string }).error,
        ).toBe(true)
      }
      await page.goto(`${origin}/profiles/${profile.id}#tab=outputs`)
      await page
        .getByRole('tab', { name: copy('profile.tab.outputs'), exact: true })
        .click()
      const prefix = page.getByRole('textbox', { name: copy('outputs.prefix') })
      await expect(prefix).toBeVisible()
      await prefix.fill('home-vpn')
      await page
        .getByRole('button', { name: copy('outputs.prefix.save') })
        .click()
      await expect(page.getByText(copy('outputs.prefix.saved'))).toBeVisible()
      await page.reload()
      await expect(prefix).toHaveValue('home-vpn')
      await page.setViewportSize({ width: 1440, height: 900 })
      const table = page.getByRole('table')
      await expect(
        table.getByRole('link', { name: copy('outputs.send'), exact: true }),
      ).toHaveCount(2)
      for (const action of await table.getByRole('link').all())
        await expect(action).toBeInViewport()
      // The original nowrap prefix form pushed the table far beyond the workspace.
      expect(
        await table.evaluate(
          (node) =>
            node.getBoundingClientRect().right <=
            document.documentElement.clientWidth,
        ),
      ).toBe(true)
      expect(
        await table
          .getByRole('cell')
          .evaluateAll((cells) =>
            cells.every((cell) => cell.scrollWidth <= cell.clientWidth + 1),
          ),
      ).toBe(true)
      await mkdir(reviewRoot, { recursive: true })
      await page.screenshot({
        path: join(reviewRoot, `connections-profile-${locale}.png`),
      })
      expect(await auditWidths(page, `connections-profile-${locale}`)).toEqual(
        [],
      )
      await page.setViewportSize({ width: 1280, height: 640 })
      await page
        .getByRole('button', { name: copy('outputs.prefix.save') })
        .scrollIntoViewIfNeeded()
      await expect(prefix).toBeVisible()
      await page.evaluate(() => {
        document.documentElement.style.fontSize = '200%'
      })
      await prefix.scrollIntoViewIfNeeded()
      expect(
        await audit(page, `connections-profile-${locale}-large-text`),
      ).toEqual([])
      await page.screenshot({
        path: join(reviewRoot, `connections-profile-${locale}-large-text.png`),
      })
      await page.evaluate(() => {
        document.documentElement.style.fontSize = ''
      })
      await page.goto(`${origin}/connections`)
      await catalogDisclosure(page, copy).click()
      await expect(page.getByRole('table')).toBeVisible()
      await page.setViewportSize({ width: 1440, height: 900 })
      await page.screenshot({
        path: join(reviewRoot, `connections-catalog-${locale}.png`),
      })
      expect(await auditWidths(page, `connections-catalog-${locale}`)).toEqual(
        [],
      )
      assertProductAlive()
    })
  })
}
