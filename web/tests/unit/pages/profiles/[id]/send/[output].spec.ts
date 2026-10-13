import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-vue'
import { ref } from 'vue'

import { useLocale } from '@/shared/i18n/useLocale'

import SendPage from '@/pages/profiles/[id]/send/[output].vue'

const PROFILE_LINK = '/profiles/profile-1'

const pageModel = () => ({
  catalog: ref(null),
  initialize: vi.fn(async () => {}),
  profile: ref({ name: 'Example profile' }),
  outputs: ref([
    { id: 'first', latest: { id: 'artifact-1' }, targetID: 'keenetic' },
    { id: 'chosen', latest: { id: 'artifact-2' }, targetID: 'keenetic' },
  ] as { id: string; latest: { id: string } | null; targetID: string }[]),
  outputTitle: () => 'Keenetic',
  selectOutput: vi.fn(),
  state: ref('failed'),
})

// `vi.mock` is hoisted above every case, so its factory needs a binding that
// already exists when the page module is imported. The holder is fixed; the
// model inside it is built fresh for each case.
const HOST: { model: ReturnType<typeof pageModel> } = { model: pageModel() }

vi.mock('@/features/view-profile/model/useProfileView', () => ({
  useProfileView: () => HOST.model,
}))

const PANEL = {
  props: ['artifactId'],
  template: '<section><h1>Send the file</h1><p>{{ artifactId }}</p></section>',
}

const renderPage = (panel: object = PANEL) =>
  render(SendPage, {
    global: {
      stubs: {
        AppShell: { template: '<main><slot /></main>' },
        NuxtLink: { props: ['to'], template: '<a :href="to"><slot /></a>' },
        SendPanel: panel,
      },
    },
  })

beforeEach(() => {
  HOST.model = pageModel()
  useLocale().setLocale('en')
  vi.stubGlobal('useRoute', () => ({
    params: { id: 'profile-1', output: 'chosen' },
  }))
  vi.stubGlobal('useHead', vi.fn())
})

afterEach(() => vi.unstubAllGlobals())

describe('send entry read states', () => {
  it('keeps a failed read distinct from a missing profile and retries the requested connection', async () => {
    const model = HOST.model
    const screen = await renderPage()

    await expect
      .element(screen.getByText('Profile is unavailable'))
      .toBeVisible()
    await expect
      .element(screen.getByText('Profile not found'))
      .not.toBeInTheDocument()
    // One page, one first-level heading, in every state below as well.
    expect(screen.getByRole('heading', { level: 1 }).all()).toHaveLength(1)

    model.initialize.mockImplementation(async () => {
      model.state.value = 'ready'
    })
    await screen.getByRole('button').click()

    await expect.element(screen.getByText('artifact-2')).toBeVisible()
    expect(model.initialize).toHaveBeenCalledTimes(2)
    expect(model.selectOutput).toHaveBeenLastCalledWith('chosen')
    expect(screen.getByRole('heading', { level: 1 }).all()).toHaveLength(1)
  })

  it.each([
    ['loading', 'Loading the profile'],
    ['missing', 'Profile not found'],
  ])(
    'keeps a heading and the correct %s state without offering delivery',
    async (state, message) => {
      HOST.model.state.value = state
      const screen = await renderPage()

      await expect
        .element(screen.getByText(message, { exact: false }))
        .toBeVisible()
      expect(screen.getByRole('heading', { level: 1 }).all()).toHaveLength(1)
      await expect
        .element(screen.getByText('artifact-2'))
        .not.toBeInTheDocument()
      expect(screen.getByRole('button').all()).toHaveLength(0)
    },
  )

  it('names a missing format, not a missing profile, and returns to that profile by the trail', async () => {
    HOST.model.state.value = 'ready'
    HOST.model.outputs.value = []
    const screen = await renderPage()

    await expect.element(screen.getByText('Format not found')).toBeVisible()
    await expect
      .element(screen.getByText('Profile not found'))
      .not.toBeInTheDocument()
    // The way back is the trail above the title, not a second button below.
    const trail = screen.getByRole('navigation', { name: 'Path' })
    await expect
      .element(trail.getByRole('link', { name: 'Example profile' }))
      .toHaveAttribute('href', PROFILE_LINK)
    expect(screen.getByRole('button').all()).toHaveLength(0)
    expect(screen.getByRole('heading', { level: 1 }).all()).toHaveLength(1)
  })

  it('keeps a format without a file distinct from a missing format', async () => {
    HOST.model.state.value = 'ready'
    HOST.model.outputs.value[1]!.latest = null
    const screen = await renderPage()

    await expect
      .element(screen.getByRole('heading', { level: 1 }))
      .toHaveTextContent('Send · Keenetic')
    await expect
      .element(screen.getByRole('link', { name: 'Example profile' }))
      .toHaveAttribute('href', PROFILE_LINK)
    expect(screen.getByRole('button').all()).toHaveLength(0)
    // Nothing is reported as absent: the connection exists, its file does not.
    await expect
      .element(screen.getByText('not found', { exact: false }))
      .not.toBeInTheDocument()
    await expect.element(screen.getByText('artifact-2')).not.toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 1 }).all()).toHaveLength(1)
  })

  it('puts the trail above the title and hands the panel the format it sends', async () => {
    HOST.model.state.value = 'ready'
    const screen = await renderPage({
      props: ['artifactId', 'breadcrumbs', 'heading'],
      template: `<section>
        <nav aria-label="Path">
          <a v-for="crumb in breadcrumbs" :key="crumb.to" :href="crumb.to">{{ crumb.label }}</a>
        </nav>
        <h1>{{ heading }}</h1><p>{{ artifactId }}</p>
      </section>`,
    })

    const trail = screen.getByRole('navigation', { name: 'Path' })
    expect(
      trail
        .getByRole('link')
        .all()
        .map((link) => [
          link.element().textContent,
          link.element().getAttribute('href'),
        ]),
    ).toEqual([
      ['Profiles', '/'],
      ['Example profile', PROFILE_LINK],
    ])
    await expect
      .element(screen.getByRole('heading', { level: 1 }))
      .toHaveTextContent('Send · Keenetic')
  })
})
