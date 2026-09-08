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
    expect(geometry.height).toBe(geometry.viewportHeight)
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
