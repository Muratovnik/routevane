/**
 * The chrome and the choices it shares with every screen: the sidebar's hover
 * and focus in both themes, the placeholder a loading list shows, and the
 * triggers of the choice family, which — like a text field — are not lit by the
 * pointer passing over them.
 *
 * The worker fixture in `support/served-product.ts` serves this suite
 * alone, over the data directory named below.
 */
import { expect, type Locator, type Page } from '@playwright/test'

import { contrastRatio, toRgb } from './support/contrast'
import { englishCopy } from './support/copy'
import { buildProfile, openFormats, settleAnimations } from './support/flows'
import {
  choicePanel,
  choiceSearch,
  deliveryField,
  menuPanel,
  nameField,
  scheduleField,
} from './support/queries'
import { test } from './support/served-product'

test.use({ locale: 'en-US', productData: 'chrome-and-choices' })

const paint = (locator: Locator) =>
  locator.evaluate((element) => {
    const style = getComputedStyle(element)
    return {
      background: style.backgroundColor,
      color: style.color,
      fontWeight: style.fontWeight,
      outlineColor: style.outlineColor,
      outlineOffset: Number.parseFloat(style.outlineOffset),
      outlineWidth: Number.parseFloat(style.outlineWidth),
    }
  })

const sidebar = (page: Page) => page.getByRole('banner')

for (const colorScheme of ['dark', 'light'] as const) {
  test(`the focus ring inside the sidebar reads at 3:1 in the ${colorScheme} theme`, async ({
    page,
    origin,
  }) => {
    await page.emulateMedia({ colorScheme })
    await page.goto(`${origin}/connections`)
    const link = sidebar(page).getByRole('link', { name: 'Lists' })
    await link.focus()
    await page.keyboard.press('Shift+Tab')
    await page.keyboard.press('Tab')
    await expect(link).toBeFocused()
    const ring = await paint(link)
    expect(ring.outlineWidth).toBeGreaterThanOrEqual(2)
    const ground = (await paint(sidebar(page))).background
    expect(
      await contrastRatio(page, ring.outlineColor, ground),
    ).toBeGreaterThan(3)
  })
}

test('in the light theme the sidebar hover is lighter than its ground and apart from the current page', async ({
  page,
  origin,
}) => {
  await page.emulateMedia({ colorScheme: 'light' })
  await page.goto(`${origin}/connections`)
  const nav = sidebar(page).getByRole('navigation')
  const ground = (await paint(sidebar(page))).background
  const current = (await paint(nav.getByRole('link', { name: 'Connections' })))
    .background
  const other = nav.getByRole('link', { name: 'Settings' })
  await other.hover()
  await settleAnimations(page)
  const hover = (await paint(other)).background

  // The band's own hue is green-grey; the hover is not the dark bluish grey the
  // dark theme's token carries.
  const [, green, blue] = await toRgb(page, hover, [0, 0, 0])
  expect(green).toBeGreaterThanOrEqual(blue)
  expect(await contrastRatio(page, hover, ground)).toBeGreaterThanOrEqual(1.5)
  expect(await contrastRatio(page, hover, current)).toBeGreaterThanOrEqual(1.2)
})

for (const colorScheme of ['dark', 'light'] as const) {
  test(`the update button keeps its text readable while hovered in the ${colorScheme} theme`, async ({
    page,
    origin,
  }) => {
    await page.emulateMedia({ colorScheme })
    await page.addInitScript(() => {
      const state = { status: 'available', version: '9.9.9' }
      Object.defineProperty(window, 'routevaneDesktop', {
        value: {
          updates: {
            apply: () => Promise.resolve(),
            state: () => Promise.resolve(state),
            subscribe: () => () => {},
          },
        },
      })
    })
    await page.goto(`${origin}/connections`)
    const update = sidebar(page).getByRole('button', {
      name: englishCopy('update.available'),
    })
    await expect(update).toBeVisible()
    await update.hover()
    await settleAnimations(page)
    const hovered = await paint(update)
    expect(
      await contrastRatio(page, hovered.color, hovered.background),
    ).toBeGreaterThanOrEqual(4.5)
  })
}

