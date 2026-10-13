import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { userEvent } from 'vitest/browser'
import { render } from 'vitest-browser-vue'

import ProfilesView from '@/features/profiles/ui/ProfilesView.vue'
import { invalidateCatalogCache } from '@/shared/api/catalog'
import { useLocale } from '@/shared/i18n/useLocale'

const PROFILE_ID = 'a'.repeat(32)
const ARTIFACT_ID = 'd'.repeat(32)
const ARCHIVE = `/v1/profiles/${PROFILE_ID}/archive`
const EXPORT = `/v1/profiles/${PROFILE_ID}/export`
const MENU = 'Actions for profile Newest'
const ORIGINAL_CLIPBOARD = Object.getOwnPropertyDescriptor(
  navigator,
  'clipboard',
)

const json = (payload: unknown, status = 200): Response =>
  new Response(JSON.stringify(payload), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })

const profile = (): Record<string, unknown> => ({
  id: PROFILE_ID,
  name: 'Newest',
  lists: ['example'],
  categories: [],
  exclusions: [],
  resolved: ['example'],
  missing_categories: [],
  archived_at: '',
  created_at: '2026-09-12T12:00:00Z',
  updated_at: '2026-09-12T12:00:00Z',
  outputs: [
    {
      id: 'output-1',
      target_id: 'keenetic',
      target_title: 'Keenetic',
      created_at: '2026-09-12T12:00:00Z',
      latest: {
        id: ARTIFACT_ID,
        snapshot_id: 'snapshot-1',
        size_bytes: 14,
        content_type: 'text/plain',
        content_created_at: '2026-09-12T12:00:00Z',
      },
    },
  ],
})

/**
 * The service for one case: the shelf holds one published profile, and the
 * archive write answers whatever the case hands it.
 */
const installService = (archive: () => Promise<Response>) => {
  const fetchMock = vi.fn((input: string, init?: RequestInit) => {
    if (input === '/v1/profile-shelf/active')
      return Promise.resolve(json({ profiles: [profile()], next: '' }))
    if (input === '/v1/lists')
      return Promise.resolve(
        json({ lists: [], list_details: [], categories: [] }),
      )
    if (input === '/v1/targets') return Promise.resolve(json({ targets: [] }))
    if (input === '/v1/deployments/targets')
      return Promise.resolve(json({ targets: [] }))
    if (input === '/v1/export-formats')
      return Promise.resolve(
        json({
          formats: [
            {
              id: 'fixture',
              renderer_id: 'fixture-renderer',
              file_extension: 'txt',
              content_type: 'text/plain',
            },
          ],
        }),
      )
    if (input === `/v1/artifacts/${ARTIFACT_ID}`)
      return Promise.resolve(new Response('verified bytes'))
    if (input === EXPORT && init?.method === 'POST')
      return Promise.resolve(json({ error: 'controlled refusal' }, 503))
    if (input === ARCHIVE && init?.method === 'POST') return archive()
    return Promise.reject(new Error(`unexpected request ${input}`))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

const renderShelf = () =>
  render(ProfilesView, {
    global: {
      stubs: {
        NuxtLink: { props: ['to'], template: '<a :href="to"><slot /></a>' },
      },
    },
  })

describe('ProfilesView row outcomes', () => {
  beforeEach(() => {
    invalidateCatalogCache()
    useLocale().setLocale('en')
    vi.stubGlobal('useRoute', () => ({ hash: '' }))
    vi.stubGlobal('useRouter', () => ({ push: vi.fn(), replace: vi.fn() }))
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText: vi.fn(async () => {}) },
    })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    if (ORIGINAL_CLIPBOARD === undefined)
      Reflect.deleteProperty(navigator, 'clipboard')
    else Object.defineProperty(navigator, 'clipboard', ORIGINAL_CLIPBOARD)
  })

  // The status line says what the last action on the shelf did. An earlier
  // success must not stand in for a later failure.
  it('reports a failed download after an earlier copy succeeded', async () => {
    installService(() => Promise.resolve(json({ error: 'unused' }, 500)))
    const screen = await renderShelf()
    const actions = screen.getByRole('button', { name: MENU })

    await actions.click()
    await screen
      .getByRole('menuitem', { name: 'Copy contents · Keenetic' })
      .click()
    await expect.element(screen.getByText('Contents copied')).toBeVisible()

    await actions.click()
    await screen.getByRole('menuitem', { name: 'Export to file' }).click()
    await screen.getByRole('menuitem', { name: 'TXT' }).click()

    await expect
      .element(screen.getByText('The file was not prepared'))
      .toBeVisible()
    await expect
      .element(screen.getByText('Contents copied'))
      .not.toBeInTheDocument()
  })

  it('withholds archiving while one is in flight and says when it was refused', async () => {
    const held = Promise.withResolvers<Response>()
    const fetchMock = installService(() => held.promise)
    const screen = await renderShelf()
    const actions = screen.getByRole('button', { name: MENU })

    await actions.click()
    await screen.getByRole('menuitem', { name: 'Archive' }).click()
    await actions.click()
    await expect
      .element(screen.getByRole('menuitem', { name: 'Archive' }))
      .toHaveAttribute('aria-disabled', 'true')
    await userEvent.keyboard('{Escape}')

    held.resolve(json({ error: 'controlled refusal' }, 503))
    await expect
      .element(screen.getByText('Profile Newest was not archived. Try again.'))
      .toBeVisible()
    // The refused profile stays on the shelf, ready for the retry it was
    // offered.
    await expect
      .element(screen.getByRole('link', { name: 'Newest' }))
      .toBeVisible()
    await actions.click()
    await expect
      .element(screen.getByRole('menuitem', { name: 'Archive' }))
      .not.toHaveAttribute('aria-disabled', 'true')
    expect(
      fetchMock.mock.calls.filter(([input]) => input === ARCHIVE),
    ).toHaveLength(1)
  })
})
