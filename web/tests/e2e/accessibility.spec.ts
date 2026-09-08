/**
 * The accessibility gates every section is held to, in both languages, and
 * the helper that decides what counts as a finding.
 *
 * The worker fixture in `support/served-product.ts` serves this suite
 * alone, over the data directory named below.
 */
import { mkdir } from 'node:fs/promises'
import { join } from 'node:path'

import { expect } from '@playwright/test'

import { dictionaries } from '../../src/shared/i18n/messages'

import {
  audit,
  type AuditFinding,
  auditWidths,
  reviewRoot,
} from './support/audits'
import { chooseFormat, openLibraryCategory } from './support/flows'
import {
  catalogDisclosure,
  escapeRegExp,
  linkTo,
  listMembership,
} from './support/queries'
import { test } from './support/served-product'

// The surface negotiates its language from the browser, and English is the
// product's primary language. `productData` is this suite's own data
// directory: it starts empty, no other suite writes to it, and it is gone
// again when the suite ends.
test.use({ locale: 'en-US', productData: 'accessibility' })

test('the accessibility helper detects undersized adjacent targets and accepts the valid control', async ({
  page,
}) => {
  await page.setContent(`<!doctype html><html lang="en"><head><title>Target size control</title>
    <style>button { width: 12px; height: 12px; padding: 0; margin: 0; border: 0; vertical-align: top; }</style>
    </head><body><main><h1>Target size control</h1><button aria-label="First"></button><button aria-label="Second"></button></main></body></html>`)
  expect(
    (await audit(page, 'invalid-target-control')).some(
      (finding) => finding.rule === 'target-size',
    ),
  ).toBe(true)
  await page.getByRole('button').evaluateAll((buttons) => {
    for (const button of buttons) {
      button.style.width = '24px'
      button.style.height = '24px'
    }
  })
  expect(await auditWidths(page, 'valid-target-control')).toEqual([])
})

for (const language of ['en', 'ru'] as const) {
  test.describe(`accessibility ${language}`, () => {
    test.use({ locale: language === 'ru' ? 'ru-RU' : 'en-US' })

    test('every section passes the accessibility gates', async ({
      page,
      origin,
      assertProductAlive,
    }) => {
      test.setTimeout(300000)
      const copy = (key: string): string => {
        const value = dictionaries[language][key]
        if (typeof value !== 'string')
          throw new Error(`Missing text for ${key}`)
        return value
      }
      // Every screen is audited before anything is asserted, so one screen's
      // regression cannot hide another's.
      const violations: AuditFinding[] = []
      await mkdir(reviewRoot, { recursive: true })

      await page.goto(`${origin}/profiles/new`)
      await expect(
        page.getByRole('heading', {
          level: 1,
          name: copy('create.title'),
        }),
      ).toBeVisible()
      await page
        .getByRole('button', {
          name: copy('listDetail.open.aria').replace('{list}', 'Discord'),
        })
        .click()
      const listDialog = page.getByRole('dialog', { name: 'Discord' })
      await expect(
        listDialog.getByRole('heading', {
          name: copy('listCard.domains'),
        }),
      ).toBeVisible()
      // The card reads its sources when it opens, so the audit is taken once the
      // observed rows have landed: the busy notice and the full table are both in
      // the same pass.
      await expect(listDialog.getByText('dns-client').first()).toBeVisible({
        timeout: 60000,
      })
      violations.push(...(await auditWidths(page, `${language}-list-card`)))
      await listDialog
        .getByRole('button', { name: copy('action.close') })
        .last()
        .click()
      await page
        .getByRole('searchbox', { name: copy('create.search') })
        .fill('youtube')
      await expect(listMembership(page, 'YouTube')).toBeVisible()
      violations.push(...(await auditWidths(page, `${language}-builder`)))
      await page.screenshot({
        path: join(reviewRoot, `${language}-builder.png`),
        fullPage: true,
      })

      await listMembership(page, 'YouTube').check()
      await chooseFormat(page, /Keenetic/, copy)
      await page.getByRole('button', { name: copy('create.submit') }).click()
      await page.waitForURL(
        new RegExp(`^${escapeRegExp(origin)}/profiles/[a-f0-9]{32}.*$`),
      )
      await expect(
        page.getByRole('heading', { level: 1, name: 'YouTube' }),
      ).toBeVisible()

      // The profile audit is taken with an output bound and its subscription block
      // showing, so the outputs table and the secret disclosure are covered too.
      await expect(
        page.getByRole('heading', {
          name: copy('profile.subscription').replace('{target}', 'Keenetic'),
        }),
      ).toBeVisible()
      const createdRoutePath = new URL(page.url()).pathname
      violations.push(...(await auditWidths(page, `${language}-list`)))
      await page.screenshot({
        path: join(reviewRoot, `${language}-list.png`),
        fullPage: true,
      })

      await page
        .getByRole('link', { name: copy('profiles.title') })
        .first()
        .click()
      await expect(linkTo(page, createdRoutePath)).toBeVisible()
      violations.push(...(await auditWidths(page, `${language}-library`)))
      await page.screenshot({
        path: join(reviewRoot, `${language}-library.png`),
        fullPage: true,
      })

      await page.goto(`${origin}/lists`)
      await expect(
        page.getByRole('heading', { level: 1, name: copy('lists.title') }),
      ).toBeVisible()
      // The section is audited with a category open, because the pane is where its
      // rows and their menus live.
      await openLibraryCategory(page, copy('category.communication'), copy)
      violations.push(...(await auditWidths(page, `${language}-lists`)))
      await page.screenshot({
        path: join(reviewRoot, `${language}-lists.png`),
        fullPage: true,
      })

      await page.goto(`${origin}/connections`)
      await expect(
        page.getByRole('heading', {
          level: 1,
          name: copy('connections.title'),
        }),
      ).toBeVisible()
      // The catalog is audited open: a disclosure hides its contents from the
      // sweep exactly as it hides them from the operator.
      await catalogDisclosure(page, copy).click()
      await expect(
        page.getByRole('row').filter({ hasText: 'Keenetic' }),
      ).toBeVisible()
      violations.push(...(await auditWidths(page, `${language}-connections`)))
      await page.screenshot({
        path: join(reviewRoot, `${language}-connections.png`),
        fullPage: true,
      })

      await page.goto(`${origin}/settings`)
      await expect(
        page.getByRole('heading', { level: 1, name: copy('settings.title') }),
      ).toBeVisible()
      violations.push(...(await auditWidths(page, `${language}-settings`)))
      await page.screenshot({
        path: join(reviewRoot, `${language}-settings.png`),
        fullPage: true,
      })

      expect(violations).toEqual([])
      assertProductAlive()
    })
  })
}
