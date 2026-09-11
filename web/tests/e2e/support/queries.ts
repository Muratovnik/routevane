/**
 * The accessible queries the browser suites share.
 *
 * A suite finds an element the way an operator perceives it: by role, by the
 * accessible name it is announced with, by the label of the field it belongs
 * to, or by the words on screen. The queries several tests repeat live here, so
 * one table's column order or one control's wording is stated once instead of
 * being spelled out at every call site.
 */
import type { Locator, Page } from '@playwright/test'

import { englishCopy, type Copy } from './copy'

/**
 * Either a page or a region of one. Both answer the same query methods, so a
 * helper takes whichever the caller already has in hand.
 */
export type Scope = Page | Locator

/**
 * The page a scope belongs to.
 *
 * A `filter({ has })` query keeps the inner locator's selector as it was
 * written and runs it again inside each candidate the outer query matched. An
 * inner locator built from a region therefore looks for that region *inside*
 * every row, and matches nothing at all — so the queries below build what they
 * filter by from the page, and let the outer query carry the region.
 */
const pageOf = (scope: Scope): Page => ('page' in scope ? scope.page() : scope)

/** Escapes a value that is spliced into a regular expression. */
export const escapeRegExp = (value: string): string =>
  value.replaceAll(/[.*+?^${}()|[\]\\]/g, '\\$&')

const nameMatching = (value: string | RegExp): RegExp =>
  typeof value === 'string' ? new RegExp(escapeRegExp(value)) : value

/**
 * The body rows of a table. Every body row of the composition table, of the
 * library table and of the shelf carries its own row header or its own leading
 * cell; the row of column headers above them carries neither, which is what
 * separates the two.
 */
export const bodyRows = (scope: Scope): Locator =>
  scope.getByRole('row').filter({ has: pageOf(scope).getByRole('rowheader') })

/**
 * The control that puts one list into the profile or takes it back out. Its
 * accessible name states the act and the list, so the list's own title is what
 * identifies it.
 */
export const listMembership = (scope: Scope, list: string | RegExp): Locator =>
  scope.getByRole('checkbox', { name: nameMatching(list) })

/** The composition row of one list, reached through the membership control on it. */
export const listRow = (scope: Scope, list: string | RegExp): Locator =>
  scope.getByRole('row').filter({ has: listMembership(pageOf(scope), list) })

/**
 * The library row of one list. The library states membership nowhere, so the
 * row is found by the header that names the list it stands for.
 */
export const libraryRow = (scope: Scope, list: string): Locator =>
  scope.getByRole('row').filter({
    has: pageOf(scope).getByRole('rowheader', { name: list, exact: true }),
  })

/** The control in a library row that opens the list the row names. */
export const libraryRowName = (row: Locator): Locator =>
  row.getByRole('rowheader').getByRole('button')

/** The composition rows the profile currently holds, in priority order. */
export const selectedListRows = (scope: Scope): Locator =>
  bodyRows(scope).filter({
    has: pageOf(scope).getByRole('checkbox', { checked: true }),
  })

/**
 * The handle a row's priority is carried by. It is the row's leading control,
 * because priority is the first thing a row states.
 */
export const priorityHandle = (row: Locator): Locator =>
  row.getByRole('button').first()

// The composition table's own cells, in cell order: priority, membership,
// category, rules, overlaps, open. The row header that names the list sits
// between membership and category and is not a cell, so it takes no index.
const CATEGORY_CELL = 2
const RULES_CELL = 3
const OVERLAPS_CELL = 4

/** What one composition row says about the categories its list belongs to. */
export const categoryCell = (row: Locator): Locator =>
  row.getByRole('cell').nth(CATEGORY_CELL)

/** What one composition row says the list would contribute in rules. */
export const rulesCell = (row: Locator): Locator =>
  row.getByRole('cell').nth(RULES_CELL)

/** What one composition row says about overlaps with the other chosen lists. */
export const overlapsCell = (row: Locator): Locator =>
  row.getByRole('cell').nth(OVERLAPS_CELL)

// The library table's own cells, in cell order: priority, category, actions.
// The list's name is the row header between the first two.
const LIBRARY_CATEGORY_CELL = 1

/** The category cell of a library row, which opens the row when it is pressed. */
export const libraryCategoryCell = (row: Locator): Locator =>
  row.getByRole('cell').nth(LIBRARY_CATEGORY_CELL)

