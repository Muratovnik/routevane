/**
 * The content security policy guard in `support/content-policy.ts`, held to
 * the violation it exists to catch.
 *
 * Every suite on the served product passes only while no page broke its
 * policy, which is worth something only while the guard can see a violation
 * at all. These cases cause one in the real product — a `<style>` element
 * written by a script, the way a component library injects its styles, which
 * `style-src 'self'` refuses — and prove that it is recorded with the
 * directive that refused it, on every page of the context, and that a test
 * which leaves it unanswered fails.
 *
 * The worker fixture in `support/served-product.ts` serves this suite alone,
 * over the data directory named below.
 */
import { expect, type Page } from '@playwright/test'

import {
  assertNoPolicyViolations,
  type PolicyViolation,
} from './support/content-policy'
import { test } from './support/served-product'

test.use({ locale: 'en-US', productData: 'content-policy' })

/** Opens the shelf and writes a style element into it, as a script would. */
const injectStyleElement = async (page: Page, origin: string) => {
  await page.goto(`${origin}/`)
  await expect(
    page.getByRole('heading', { level: 1, name: 'Profiles' }),
  ).toBeVisible()
  await page.evaluate(() => {
    const style = document.createElement('style')
    style.textContent = 'body { outline: 3px solid red; }'
    document.head.append(style)
  })
}

const pagesOfContext: {
  name: string
  open: (page: Page) => Promise<Page>
}[] = [
  { name: 'the page the test starts on', open: async (page) => page },
  {
    name: 'a page the context opens later',
    open: (page) => page.context().newPage(),
  },
]

for (const { name, open } of pagesOfContext) {
  test(`a style element refused on ${name} is recorded with the directive that refused it`, async ({
    page,
    origin,
    policyViolations,
  }) => {
    const target = await open(page)
    await injectStyleElement(target, origin)

    await expect
      .poll(() => policyViolations.seen())
      .toEqual([
        expect.objectContaining({
          blockedURI: 'inline',
          directive: 'style-src-elem',
          disposition: 'enforce',
          documentURI: `${origin}/`,
        }),
      ])
    // The refusal is real, not merely reported: the rule never applied.
    expect(
      await target.evaluate(() => getComputedStyle(document.body).outlineStyle),
    ).toBe('none')
    // This case caused the violation it asserted, so it answers for it.
    policyViolations.clear()
  })
}

test('a test that leaves a violation unanswered fails', async ({
  page,
  origin,
  policyViolations,
}) => {
  // The body below succeeds; the guard's teardown is what fails the test. A
  // guard that recorded the violation and let the test pass would turn this
  // expected failure into an unexpected pass.
  test.fail()
  await injectStyleElement(page, origin)
  await expect.poll(() => policyViolations.seen()).toHaveLength(1)
})

test('an unanswered violation is reported with where it came from', () => {
  const violation: PolicyViolation = {
    blockedURI: 'inline',
    columnNumber: 7,
    directive: 'style-src-elem',
    disposition: 'enforce',
    documentURI: 'http://127.0.0.1:8080/profiles',
    lineNumber: 12,
    sample: 'body{outline',
    sourceFile: 'http://127.0.0.1:8080/_nuxt/entry.js',
  }

  expect(() => assertNoPolicyViolations([violation, violation])).toThrow(
    'the page broke its content security policy 2 time(s):\n' +
      '  style-src-elem refused inline on http://127.0.0.1:8080/profiles (enforce) from http://127.0.0.1:8080/_nuxt/entry.js:12:7, sample body{outline\n' +
      '  style-src-elem refused inline on http://127.0.0.1:8080/profiles (enforce) from http://127.0.0.1:8080/_nuxt/entry.js:12:7, sample body{outline',
  )
  expect(() => assertNoPolicyViolations([])).not.toThrow()
})
