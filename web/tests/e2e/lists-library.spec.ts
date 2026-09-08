/**
 * The «Списки» section: the order new profiles start from, and the categories
 * an operator makes, fills, filters by and deletes.
 *
 * The worker fixture in `support/served-product.ts` serves this suite
 * alone, over the data directory named below.
 */

import { expect, type Locator, type Page } from '@playwright/test'

import { englishCopy } from './support/copy'
import {
  chooseFormat,
  expectLibraryCategory,
  openCategoryActions,
  openLibraryCategory,
} from './support/flows'
import {
  bodyRows,
  categoryChip,
  categoryMore,
  choiceSearch,
  escapeRegExp,
  libraryRegion,
  libraryRow,
  listMembership,
  listRow,
  menuPanel,
  nameField,
  priorityHandle,
  selectedListRows,
} from './support/queries'
import { test } from './support/served-product'

// The surface negotiates its language from the browser, and English is the
// product's primary language. `productData` is this suite's own data
// directory: it starts empty, no other suite writes to it, and it is gone
// again when the suite ends.
test.use({ locale: 'en-US', productData: 'lists-library' })

test('the visible library order seeds new profiles without rewriting saved profiles', async ({
  page,
  origin,
}) => {
  const headers = { 'X-Routevane-Request': '1' }
  const catalog = (await (
    await page.request.get(`${origin}/v1/lists`)
  ).json()) as {
    default_priority: string[]
    list_details: { id: string; title: string }[]
  }
  const original = [...catalog.default_priority]
  // The order is stored as identifiers and read on screen as titles, so the
  // expectations below name the list the same way the row does.
  const titles = new Map(
    catalog.list_details.map((list) => [list.id, list.title]),
  )
  const rowTitle = (listID: string): string => titles.get(listID) ?? listID

  try {
    await page.goto(`${origin}/lists`)
    const sheet = libraryRegion(page)
    await expect(sheet).not.toContainText(
      'New profiles start with this order. Existing saved profiles do not change.',
    )

    const first = bodyRows(sheet).first()
    const firstHandle = await priorityHandle(first).boundingBox()
    const secondRow = await bodyRows(sheet).nth(1).boundingBox()
    await page.mouse.move(firstHandle!.x + 8, firstHandle!.y + 8)
    await page.mouse.down()
    await page.mouse.move(
      firstHandle!.x + 8,
      secondRow!.y + secondRow!.height - 4,
      { steps: 12 },
    )
    await page.mouse.up()
    await expect(first.getByRole('rowheader')).toHaveText(
      rowTitle(original[1]!),
    )
    await sheet.getByRole('button', { name: 'Reset order' }).click()
    await expect(first.getByRole('rowheader')).toHaveText(
      rowTitle(original[0]!),
    )

    const handle = priorityHandle(libraryRow(sheet, 'YouTube'))
    for (let index = original.indexOf('youtube'); index > 0; index -= 1)
      await handle.press('ArrowUp')
    await expect(first.getByRole('rowheader')).toHaveText('YouTube')
    await sheet.getByRole('button', { name: 'Save order' }).click()
    await expect(sheet.getByRole('button', { name: 'Save order' })).toBeHidden()

    await page.goto(`${origin}/profiles/new`)
    const search = page.getByRole('searchbox', { name: 'Find a list' })
    await search.fill('Discord')
    await listMembership(page, 'Discord').check()
    await search.fill('YouTube')
    await listMembership(page, 'YouTube').check()
    await search.fill('')
    // A row states its own priority on the handle that changes it, which is
    // where an operator reads the position too.
    await expect(priorityHandle(listRow(page, 'YouTube'))).toHaveAccessibleName(
      'Change priority of list YouTube, position 1',
    )
    await expect(priorityHandle(listRow(page, 'Discord'))).toHaveAccessibleName(
      'Change priority of list Discord, position 2',
    )
  } finally {
    const restored = await page.request.post(`${origin}/v1/lists/priority`, {
      data: { default_priority: original },
      headers,
    })
    expect(restored.ok(), await restored.text()).toBe(true)
  }
})

/**
 * A category is the operator's as much as the catalog's (ADR 0028): they can
 * make one, fill it from the whole catalog, and every profile naming it follows
 * what it holds. The one thing they cannot do is take it out from under a profile
 * still built from it — and the refusal names that profile, so the way forward is
 * the one the message states.
 */
