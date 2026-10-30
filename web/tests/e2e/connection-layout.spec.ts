import { mkdir } from 'node:fs/promises'
import { join } from 'node:path'
import { expect, type Page } from '@playwright/test'
import { audit, auditWidths, reviewRoot } from './support/audits'
import { copyFor, type Copy } from './support/copy'
import {
  chooseFormatAction,
  deviceField,
  formatOption,
} from './support/queries'
import { test } from './support/served-product'

test.use({ productData: 'connection-layout', productCatalog: 'catalog' })

const expectAlignedFields = async (page: Page, copy: Copy): Promise<void> => {
  const boxes = await Promise.all(
    [
      'devices.field.name',
      'deploy.field.address.keenetic',
      'deploy.field.account.keenetic',
      'deploy.field.interface.keenetic',
    ].map(async (key) => {
      const field = page.getByRole('textbox', { name: copy(key), exact: true })
      await expect(field).toBeVisible()
      return (await field.boundingBox())!
    }),
  )
  expect(boxes[0]!.x).toBeLessThan(boxes[1]!.x)
  expect(boxes[0]!.y).toBeCloseTo(boxes[1]!.y, 0)
  expect(boxes[0]!.width).toBeCloseTo(boxes[1]!.width, 0)
  expect(boxes[2]!.x).toBeLessThan(boxes[3]!.x)
  expect(boxes[2]!.y).toBeCloseTo(boxes[3]!.y, 0)
}

// The domain-based format's name, as the row menu announces it.
const DNS_TITLE = {
  en: 'Keenetic (domain-based)',
  ru: 'Keenetic (по доменам)',
} as const

