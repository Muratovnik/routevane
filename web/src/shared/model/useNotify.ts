import { readonly, ref } from 'vue'

/**
 * What one finished act says about itself once it is done.
 *
 * A notice is the outcome of an act the operator took and can no longer see
 * finishing — a list created, a password saved. It is not
 * where a failure goes: a refusal belongs beside the control that caused it,
 * where it can be corrected, so `tone` has no failed value. An act that can be
 * taken back, or whose result now lives elsewhere, offers that one step as
 * `action`.
 */
export type Notice = {
  title: string
  body?: string
  tone?: 'ready' | 'waiting'
  action?: { label: string; run: () => void }
}

export type ShownNotice = Notice & { id: number }

/** How long a notice stays, unless the pointer or the keyboard holds it. */
export const NOTICE_DURATION = 6000
/** A notice that offers a step stays long enough to reach for it. */
export const NOTICE_ACTION_DURATION = 10_000
/** Older notices give way once this many stand at once. */
const NOTICE_LIMIT = 3

// One stack for the whole interface: a notice outlives the page that raised it,
// because an act that navigates — deleting what was open — still has to be
// heard on the page it lands on.
const shown = ref<ShownNotice[]>([])
let next = 0

export const useNotify = () => {
  const dismiss = (id: number): void => {
    shown.value = shown.value.filter((notice) => notice.id !== id)
  }

  const notify = (notice: Notice): number => {
    next += 1
    const id = next
    shown.value = [...shown.value, { ...notice, id }].slice(-NOTICE_LIMIT)
    return id
  }

  return { dismiss, notices: readonly(shown), notify }
}
