import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-vue'

import { usePublishedProfile } from '@/entities/profile-build/model/publishedProfile'
import { invalidateCatalogCache } from '@/shared/api/catalog'
import { useLocale } from '@/shared/i18n/useLocale'

import ProfileView from '@/features/view-profile/ui/ProfileView.vue'

const TIMESTAMP = '2026-09-03T09:00:00Z'
const LOADING = 'Loading the profile…'

/**
 * What the service answers for one case. Built per case rather than kept at
 * module scope, so no case can be read as the setup for the next one.
 */
type Scenario = {
  diagnosticsStatus: number
  failedRebuild: boolean
  fileStatus: number
  holdRoute: Promise<void> | undefined
  published: boolean
  routeStatus: number
  scheduleStatus: number
}

const defaultScenario = (): Scenario => ({
  diagnosticsStatus: 200,
  failedRebuild: false,
  fileStatus: 200,
  holdRoute: undefined,
  published: true,
  routeStatus: 200,
  scheduleStatus: 503,
})

const json = (payload: unknown, status = 200) =>
  new Response(JSON.stringify(payload), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })

const routePayload = (scenario: Scenario) => ({
  profile: {
    id: 'profile-1',
    name: 'Known profile',
    lists: [],
    categories: [],
    exclusions: [],
    created_at: TIMESTAMP,
    updated_at: TIMESTAMP,
  },
  outputs: [
    {
      id: 'output-1',
      target_id: 'keenetic',
      target_title: 'Keenetic',
      created_at: TIMESTAMP,
      latest: scenario.published
        ? {
            id: 'artifact-1',
            snapshot_id: 'snapshot-1',
            size_bytes: 14,
            content_type: 'text/plain',
            content_created_at: TIMESTAMP,
          }
        : null,
      last_attempt: scenario.failedRebuild
        ? { status: 'failed', code: 'source_failed', completed_at: TIMESTAMP }
        : null,
    },
  ],
  resolved: [],
  missing_categories: [],
  schedule: { interval: '', effective: 'off', follows_default: true },
})

const installFetch = (overrides: Partial<Scenario> = {}) => {
  const scenario: Scenario = { ...defaultScenario(), ...overrides }
  const fetchMock = vi.fn(async (input: unknown) => {
    switch (String(input)) {
      case '/v1/profiles/profile-1/schedule':
        return json(
          scenario.scheduleStatus === 200
            ? {
                schedule: {
                  interval: 'daily',
                  effective: 'daily',
                  follows_default: false,
                },
              }
            : { error: 'controlled refusal' },
          scenario.scheduleStatus,
        )
      case '/v1/profiles/profile-1':
        await scenario.holdRoute
        return json(
          scenario.routeStatus === 200
            ? routePayload(scenario)
            : { error: 'controlled refusal' },
          scenario.routeStatus,
        )
      case '/v1/lists':
        return json({ lists: [], list_details: [], categories: [] })
      case '/v1/targets':
      case '/v1/deployments/targets':
        return json({ targets: [] })
      case '/v1/export-formats':
        return json({ formats: [] })
      case '/v1/devices':
        return json({ devices: [], secret_store_available: false })
      case '/v1/artifacts/artifact-1':
        return new Response('verified bytes', { status: scenario.fileStatus })
      case '/v1/snapshots/snapshot-1':
        return json(
          { routing_plan: { rules: [], excluded: [] } },
          scenario.diagnosticsStatus,
        )
      default:
        throw new Error(`Unexpected request ${String(input)}`)
    }
  })
  vi.stubGlobal('fetch', fetchMock)
  return { fetchMock, scenario }
}

const renderRoute = (realOutputs = false) =>
  render(ProfileView, {
    props: { profileId: 'profile-1' },
    global: {
      stubs: {
        ProfileEditor: true,
        OutputsPanel: !realOutputs,
        RvMenu: true,
        RvInfoTip: true,
        RvSelect: !realOutputs,
        NuxtLink: { props: ['to'], template: '<a :href="to"><slot /></a>' },
      },
    },
  })

beforeEach(() => {
  invalidateCatalogCache()
  usePublishedProfile().clear()
  useLocale().setLocale('en')
  vi.stubGlobal('useRoute', () => ({ hash: '' }))
  vi.stubGlobal('useRouter', () => ({
    replace: vi.fn(async () => {}),
    push: vi.fn(async () => {}),
  }))
})

afterEach(() => vi.unstubAllGlobals())