for (const locale of ['en', 'ru'] as const) {
  test.describe(`connection layout (${locale})`, () => {
    test.use({
      locale,
      colorScheme: 'dark',
    })

    test('aligns inputs with unequal help text in both connection forms', async ({
      page,
      origin,
      assertProductAlive,
    }) => {
      const copy = copyFor(locale)
      const seed = await page.request.post(`${origin}/v1/devices`, {
        headers: { Origin: origin, 'X-Routevane-Request': '1' },
        data: {
          target_id: 'keenetic-dns',
          name: `Saved connection ${locale}`,
          address: 'http://192.0.2.1',
          account: 'admin',
          interface: 'Wireguard0',
        },
      })
      expect(seed.ok()).toBe(true)
      await mkdir(reviewRoot, { recursive: true })
      await page.setViewportSize({ width: 1920, height: 1080 })
      await page.goto(`${origin}/connections`)
      await expectAlignedFields(page, copy)
      await page.screenshot({
        animations: 'disabled',
        path: join(reviewRoot, `connection-form-${locale}-saved-1920.png`),
        fullPage: true,
      })

      await page
        .getByRole('button', { name: copy('devices.add'), exact: true })
        .click()
      await deviceField(page, 'devices.field.target', copy).click()
      await formatOption(page, 'Keenetic').click()
      await expectAlignedFields(page, copy)
      await page.screenshot({
        animations: 'disabled',
        path: join(reviewRoot, `connection-form-${locale}-new-1920.png`),
        fullPage: true,
      })

      const name = page.getByRole('textbox', {
        name: copy('devices.field.name'),
        exact: true,
      })
      await name.fill(`New connection ${locale}`)
      await page
        .getByRole('textbox', {
          name: copy('deploy.field.address.keenetic'),
          exact: true,
        })
        .fill('http://10.255.0.2')
      await page
        .getByRole('textbox', {
          name: copy('deploy.field.account.keenetic'),
          exact: true,
        })
        .fill('admin')
      await page
        .getByRole('textbox', {
          name: copy('deploy.field.interface.keenetic'),
          exact: true,
        })
        .fill('Wireguard1')
      await page
        .getByRole('button', { name: copy('action.save'), exact: true })
        .click()
      await expect(
        page.getByRole('heading', {
          level: 2,
          name: `New connection ${locale}`,
          exact: true,
        }),
      ).toBeVisible()
      await expectAlignedFields(page, copy)

      await name.fill(`Resized draft ${locale}`)
      await page.setViewportSize({ width: 768, height: 900 })
      await expect(name).toHaveValue(`Resized draft ${locale}`)
      const address = page.getByRole('textbox', {
        name: copy('deploy.field.address.keenetic'),
        exact: true,
      })
      expect((await address.boundingBox())!.y).toBeGreaterThan(
        (await name.boundingBox())!.y,
      )
      await page
        .getByRole('button', { name: copy('action.cancel'), exact: true })
        .scrollIntoViewIfNeeded()
      await expect(name).toBeInViewport()
      await expect(address).toBeInViewport()
      await page.screenshot({
        animations: 'disabled',
        path: join(reviewRoot, `connection-form-${locale}-draft-768.png`),
        fullPage: true,
      })
      await page.setViewportSize({ width: 1920, height: 1080 })
      await expectAlignedFields(page, copy)
      await expect(name).toHaveValue(`Resized draft ${locale}`)
      await page
        .getByRole('button', { name: copy('action.cancel'), exact: true })
        .click()
      await expect(name).toHaveValue(`New connection ${locale}`)
      assertProductAlive()
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
      // The prefix lives in the format's settings dialog, not beside the table.
      const dnsTitle = DNS_TITLE[locale]
      await chooseFormatAction(page, dnsTitle, 'outputs.menu.settings', copy)
      const settings = page.getByRole('dialog', {
        name: copy('outputs.settings.title').replace('{target}', dnsTitle),
      })
      const prefix = settings.getByRole('textbox', {
        name: copy('outputs.prefix'),
      })
      await prefix.fill('home-vpn')
      await settings
        .getByRole('button', {
          name: copy('outputs.settings.save'),
          exact: true,
        })
        .click()
      await expect(settings).toBeHidden()
      await page.reload()
      await chooseFormatAction(page, dnsTitle, 'outputs.menu.settings', copy)
      await expect(prefix).toHaveValue('home-vpn')
      await page.keyboard.press('Escape')
      await expect(settings).toBeHidden()
      await page.setViewportSize({ width: 1440, height: 900 })
      const table = page.getByRole('table')
      for (const title of ['Keenetic', dnsTitle])
        await expect(
          table.getByRole('button', {
            name: copy('outputs.send.aria').replace('{target}', title),
            exact: true,
          }),
        ).toHaveCount(1)
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
      // The settings dialog keeps the prefix and its Save reachable in a short
      // window and with enlarged text.
      await page.setViewportSize({ width: 1280, height: 640 })
      await chooseFormatAction(page, dnsTitle, 'outputs.menu.settings', copy)
      const save = settings.getByRole('button', {
        name: copy('outputs.settings.save'),
        exact: true,
      })
      await save.scrollIntoViewIfNeeded()
      await expect(save).toBeInViewport()
      await expect(prefix).toBeVisible()
      await page.evaluate(() => {
        document.documentElement.style.fontSize = '200%'
      })
      await prefix.scrollIntoViewIfNeeded()
      await expect(prefix).toBeInViewport()
      await save.scrollIntoViewIfNeeded()
      await expect(save).toBeInViewport()
      expect(
        await audit(page, `connections-profile-${locale}-large-text`),
      ).toEqual([])
      await page.screenshot({
        path: join(reviewRoot, `connections-profile-${locale}-large-text.png`),
      })
      await page.evaluate(() => {
        document.documentElement.style.fontSize = ''
      })
      await page.keyboard.press('Escape')
      await expect(settings).toBeHidden()
      await page.goto(`${origin}/connections`)
      await expect(page.getByRole('table')).toHaveCount(0)
      await page.setViewportSize({ width: 1440, height: 900 })
      await page.screenshot({
        path: join(reviewRoot, `connections-page-${locale}.png`),
      })
      expect(await auditWidths(page, `connections-page-${locale}`)).toEqual([])
      assertProductAlive()
    })
  })
}