for (const colorScheme of ['dark', 'light'] as const) {
  test(`a loading list card draws placeholders that can be seen in the ${colorScheme} theme`, async ({
    page,
    origin,
  }) => {
    await page.emulateMedia({ colorScheme })
    let release = () => {}
    const held = new Promise<void>((resolve) => {
      release = resolve
    })
    await page.route('**/v1/lists/*/contents', async (route) => {
      await held
      await route.continue()
    })
    await page.goto(`${origin}/lists`)
    await page
      .getByRole('button', { name: 'Open the contents of list Discord' })
      .click()
    const row = page.getByTestId('rv-loading-row').first()
    await expect(row).toBeVisible()
    const { placeholder, ground } = await row.evaluate((element) => {
      const panel = element.closest('[role="dialog"]')!
      return {
        ground: getComputedStyle(panel).backgroundColor,
        placeholder: getComputedStyle(element, '::before').backgroundColor,
      }
    })
    expect(
      await contrastRatio(page, placeholder, ground),
    ).toBeGreaterThanOrEqual(1.5)
    release()
  })

  test(`a loading profile page draws placeholders that can be seen in the ${colorScheme} theme`, async ({
    page,
    origin,
  }) => {
    const { profileId } = await buildProfile(page, origin)
    await page.emulateMedia({ colorScheme })
    let release = () => {}
    const held = new Promise<void>((resolve) => {
      release = resolve
    })
    await page.route(`**/v1/profiles/${profileId}`, async (route) => {
      await held
      await route.continue()
    })
    await page.goto(`${origin}/profiles/${profileId}`)
    const parts = page.getByTestId('rv-skeleton-part')
    await expect(parts.first()).toBeVisible()
    // Breadcrumb, title, four tabs and the panel: every one of them stands in
    // for content and has to be seen against the page it sits on.
    await expect(parts).toHaveCount(7)
    const painted = await parts.evaluateAll((elements) =>
      elements.map((element) => {
        let ground = element.parentElement
        while (
          ground !== null &&
          getComputedStyle(ground).backgroundColor === 'rgba(0, 0, 0, 0)'
        )
          ground = ground.parentElement
        return {
          ground: ground ? getComputedStyle(ground).backgroundColor : '',
          placeholder: getComputedStyle(element).backgroundColor,
        }
      }),
    )
    for (const { ground, placeholder } of painted)
      expect(
        await contrastRatio(page, placeholder, ground),
      ).toBeGreaterThanOrEqual(1.5)
    release()
  })
}

// A panel opened over the page has to stand apart from the canvas it opens on.
// In the dark theme the library's own ground is the canvas, so a list or a menu
// would otherwise melt into the page: the ground is a raised surface and the
// edge is a hairline that reads against the canvas.
const MIN_PANEL_EDGE_CONTRAST = 1.8

const panelPaint = (panel: Locator) =>
  panel.evaluate((element) => {
    const host = element.closest('.rv-overlay, .rv-menu__panel') ?? element
    const style = getComputedStyle(host)
    return {
      edge: style.borderTopColor,
      edgeWidth: Number.parseFloat(style.borderTopWidth),
      ground: style.backgroundColor,
      isPanel: host.matches('.rv-overlay, .rv-menu__panel'),
    }
  })

