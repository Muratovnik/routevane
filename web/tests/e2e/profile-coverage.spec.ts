/**
 * A build the format cannot cover: the profile page names the lists behind the
 * refusal and links each one to the library.
 *
 * The worker fixture in `support/served-product.ts` serves this suite
 * alone, over the data directory named below.
 */

import { expect, type Page } from '@playwright/test'

import { englishCopy as copy } from './support/copy'
import { test } from './support/served-product'

test.use({ locale: 'en-US', productData: 'profile-coverage' })

type Created = { id: string; title: string }

// The Keenetic route file carries addresses and networks only, so a list of
// domain names alone has nothing it can publish there: its one required part
// stays uncovered, however often its sources are read.
const refuseCoverage = async (
  page: Page,
  origin: string,
): Promise<{ lists: Created[]; profileID: string }> => {
  const headers = { Origin: origin, 'X-Routevane-Request': '1' }
  const lists: Created[] = []
  for (const title of ['Coverage alpha', 'Coverage beta']) {
    const created = await page.request.post(`${origin}/v1/lists`, {
      data: { title, domains: [`${title.split(' ')[1]}.example`] },
      headers,
    })
    expect(created.ok()).toBe(true)
    const { list } = (await created.json()) as { list: { id: string } }
    lists.push({ id: list.id, title })
  }
  const profile = await page.request.post(`${origin}/v1/profiles`, {
    data: { name: 'Coverage fixture', lists: lists.map((list) => list.id) },
    headers,
  })
  expect(profile.ok()).toBe(true)
  const profileID = ((await profile.json()) as { profile: { id: string } })
    .profile.id
  const output = await page.request.post(
    `${origin}/v1/profiles/${profileID}/outputs`,
    { data: { target_id: 'keenetic' }, headers },
  )
  expect(output.ok()).toBe(true)
  const outputID = ((await output.json()) as { output: { id: string } }).output
    .id
  const refreshed = await page.request.post(
    `${origin}/v1/profiles/${profileID}/refresh`,
    { data: {}, headers },
  )
  expect(refreshed.ok()).toBe(true)

  const refused = await page.request.post(
    `${origin}/v1/outputs/${outputID}/build`,
    { data: {}, headers },
  )
  expect(refused.status()).toBe(422)
  const body = (await refused.json()) as {
    code: string
    uncovered_lists: Created[]
  }
  expect(body.code).toBe('partial_coverage')
  // The refusal names every list it could not cover, by identity and title.
  expect(
    [...body.uncovered_lists].sort((a, b) => a.id.localeCompare(b.id)),
  ).toEqual([...lists].sort((a, b) => a.id.localeCompare(b.id)))
  return { lists, profileID }
}

test('a refused build names the uncovered lists and links each to the library', async ({
  page,
  origin,
}) => {
  test.setTimeout(120000)
  await page.goto(`${origin}/`)
  const { lists, profileID } = await refuseCoverage(page, origin)

  await page.goto(`${origin}/profiles/${profileID}`)
  const notice = page
    .getByRole('status')
    .filter({ hasText: copy('profile.notice.build.partial_coverage.lists') })
  await expect(notice).toBeVisible()
  for (const list of lists) {
    await expect(
      notice.getByRole('link', { name: list.title }),
    ).toHaveAttribute('href', `/lists#list=${list.id}`)
  }
  // The names stay after a reload: they were stored with the failed attempt.
  await page.reload()
  await expect(notice).toBeVisible()
  await expect(
    notice.getByRole('link', { name: 'Coverage alpha' }),
  ).toBeVisible()

  // The link opens the list in the library, ready to have its sources checked.
  await notice.getByRole('link', { name: 'Coverage beta' }).click()
  await page.waitForURL(/\/lists#list=/)
  await expect(
    page.getByRole('dialog', { name: 'Coverage beta', exact: true }),
  ).toBeVisible()
})
