/**
 * What a screen says when its first read fails: whether the service did not
 * answer at all or answered with an error. The two ask for different things, so
 * a screen that says the first when the second is true sends the operator to
 * check a process that is running.
 *
 * The worker fixture in `support/served-product.ts` serves this suite
 * alone, over the data directory named below.
 */
import { expect, type Page } from '@playwright/test'

import { englishCopy } from './support/copy'
import { test } from './support/served-product'

test.use({ locale: 'en-US', productData: 'service-failure' })

const REFUSED = englishCopy('failure.refused.body')
const SILENT = englishCopy('failure.unreachable.body')

const answerWithError = (page: Page, path: string) =>
  page.route(path, (route) =>
    route.fulfill({ status: 500, json: { error: 'internal' } }),
  )

test('the library says the service answered with an error on HTTP 500', async ({
  page,
  origin,
}) => {
  await answerWithError(page, '**/v1/lists')
  await page.goto(`${origin}/lists`)
  await expect(page.getByText('Lists are unavailable')).toBeVisible()
  await expect(page.getByText(REFUSED)).toBeVisible()
  await expect(page.getByText(SILENT)).toHaveCount(0)
  // Nothing was read, so what needs the lists is withdrawn.
  await expect(
    page.getByRole('button', { name: 'Categories', exact: true }),
  ).toBeDisabled()
  await expect(
    page.getByRole('button', { name: 'New list', exact: true }),
  ).toBeDisabled()
})

test('the library says nothing answered when the request is lost', async ({
  page,
  origin,
}) => {
  await page.route('**/v1/lists', (route) => route.abort())
  await page.goto(`${origin}/lists`)
  await expect(page.getByText('Lists are unavailable')).toBeVisible()
  await expect(page.getByText(SILENT)).toBeVisible()
  await expect(page.getByText(REFUSED)).toHaveCount(0)
})

test('the composer tells the two apart as well', async ({ page, origin }) => {
  await answerWithError(page, '**/v1/lists')
  await page.goto(`${origin}/profiles/new`)
  await expect(page.getByText('Catalog unavailable')).toBeVisible()
  await expect(page.getByText(REFUSED)).toBeVisible()
  await expect(page.getByText(SILENT)).toHaveCount(0)
})
