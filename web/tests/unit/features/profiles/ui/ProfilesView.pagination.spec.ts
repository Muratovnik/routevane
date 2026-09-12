import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-vue'

import ProfilesView from '@/features/profiles/ui/ProfilesView.vue'
import { invalidateCatalogCache } from '@/shared/api/catalog'
import { useLocale } from '@/shared/i18n/useLocale'

const cursor = 'b'.repeat(32)
const firstID = 'a'.repeat(32)
const secondID = 'c'.repeat(32)

const json = (payload: unknown): Response =>
  new Response(JSON.stringify(payload), {
    headers: { 'Content-Type': 'application/json' },
  })

const profile = (id: string, name: string): Record<string, unknown> => ({
  id,
  name,
  lists: ['example'],
  categories: [],
  exclusions: [],
  resolved: ['example'],
  missing_categories: [],
  created_at: '2026-09-12T12:00:00Z',
  updated_at: '2026-09-12T12:00:00Z',
  outputs: [],
})

describe('ProfilesView pagination', () => {
  beforeEach(() => {
    invalidateCatalogCache()
    useLocale().setLocale('en')
    vi.stubGlobal('useRoute', () => ({ hash: '' }))
    vi.stubGlobal('useRouter', () => ({ push: vi.fn(), replace: vi.fn() }))
  })

  afterEach(() => vi.unstubAllGlobals())

  it('keeps the loaded shelf visible when a continuation fails and retries the same page', async () => {
    let continuationReads = 0
    vi.stubGlobal(
      'fetch',
      vi.fn((input: string) => {
        if (input === '/v1/profiles')
          return Promise.resolve(
            json({
              profiles: [profile(firstID, 'Newest profile')],
              next: cursor,
            }),
          )
        if (input === '/v1/lists')
          return Promise.resolve(
            json({ lists: [], list_details: [], categories: [] }),
          )
        if (input === '/v1/deployments/targets')
          return Promise.resolve(json({ targets: [] }))
        if (input === '/v1/export-formats')
          return Promise.resolve(json({ formats: [] }))
        if (input === `/v1/profile-pages/${cursor}`) {
          continuationReads += 1
          if (continuationReads === 1)
            return Promise.reject(new TypeError('service unavailable'))
          return Promise.resolve(
            json({ profiles: [profile(secondID, 'Older profile')], next: '' }),
          )
        }
        return Promise.reject(new Error(`unexpected request ${input}`))
      }),
    )

    const screen = await render(ProfilesView, {
      global: {
        stubs: {
          NuxtLink: { props: ['to'], template: '<a :href="to"><slot /></a>' },
        },
      },
    })
    await expect
      .element(screen.getByRole('link', { name: 'Newest profile' }))
      .toBeVisible()

    await screen.getByRole('button', { name: 'Load more' }).click()
    await expect
      .element(screen.getByText('Could not load more profiles.'))
      .toBeVisible()
    await expect
      .element(screen.getByRole('link', { name: 'Newest profile' }))
      .toBeVisible()

    await screen.getByRole('button', { name: 'Retry' }).click()
    const older = screen.getByRole('link', { name: 'Older profile' })
    await expect.element(older).toBeVisible()
    await expect.element(older).toHaveAttribute('href', `/profiles/${secondID}`)
    await expect
      .element(screen.getByRole('button', { name: 'Load more' }))
      .not.toBeInTheDocument()
  })
})
