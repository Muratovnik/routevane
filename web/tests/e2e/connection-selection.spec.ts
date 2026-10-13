/**
 * How a saved connection is drawn in the list and in the header of its own
 * page: the format's mark stands on the name's line, and the chosen row is
 * marked by a calm ground and a shape rather than by a frame.
 *
 * The worker fixture in `support/served-product.ts` serves this suite alone,
 * over the data directory named below.
 */

import { expect, type Locator } from '@playwright/test'

import { englishCopy } from './support/copy'
import { test } from './support/served-product'

test.use({
  locale: 'en-US',
  productCatalog: 'catalog',
  productData: 'connection-selection',
})

// Two saved connections, so one row is chosen and one is not.
const NAMES = ['Rack router', 'Spare router']

const centre = async (locator: Locator): Promise<number> => {
  const box = await locator.boundingBox()
  expect(box).not.toBeNull()
  return box!.y + box!.height / 2
}

const frame = (row: Locator) =>
  row.evaluate((element) => {
    const style = getComputedStyle(element)
    return {
      background: style.backgroundColor,
      widths: [
        style.borderTopWidth,
        style.borderRightWidth,
        style.borderBottomWidth,
        style.borderLeftWidth,
      ],
    }
  })

test('the format mark stands on the name line and the chosen row has no frame', async ({
  page,
  origin,
  assertProductAlive,
}) => {
  for (const name of NAMES) {
    const seed = await page.request.post(`${origin}/v1/devices`, {
      data: {
        account: 'admin',
        address: 'http://192.168.1.1',
        interface: 'Wireguard0',
        name,
        target_id: 'keenetic-dns',
      },
      headers: { Origin: origin, 'X-Routevane-Request': '1' },
    })
    expect(seed.ok()).toBe(true)
  }
  await page.goto(`${origin}/connections`)

  const row = (name: string) =>
    page.getByRole('button', {
      name: englishCopy('devices.configure.aria').replace('{name}', name),
    })
  const chosen = row(NAMES[0]!)
  const other = row(NAMES[1]!)
  await expect(chosen).toHaveAttribute('aria-pressed', 'true')
  await expect(other).toHaveAttribute('aria-pressed', 'false')

  // The mark is the first glyph in the row and the name its own text: their
  // centres coincide, where a mark at the top of a three-line block sits above.
  for (const [button, name] of [
    [chosen, NAMES[0]!],
    [other, NAMES[1]!],
  ] as const) {
    const mark = await centre(button.getByTestId('rv-icon').first())
    const text = await centre(button.getByText(name, { exact: true }))
    expect(Math.abs(mark - text), name).toBeLessThanOrEqual(1)
  }

  // The header of the chosen connection holds the same rule.
  const heading = page.getByRole('heading', { level: 2, name: NAMES[0]! })
  await expect(heading).toBeVisible()
  const mark = await centre(heading.getByTestId('rv-icon'))
  const text = await centre(heading.getByText(NAMES[0]!, { exact: true }))
  expect(Math.abs(mark - text)).toBeLessThanOrEqual(1)

  // The chosen row stands on its own ground, and neither row is framed: the
  // rows are divided by the list's hairline and the chosen one by its shape.
  const [selected, resting] = [await frame(chosen), await frame(other)]
  expect(selected.widths).toEqual(['0px', '0px', '0px', '0px'])
  expect(resting.widths).toEqual(['0px', '0px', '0px', '0px'])
  expect(selected.background).not.toBe(resting.background)
  const shape = (button: Locator) =>
    button
      .getByTestId('rv-icon')
      .last()
      .evaluate((element) => getComputedStyle(element).opacity)
  expect(await shape(chosen)).toBe('1')
  expect(await shape(other)).toBe('0')
  assertProductAlive()
})
