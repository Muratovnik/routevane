/**
 * The content security policy the browser suites hold the product to.
 *
 * The interface is served with a strict policy — `style-src 'self'` and no
 * nonce — and what the browser refuses under it does not break anything a
 * test reads: a component library that writes a `<style>` element or a
 * `style` attribute renders on, merely unstyled in the corner that needed it.
 * Reading the header proves the policy is sent, not that the product lives
 * inside it. This fixture listens for the refusals themselves and fails the
 * test during which one happened.
 *
 * It is an automatic fixture on the browser context. The listener is an init
 * script, so it is attached to every document before that document's own
 * scripts run — each navigation, each frame, and each page the context opens,
 * a popup included. It reports through a binding on the same context, which
 * carries a violation to the test runner as it happens instead of leaving it
 * in a document the next navigation discards. The browser also logs a
 * refusal to the console, but as a sentence of its own wording; the
 * `securitypolicyviolation` event carries the directive and the source as
 * fields. A context a test opens for itself through `browser` is outside it.
 */
import { test as base, type BrowserContext } from '@playwright/test'

/** One refusal, as the browser described it in its violation event. */
export interface PolicyViolation {
  /** The document that broke its policy. */
  readonly documentURI: string
  /** The directive that refused the resource, e.g. `style-src-elem`. */
  readonly directive: string
  /** What was refused: a URL, or `inline` / `eval` for code without one. */
  readonly blockedURI: string
  /** The script or document the refused act came from, when the browser knows it. */
  readonly sourceFile: string
  readonly lineNumber: number
  readonly columnNumber: number
  /** The start of the refused code, which a policy only reports with `'report-sample'`. */
  readonly sample: string
  /** `enforce` for a refusal, `report` for a report-only policy. */
  readonly disposition: string
}

/** What a test may do with the violations recorded while it runs. */
export interface PolicyViolationLog {
  /** Everything recorded so far, in the order the browser reported it. */
  seen(): readonly PolicyViolation[]
  /**
   * Forgets what has been recorded. Only a case whose subject is a violation
   * does this, after it has asserted the one it caused.
   */
  clear(): void
}

interface PolicyFixtures {
  policyViolations: PolicyViolationLog
}

const BINDING = '__routevaneReportPolicyViolation'

/**
 * Serialized into every document, so it names nothing outside itself. The
 * binding is looked up when a violation arrives rather than when the script
 * starts, which leaves the order Playwright installs the two in irrelevant.
 */
const listenForViolations = (binding: string): void => {
  window.addEventListener(
    'securitypolicyviolation',
    (event) => {
      const report = Reflect.get(window, binding) as (value: unknown) => unknown
      void report({
        blockedURI: event.blockedURI,
        columnNumber: event.columnNumber,
        directive: event.effectiveDirective,
        disposition: event.disposition,
        documentURI: event.documentURI,
        lineNumber: event.lineNumber,
        sample: event.sample,
        sourceFile: event.sourceFile,
      })
    },
    true,
  )
}

const describeViolation = (violation: PolicyViolation): string => {
  const source =
    violation.sourceFile === ''
      ? 'no source file'
      : `${violation.sourceFile}:${violation.lineNumber}:${violation.columnNumber}`
  const sample = violation.sample === '' ? '' : `, sample ${violation.sample}`
  return `  ${violation.directive} refused ${violation.blockedURI || '(no URI)'} on ${violation.documentURI} (${violation.disposition}) from ${source}${sample}`
}

/** Throws, naming every violation, unless there is none. */
export const assertNoPolicyViolations = (
  violations: readonly PolicyViolation[],
): void => {
  if (violations.length === 0) return
  throw new Error(
    `the page broke its content security policy ${violations.length} time(s):\n${violations.map(describeViolation).join('\n')}`,
  )
}

/**
 * A violation event is a task the browser queues after the refusal, so one
 * the last step caused may still be on its way. One round trip to every open
 * page, after a task of its own, lets it arrive; a page that is closing or
 * navigating has nothing left to deliver.
 */
const settle = async (context: BrowserContext): Promise<void> => {
  await Promise.all(
    context.pages().map((page) =>
      page
        .evaluate(
          () =>
            new Promise<void>((resolve) => {
              setTimeout(resolve, 0)
            }),
        )
        .catch(() => undefined),
    ),
  )
}

export const test = base.extend<PolicyFixtures>({
  policyViolations: [
    async ({ context }, use) => {
      const recorded: PolicyViolation[] = []
      await context.exposeBinding(BINDING, (_source, violation) => {
        recorded.push(violation as PolicyViolation)
      })
      await context.addInitScript(listenForViolations, BINDING)
      await use({
        clear: () => {
          recorded.length = 0
        },
        seen: () => [...recorded],
      })
      await settle(context)
      assertNoPolicyViolations(recorded)
    },
    { auto: true },
  ],
})
