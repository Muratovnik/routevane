/**
 * The acts the browser suites share.
 *
 * A query says where something is; these say what an operator does with it —
 * choosing a connection format, opening a category, publishing a profile — and
 * what a suite reads back from the flow afterwards. Each is written once here
 * because several suites walk the same ground on the way to their own subject.
 */
import { expect, type Locator, type Page } from '@playwright/test'

import { englishCopy, type Copy } from './copy'
import {
  addConnectionField,
  categoryChip,
  categoryFilters,
  categoryMore,
  deliveryField,
  escapeRegExp,
  formatList,
  listMembership,
  type Scope,
} from './queries'

// A read that stores nothing: the composer and the profile editor ask what a
// draft would weigh, and no flow these tests state is made of that question.
export const FORECAST_PATH = '/v1/profiles/preview'

/**
 * The connection format is one searchable list, so a test chooses it the way an
 * operator does: open the list, take the option that names the format. Every
 * format's projected size is read from the same list, because that is where it
 * is stated. The field is captioned in the operator's own language, so a
 * localized suite hands its dictionary in.
 */
export const openFormats = async (
  page: Page,
  copy: Copy = englishCopy,
): Promise<Locator> => {
  await deliveryField(page, copy).click()
  await expect(formatList(page, copy)).toBeVisible()
  return formatList(page, copy)
}

export const chooseFormat = async (
  page: Page,
  name: RegExp,
  copy: Copy = englishCopy,
): Promise<void> => {
  await openFormats(page, copy)
  await page.getByRole('option', { name }).click()
  await expect(deliveryField(page, copy)).not.toHaveText(
    copy('create.target.placeholder'),
  )
}

/**
 * buildList walks the surface the way an operator does — a profile holding
 * Discord and YouTube, published as a Keenetic output — and leaves the page on
 * the published profile. It answers with the profile and output identities, since
 * every output-scoped mutation and address names them rather than the profile
 * alone.
 */
export const buildProfile = async (
  page: Page,
  origin: string,
): Promise<{ profileId: string; outputId: string }> => {
  await page.goto(`${origin}/profiles/new`)
  await expect(
    page.getByRole('heading', {
      level: 1,
      name: 'New profile',
    }),
  ).toBeVisible()
  const search = page.getByRole('searchbox', { name: 'Find a list' })
  await search.fill('discord')
  await listMembership(page, 'Discord').check()
  await search.fill('youtube')
  await listMembership(page, 'YouTube').check()
  await chooseFormat(page, /Keenetic/)
  const outputsResponse = page.waitForResponse(
    (candidate) =>
      candidate.request().method() === 'POST' &&
      /\/v1\/profiles\/[a-f0-9]{32}\/outputs$/.test(
        new URL(candidate.url()).pathname,
      ),
  )
  await page.getByRole('button', { name: 'Create and prepare' }).click()
  await page.waitForURL(
    new RegExp(`^${escapeRegExp(origin)}/profiles/[a-f0-9]{32}.*$`),
  )
  await expect(
    page.getByRole('heading', { level: 1, name: 'Discord, YouTube' }),
  ).toBeVisible()
  const profileId = profileIDFromURL(page.url())
  const outputPayload = (await (await outputsResponse).json()) as {
    output: { id: string }
  }
  await expect(
    page.getByRole('heading', { name: 'Subscription link · Keenetic' }),
  ).toBeVisible()
  await expect(addConnectionField(page)).toBeEnabled()
  await page
    .getByRole('tablist', { name: 'Profile sections' })
    .getByRole('tab', { name: 'Contents' })
    .click()

  return { profileId, outputId: outputPayload.output.id }
}

/**
 * What the filter row says it is filtered by.
 *
 * The row reports its choice as a pressed chip, and the chip states the
 * category and then its size — so the category is what its name starts with. A
 * category the row has no room to show as a chip is stated by the More control
 * instead, whose whole name is the collection heading and that category.
 */