for (const colorScheme of ['dark', 'light'] as const) {
  test(`menus, choice lists and palettes stand apart from the canvas in the ${colorScheme} theme`, async ({
    page,
    origin,
  }) => {
    await page.emulateMedia({ colorScheme })
    await page.goto(`${origin}/profiles/new`)
    const canvas = await page.evaluate(
      () => getComputedStyle(document.documentElement).backgroundColor,
    )

    const checkPanel = async (panel: Locator): Promise<void> => {
      await settleAnimations(page)
      const drawn = await panelPaint(panel)
      expect(drawn.isPanel).toBe(true)
      expect(drawn.ground).not.toBe(canvas)
      expect(drawn.edgeWidth).toBeGreaterThan(0)
      expect(
        await contrastRatio(page, drawn.edge, canvas),
      ).toBeGreaterThanOrEqual(MIN_PANEL_EDGE_CONTRAST)
    }

    // The library's palette in a popover, opened from the format field.
    await openFormats(page)
    await checkPanel(choicePanel(page, englishCopy('create.target.toggle')))
    await page.keyboard.press('Escape')

    // The library's list of a select, opened from its field.
    await buildProfile(page, origin)
    await page.getByRole('tab', { name: 'Publishing', exact: true }).click()
    await scheduleField(page).click()
    await checkPanel(page.getByRole('listbox'))
    await page.keyboard.press('Escape')

    // Reka's own menu.
    await page
      .getByRole('button', { name: /^Actions for / })
      .first()
      .click()
    await checkPanel(menuPanel(page))
  })
}

test('a choice trigger is not lit by the pointer, as a text field is not', async ({
  page,
  origin,
}) => {
  await page.goto(`${origin}/profiles/new`)
  const format = deliveryField(page)
  const rest = (await paint(format)).background
  await format.hover()
  await settleAnimations(page)
  expect((await paint(format)).background).toBe(rest)
  // The pointer cursor stays: it is still something to press.
  expect(await format.evaluate((el) => getComputedStyle(el).cursor)).toBe(
    'pointer',
  )

  await buildProfile(page, origin)
  await page.getByRole('tab', { name: 'Publishing', exact: true }).click()
  const schedule = scheduleField(page)
  const selectRest = (await paint(schedule)).background
  await schedule.hover()
  await settleAnimations(page)
  expect((await paint(schedule)).background).toBe(selectRest)
})

test('a trigger shows its placeholder in the regular weight and muted, as a text field does', async ({
  page,
  origin,
}) => {
  await page.goto(`${origin}/profiles/new`)
  const format = deliveryField(page)
  await expect(format).toHaveText(englishCopy('create.target.placeholder'))
  const fieldWeight = (await paint(nameField(page))).fontWeight
  expect((await paint(format)).fontWeight).toBe(fieldWeight)
  const placeholder = await format.evaluate((trigger) => {
    const label = [...trigger.querySelectorAll('span')].find(
      (span) => span.textContent?.trim() === 'Choose a format',
    )!
    return getComputedStyle(label).color
  })
  const muted = await nameField(page).evaluate(
    (input) => getComputedStyle(input, '::placeholder').color,
  )
  expect(await contrastRatio(page, placeholder, '#ffffff')).toBeCloseTo(
    await contrastRatio(page, muted, '#ffffff'),
    1,
  )
})

test('the search ring in the format panel stays inside the panel', async ({
  page,
  origin,
}) => {
  await page.goto(`${origin}/profiles/new`)
  await openFormats(page)
  await expect(choiceSearch(page)).toBeFocused()
  await settleAnimations(page)
  const reach = await choiceSearch(page).evaluate((input) => {
    const style = getComputedStyle(input)
    const grow =
      Number.parseFloat(style.outlineOffset) +
      Number.parseFloat(style.outlineWidth)
    const box = input.getBoundingClientRect()
    const panel = input.closest('[role="dialog"]')!.getBoundingClientRect()
    return {
      bottom: panel.bottom - (box.bottom + grow),
      left: box.left - grow - panel.left,
      right: panel.right - (box.right + grow),
      top: box.top - grow - panel.top,
    }
  })
  for (const side of Object.values(reach))
    expect(side).toBeGreaterThanOrEqual(0)
  await expect(
    choicePanel(page, englishCopy('create.target.toggle')),
  ).toBeVisible()
})