// The shelf's own cells, in cell order: name, connections, content date,
// actions.
const PROFILE_OUTPUTS_CELL = 1

/** What one shelf row says the profile is connected to. */
export const profileOutputsCell = (row: Locator): Locator =>
  row.getByRole('cell').nth(PROFILE_OUTPUTS_CELL)

/**
 * The switch on an open list card that says — and sets — whether the route being
 * composed carries that list. Its name is the same at either position, so this
 * is an exact match; a caller that wants the state reads `aria-checked` from
 * the same control it presses.
 */
export const cardMembership = (card: Locator, copy: Copy): Locator =>
  card.getByRole('switch', {
    exact: true,
    name: copy('listCard.membership.in'),
  })

/**
 * The list card's contents table: one list, whose rows are its entries. The
 * card holds exactly one, so the role is the whole address.
 */
export const cardContents = (card: Locator): Locator => card.getByRole('list')

/** The rows of the list card's contents table. */
export const cardRows = (card: Locator): Locator => card.getByRole('listitem')

/** One row of the list card's contents table, addressed by what it names. */
export const cardRow = (card: Locator, value: string): Locator =>
  cardRows(card).filter({ hasText: value })

/**
 * The panel an action menu opens. Every entry in it is a menu item, so the
 * panel is the menu itself.
 */
export const menuPanel = (page: Page): Locator => page.getByRole('menu')

/**
 * A link addressed by where it leads. Several profiles in these suites are
 * built from the same lists and therefore carry the same title, so the address
 * — what a reader would actually follow — is the only thing that tells their
 * rows apart. The identity comes from the argument, so this is not a fixed
 * markup selector.
 */
export const linkTo = (page: Page, href: string): Locator =>
  page.getByRole('link').and(page.locator(`[href="${href}"]`))

/**
 * The dimming layer behind a modal surface.
 *
 * A layer has no role and no name, so RvDialog puts a hook on the one it owns
 * itself — the panel variant's. The sheet's layer belongs to Nuxt UI's
 * USlideover, which renders it with an internal data-slot of its own and takes
 * no attributes for it, so nothing can hook that one. The class both variants
 * carry is therefore the only handle that reaches a whole stack, which is what
 * a test counting the dimming of a stack needs. It is one of the three
 * selectors `playwright/no-raw-locators` allows, and it is written here alone.
 */
export const dialogScrims = (page: Page): Locator =>
  page.locator('.rv-dialog__scrim')

/**
 * The clone SortableJS drags. It is the library's own artifact, a copy of the
 * row under the pointer, so it carries no role or name that would tell it from
 * the original. The second selector the lint rule allows.
 */
export const dragGhost = (page: Page): Locator =>
  page.locator('.sortable-fallback')

/**
 * Every control an operator can press, for a target-size sweep.
 *
 * Three kinds reach the screen. The buttons answer to their role. A product
 * action that navigates is rendered as a link, and no role tells that apart
 * from a link inside a sentence — which the size criterion exempts — so the
 * design system's own class is the only handle for the distinction, and it is
 * the third selector the lint rule allows. A segmented choice carries its
 * state on a radio that is one clipped pixel behind the label that is actually
 * pressed, so a caller measures the label around the input rather than the
 * input itself.
 */
export const pressableTargets = (page: Page): Locator =>
  page
    .getByRole('button', { disabled: false })
    .or(page.locator('a.rv-button'))
    .or(page.getByRole('radio'))

/**
 * The widths of one row's own cells, in document order. Read from the row
 * itself so the row header counts alongside the data cells, which is what
 * makes a dragged clone comparable to the row it copies.
 */
export const rowCellWidths = (row: Locator): Promise<number[]> =>
  row.evaluate((element) =>
    Array.from(element.children, (cell) => cell.getBoundingClientRect().width),
  )

// The fields, choices and regions the suites name below are captioned by the
// product's own words, so each takes a dictionary and defaults to English: a
// localized suite hands its own words to the same query.

/** The composer's name field, which the editor captions the same way. */
export const nameField = (scope: Scope): Locator =>
  scope.getByLabel(englishCopy('create.name'), { exact: true })

/** The field that states where a profile is delivered. */
export const deliveryField = (
  scope: Scope,
  copy: Copy = englishCopy,
): Locator => scope.getByLabel(copy('create.target'))

/** The field a published profile adds another connection with. */
export const addConnectionField = (
  scope: Scope,
  copy: Copy = englishCopy,
): Locator => scope.getByLabel(copy('outputs.add'), { exact: true })

