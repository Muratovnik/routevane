import { expect } from '@playwright/test'
import { join } from 'node:path'

import { audit, auditWidths, reviewRoot } from './support/audits'
import { pressSegment } from './support/flows'
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
  await expect(
    page.getByRole('region', { name: 'Добавить подключение', exact: true }),
  ).toBeVisible()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await page.getByLabel('Устройство или приложение', { exact: true }).click()
  const option = page.getByRole('option', { name: 'Keenetic', exact: true })
  const inset = await option.evaluate((element) => {
    const walker = document.createTreeWalker(element, NodeFilter.SHOW_TEXT)
    let node = walker.nextNode()
    while (node && !node.textContent?.trim()) node = walker.nextNode()
    if (!node) throw new Error('The option must contain its visible name')
    const range = document.createRange()
    range.selectNodeContents(node)
    return (
      range.getBoundingClientRect().left - element.getBoundingClientRect().left
    )
  })
  expect(inset).toBeGreaterThanOrEqual(8)
  expect(inset).toBeLessThanOrEqual(16)
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
  await page.getByRole('button', { name: 'Отменить', exact: true }).click()
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
  // First connection setup is already on the page. Closing it reveals the
  // collection action; reopening must stay in document flow.
  await expect(
    page.getByRole('region', { name: 'Add a connection', exact: true }),
  ).toBeVisible()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await page.getByRole('button', { name: 'Cancel', exact: true }).click()
  const connectionAction = await page
    .getByRole('button', { name: 'Add a connection', exact: true })
    .boundingBox()
  expect(profileAction?.height).toBe(44)
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
  await page.getByRole('button', { name: 'Cancel', exact: true }).click()
  await expect(
    page.getByRole('button', { name: 'Add a connection', exact: true }),
  ).toBeFocused()

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
