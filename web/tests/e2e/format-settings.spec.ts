/**
 * What a format's settings dialog offers, and that a choice made there is kept.
 *
 * The worker fixture in `support/served-product.ts` serves this suite alone,
 * over the data directory named below and the full catalog, whose formats are
 * the ones the cases below choose between.
 */
import { expect, type APIRequestContext, type Page } from '@playwright/test'

import { audit } from './support/audits'
import { copyFor, type Copy } from './support/copy'
import { chooseFormatAction, formatMenu, menuPanel } from './support/queries'
import { test } from './support/served-product'

test.use({ productData: 'format-settings', productCatalog: 'catalog' })

// The format names as the row menus announce them. A name the catalog
// translates is the only one that differs between the languages.
const TITLES = {
  en: { bat: 'Keenetic', dns: 'Keenetic (domain-based)' },
  ru: { bat: 'Keenetic', dns: 'Keenetic (по доменам)' },
} as const

type Seeded = { profileId: string; outputs: Record<string, string> }

/** A profile with one format each, made through the API the screens call. */
const seedProfile = async (
  request: APIRequestContext,
  origin: string,
  name: string,
  targets: string[],
): Promise<Seeded> => {
  const headers = { Origin: origin, 'X-Routevane-Request': '1' }
  const list = await request.post(`${origin}/v1/lists`, {
    headers,
    data: { title: `${name} list`, domains: ['example.com'] },
  })
  expect(list.ok()).toBe(true)
  const { list: created } = (await list.json()) as { list: { id: string } }
  // An address beside the domain, so a format that takes only addresses has
  // something to publish.
  const entries = await request.post(
    `${origin}/v1/lists/${created.id}/domains`,
    {
      headers,
      data: { values: ['192.0.2.10'], verdict: 'include' },
    },
  )
  expect(entries.ok()).toBe(true)
  const profile = await request.post(`${origin}/v1/profiles`, {
    headers,
    data: {
      name,
      lists: [created.id],
      categories: [],
      exclusions: [],
      list_domains: {},
    },
  })
  expect(profile.ok()).toBe(true)
  const { profile: stored } = (await profile.json()) as {
    profile: { id: string }
  }
  // A build reads the profile's lists, which a refresh has resolved.
  const refreshed = await request.post(
    `${origin}/v1/profiles/${stored.id}/refresh`,
    { headers, data: {} },
  )
  expect(refreshed.ok()).toBe(true)
  const outputs: Record<string, string> = {}
  for (const target of targets) {
    const output = await request.post(
      `${origin}/v1/profiles/${stored.id}/outputs`,
      { headers, data: { target_id: target } },
    )
    expect(output.ok()).toBe(true)
    const body = (await output.json()) as { output: { id: string } }
    outputs[target] = body.output.id
  }
  return { profileId: stored.id, outputs }
}

const storedKeep = async (
  request: APIRequestContext,
  origin: string,
  outputId: string,
): Promise<boolean> => {
  const response = await request.get(`${origin}/v1/outputs/${outputId}`)
  expect(response.ok()).toBe(true)
  return ((await response.json()) as { keep_addresses: boolean }).keep_addresses
}

const openPublishing = async (
  page: Page,
  origin: string,
  profileId: string,
  copy: Copy,
): Promise<void> => {
  await page.goto(`${origin}/profiles/${profileId}`)
  await page
    .getByRole('tab', { name: copy('profile.tab.outputs'), exact: true })
    .click()
}

