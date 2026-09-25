import { expect } from '@playwright/test'
import { join } from 'node:path'

import { audit, auditWidths, reviewRoot } from './support/audits'
import { localConfigAddress } from './support/environment'
import { pressSegment } from './support/flows'
import { CONTROL_TOUCH } from './support/geometry'
import { libraryRow, libraryRowName, listRow } from './support/queries'
import { test } from './support/served-product'

test.use({
  locale: 'en-US',
  productData: 'workspace-consistency',
  productCatalog: 'catalog',
})

test('graphite settings and inline connection choices preserve readable alignment', async ({
  page,
  origin,
}) => {
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto(`${origin}/settings`)
  await pressSegment(page, 'Dark')
  // The requested palette separates graphite grounds from brand and success.
  const palette = await page.evaluate(() => {
    const style = getComputedStyle(document.documentElement)
    return [
      'chrome',
      'canvas',
      'surface',
      'surface-muted',
      'surface-hover',
      'accent',
      'status-ready',
    ].map((role) => style.getPropertyValue(`--rv-color-${role}`).trim())
  })
  expect(palette).toEqual([
    '#151719',
    '#191b1d',
    '#202326',
    '#272a2d',
    '#2d3135',
    '#63b98d',
    '#58c08a',
  ])
  const title = (await page
    .getByRole('heading', { name: 'Source refresh', exact: true })
    .boundingBox())!
  const choice = (await page
    .getByRole('group', { name: 'Source refresh', exact: true })
    .boundingBox())!
  expect(Math.abs(title.y - choice.y)).toBeLessThanOrEqual(12)
  const sections = [
    'Interface',
    'Source refresh',
    'Configuration transfer',
    'Service',
  ]
  const geometry = await Promise.all(
    sections.map(async (name) => {
      const section = page.getByRole('region', { name, exact: true })
      return section.evaluate((element) => {
        const heading = element.querySelector('h2')!.getBoundingClientRect()
        const content = element.lastElementChild!.getBoundingClientRect()
        return {
          heading: heading.x,
          content: content.x,
          background: getComputedStyle(element).backgroundColor,
        }
      })
    }),
  )
  for (const section of geometry) {
    expect(section.heading).toBe(geometry[0]!.heading)
    expect(section.content).toBe(geometry[0]!.content)
    expect(section.background).toBe('rgb(32, 35, 38)')
  }
  expect(await audit(page, 'graphite-settings')).toEqual([])
  await pressSegment(page, 'Русский')
  await page.screenshot({
    path: join(reviewRoot, 'settings-graphite.png'),
    fullPage: true,
  })
  await page
    .getByRole('navigation', { name: 'Разделы' })
    .getByRole('link', { name: 'Подключения' })
    .click()
  // An empty registry states that it holds nothing; the form is what its own
  // action opens.
  await page
    .getByRole('button', { name: 'Добавить подключение', exact: true })
    .click()
  await expect(
    page.getByRole('region', { name: 'Добавить подключение', exact: true }),
  ).toBeVisible()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await page.getByLabel('Что подключаем', { exact: true }).click()
  const option = page.getByRole('option', { name: 'Keenetic', exact: true })
  const labelLeft = await option.evaluate((element) => {
    const walker = document.createTreeWalker(element, NodeFilter.SHOW_TEXT)
    let node = walker.nextNode()
    while (node && !node.textContent?.trim()) node = walker.nextNode()
    if (!node) throw new Error('The option must contain its visible name')
    const range = document.createRange()
    range.selectNodeContents(node)
    return range.getBoundingClientRect().left
  })
  // Target icons now occupy the leading slot; the selection mark stays trailing.
  const icon = option.getByTestId('rv-icon').filter({ visible: true })
  await expect(icon).toHaveCount(1)
  const iconBox = await icon.boundingBox()
  const optionBox = await option.boundingBox()
  expect(iconBox).not.toBeNull()
  expect(optionBox).not.toBeNull()
  const inset = iconBox!.x - optionBox!.x
  expect(inset).toBeGreaterThanOrEqual(8)
  expect(inset).toBeLessThanOrEqual(16)
  const gap = labelLeft - (iconBox!.x + iconBox!.width)
  expect(gap).toBeGreaterThanOrEqual(8)
  expect(gap).toBeLessThanOrEqual(16)
  expect(await audit(page, 'graphite-connection-choice')).toEqual([])
  await page.screenshot({
    path: join(reviewRoot, 'connection-choice-graphite.png'),
  })
  await option.click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await page
    .getByLabel('Название подключения', { exact: true })
    .fill('Домашний роутер')
  await page.screenshot({
    path: join(reviewRoot, 'connection-form-graphite.png'),
    fullPage: true,
  })
  expect(await auditWidths(page, 'graphite-connection-form')).toEqual([])
  await page.evaluate(() => {
    document.documentElement.style.fontSize = '200%'
  })
  expect(await auditWidths(page, 'graphite-connection-form-text-200')).toEqual(
    [],
  )
  // The form leaves through its own cancel, and what it replaced comes back:
  // with nothing saved that is the registry's own statement, not an empty
  // work area.
  await page.getByRole('button', { name: 'Отменить', exact: true }).click()
  await expect(
    page.getByRole('heading', { name: 'Подключений пока нет' }),
  ).toBeFocused()
  await expect(
    page.getByRole('region', { name: 'Добавить подключение', exact: true }),
  ).toHaveCount(0)
})