/** The list of connection formats a choice offers. */
export const formatList = (page: Page): Locator => page.getByRole('listbox')

/**
 * The list card's own title field. The section that holds it is announced by
 * the same caption, so the field is addressed as the control it is rather than
 * by the caption the two of them share.
 */
export const titleField = (scope: Scope, copy: Copy = englishCopy): Locator =>
  scope.getByRole('textbox', {
    exact: true,
    name: copy('listCard.title.field'),
  })

/** The choice that says how often a published profile is refreshed. */
export const scheduleField = (page: Page, copy: Copy = englishCopy): Locator =>
  page.getByRole('combobox', { name: copy('profile.schedule'), exact: true })

/** One field of the connection form, by the caption its target asks for. */
export const deviceField = (
  scope: Scope,
  key: string,
  copy: Copy = englishCopy,
): Locator => scope.getByLabel(copy(key), { exact: true })

/** The panel a searchable choice opens, by the heading it announces. */
export const choicePanel = (page: Page, heading: string): Locator =>
  page.getByRole('dialog', { name: heading })

/** The panel an informer opens, named by the control that offers it. */
export const infoPanel = (page: Page, label: string): Locator =>
  page.getByRole('dialog', { name: label })

/**
 * What the composition table says about the forecast — that it is calculating
 * the overlaps, that some lists could not be calculated, or that the
 * calculation is unavailable. The settings rail repeats the same sentence
 * beside the format it belongs to, and the table announces it a second time
 * for a reader's software, so the message is read from the status the table
 * itself carries.
 */
export const forecastStatus = (page: Page, message: string): Locator =>
  page.getByRole('status').filter({ hasText: message })

/** The field that narrows a searchable choice to what was typed. */
export const choiceSearch = (page: Page, copy: Copy = englishCopy): Locator =>
  page.getByRole('textbox', { name: copy('choice.search'), exact: true })

/** The disclosure that holds the catalog of everything this build can reach. */
export const catalogDisclosure = (
  page: Page,
  copy: Copy = englishCopy,
): Locator => page.getByRole('button', { name: copy('targets.title') })

/**
 * One option of a segmented choice, by the words it is announced with. The
 * radio carries the name and the state.
 */
export const segmentOption = (scope: Scope, label: string): Locator =>
  scope.getByRole('radio', { name: label, exact: true })

/** The interval a profile without a rule of its own follows. */
export const refreshInterval = (
  page: Page,
  copy: Copy = englishCopy,
): Locator => page.getByRole('group', { name: copy('settings.refresh') })

/** The library section of «Списки», by the heading it is announced with. */
export const libraryRegion = (page: Page, copy: Copy = englishCopy): Locator =>
  page.getByRole('region', { name: copy('lists.title'), exact: true })

/** The filter row the library and the composer share. */
export const categoryFilters = (
  page: Page,
  copy: Copy = englishCopy,
): Locator => page.getByRole('group', { name: copy('listPicker.filter.label') })

/**
 * The control that offers the categories the filter row has no room to show as
 * chips. It announces the collection heading and then its own choice.
 */
export const categoryMore = (page: Page, copy: Copy = englishCopy): Locator =>
  categoryFilters(page, copy).getByRole('button', {
    name: new RegExp(`^${escapeRegExp(copy('listPicker.collections'))}: `),
  })

/** One quick-filter chip, which states its category and then its size. */
export const categoryChip = (
  page: Page,
  category: string,
  copy: Copy = englishCopy,
): Locator =>
  categoryFilters(page, copy).getByRole('button', { name: category })

/**
 * The refresh the composer offers for the lists a draft already holds. It is
 * named for the lists it reads, which is what tells it from rebuilding the
 * profile's files — a different act on a different object, named for those.
 */
export const compositionRefresh = (
  page: Page,
  copy: Copy = englishCopy,
): Locator =>
  page.getByRole('button', { name: copy('listPicker.refresh'), exact: true })

/** The settings rail beside the composition table. */
export const composerSettings = (
  page: Page,
  copy: Copy = englishCopy,
): Locator => page.getByRole('complementary', { name: copy('create.settings') })

/** The control on a row that opens the list it stands for. */
export const openList = (
  scope: Scope,
  list: string,
  copy: Copy = englishCopy,
): Locator =>
  scope.getByRole('button', {
    name: copy('listDetail.open.aria').replace('{list}', list),
  })
