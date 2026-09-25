/**
 * The choice family as the built product draws it. The unit suite renders the
 * facades over doubles of the library's components; this suite shows that the
 * library keeps what the facades promise: a trigger named by its field label,
 * the select's keyboard contract, the searchable menu's search and focus
 * return, a panel no narrower than its field and aligned to it, the product's
 * own words in both languages, the category filter's wider panel, a choice
 * opened inside a dialog standing above it, a list longer than its panel that
 * scrolls within reach of the keyboard, and an audit whose two accepted findings
 * stays that narrow (docs/adr/0043-accept-two-command-palette-findings.md).
 */

import { expect, type Locator, type Page } from '@playwright/test'

import { audit } from './support/audits'
import { PALETTE_PANEL } from './support/axe'
import { copyFor, englishCopy } from './support/copy'
import {
  assertPainted,
  openLibraryCategory,
  settleAnimations,
} from './support/flows'
import { CONTROL_DEFAULT } from './support/geometry'
import {
  categoryMore,
  choicePanel,
  choiceSearch,
  deviceField,
} from './support/queries'
import { test } from './support/served-product'

test.use({ productData: 'choice-family' })

/** The panel measure the category filter's list takes, `--rv-panel-width`. */
const PANEL_WIDTH = 320

// A plain select's list is the named listbox itself; a searchable choice's is a
// named popover that holds its list.
const selectPanel = (page: Page, name: string): Locator =>
  page.getByRole('listbox', { name, exact: true })

const box = async (locator: Locator) => {
  const bounds = await locator.boundingBox()
  expect(bounds).not.toBeNull()
  return bounds!
}

// A list opened from a field starts at the field's leading edge and is never
// narrower than it (docs/UI.md, Components). The field is measured before it
// opens: an open select hides the rest of the page from assistive technology,
// and with it the role the trigger is found by.
const expectAnchored = async (
  panel: Locator,
  triggerBox: { x: number; width: number },
) => {
  await settleAnimations(panel.page())
  const panelBox = await box(panel)
  expect(Math.abs(panelBox.x - triggerBox.x)).toBeLessThanOrEqual(1)
  expect(panelBox.width).toBeGreaterThanOrEqual(triggerBox.width - 1)
}

const openFeedForm = async (page: Page, origin: string) => {
  await page.goto(`${origin}/lists`)
  await openLibraryCategory(page, 'Communication')
  await page
    .getByRole('button', { name: 'Open the contents of list Discord' })
    .click()
  const card = page.getByRole('dialog', { exact: true, name: 'Discord' })
  await card
    .getByRole('button', { name: englishCopy('listCard.sources.configure') })
    .click()
  await page
    .getByRole('button', { name: englishCopy('listCard.feed.add') })
    .click()
}

test('a plain choice keeps the select keyboard contract inside a sheet', async ({
  page,
  origin,
  assertProductAlive,
}) => {
  await openFeedForm(page, origin)
  const caption = englishCopy('listCard.feed.format')
  // The trigger is the select's combobox, and its name is the field's label.
  const format = page.getByRole('combobox', { name: caption, exact: true })
  await expect(format).toHaveAccessibleName(caption)
  const formatBox = await box(format)
  expect(formatBox.height).toBe(CONTROL_DEFAULT)
  await expect(format).toHaveText(englishCopy('listCard.feed.format.text'))

  await format.focus()
  await page.keyboard.press('Enter')
  const panel = selectPanel(page, caption)
  await expect(panel).toBeVisible()
  await assertPainted(panel, 'plain choice')
  await expectAnchored(panel, formatBox)
  const options = panel.getByRole('option')
  await expect(options).toHaveCount(3)
  await page.keyboard.press('End')
  await expect(options.last()).toHaveAttribute('data-highlighted', '')
  await page.keyboard.press('Home')
  await expect(options.first()).toHaveAttribute('data-highlighted', '')
  await page.keyboard.press('ArrowDown')
  await expect(options.nth(1)).toHaveAttribute('data-highlighted', '')
  // Type-ahead moves to the choice whose name starts with what was typed.
  await page.keyboard.type('JSON')
  const json = panel.getByRole('option', {
    name: englishCopy('listCard.feed.format.json'),
  })
  await expect(json).toHaveAttribute('data-highlighted', '')
  expect(await audit(page, 'plain-choice-open')).toEqual([])
  await page.keyboard.press('Enter')
  await expect(panel).toBeHidden()
  await expect(format).toBeFocused()
  await expect(format).toHaveText(englishCopy('listCard.feed.format.json'))

  // Escape leaves the choice as it was and hands the keyboard back.
  await page.keyboard.press('ArrowDown')
  await expect(panel).toBeVisible()
  await page.keyboard.press('Home')
  await page.keyboard.press('Escape')
  await expect(panel).toBeHidden()
  await expect(format).toBeFocused()
  await expect(format).toHaveText(englishCopy('listCard.feed.format.json'))
  assertProductAlive()
})