for (const reducedMotion of ['reduce', 'no-preference'] as const) {
  test(`list loading reserves space without a first-frame flash (${reducedMotion})`, async ({
    page,
    origin,
  }) => {
    await page.emulateMedia({ reducedMotion })
    const held = Promise.withResolvers<undefined>()
    await page.route('**/v1/lists/grok/contents', async (route) => {
      await held.promise
      await route.continue()
    })
    // Keep this geometry scenario independent from the availability of feeds.
    await page.route('**/v1/lists/grok/refresh', (route) =>
      route.abort('connectionfailed'),
    )
    await page.goto(`${origin}/lists`)
    await expect(libraryRowName(libraryRow(page, 'Grok'))).toBeVisible()
    const firstFrame = page.evaluate(
      () =>
        new Promise<{ visibility: string; height: number }>((resolve) => {
          const observer = new MutationObserver(() => {
            const placeholder = document.querySelector('li[aria-hidden="true"]')
            if (placeholder === null) return
            observer.disconnect()
            requestAnimationFrame(() =>
              resolve({
                visibility: getComputedStyle(placeholder).visibility,
                height: placeholder.getBoundingClientRect().height,
              }),
            )
          })
          observer.observe(document.body, { childList: true, subtree: true })
        }),
    )
    try {
      await libraryRowName(libraryRow(page, 'Grok')).click()
      const initial = await firstFrame
      expect(initial.visibility).toBe('hidden')
      expect(initial.height).toBeGreaterThan(0)
      await expect
        .poll(() =>
          page.evaluate(() => {
            const placeholder = document.querySelector('li[aria-hidden="true"]')
            return placeholder === null
              ? ''
              : getComputedStyle(placeholder).visibility
          }),
        )
        .toBe('visible')
    } finally {
      held.resolve(undefined)
    }
    await expect(
      page
        .getByRole('dialog', { name: 'Grok', exact: true })
        .getByText('grok.com', { exact: true }),
    ).toBeVisible()
  })
}

test('the shipped catalog starts unread and recovers an interrupted source read in its card', async ({
  page,
  origin,
}) => {
  const response = await page.request.get(`${origin}/v1/lists/grok/contents`)
  expect(response.ok()).toBe(true)
  const before = (await response.json()) as {
    observed: boolean
    sources: { state: string }[]
  }
  expect(before.observed).toBe(false)
  expect(before.sources.length).toBeGreaterThan(1)
  expect(before.sources.every((source) => source.state === 'unread')).toBe(true)
  const forecast = await page.request.post(`${origin}/v1/profiles/preview`, {
    data: {
      lists: ['grok', 'chatgpt'],
      categories: [],
      exclusions: [],
      list_domains: {},
      priority: [],
    },
    headers: { Origin: origin, 'X-Routevane-Request': '1' },
  })
  expect(forecast.ok()).toBe(true)

  let attempts = 0
  await page.route('**/v1/lists/grok/refresh', async (route) => {
    attempts++
    await route.abort('connectionfailed')
  })
  await page.goto(`${origin}/lists`)
  await libraryRowName(libraryRow(page, 'Grok')).click()
  const card = page.getByRole('dialog', { name: 'Grok', exact: true })
  await expect(card.getByText('grok.com', { exact: true })).toBeVisible()
  await expect(
    card.getByRole('button', { name: 'Retry', exact: true }),
  ).toBeVisible()
  await expect(card.getByLabel('Sources read', { exact: true })).toHaveCount(0)
  expect(attempts).toBe(1)
  await card.getByRole('button', { name: 'Retry', exact: true }).click()
  await expect.poll(() => attempts).toBe(2)
  await expect(
    card.getByRole('button', { name: 'Retry', exact: true }),
  ).toBeEnabled()
})

