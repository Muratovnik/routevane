import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-vue'

import { usePublishedProfile } from '@/entities/profile-build/model/publishedProfile'
import { invalidateCatalogCache } from '@/shared/api/catalog'
import { useLocale } from '@/shared/i18n/useLocale'

import ProfileView from '@/features/view-profile/ui/ProfileView.vue'

const TIMESTAMP = '2026-09-03T09:00:00Z'
const LOADING = 'Loading the profile…'
const NO_ANSWER =
  'The local service did not answer. Check that Routevane is running.'
const ERROR_ANSWER =
  'The service received the request but answered with an error. Try again.'

/**
 * What the service answers for one case. Built per case rather than kept at
 * module scope, so no case can be read as the setup for the next one.
 */
type Scenario = {
  boundDevice: boolean
  diagnosticsStatus: number
  failedRebuild: boolean
  // What the failed attempt says: its code, and the lists it names, if any.
  failureCode: string
  failureLists: { id: string; title: string }[]
  fileStatus: number
  holdRoute: Promise<void> | undefined
  published: boolean
  routeStatus: number
  scheduleStatus: number
}

const defaultScenario = (): Scenario => ({
  boundDevice: false,
  diagnosticsStatus: 200,
  failedRebuild: false,
  failureCode: 'source_failed',
  failureLists: [],
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
      target_kind: 'router',
      device_id: scenario.boundDevice ? 'device-1' : '',
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
        ? {
            status: 'failed',
            code: scenario.failureCode,
            completed_at: TIMESTAMP,
            ...(scenario.failureLists.length > 0
              ? { uncovered_lists: scenario.failureLists }
              : {}),
          }
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
        return json({ targets: [] })
      case '/v1/deployments/targets':
        return json({
          targets: scenario.boundDevice
            ? [
                {
                  target_id: 'keenetic',
                  title: 'Keenetic',
                  deployer_id: 'keenetic',
                  requirements: {
                    address_label: 'Device address',
                    address_example: 'http://192.0.2.1',
                    needs_credential: true,
                    needs_interface: true,
                    interface_label: 'Interface for routes',
                  },
                },
              ]
            : [],
        })
      case '/v1/export-formats':
        return json({ formats: [] })
      case '/v1/devices':
        return json({
          devices: scenario.boundDevice
            ? [
                {
                  id: 'device-1',
                  target_id: 'keenetic',
                  target_title: 'Keenetic',
                  name: 'Home router',
                  address: 'http://192.0.2.1',
                  account: 'admin',
                  interface: 'Wireguard0',
                  auto_deliver: false,
                  deployable: true,
                },
              ]
            : [],
          secret_store_available: false,
        })
      case '/v1/outputs/output-1/device':
        // The write is confirmed and the stored output changes with it, so the
        // profile read that follows reports the device as detached.
        scenario.boundDevice = false
        return json({
          output: {
            id: 'output-1',
            list_id: 'profile-1',
            target_id: 'keenetic',
            device_id: '',
          },
        })
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
  // The screen asks whether this render is the document's first one, because
  // it only moves the keyboard to the title when the operator arrived here
  // from somewhere else. A mounted component is that later arrival.
  vi.stubGlobal('useNuxtApp', () => ({ isHydrating: false }))
  vi.stubGlobal('useRoute', () => ({ hash: '' }))
  vi.stubGlobal('useRouter', () => ({
    replace: vi.fn(async () => {}),
    push: vi.fn(async () => {}),
  }))
})

afterEach(() => vi.unstubAllGlobals())