test('an operator category carries its lists into a profile and is kept while a profile names it', async ({
  page,
  origin,
  assertProductAlive,
}) => {
  test.setTimeout(60000)
  // Curating happens in its own section (ADR 0029), so the category exists
  // before the composer is opened at all.
  await page.goto(`${origin}/lists`)
  await expect(
    page.getByRole('heading', { level: 1, name: 'Lists' }),
  ).toBeVisible()

  // The library is an operational workspace, not a short card surrounded by
  // unused page. Its rows stay dense while the two panes claim the available
  // viewport height.
  const viewport = page.viewportSize()
  const workspaceBox = await page
    .getByTestId('rv-lists-workspace')
    .boundingBox()
  const firstListBox = await bodyRows(libraryRegion(page)).first().boundingBox()
  expect(workspaceBox?.height ?? 0).toBeGreaterThanOrEqual(
    Math.floor((viewport?.height ?? 0) * 0.55),
  )
  expect(firstListBox?.height ?? Number.POSITIVE_INFINITY).toBeLessThanOrEqual(
    48,
  )

  await page.getByRole('button', { name: 'Categories', exact: true }).click()
  await page.getByRole('button', { name: 'New category' }).click()
  const categoryForm = page.getByRole('dialog', { name: 'New category' })
  // The panel opens with the keyboard in its field, so typing starts there
  // and is not undone by a focus that arrives late.
  await expect(categoryForm.getByLabel('Name')).toBeFocused()
  await categoryForm.getByLabel('Name').fill('Домашние')
  await categoryForm
    .getByRole('button', { exact: true, name: 'Create' })
    .click()
  await expect(categoryForm).toBeHidden()

  // The category just made is the one on screen, because it was made to be
  // filled.
  const details = libraryRegion(page)
  await expectLibraryCategory(page, 'Домашние')

  await page.getByRole('button', { name: 'New list' }).click()
  const addList = page.getByRole('dialog', { name: 'New list' })
  await addList.getByText('Existing list', { exact: true }).click()
  await addList
    .getByRole('button', {
      name: englishCopy('lists.category.pick.placeholder'),
    })
    .click()
  await choiceSearch(page).fill('Discord')
  await page.getByRole('option', { name: 'Discord' }).click()
  await addList.getByRole('button', { exact: true, name: 'Add' }).click()
  await expect(addList).toBeHidden()
  await expect(details).toContainText('Discord')

  await page.goto(`${origin}/profiles/new`)
  await expect(
    page.getByRole('heading', { level: 1, name: 'New profile' }),
  ).toBeVisible()

  // A filtered category exposes an explicit live-reference choice, so the
  // profile follows it rather than freezing today's members.
  const categoryFilter = categoryMore(page)
  await categoryFilter.click()
  await page.getByRole('option', { name: 'Домашние' }).click()
  await page.getByRole('button', { name: 'Follow “Домашние”' }).click()
  await page
    .getByRole('menuitem', {
      name: 'Automatically include new lists in this category',
    })
    .click()
  await categoryFilter.click()
  await page.getByRole('option', { name: 'Video' }).click()
  await listMembership(page, 'YouTube').check()
  await expect(nameField(page)).toHaveValue('Домашние, YouTube')

  await chooseFormat(page, /Keenetic/)
  await page.getByRole('button', { name: 'Create and prepare' }).click()
  await page.waitForURL(
    new RegExp(`^${escapeRegExp(origin)}/profiles/[a-f0-9]{32}.*$`),
  )
  await expect(
    page.getByRole('heading', { level: 1, name: 'Домашние, YouTube' }),
  ).toBeVisible()

  // The saved profile still states the category reference through the filter,
  // while the ordered rail shows the concrete list it currently contributes.
  await page
    .getByRole('tablist', { name: 'Profile sections' })
    .getByRole('tab', { name: 'Contents' })
    .click()
  const editorFilter = categoryMore(page)
  await editorFilter.click()
  await page.getByRole('option', { name: 'Домашние' }).click()
  await expect(
    page.getByRole('button', { name: 'Follow “Домашние”' }),
  ).toContainText('New lists: automatic')
  await expect(
    selectedListRows(page).filter({ hasText: 'Discord' }),
  ).toHaveCount(1)

  const routeURL = page.url()

  // A profile is built from this category, so the category stays where it is and
  // the refusal names the profile standing in the way — beside the act it
  // refused, which is the one place the operator is standing.
  await page.goto(`${origin}/lists`)
  await openCategoryActions(page, 'Домашние')
  await menuPanel(page)
    .getByRole('menuitem', { name: 'Delete the category' })
    .click()
  const removal = page.getByRole('dialog', { name: 'Delete the category' })
  await removal.getByRole('button', { exact: true, name: 'Delete' }).click()
  await expect(removal.getByText('The category was not deleted')).toBeVisible()
  await expect(
    removal.getByText(
      'It is part of these profiles: Домашние, YouTube. Remove it there and try again.',
    ),
  ).toBeVisible()
  await removal.getByRole('button', { name: 'Cancel' }).click()
  await page.getByRole('button', { name: 'Categories', exact: true }).click()
  const categories = page.getByRole('dialog', {
    name: 'Categories',
    exact: true,
  })
  await expect(categories).toContainText('Домашние')
  await categories.getByRole('button', { name: 'Close', exact: true }).click()

  // Doing what the refusal asks is what makes the deletion possible, and the
  // profile keeps everything it did not follow through the category.
  await page.goto(routeURL)
  await page
    .getByRole('tablist', { name: 'Profile sections' })
    .getByRole('tab', { name: 'Contents' })
    .click()
  const routeEditorFilter = categoryMore(page)
  await routeEditorFilter.click()
  await page.getByRole('option', { name: 'Домашние' }).click()
  await page.getByRole('button', { name: 'Follow “Домашние”' }).click()
  await page
    .getByRole('menuitem', { name: 'Select new lists manually' })
    .click()
  await page.getByRole('button', { name: 'Save and rebuild' }).click()
  await expect(
    page.getByRole('button', { name: 'Follow “Домашние”' }),
  ).toContainText('New lists: manual')
  await page.getByRole('searchbox', { name: 'Find a list' }).fill('')
  await page
    .getByRole('button', { name: 'All categories', exact: true })
    .click()
  await expect(
    selectedListRows(page).filter({ hasText: 'YouTube' }),
  ).toHaveCount(1)

  // Nothing names it now, so it goes — and the list it held keeps the category
  // it already belonged to.
  await page.goto(`${origin}/lists`)
  await openCategoryActions(page, 'Домашние')
  await menuPanel(page)
    .getByRole('menuitem', { name: 'Delete the category' })
    .click()
  await removal.getByRole('button', { exact: true, name: 'Delete' }).click()
  await expect(removal).toBeHidden()
  await page.getByRole('button', { name: 'Categories', exact: true }).click()
  await expect(categories).not.toContainText('Домашние')
  await categories.getByRole('button', { name: 'Close', exact: true }).click()
  await openLibraryCategory(page, 'Communication')
  await expect(page.getByRole('table')).toContainText('Discord')
  assertProductAlive()
})