test('a searchable field choice searches, chooses and returns the keyboard', async ({
  page,
  origin,
  assertProductAlive,
}) => {
  await page.goto(`${origin}/connections`)
  await page
    .getByRole('button', { name: englishCopy('devices.add'), exact: true })
    .click()
  const caption = englishCopy('devices.field.target')
  const target = deviceField(page, 'devices.field.target')
  // The field's label is the name an operator hears for the trigger.
  await expect(target).toHaveAccessibleName(caption)
  await expect(target).not.toHaveAttribute('aria-label')
  const targetBox = await box(target)
  expect(targetBox.height).toBe(CONTROL_DEFAULT)

  await target.click()
  const panel = choicePanel(page, englishCopy('devices.field.target.pick'))
  await expect(panel).toBeVisible()
  await assertPainted(panel, 'searchable choice')
  await expectAnchored(panel, targetBox)
  const search = choiceSearch(page)
  await expect(search).toBeFocused()
  await expect(search).toHaveAttribute(
    'placeholder',
    englishCopy('choice.search'),
  )
  expect(await audit(page, 'searchable-choice-open')).toEqual([])

  await search.fill('no format has this name')
  await expect(panel.getByRole('option')).toHaveCount(0)
  await expect(panel.getByText(englishCopy('choice.empty'))).toBeVisible()
  await search.fill('sing')
  await expect(panel.getByRole('option')).toHaveCount(1)
  await page.keyboard.press('ArrowDown')
  await page.keyboard.press('Enter')
  await expect(panel).toBeHidden()
  await expect(target).toBeFocused()
  await expect(target).toContainText('sing-box')

  await target.click()
  await expect(search).toBeFocused()
  await expect(search).toHaveValue('')
  await page.keyboard.press('Escape')
  await expect(panel).toBeHidden()
  await expect(target).toBeFocused()
  await expect(target).toContainText('sing-box')
  assertProductAlive()
})

// A listbox the test places on the page, with a child the rule looks at:
// outside any palette, or inside the palette panel next to its own list.
const injectListbox = (page: Page, inside: string | null) =>
  page.evaluate(
    ({ panel }) => {
      const list = document.createElement('div')
      list.setAttribute('role', 'listbox')
      list.innerHTML = '<div role="option" aria-selected="false">Probe</div>'
      const host =
        panel === null ? document.body : document.querySelector(panel)
      host?.append(list)
    },
    { panel: inside },
  )

test('the accepted audit finding covers the palette list and nothing else', async ({
  page,
  origin,
}) => {
  await page.goto(`${origin}/connections`)
  await page
    .getByRole('button', { name: englishCopy('devices.add'), exact: true })
    .click()
  await deviceField(page, 'devices.field.target').click()
  const panel = choicePanel(page, englishCopy('devices.field.target.pick'))
  await expect(panel).toBeVisible()
  // Open, the palette passes every rule with only its list's name waived.
  expect(await audit(page, 'palette-open')).toEqual([])

  // A nameless list outside any palette, and one inside the palette panel
  // that is not the palette's own list: both are reported.
  await injectListbox(page, null)
  await injectListbox(page, PALETTE_PANEL)
  const nameless = (await audit(page, 'palette-probes')).filter(
    (finding) => finding.rule === 'aria-input-field-name',
  )
  expect(nameless).toHaveLength(1)
  expect(nameless[0]!.targets).toHaveLength(2)

  // A panel that has lost its own name loses the waiver for its list too.
  await panel.evaluate((element) => {
    element.removeAttribute('aria-labelledby')
    element.removeAttribute('aria-label')
  })
  const unnamed = (await audit(page, 'palette-unnamed')).filter(
    (finding) => finding.rule === 'aria-input-field-name',
  )
  expect(unnamed[0]!.targets).toHaveLength(3)
})

