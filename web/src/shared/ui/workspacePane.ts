import type { ComputedRef, InjectionKey, Ref } from 'vue'

export const workspacePane: InjectionKey<{
  target: string
  docked: ComputedRef<boolean>
  open: Ref<boolean>
}> = Symbol('workspacePane')