/**
 * The other answer to the one question deleting a category asks — and the
 * reason the question exists at all: a category made to hold one draft list
 * takes that list with it. Composing, meanwhile, offers none of this.
 */
test('the library deletes a category with its lists, and composing offers none of it', async ({
  page,
  origin,
  assertProductAlive,
}) => {
  test.setTimeout(180000)
  await page.goto(`${origin}/lists`)
  await expect(
    page.getByRole('heading', { level: 1, name: 'Lists' }),
  ).toBeVisible()

  await page.getByRole('button', { name: 'Categories', exact: true }).click()
  await page.getByRole('button', { name: 'New category' }).click()
  const categoryForm = page.getByRole('dialog', { name: 'New category' })
  await categoryForm.getByLabel('Name').fill('Черновик')
  await categoryForm
    .getByRole('button', { exact: true, name: 'Create' })
    .click()
  await expect(categoryForm).toBeHidden()

  // A list made while a category is open joins that category.
  await page.getByRole('button', { name: 'New list' }).click()
  const newProfile = page.getByRole('dialog', { name: 'New list' })
  await newProfile.getByLabel('Name').fill('Draft fixture')
  await newProfile.getByLabel('Domains').fill('draft.example')
  await newProfile.getByRole('button', { exact: true, name: 'Create' }).click()
  await expect(newProfile).toBeHidden()
  await expect(libraryRegion(page)).toContainText('Draft fixture')

  await openCategoryActions(page, 'Черновик')
  await menuPanel(page)
    .getByRole('menuitem', { name: 'Delete the category' })
    .click()
  const removal = page.getByRole('dialog', { name: 'Delete the category' })
  await removal.getByText('Delete them with it').click()
  await removal.getByRole('button', { exact: true, name: 'Delete' }).click()
  await expect(removal).toBeHidden()
  await page.getByRole('button', { name: 'Categories', exact: true }).click()
  const categories = page.getByRole('dialog', {
    name: 'Categories',
    exact: true,
  })
  await expect(categories).not.toContainText('Черновик')
  await categories.getByRole('button', { name: 'Close', exact: true }).click()
  await openLibraryCategory(page, 'Uncategorized')
  await expect(page.getByRole('table')).not.toContainText('Draft fixture')

  // Selection is a checkbox and nothing else: the composer has no category
  // menu, no way to make or unmake anything, and no bin on a list row.
  await page.goto(`${origin}/profiles/new`)
  await expect(page.getByRole('table')).toBeVisible()
  for (const gone of ['New category', 'New list'])
    await expect(page.getByRole('button', { name: gone })).toHaveCount(0)
  await expect(
    page.getByRole('button', { name: /^Actions for list/ }),
  ).toHaveCount(0)
  await expect(
    page.getByRole('button', { name: 'Delete the list', exact: true }),
  ).toHaveCount(0)
  assertProductAlive()
})

