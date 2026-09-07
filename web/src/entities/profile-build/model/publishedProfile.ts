import { ref } from 'vue'

import type { BuildResult } from '@/shared/api/outputs'

/**
 * The builds this browser session has finished, keyed by output.
 *
 * The subscription URL is a secret the server issues once. It lives here, in
 * memory, for exactly as long as the tab does: never in storage, never in the
 * URL, never in a query. A refresh restores the profiles and their facts from the
 * server, but never the links — the product says so, and this module is where
 * that promise is kept. The build summary rides along so the profile page can
 * state warnings the server does not repeat.
 *
 * One profile feeds several outputs, so this is a map rather than a single
 * record: rebuilding one output must not erase what another one was told.
 */

export type FreshBuild = BuildResult & {
  subscriptionURL: string
  stale: boolean
}

const published = ref<Record<string, FreshBuild>>({})

export const usePublishedProfile = () => ({
  /** clear drops every secret as soon as they stop describing a build. */
  clear(): void {
    published.value = {}
  },
  forOutput(outputID: string): FreshBuild | null {
    return published.value[outputID] ?? null
  },
  publish(build: FreshBuild): void {
    published.value = { ...published.value, [build.output.id]: build }
  },
})
