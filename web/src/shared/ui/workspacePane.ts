import type { ComputedRef, InjectionKey, Ref } from 'vue'

export const workspacePane: InjectionKey<{
  target: string
  docked: ComputedRef<boolean>
  open: Ref<boolean>
  locked: Ref<boolean>
}> = Symbol('workspacePane')
