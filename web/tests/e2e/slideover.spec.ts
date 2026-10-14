import { expect } from '@playwright/test'

import { test } from './support/served-product'

test.use({ locale: 'en-US', productData: 'slideover' })

for (const reducedMotion of ['no-preference', 'reduce'] as const) {
  test(`ordinary sheets keep focus and geometry under CSP with ${reducedMotion} motion`, async ({
    page,
    origin,
  }) => {
    await page.emulateMedia({ reducedMotion })
    const cspErrors: string[] = []
    page.on('console', (message) => {
      if (/content security policy/i.test(message.text()))
        cspErrors.push(message.text())
    })
    await page.goto(`${origin}/lists`)
    const opener = page.getByRole('button', {
      name: 'New list',
      exact: true,
    })
    const sheet = page.getByRole('dialog', {
      name: 'New list',
      exact: true,
    })
    const originalOverflow = await page.evaluate(
      () => document.body.style.overflow,
    )
    await opener.click()
    await expect(sheet.getByLabel('Name', { exact: true })).toBeFocused()
    await sheet.evaluate(async (element) => {
      await Promise.all(
        element
          .getAnimations({ subtree: true })
          .map((animation) => animation.finished),
      )
    })
    const geometry = await sheet.evaluate((element) => {
      const box = element.getBoundingClientRect()
      return {
        left: box.left,
        right: box.right,
        height: box.height,
        width: innerWidth,
        viewportHeight: innerHeight,
        overflow: document.body.style.overflow,
      }
    })
    expect(geometry.left).toBeGreaterThanOrEqual(0)
    expect(geometry.right).toBeLessThanOrEqual(geometry.width + 1)
    // "New list" is one bounded dialog, not a full-height working sheet.
    expect(geometry.height).toBeLessThan(geometry.viewportHeight)
    expect(geometry.left).toBeGreaterThan(0)
    expect(geometry.overflow).toBe('hidden')
    await page.keyboard.press('Escape')
    await expect(sheet).toBeHidden()
    await expect(opener).toBeFocused()
    expect(await page.evaluate(() => document.body.style.overflow)).toBe(
      originalOverflow,
    )

    // Close during entrance, then reopen after dismissal. A stale animation
    // must not hide the new panel or restore focus to its background.
    await opener.click()
    await page.keyboard.press('Escape')
    await expect(sheet).toBeHidden()
    await expect(opener).toBeFocused()
    await opener.click()
    await expect(sheet.getByLabel('Name', { exact: true })).toBeFocused()
    await sheet.getByRole('button', { name: 'Close', exact: true }).click()
    await expect(sheet).toBeHidden()
    await expect(opener).toBeFocused()
    expect(await page.evaluate(() => document.body.style.overflow)).toBe(
      originalOverflow,
    )
    expect(cspErrors).toEqual([])
  })
}

// A bounded panel is a modal of its own: the press outside it is a way out
// while nothing is in flight, and while a write it started is unanswered no
// way out is offered — not Escape, not the press outside, not its close
// control, which says so by being drawn as every unavailable button is. The
// page behind stays locked throughout, and the keyboard comes back to the
// control that opened the panel once the answer has closed it.
test('a bounded panel holds while its write is in flight', async ({
  page,
  origin,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto(`${origin}/lists`)
  const opener = page.getByRole('button', {
    name: 'New category',
    exact: true,
  })
  const panel = page.getByRole('dialog', { name: 'New category', exact: true })
  const close = panel.getByRole('button', { name: 'Close', exact: true })
  const outside = { x: 8, y: 8 }

  await opener.click()
  await expect(panel.getByLabel('Name', { exact: true })).toBeFocused()
  await page.mouse.click(outside.x, outside.y)
  await expect(panel).toBeHidden()
  await expect(opener).toBeFocused()

  const held = Promise.withResolvers<undefined>()
  await page.route(`${origin}/v1/categories`, async (route) => {
    await held.promise
    await route.continue()
  })
  await opener.click()
  await panel.getByLabel('Name', { exact: true }).fill('Held category')
  await panel.getByRole('button', { name: 'Create', exact: true }).click()
  await expect(close).toBeDisabled()
  expect(
    await close.evaluate((control) => getComputedStyle(control).opacity),
  ).toBe('1')

  await page.keyboard.press('Escape')
  await page.mouse.click(outside.x, outside.y)
  await expect(panel).toBeVisible()
  expect(await page.evaluate(() => document.body.style.overflow)).toBe('hidden')

  held.resolve(undefined)
  await expect(panel).toBeHidden()
  await expect(opener).toBeFocused()
})
