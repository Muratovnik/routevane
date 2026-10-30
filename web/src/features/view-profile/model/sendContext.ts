import type { TargetOption } from '@/shared/api/catalog'

/**
 * What a send of one format needs: the file, where it goes and what to call it.
 * The page that composes the profile fills the send panel from it, because the
 * act of sending is a feature of its own.
 */
export type SendContext = {
  artifactID: string
  groupPrefix: string
  heading: string
  outputID: string
  preferredDeviceID: string
  target: TargetOption | null
  targetID: string
}
