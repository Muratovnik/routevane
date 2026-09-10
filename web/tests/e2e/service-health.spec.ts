/**
 * What the surface says when it cannot reach the service it is drawn for.
 *
 * A screen states what its own read did. Nothing states the fact underneath —
 * that this browser is not reaching the product at all, so pressing anything
 * anywhere will fail the same way — and that fact is what this proves: it
 * appears, it carries one action, and pressing that action once the way back is
 * open takes it away and leaves a working product behind.
 *
 * The service keeps running throughout. Stopping the fixture's process would
 * prove the message and lose the product with it; taking the browser's own
 * network away proves the same thing and leaves the product able to answer.
 *
 * The worker fixture in `support/served-product.ts` serves this suite alone,
 * over the data directory named below.
 */

import { expect } from '@playwright/test'

import { englishCopy } from './support/copy'
import { libraryRegion } from './support/queries'
import { test as servedProduct } from './support/served-product'

/**
 * The browser's network, as something a test can take away and a fixture always
 * gives back. Offline is state on the browser context rather than on the page,
 * and the product this suite drives is a worker fixture that outlives the test,
 * so restoring it belongs to teardown rather than to the end of a test body
 * that may not be reached.
 */
const test = servedProduct.extend<{
  network: { lose: () => Promise<void>; restore: () => Promise<void> }
}>({
  network: async ({ page }, use) => {
    const context = page.context()
    await use({
      lose: () => context.setOffline(true),
      restore: () => context.setOffline(false),
    })
    await context.setOffline(false)
  },
})

test.use({ locale: 'en-US', productData: 'service-health' })

test('states that the service is not answering, and takes it back when it answers', async ({
  page,
  origin,
  network,
  assertProductAlive,
}) => {
  await page.goto(`${origin}/`)
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Profiles')

  // A section still opens with the network gone — its code is already in the
  // browser — and what it cannot do is read anything.
  await network.lose()
  await page.getByRole('link', { name: 'Lists', exact: true }).click()

  const notice = page
    .getByRole('status')
    .filter({ hasText: englishCopy('shell.notice.unreachable') })
  await expect(notice).toBeVisible()
  const retry = notice.getByRole('button', {
    name: englishCopy('action.retry'),
    exact: true,
  })
  await expect(retry).toBeVisible()

  // The action asks the service again, and while the way back is still closed
  // it keeps the message instead of claiming a recovery it did not get.
  const asked = page.waitForRequest((request) =>
    request.url().endsWith('/health'),
  )
  await retry.click()
  await asked
  await expect(notice).toBeVisible()

  // The service was answering all along; the browser simply could not reach it.
  assertProductAlive()

  // A browser that reports its network back is itself a reason to ask, so the
  // way back opening is what ends the message — the operator does not have to
  // press anything to be told the truth again.
  await network.restore()
  await expect(notice).toBeHidden()

  // And the product underneath is the working one: its library reads and draws.
  await page.getByRole('link', { name: 'Profiles', exact: true }).click()
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Profiles')
  await page.getByRole('link', { name: 'Lists', exact: true }).click()
  await expect(libraryRegion(page)).toBeVisible()
  await expect(notice).toBeHidden()
})
