// Chromium reports a navigation the application itself replaced or cancelled
// as net::ERR_ABORTED. That is not an outcome the operator has to answer for.
const ABORTED = -3

// Whether a window failure is worth interrupting the operator for, and whether
// another attempt is still allowed, is policy. The window, its dialogs and the
// reload itself stay in the main process.
export function createRecovery({ retries = 2 } = {}) {
  let asking = false
  let used = 0
  let hung = false
  return {
    // 'retry' offers another attempt, 'stop' has spent them all, and 'ignore'
    // is not a failure the operator has to see. A burst of failures never
    // stacks a second question on top of the one already open.
    failed({ isMainFrame = false, errorCode = 0 } = {}) {
      if (!isMainFrame || errorCode === ABORTED || asking) return 'ignore'
      asking = true
      return used < retries ? 'retry' : 'stop'
    },
    // The operator asked for another attempt. It is spent either way, so a
    // reload that fails again cannot reopen the question without end.
    retried() {
      if (!asking) return false
      asking = false
      used++
      return true
    },
    // A completed document frees the attempts for a later, unrelated failure.
    // While a question is open, what completed is the error document for the
    // very failure being asked about, not a recovery from it.
    loaded() {
      if (!asking) used = 0
    },
    // One question per hang. The next one is asked only after the window has
    // actually recovered in between.
    unresponsive() {
      if (hung) return false
      hung = true
      return true
    },
    responsive() {
      hung = false
    },
  }
}
