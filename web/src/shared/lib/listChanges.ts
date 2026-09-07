import { createEventHook } from '@vueuse/core'

/** Successful source and entry changes affect forecasts in every mounted consumer. */
export const listChanges = createEventHook<{
  listID: string
  observed: boolean
}>()