test('page actions, category context and composition controls share consistent geometry', async ({
  page,
  origin,
}) => {
  await page.goto(origin)
  const pageTitle = await page
    .getByRole('heading', { name: 'Profiles', level: 1 })
    .boundingBox()
  const profileAction = await page
    .getByRole('link', { name: 'Build a profile', exact: true })
    .first()
    .boundingBox()
  await page
    .getByRole('navigation', { name: 'Sections' })
    .getByRole('link', { name: 'Lists', exact: true })
    .click()
  const listAction = await page
    .getByRole('button', { name: 'New list', exact: true })
    .boundingBox()
  await page.getByRole('button', { name: 'Categories', exact: true }).click()
  await expect(
    page.getByRole('dialog', { name: 'Categories', exact: true }),
  ).toBeVisible()
  await page.keyboard.press('Escape')
  await page
    .getByRole('navigation', { name: 'Sections' })
    .getByRole('link', { name: 'Connections', exact: true })
    .click()
  // Returning operators land on their saved connection; adding is navigation.
  await page.waitForURL(`${origin}/connections`)
  const created = await page.request.post(`${origin}/v1/devices`, {
    headers: { Origin: origin, 'X-Routevane-Request': '1' },
    data: {
      target_id: 'singbox',
      name: 'Geometry client',
      address: localConfigAddress('geometry.json'),
    },
  })
  expect(created.ok()).toBe(true)
  const deviceID = ((await created.json()) as { device: { id: string } }).device
    .id
  await page.reload()
  await expect(
    page.getByRole('heading', { name: 'Geometry client', exact: true }),
  ).toBeVisible()
  const connectionAction = await page
    .getByRole('button', { name: 'Add a connection', exact: true })
    .boundingBox()
  expect(profileAction?.height).toBe(CONTROL_TOUCH)
  expect(listAction?.height).toBe(profileAction?.height)
  expect(connectionAction?.height).toBe(profileAction?.height)
  expect(await audit(page, 'connections-collection')).toEqual([])
  await page
    .getByRole('button', { name: 'Add a connection', exact: true })
    .click()
  await expect(
    page.getByRole('heading', { name: 'Add a connection', exact: true }),
  ).toBeFocused()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  expect(await auditWidths(page, 'connection-create')).toEqual([])
  expect(await audit(page, 'connection-create')).toEqual([])
  // Leaving the form returns to the saved connection: the work area never
  // collapses into the header action.
  await page.getByRole('button', { name: 'Cancel', exact: true }).click()
  await expect(
    page.getByRole('heading', { name: 'Geometry client', exact: true }),
  ).toBeFocused()
  await expect(
    page.getByRole('region', { name: 'Geometry client', exact: true }),
  ).toBeVisible()
  const forgotten = await page.request.post(
    `${origin}/v1/devices/${deviceID}/forget`,
    {
      headers: { Origin: origin, 'X-Routevane-Request': '1' },
      data: {},
    },
  )
  expect(forgotten.ok()).toBe(true)

  await page.goto(`${origin}/profiles/new`)
  const row = listRow(page, 'Grok')
  const checkbox = await row.getByRole('checkbox').boundingBox()
  const handle = await row.getByRole('button').first().boundingBox()
  expect(checkbox).not.toBeNull()
  expect(handle).not.toBeNull()
  expect(
    Math.abs(
      checkbox!.y + checkbox!.height / 2 - (handle!.y + handle!.height / 2),
    ),
  ).toBeLessThanOrEqual(1)

  await page.goto(`${origin}/settings`)
  const settingsTitle = await page
    .getByRole('heading', { name: 'Settings', level: 1 })
    .boundingBox()
  expect(settingsTitle?.x).toBe(pageTitle?.x)
  expect(await auditWidths(page, 'settings-composition')).toEqual([])
  expect(await audit(page, 'settings-composition')).toEqual([])
  await pressSegment(page, 'Русский')
  await page.screenshot({
    path: join(reviewRoot, 'settings-ru-final.png'),
    fullPage: true,
  })
  await page.evaluate(() => {
    document.documentElement.style.fontSize = '200%'
  })
  expect(await auditWidths(page, 'settings-ru-text-200')).toEqual([])
  await page.screenshot({
    path: join(reviewRoot, 'settings-ru-text-200-final.png'),
  })
  await pressSegment(page, 'English')
  expect(await audit(page, 'settings-en-text-200')).toEqual([])
})