// Enough lists that the palette's own list is longer than its panel, placed
// through the service the way the library would hold them.
const seedLists = async (page: Page, origin: string, count: number) => {
  const headers = { Origin: origin, 'X-Routevane-Request': '1' }
  for (let index = 0; index < count; index += 1) {
    const response = await page.request.post(`${origin}/v1/lists`, {
      data: {
        domains: [`overflow-${index}.example`],
        title: `Overflow list ${String(index).padStart(2, '0')}`,
      },
      headers,
    })
    expect(response.ok()).toBe(true)
  }
}

test('a palette list longer than its panel scrolls within reach of the keyboard', async ({
  page,
  origin,
}) => {
  await seedLists(page, origin, 30)
  await page.goto(`${origin}/lists`)
  await openLibraryCategory(page, 'Communication')
  await page.getByRole('button', { name: englishCopy('lists.addList') }).click()
  const sheet = page.getByRole('dialog', {
    name: englishCopy('lists.addList'),
  })
  await sheet
    .getByText(englishCopy('lists.list.flow.existing'), { exact: true })
    .click()
  const trigger = sheet.getByRole('button', {
    name: englishCopy('lists.category.pick.placeholder'),
  })
  await trigger.click()
  // The combobox's panel is named for the act of choosing, as its toggle is.
  const panel = choicePanel(page, englishCopy('lists.category.pick.toggle'))
  await expect(panel).toBeVisible()
  await settleAnimations(page)
  const list = panel.getByRole('listbox')
  const overflow = await list.evaluate((element) =>
    [element, ...element.querySelectorAll('*')].some(
      (node) => node.scrollHeight > node.clientHeight + 1,
    ),
  )
  expect(overflow, 'the list must be longer than its panel').toBe(true)
  await expect(panel.getByRole('option')).toHaveCount(32)

  // The keyboard reaches every row through the search field: the last one is
  // highlighted, scrolled into the list's own view, and the focus never left
  // the field that drives the list as its active descendant.
  const search = panel.getByRole('textbox', {
    name: englishCopy('choice.search'),
  })
  await expect(search).toBeFocused()
  const rows = panel.getByRole('option')
  const count = await rows.count()
  for (let step = 0; step < count; step += 1)
    await page.keyboard.press('ArrowDown')
  await expect(rows.last()).toHaveAttribute('data-highlighted', '')
  await expect(rows.last()).toBeInViewport()
  await expect(search).toBeFocused()
  const lastRow = await rows.last().getAttribute('id')
  await expect(search).toHaveAttribute('aria-activedescendant', lastRow!)
  // Open and scrolled, the palette passes every rule the suites hold a screen
  // to, with only its two accepted findings waived (ADR 0043).
  expect(await audit(page, 'palette-overflow')).toEqual([])

  // A scrolling region without focusable content that is not the palette's
  // own viewport is reported, even inside the palette's panel, and so is the
  // palette's own viewport once its search no longer drives the list.
  await panel.evaluate((host) => {
    const region = document.createElement('div')
    region.setAttribute('aria-label', 'Probe region')
    region.style.setProperty('overflow-y', 'auto')
    region.style.setProperty('height', '4rem')
    // Styles set through the object model, as the page's policy allows.
    const text = document.createElement('p')
    text.textContent = 'Probe text'
    // Text below the region's fold, which only scrolling would reveal.
    text.style.setProperty('margin-top', '12rem')
    region.append(text)
    host.append(region)
  })
  const unmarked = (await audit(page, 'palette-overflow-probe')).filter(
    (finding) => finding.rule === 'scrollable-region-focusable',
  )
  expect(unmarked).toHaveLength(1)
  expect(unmarked[0]!.targets).toHaveLength(1)
  await search.evaluate((input) =>
    input.removeAttribute('aria-activedescendant'),
  )
  const undriven = (await audit(page, 'palette-overflow-undriven')).filter(
    (finding) => finding.rule === 'scrollable-region-focusable',
  )
  expect(undriven[0]!.targets).toHaveLength(2)
})