export const expectLibraryCategory = async (
  page: Page,
  category: string,
  copy: Copy = englishCopy,
): Promise<void> => {
  await expect(
    categoryFilters(page, copy)
      .getByRole('button', {
        name: new RegExp(`^${escapeRegExp(category)}`),
        pressed: true,
      })
      .or(
        page.getByRole('button', {
          name: new RegExp(
            `^${escapeRegExp(copy('listPicker.collections'))}: ${escapeRegExp(category)}$`,
          ),
        }),
      ),
  ).toBeVisible()
}

/** One category's pane in «Списки», opened the way the column opens it. */
export const openLibraryCategory = async (
  page: Page,
  category: string,
  copy: Copy = englishCopy,
): Promise<void> => {
  await categoryChip(page, copy('listPicker.filter.all'), copy).click()
  await categoryMore(page, copy).click()
  await page.getByRole('option', { name: category }).click()
  await page.keyboard.press('Escape')
  await expectLibraryCategory(page, category, copy)
}

/**
 * The action menu of one category. Every act it offers is global, so it lives
 * in «Списки» and nowhere else (ADR 0029).
 */
export const openCategoryActions = async (
  page: Page,
  category: string,
): Promise<void> => {
  await openLibraryCategory(page, category)
  await page.getByRole('button', { name: 'Categories', exact: true }).click()
  await page
    .getByRole('button', { name: `Actions for category ${category}` })
    .click()
}

/**
 * Presses one segmented option. The radio itself is a clipped pixel behind its
 * own label, so what an operator presses — and what the browser turns into a
 * change on the radio — is the label's words.
 */
export const pressSegment = async (
  scope: Scope,
  label: string,
): Promise<void> => {
  await scope.getByText(label, { exact: true }).click()
}

/**
 * Whether a panel was drawn by its own component or merely placed. Both facts
 * come from one read, so a panel that lost its stylesheet fails on the ground
 * it should have had rather than on a coincidence of geometry.
 */
export const assertPainted = async (
  panel: Locator,
  name: string,
): Promise<void> => {
  await expect(panel).toBeVisible()
  const painted = await panel.evaluate((element) => {
    const style = getComputedStyle(element)
    // An edge is a border or, as the library draws its panels, a ring: a
    // box-shadow layer with no offset or blur and a spread of its own.
    const ring = / 0px 0px 0px [1-9]/.test(style.boxShadow)
    return {
      edge: style.borderTopWidth !== '0px' || ring,
      ground: style.backgroundColor,
    }
  })
  expect(painted.ground, `${name} panel has no ground`).not.toBe(
    'rgba(0, 0, 0, 0)',
  )
  expect(painted.edge, `${name} panel has no edge`).toBe(true)
}

/**
 * Waits until every finite animation on the page has finished, so a panel is
 * measured at the size it settles at rather than part-way through its
 * entrance.
 */
export const settleAnimations = (page: Page): Promise<void> =>
  page.evaluate(async () => {
    await Promise.all(
      document
        .getAnimations()
        .filter(
          (animation) =>
            animation.effect?.getComputedTiming().iterations !== Infinity,
        )
        .map((animation) => animation.finished.catch(() => undefined)),
    )
  })

/**
 * Whether a mutation is part of a flow these tests state.
 *
 * Two reads are not: the list card observes a list's sources as soon as it
 * opens, and a forecast observes an unread draft's lists once so it can be
 * weighed. Both change what a list knows, neither changes a profile, and neither
 * is anything the operator asked for by name.
 */
export const statesFlow = (path: string): boolean =>
  !path.startsWith('/v1/lists/') && path !== FORECAST_PATH

export const profileFlow = (
  mutations: { body: string | null; path: string }[],
): { body: string | null; path: string }[] =>
  mutations.filter((mutation) => statesFlow(mutation.path))

/** The language the document declares, which a reader's software pronounces. */
export const documentLanguage = (page: Page): Promise<string> =>
  page.evaluate(() => document.documentElement.lang)

/** The profile a page is on, read from its own address. */
export const profileIDFromURL = (value: string): string =>
  /\/profiles\/([a-f0-9]{32})/.exec(new URL(value).pathname)?.[1] ?? ''