for (const language of ['en', 'ru'] as const) {
  test.describe(`format settings (${language})`, () => {
    test.use({ locale: language === 'ru' ? 'ru-RU' : 'en-US' })
    const copy = copyFor(language)
    const titles = TITLES[language]
    const dialogFor = (page: Page, title: string) =>
      page.getByRole('dialog', {
        name: copy('outputs.settings.title').replace('{target}', title),
      })
    const keepSwitch = (page: Page, title: string) =>
      dialogFor(page, title).getByRole('switch', {
        name: copy('outputs.keepAddresses'),
      })

    test('keeps the choice to hold addresses beside domains across a reload, for the formats that can hold it', async ({
      page,
      origin,
      assertProductAlive,
    }) => {
      test.setTimeout(120000)
      const seeded = await seedProfile(
        page.request,
        origin,
        `Settings ${language}`,
        ['mikrotik', 'keenetic-dns'],
      )
      const mikrotik = seeded.outputs['mikrotik']!
      await openPublishing(page, origin, seeded.profileId, copy)

      // A format without group names has the switch and no prefix.
      await chooseFormatAction(page, 'MikroTik', 'outputs.menu.settings', copy)
      const dialog = dialogFor(page, 'MikroTik')
      const keep = keepSwitch(page, 'MikroTik')
      await expect(keep).toBeVisible()
      await expect(keep).not.toBeChecked()
      await expect(
        dialog.getByRole('textbox', { name: copy('outputs.prefix') }),
      ).toHaveCount(0)
      expect(await audit(page, `format-settings-${language}`)).toEqual([])
      await keep.click()
      await expect(keep).toBeChecked()
      // A change is a draft until Save: nothing is stored yet.
      expect(await storedKeep(page.request, origin, mikrotik)).toBe(false)
      await dialog
        .getByRole('button', {
          name: copy('outputs.settings.save'),
          exact: true,
        })
        .click()
      await expect(dialog).toBeHidden()
      expect(await storedKeep(page.request, origin, mikrotik)).toBe(true)

      // The page read the format again, so reopening shows what was stored.
      await chooseFormatAction(page, 'MikroTik', 'outputs.menu.settings', copy)
      await expect(keep).toBeChecked()
      await page.keyboard.press('Escape')
      await expect(dialog).toBeHidden()

      await page.reload()
      await page
        .getByRole('tab', { name: copy('profile.tab.outputs'), exact: true })
        .click()
      await chooseFormatAction(page, 'MikroTik', 'outputs.menu.settings', copy)
      await expect(keep).toBeChecked()
      await keep.click()
      await dialog
        .getByRole('button', {
          name: copy('outputs.settings.save'),
          exact: true,
        })
        .click()
      await expect(dialog).toBeHidden()
      expect(await storedKeep(page.request, origin, mikrotik)).toBe(false)

      // The domain-based Keenetic format has both of its settings in one
      // dialog, and one Save writes them.
      const dns = seeded.outputs['keenetic-dns']!
      await chooseFormatAction(page, titles.dns, 'outputs.menu.settings', copy)
      const dnsDialog = dialogFor(page, titles.dns)
      const prefix = dnsDialog.getByRole('textbox', {
        name: copy('outputs.prefix'),
      })
      await prefix.fill('both')
      await keepSwitch(page, titles.dns).click()
      await dnsDialog
        .getByRole('button', {
          name: copy('outputs.settings.save'),
          exact: true,
        })
        .click()
      await expect(dnsDialog).toBeHidden()
      expect(await storedKeep(page.request, origin, dns)).toBe(true)
      const stored = await page.request.get(`${origin}/v1/outputs/${dns}`)
      expect(await stored.json()).toMatchObject({ fqdn_group_prefix: 'both' })
      assertProductAlive()
    })

    test('writes the choice and builds at once from Rebuild now', async ({
      page,
      origin,
    }) => {
      const seeded = await seedProfile(
        page.request,
        origin,
        `Rebuild ${language}`,
        ['mikrotik'],
      )
      const mikrotik = seeded.outputs['mikrotik']!
      await openPublishing(page, origin, seeded.profileId, copy)

      await chooseFormatAction(page, 'MikroTik', 'outputs.menu.settings', copy)
      await keepSwitch(page, 'MikroTik').click()
      const built = page.waitForResponse(
        (response) =>
          response.url() === `${origin}/v1/outputs/${mikrotik}/build` &&
          response.request().method() === 'POST',
      )
      await dialogFor(page, 'MikroTik')
        .getByRole('button', { name: copy('outputs.settings.rebuild') })
        .click()
      expect((await built).ok()).toBe(true)
      await expect(dialogFor(page, 'MikroTik')).toBeHidden()
      expect(await storedKeep(page.request, origin, mikrotik)).toBe(true)
    })

    test('offers no settings to a format that cannot hold the choice', async ({
      page,
      origin,
    }) => {
      const seeded = await seedProfile(
        page.request,
        origin,
        `No settings ${language}`,
        ['keenetic', 'openwrt'],
      )
      await openPublishing(page, origin, seeded.profileId, copy)

      for (const title of [titles.bat, 'OpenWrt']) {
        await formatMenu(page, title, copy).click()
        await expect(
          menuPanel(page).getByRole('menuitem', {
            name: copy('outputs.menu.rebuild'),
            exact: true,
          }),
        ).toBeVisible()
        await expect(
          menuPanel(page).getByRole('menuitem', {
            name: copy('outputs.menu.settings'),
            exact: true,
          }),
        ).toHaveCount(0)
        await page.keyboard.press('Escape')
        await expect(menuPanel(page)).toHaveCount(0)
      }
    })
  })
}
