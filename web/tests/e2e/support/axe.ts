/**
 * Every accessibility run in the browser suites, with the two findings the
 * product has accepted on Nuxt UI's command palette
 * (docs/adr/0043-accept-two-command-palette-findings.md).
 *
 * The palette renders its list, `role="listbox"`, without an accessible name
 * and passes no attribute on to it, which axe reports as
 * `aria-input-field-name`. A list longer than its panel scrolls in the
 * palette's viewport, whose options take no focus of their own because the
 * search field drives them as their active descendant, which axe reports as
 * `scrollable-region-focusable`. Each finding is waived on the palette's own
 * element alone and for that cause alone:
 *
 * - the node fails only the checks of that one rule and appears under no
 *   other rule;
 * - it is the palette's list — the library's `ListboxContent` directly inside
 *   the palette root — or that list's viewport, inside a panel the facade
 *   marks;
 * - for the list's name: the panel has an accessible name and the palette's
 *   search field is labelled;
 * - for the viewport: the search field names an option of that list as its
 *   active descendant, so the keyboard reaches the rows through it.
 *
 * Every other rule runs everywhere; a listbox or a scrolling region anywhere
 * else is reported as before, and so is the palette's own element once the
 * condition it was waived on no longer holds.
 */
import AxeBuilder from '@axe-core/playwright'
import type { Page } from '@playwright/test'

type AxeResults = Awaited<ReturnType<AxeBuilder['analyze']>>
type Result = AxeResults['violations'][number]
type NodeResult = Result['nodes'][number]

/** The mark `RvSearchSelect` puts on its palette's panel. */
export const PALETTE_PANEL = '[data-rv-choice-panel="palette"]'

type Accepted = 'list-name' | 'viewport'

// The rules waived and, for each, the checks a node may fail under it.
const ACCEPTED: Record<string, { checks: Set<string>; element: Accepted }> = {
  'aria-input-field-name': {
    checks: new Set(['aria-label', 'aria-labelledby', 'non-empty-title']),
    element: 'list-name',
  },
  'scrollable-region-focusable': {
    checks: new Set(['focusable-content', 'focusable-element']),
    element: 'viewport',
  },
}

// What axe itself reports for the accepted cause, before the page is asked.
const hasAcceptedShape = (node: NodeResult, checks: Set<string>): boolean =>
  node.target.length === 1 &&
  node.all.length === 0 &&
  node.none.length === 0 &&
  node.any.length > 0 &&
  node.any.every((check) => checks.has(check.id))

// What the page says about the node, the palette it belongs to and its search.
const isPaletteElement = (
  page: Page,
  node: NodeResult,
  element: Accepted,
): Promise<boolean> =>
  page.evaluate(
    ({ kind, mark, target }) => {
      const node = document.querySelector(target)
      const list =
        kind === 'viewport' ? node?.parentElement : (node ?? undefined)
      const root = list?.parentElement
      const panel = root?.closest(mark)
      // The palette's search field: the input in its root that is not part of
      // the list it drives.
      const search = [...(root?.querySelectorAll('input') ?? [])].find(
        (input) => !list?.contains(input),
      )
      if (!node || !list || !root || !panel || !search) return false
      const text = (id: string) =>
        document.getElementById(id)?.textContent?.trim() ?? ''
      const named = (item: Element): boolean =>
        (item.getAttribute('aria-label')?.trim() ?? '') !== '' ||
        (item.getAttribute('aria-labelledby') ?? '')
          .split(/\s+/)
          .some((id) => id !== '' && text(id) !== '')
      const isPaletteList =
        list.matches('[role="listbox"][data-slot="content"]') &&
        root.matches('[data-slot="root"]') &&
        panel.contains(root)
      if (!isPaletteList) return false
      if (kind === 'list-name') return named(panel) && named(search)
      const active = search.getAttribute('aria-activedescendant') ?? ''
      const option = active === '' ? null : document.getElementById(active)
      return (
        node.matches('[data-slot="viewport"]') &&
        option !== null &&
        option.matches('[role="option"]') &&
        list.contains(option)
      )
    },
    { kind: element, mark: PALETTE_PANEL, target: String(node.target[0]) },
  )

/** The builder every suite starts from. */
export const axeFor = (page: Page): AxeBuilder => new AxeBuilder({ page })

/**
 * Runs axe on the page — through the given builder when a suite configures one
 * — and removes the accepted findings and nothing else.
 */
export const analyze = async (
  page: Page,
  builder: AxeBuilder = axeFor(page),
): Promise<AxeResults> => {
  const results = await builder.analyze()
  const rulesOf = (node: NodeResult) =>
    results.violations.filter((violation) =>
      violation.nodes.some(
        (other) => other.target.join(' ') === node.target.join(' '),
      ),
    ).length
  const accepted = new Set<NodeResult>()
  for (const violation of results.violations) {
    const rule = ACCEPTED[violation.id]
    if (rule === undefined) continue
    for (const node of violation.nodes)
      if (
        hasAcceptedShape(node, rule.checks) &&
        rulesOf(node) === 1 &&
        (await isPaletteElement(page, node, rule.element))
      )
        accepted.add(node)
  }
  const violations = results.violations.flatMap((violation): Result[] => {
    const nodes = violation.nodes.filter((node) => !accepted.has(node))
    return nodes.length === 0 ? [] : [{ ...violation, nodes }]
  })
  return { ...results, violations }
}