test('category tabs and More toggle a union that bulk selection can add to a profile', async ({
  page,
  origin,
}) => {
  await page.setViewportSize({ width: 1920, height: 960 })
  // Filtering by category is the same act on both surfaces, and what each does
  // with the union it produces is its own: the library bookmarks it, the
  // composer adds it to the draft. Each surface therefore carries what it
  // claims instead of the body branching on which one it is on.
  for (const { assertUnionUse, path } of [
    { assertUnionUse: assertLibraryBookmark, path: '/lists' },
    { assertUnionUse: assertComposerBulkSelection, path: '/profiles/new' },
  ]) {
    await page.goto(origin + path)
    await categoryChip(page, 'Communication').click()
    await categoryChip(page, 'Video').click()
    // Both surfaces answer with rows of their own table, each naming the list
    // it stands for.
    const rows = bodyRows(page)
    await expect
      .poll(async () =>
        (await rows.getByRole('rowheader').allInnerTexts())
          .map((name) => name.split('\n')[0])
          .sort(),
      )
      .toEqual(['Discord', 'YouTube'])
    await assertUnionUse(page, rows)
    await categoryMore(page).click()
    const video = page.getByRole('option', { name: /^Video/ })
    await expect(video).toHaveAttribute('aria-selected', 'true')
    await video.click()
    await expect(video).toHaveAttribute('aria-selected', 'false')
    await expect(
      page.getByRole('option', { name: /^Communication/ }),
    ).toHaveAttribute('aria-selected', 'true')
    await page.keyboard.press('Escape')
    await expect.poll(() => rows.count()).toBe(1)
    await categoryChip(page, 'Communication').click()
    await expect(
      categoryChip(page, englishCopy('listPicker.filter.all')),
    ).toHaveAttribute('aria-pressed', 'true')
    await expect.poll(() => rows.count()).toBeGreaterThan(2)
  }
})

/**
 * What the library does with the union: the categories it narrowed by are the
 * bookmark, so a reload comes back to the same two of them.
 */
const assertLibraryBookmark = async (
  page: Page,
  rows: Locator,
): Promise<void> => {
  // Reload the committed bookmark, not an in-flight router replacement.
  await expect
    .poll(() =>
      new URLSearchParams(new URL(page.url()).hash.slice(1))
        .getAll('category')
        .sort(),
    )
    .toEqual(['communication', 'video'])
  await page.reload()
  await expect.poll(() => rows.count()).toBe(2)
  for (const category of ['Communication', 'Video'])
    await expect(categoryChip(page, category)).toHaveAttribute(
      'aria-pressed',
      'true',
    )
}

/**
 * What the composer does with the union: one act puts every row it holds into
 * the draft, and widening the filter afterwards adds nothing on its own.
 */
const assertComposerBulkSelection = async (page: Page): Promise<void> => {
  await page
    .getByRole('checkbox', { name: englishCopy('listPicker.selectVisible') })
    .check()
  for (const title of ['Discord', 'YouTube'])
    await expect(listMembership(page, title)).toBeChecked()
  await categoryChip(page, englishCopy('listPicker.filter.all')).click()
  await expect(listMembership(page, 'Limit fixture')).not.toBeChecked()
  await categoryChip(page, 'Communication').click()
  await categoryChip(page, 'Video').click()
}