test.describe('choices in Russian', () => {
  test.use({ locale: 'ru-RU' })

  test('a searchable choice speaks the product dictionary, not the library', async ({
    page,
    origin,
  }) => {
    const copy = copyFor('ru')
    await page.goto(`${origin}/connections`)
    await page
      .getByRole('button', { name: copy('devices.add'), exact: true })
      .click()
    await deviceField(page, 'devices.field.target', copy).click()
    const search = choiceSearch(page, copy)
    await expect(search).toHaveAttribute('placeholder', copy('choice.search'))
    await search.fill('нет такого формата')
    await expect(page.getByText(copy('choice.empty'))).toBeVisible()
    // Nuxt UI's own English empty line never reaches the screen.
    await expect(page.getByText('No data')).toHaveCount(0)
    await expect(page.getByText(/No matching data/)).toHaveCount(0)
  })
})

test('a choice opened in a dialog stands above it and inside it', async ({
  page,
  origin,
}) => {
  await openFeedForm(page, origin)
  // The source form is a dialog of its own, opened over the list card.
  const card = page.getByRole('dialog', {
    exact: true,
    name: englishCopy('listCard.sources'),
  })
  const format = page.getByRole('combobox', {
    name: englishCopy('listCard.feed.format'),
    exact: true,
  })
  await format.click()
  const panel = selectPanel(page, englishCopy('listCard.feed.format'))
  await expect(panel).toBeVisible()
  await settleAnimations(page)
  // The panel is portalled into the dialog, so it is part of its layer: the
  // point at its centre is the panel's own, not the dialog's underneath, and
  // it stays inside the dialog without lengthening the dialog's scroll.
  expect(
    await card.evaluate(
      (dialog, listbox) => dialog.contains(listbox),
      await panel.elementHandle(),
    ),
  ).toBe(true)
  const hit = await panel.evaluate((element) => {
    const bounds = element.getBoundingClientRect()
    const top = document.elementFromPoint(
      bounds.x + bounds.width / 2,
      bounds.y + bounds.height / 2,
    )
    return element.contains(top)
  })
  expect(hit).toBe(true)
  const cardBox = await box(card)
  const panelBox = await box(panel)
  expect(panelBox.y + panelBox.height).toBeLessThanOrEqual(
    cardBox.y + cardBox.height + 1,
  )
  // Choosing keeps the keyboard in the dialog, on the field it came from.
  await panel.getByRole('option').last().click()
  await expect(panel).toBeHidden()
  await expect(format).toBeFocused()
  await expect(card).toBeVisible()
})

test('the category filter opens a panel at the panel measure, not the chip width', async ({
  page,
  origin,
}) => {
  await page.goto(`${origin}/lists`)
  const more = categoryMore(page)
  await more.click()
  const panel = choicePanel(page, englishCopy('listPicker.collections'))
  await expect(panel).toBeVisible()
  await settleAnimations(page)
  const panelBox = await box(panel)
  const triggerBox = await box(more)
  expect(Math.round(panelBox.width)).toBe(PANEL_WIDTH)
  expect(panelBox.width).toBeGreaterThan(triggerBox.width)
  // Every category name fits the panel: the filter cuts none of them.
  const cut = await panel
    .getByRole('option')
    .evaluateAll(
      (options) =>
        options.filter((option) =>
          [...option.querySelectorAll('span')].some(
            (span) => span.scrollWidth > span.clientWidth + 1,
          ),
        ).length,
    )
  expect(cut).toBe(0)

  // The filter is a multiple choice: a pick applies at once and keeps the
  // panel open for the next one; Escape closes it and hands the keyboard back.
  await panel.getByRole('option').first().click()
  await expect(panel).toBeVisible()
  await expect(panel.getByRole('option').first()).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await page.keyboard.press('Escape')
  await expect(panel).toBeHidden()
  await expect(more).toBeFocused()
  expect(await audit(page, 'category-filter')).toEqual([])
})
