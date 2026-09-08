import { createEventHook } from '@vueuse/core'

/** Source attempts (including partial failure) and edits invalidate consumers.
 * observed distinguishes an attempted read from a configuration edit; it is
 * never the source-readiness result displayed to the operator.
 */
export const listChanges = createEventHook<{
  listID: string
  observed: boolean
}>()
