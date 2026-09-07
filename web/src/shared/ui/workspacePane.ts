import {
  inject,
  nextTick,
  onScopeDispose,
  type ComputedRef,
  type InjectionKey,
  type Ref,
} from 'vue'

export const workspacePane: InjectionKey<{
  target: string
  docked: ComputedRef<boolean>
  open: Ref<boolean>
  locked: Ref<boolean>
}> = Symbol('workspacePane')

// Capture before the selection changes, not from a watcher after Vue has
// already moved the form and table. All inspection entry points share this.
export const useWorkspaceInspection = () => {
  const pane = inject(workspacePane, null)
  let transition: ViewTransition | undefined
  let generation = 0
  let disposed = false
  onScopeDispose(() => {
    disposed = true
    generation += 1
    transition?.skipTransition()
  })
  return (opening: boolean, update: () => void): void => {
    const request = ++generation
    if (
      !pane ||
      pane.open.value === opening ||
      !document.startViewTransition ||
      window.matchMedia('(prefers-reduced-motion: reduce)').matches
    ) {
      update()
      return
    }
    transition?.skipTransition()
    document.documentElement.dataset.rvWorkspaceTransition = ''
    const current = document.startViewTransition(async () => {
      if (disposed || request !== generation) return
      update()
      await nextTick()
      // Reka mounts portalled content on its next render. Capture the populated
      // sheet, not the empty destination that existed at the first Vue flush.
      await nextTick()
    })
    transition = current
    void current.finished
      .catch(() => {})
      .finally(() => {
        if (transition !== current) return
        delete document.documentElement.dataset.rvWorkspaceTransition
        transition = undefined
      })
  }
}
