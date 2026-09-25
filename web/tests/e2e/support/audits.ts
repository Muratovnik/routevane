/**
 * The sweeps a browser suite holds a screen to.
 *
 * Every section is audited the same way: the axe rules for the WCAG levels the
 * product commits to, the document fitting its own width, and every control an
 * operator can press being large enough to press. One module owns all three so
 * a suite states which screen it is auditing and nothing about how.
 *
 * Screenshots, overflow dumps and their JSON go below the ignored `tmp/`,
 * never beside the tests or the product's own assets.
 */
import { writeFile } from 'node:fs/promises'
import { join } from 'node:path'

import { analyze, axeFor } from './axe'
import { expect, test, type Page } from '@playwright/test'

import { pressableTargets } from './queries'
import { repositoryRoot } from './served-product'

/** Where a sweep leaves what a reviewer looks at afterwards. */
export const reviewRoot = join(
  repositoryRoot,
  'tmp',
  'test-screenshots',
  'browser-review',
)

export type AuditFinding = {
  detail: string
  rule: string
  screen: string
  targets: string[]
}

export const audit = async (
  page: Page,
  screen: string,
): Promise<AuditFinding[]> => {
  // Measure settled contrast, not the intermediate opacity of an opening panel.
  // Loading indicators may run indefinitely; they are still audited as drawn.
  await page.evaluate(async () => {
    await Promise.all(
      document
        .getAnimations()
        .filter(
          (animation) =>
            animation.effect?.getComputedTiming().iterations !== Infinity,
        )
        .map((animation) => animation.finished.catch(() => {})),
    )
  })
  const result = await analyze(
    page,
    axeFor(page).withTags([
      'wcag2a',
      'wcag2aa',
      'wcag21a',
      'wcag21aa',
      'wcag22aa',
    ]),
  )
  return result.violations.map((violation) => ({
    detail:
      violation.nodes[0]?.failureSummary?.replaceAll(/\s+/g, ' ').trim() ?? '',
    rule: violation.id,
    screen,
    targets: violation.nodes.flatMap((node) => node.target.map(String)),
  }))
}

export const auditWidths = async (
  page: Page,
  screen: string,
): Promise<AuditFinding[]> => {
  const findings: AuditFinding[] = []
  const previous = page.viewportSize()
  for (const width of [320, 768, 1024, 1440]) {
    await page.setViewportSize({ height: 900, width })
    if (!(await fits(page))) {
      await page.screenshot({
        path: join(reviewRoot, `${screen}-${width}-overflow.png`),
        fullPage: true,
      })
      // A diagnostic dump rather than an assertion: every descendant of the
      // content column is measured in the page, so this reads the document
      // directly instead of addressing elements one at a time.
      const bounds = await page.evaluate(() =>
        [...document.querySelectorAll('main *')].flatMap((element) => {
          const rect = element.getBoundingClientRect()
          return rect.right > document.documentElement.clientWidth &&
            rect.width > 0
            ? [
                {
                  tag: element.tagName,
                  className: String(element.className),
                  left: rect.left,
                  right: rect.right,
                  width: rect.width,
                },
              ]
            : []
        }),
      )
      await test.info().attach(`${screen}-${width}-overflow`, {
        body: JSON.stringify(bounds),
        contentType: 'application/json',
      })
      await writeFile(
        join(reviewRoot, `${screen}-${width}-overflow.json`),
        JSON.stringify(bounds, null, 2),
      )
    }
    expect(await fits(page), `${screen} overflows at ${width}px`).toBe(true)
    findings.push(...(await audit(page, `${screen}-${width}`)))
    const smallControls = await pressableTargets(page).evaluateAll((controls) =>
      controls.flatMap((control) => {
        // A native input drawn as one clipped pixel is not the target: the
        // label around it is, and a file input behind its own button has none,
        // so nothing there is pressed directly.
        const target =
          control instanceof HTMLInputElement
            ? control.closest('label')
            : control
        if (target === null) return []
        const bounds = target.getBoundingClientRect()
        const style = getComputedStyle(target)
        if (
          bounds.width === 0 ||
          bounds.height === 0 ||
          style.visibility === 'hidden'
        )
          return []
        return bounds.width < 24 || bounds.height < 24
          ? [
              {
                label:
                  target.getAttribute('aria-label') ??
                  target.textContent?.trim(),
                width: bounds.width,
                height: bounds.height,
              },
            ]
          : []
      }),
    )
    expect(
      smallControls,
      `${screen} actionable targets below 24px at ${width}px`,
    ).toEqual([])
  }
  if (previous !== null) await page.setViewportSize(previous)
  return findings
}

export const assertNoOverflow = async (
  page: Page,
  screen: string,
): Promise<void> => {
  for (const width of [320, 768, 1024, 1440]) {
    await page.setViewportSize({ height: 900, width })
    expect(await fits(page), `${screen} overflows at ${width}px`).toBe(true)
  }
  await page.setViewportSize({ height: 900, width: 1280 })
}

export const fits = (page: Page): Promise<boolean> =>
  page.evaluate(
    () =>
      document.documentElement.scrollWidth <=
      document.documentElement.clientWidth,
  )