describe('profile screen read boundaries', () => {
  it('carries the unavailable secret store through to Publishing without removing manual send', async () => {
    installFetch({ boundDevice: true })
    const screen = await renderRoute(true)

    await screen.getByRole('tab', { name: 'Publishing' }).click()
    const publishing = screen.getByRole('tabpanel', { name: 'Publishing' })
    // The choice is named by the connection; what it can do is stated once.
    await expect
      .element(publishing.getByLabelText('Connection for Keenetic'))
      .toHaveTextContent('Home router')
    await expect
      .element(publishing.getByLabelText('Connection for Keenetic'))
      .not.toHaveTextContent('·')
    await expect
      .element(
        publishing.getByText(
          'This system has no password store: manual sending only',
          { exact: true },
        ),
      )
      .toBeVisible()
    await expect
      .element(publishing.getByRole('link', { name: 'Send', exact: true }))
      .toHaveAttribute('href', '/profiles/profile-1/send/output-1')
    await expect
      .element(publishing.getByText('Automatic delivery is off'))
      .not.toBeInTheDocument()
  })

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

  it('tells an error answer from a lost request when the read fails', async () => {
    installFetch({ routeStatus: 500 })
    const answered = await renderRoute()
    await expect.element(answered.getByText(ERROR_ANSWER)).toBeVisible()
    await expect.element(answered.getByText(NO_ANSWER)).not.toBeInTheDocument()
    answered.unmount()

    // The same read, lost on the way: nothing answered, so the service is the
    // thing to check.
    const lost = Promise.withResolvers<undefined>()
    installFetch({ holdRoute: lost.promise })
    const silent = await renderRoute()
    lost.reject(new TypeError('Failed to fetch'))
    await expect.element(silent.getByText(NO_ANSWER)).toBeVisible()
    await expect.element(silent.getByText(ERROR_ANSWER)).not.toBeInTheDocument()
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

  // A build refused for incomplete coverage names each list it could not
  // cover, as a link to that list in the library, so the operator can go to
  // the list that needs its sources looked at.
  it.each([
    ['en', 'A required part of these lists has no rules in this format:'],
    ['ru', 'Обязательная часть этих списков не покрыта правилами формата:'],
  ] as const)(
    'links the uncovered lists to the library in %s',
    async (language, lead) => {
      useLocale().setLocale(language)
      installFetch({
        failedRebuild: true,
        failureCode: 'partial_coverage',
        failureLists: [
          { id: 'delta', title: 'Дельта' },
          { id: 'custom-0102030405060708', title: 'Мой список' },
        ],
      })
      const screen = await renderRoute()

      await expect.element(screen.getByText(lead)).toBeVisible()
      await expect
        .element(screen.getByRole('link', { name: 'Дельта' }))
        .toHaveAttribute('href', '/lists#list=delta')
      await expect
        .element(screen.getByRole('link', { name: 'Мой список' }))
        .toHaveAttribute('href', '/lists#list=custom-0102030405060708')
    },
  )

  // An attempt recorded before the service named the lists, or a coverage
  // failure that carries none, still says what it can without inventing lists.
  it('keeps the sentence alone when a coverage failure names no list', async () => {
    installFetch({ failedRebuild: true, failureCode: 'partial_coverage' })
    const screen = await renderRoute()

    await expect
      .element(
        screen.getByText(
          'A required part of one of the lists is not covered by rules in this format. Refresh the list sources and rebuild.',
        ),
      )
      .toBeVisible()
    await expect
      .element(
        screen.getByText('A required part of these lists has no rules', {
          exact: false,
        }),
      )
      .not.toBeInTheDocument()
  })

  it('lists no uncovered list under another failure', async () => {
    installFetch({
      failedRebuild: true,
      failureCode: 'source_failed',
      failureLists: [{ id: 'delta', title: 'Дельта' }],
    })
    const screen = await renderRoute()

    await expect.element(screen.getByText('Format not updated')).toBeVisible()
    await expect
      .element(screen.getByRole('link', { name: 'Дельта' }))
      .not.toBeInTheDocument()
  })
})

it('keeps the saved schedule on refusal and retries the requested interval', async () => {
  const { scenario, fetchMock } = installFetch()
  const screen = await renderRoute(true)
  await screen.getByRole('tab', { name: 'Publishing' }).click()
  const schedule = screen.getByRole('combobox', {
    name: 'Profile refresh',
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

// A write on one facet re-reads the profile. The page that asked for it stays
// mounted through that read: the facets it is not showing hold the operator's
// unsaved work, and a skeleton in their place would discard it (docs/UI.md,
// layout stability).
describe('profile re-read after a write', () => {
  const renderWorkspace = () =>
    render(ProfileView, {
      props: { profileId: 'profile-1' },
      global: {
        stubs: {
          RvMenu: true,
          RvInfoTip: true,
          NuxtLink: { props: ['to'], template: '<a :href="to"><slot /></a>' },
        },
      },
    })

  // Leaving a format without a connection is the «None» choice of its own
  // connection field.
  const unbind = async (
    screen: Awaited<ReturnType<typeof renderWorkspace>>,
  ): Promise<void> => {
    await screen
      .getByRole('tabpanel', { name: 'Publishing' })
      .getByLabelText('Connection for Keenetic')
      .click()
    await screen.getByRole('option', { name: 'None', exact: true }).click()
  }

  const profileReads = (fetchMock: ReturnType<typeof vi.fn>): number =>
    fetchMock.mock.calls.filter(
      ([input]) => String(input) === '/v1/profiles/profile-1',
    ).length

  it('keeps an unsaved rename on Contents while a device change on Publishing re-reads the profile', async () => {
    const { fetchMock, scenario } = installFetch({ boundDevice: true })
    const screen = await renderWorkspace()
    const name = screen.getByLabelText('Name', { exact: true })
    await expect.element(name).toHaveValue('Known profile')
    await name.fill('Unsaved rename')

    await screen.getByRole('tab', { name: 'Publishing' }).click()
    const publishing = screen.getByRole('tabpanel', { name: 'Publishing' })
    const reread = Promise.withResolvers<undefined>()
    scenario.holdRoute = reread.promise
    await unbind(screen)

    await expect.poll(() => profileReads(fetchMock)).toBe(2)
    await expect
      .element(screen.getByRole('status', { name: LOADING }))
      .not.toBeInTheDocument()
    await expect
      .element(screen.getByRole('tab', { name: 'Publishing' }))
      .toHaveAttribute('aria-selected', 'true')

    reread.resolve(undefined)
    await expect
      .element(publishing.getByText('Home router'))
      .not.toBeInTheDocument()

    await screen.getByRole('tab', { name: 'Contents' }).click()
    await expect.element(name).toHaveValue('Unsaved rename')
    await expect
      .element(screen.getByRole('button', { name: 'Discard changes' }))
      .toBeVisible()
  })

  it('keeps the loaded profile and offers a retry when the re-read fails', async () => {
    const { fetchMock, scenario } = installFetch({ boundDevice: true })
    const screen = await renderWorkspace()
    await screen.getByRole('tab', { name: 'Publishing' }).click()
    const publishing = screen.getByRole('tabpanel', { name: 'Publishing' })

    scenario.routeStatus = 503
    await unbind(screen)

    await expect
      .element(screen.getByText('The page was not updated'))
      .toBeVisible()
    await expect
      .element(screen.getByRole('heading', { level: 1 }))
      .toHaveTextContent('Known profile')
    await expect.element(publishing).toBeVisible()
    await expect
      .element(screen.getByText('Profiles are unavailable'))
      .not.toBeInTheDocument()

    scenario.routeStatus = 200
    await screen.getByRole('button', { name: 'Retry', exact: true }).click()
    await expect
      .element(screen.getByText('The page was not updated'))
      .not.toBeInTheDocument()
    expect(profileReads(fetchMock)).toBe(3)
  })
})