describe('profile screen read boundaries', () => {
  it('shows loading, then a failed read with retry, then the recovered profile', async () => {
    const held = Promise.withResolvers<undefined>()
    const { scenario } = installFetch({
      holdRoute: held.promise,
      routeStatus: 503,
    })
    const screen = await renderRoute()

    const loading = screen.getByRole('status', { name: LOADING })
    await expect.element(loading).toBeVisible()
    await expect.element(loading).toHaveAttribute('aria-busy', 'true')
    // The skeleton stands in place of a notice: nothing is reported about the
    // profile until the read has actually answered.
    expect(screen.getByRole('status').all()).toHaveLength(1)

    held.resolve(undefined)

    await expect
      .element(screen.getByText('Profiles are unavailable'))
      .toBeVisible()
    await expect
      .element(screen.getByText('Profile not found'))
      .not.toBeInTheDocument()

    scenario.routeStatus = 200
    await screen.getByRole('button').click()

    await expect
      .element(screen.getByRole('heading', { level: 1 }))
      .toHaveTextContent('Known profile')
    await expect
      .element(screen.getByText('Profiles are unavailable'))
      .not.toBeInTheDocument()
  })

  it('renders a true 404 without advertising a retry or editable profile', async () => {
    installFetch({ routeStatus: 404 })
    const screen = await renderRoute()

    await expect.element(screen.getByText('Profile not found')).toBeVisible()
    // A profile that does not exist offers neither a retry nor anything to edit.
    expect(screen.getByRole('button').all()).toHaveLength(0)
    await expect.element(screen.getByRole('tablist')).not.toBeInTheDocument()
  })

  it('keeps file and diagnostics failures distinct from an unpublished profile', async () => {
    installFetch({ diagnosticsStatus: 503, fileStatus: 503 })
    const screen = await renderRoute()

    await screen.getByRole('tab', { name: 'File' }).click()
    const filePanel = screen.getByRole('tabpanel', { name: 'File' })
    await expect.element(filePanel).toMatchTextContent('The file was not read')
    await expect.element(filePanel).not.toMatchTextContent('no published file')

    await screen.getByRole('tab', { name: 'Diagnostics' }).click()
    await expect
      .element(screen.getByRole('tabpanel', { name: 'Diagnostics' }))
      .toMatchTextContent('Diagnostics unavailable')
    await expect
      .element(screen.getByRole('heading', { level: 1 }))
      .toHaveTextContent('Known profile')
  })

  it('states that an unpublished connection has no file or diagnostics without fetching them', async () => {
    const { fetchMock } = installFetch({ published: false })
    const screen = await renderRoute()

    for (const tab of ['File', 'Diagnostics']) {
      await screen.getByRole('tab', { name: tab }).click()
      await expect
        .element(screen.getByRole('tabpanel', { name: tab }))
        .toMatchTextContent('This profile has no published file.')
    }
    expect(
      fetchMock.mock.calls.some(
        ([url]) =>
          String(url).startsWith('/v1/artifacts/') ||
          String(url).startsWith('/v1/snapshots/'),
      ),
    ).toBe(false)
  })

  it('labels a retained verified file as stale after a failed rebuild and can still read it', async () => {
    installFetch({ failedRebuild: true })
    const screen = await renderRoute()

    await expect
      .element(screen.getByText('Showing the previous verified file'))
      .toBeVisible()

    await screen.getByRole('tab', { name: 'File' }).click()
    await expect
      .element(screen.getByRole('tabpanel', { name: 'File' }))
      .toMatchTextContent('verified bytes')

    await screen.getByRole('tab', { name: 'Diagnostics' }).click()
    await expect
      .element(screen.getByRole('tabpanel', { name: 'Diagnostics' }))
      .toMatchTextContent('No exclusions: every rule entered the file.')
  })
})

it('keeps the saved schedule on refusal and retries the requested interval', async () => {
  const { scenario, fetchMock } = installFetch()
  const screen = await renderRoute(true)
  await screen.getByRole('tab', { name: 'Publishing' }).click()
  const schedule = screen.getByRole('combobox', {
    name: 'Refresh',
    exact: true,
  })
  await schedule.click()
  await screen.getByRole('option', { name: 'Daily', exact: true }).click()
  await expect
    .element(screen.getByText('The schedule was not changed'))
    .toBeVisible()
  await expect.element(schedule).toHaveTextContent('As in settings')
  scenario.scheduleStatus = 200
  await screen.getByRole('button', { name: 'Retry', exact: true }).click()
  await expect.element(schedule).toHaveTextContent('Daily')
  await expect
    .element(screen.getByText('The schedule was not changed'))
    .not.toBeInTheDocument()
  expect(
    fetchMock.mock.calls.filter(([input]) =>
      String(input).endsWith('/schedule'),
    ),
  ).toHaveLength(2)
})
