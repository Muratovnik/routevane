import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-vue'

import SettingsView from '@/features/settings/ui/SettingsView.vue'
import { useLocale } from '@/shared/i18n/useLocale'
import { useSurfacePreferences } from '@/shared/model/useSurfacePreferences'

const json = (payload: unknown, status = 200): Response =>
  new Response(JSON.stringify(payload), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })

const TARGETS = {
  targets: [
    {
      id: 'keenetic',
      title: 'Keenetic',
      kind: 'router',
      format_key: 'keenetic-bat-ipv4-v1',
      renderer_id: 'keenetic-route-bat',
      file_extension: 'bat',
      manual_installation_hint: 'Upload the file.',
    },
    {
      id: 'singbox',
      title: 'sing-box',
      kind: 'app',
      format_key: 'singbox-json-v1',
      renderer_id: 'singbox-json',
      file_extension: 'json',
      manual_installation_hint: 'Place the file.',
    },
  ],
}

const installFetch = (
  targets: () => Promise<Response> = () => Promise.resolve(json(TARGETS)),
): ReturnType<typeof vi.fn> => {
  const fetchMock = vi.fn((input: string) => {
    if (input === '/v1/settings')
      return Promise.resolve(json({ refresh_interval: 'off' }))
    if (input === '/health')
      return Promise.resolve(json({ status: 'ok', version: '0.2.7' }))
    if (input === '/v1/lists')
      return Promise.resolve(
        json({ lists: [], list_details: [], categories: [] }),
      )
    if (input === '/v1/targets') return targets()
    return Promise.reject(new Error(`unexpected request ${input}`))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

// The preference is module state read once from storage, so a case sets it
// through the one owner of it rather than by writing the key behind its back.
const showEveryFormat = (): void => {
  for (const { id } of TARGETS.targets)
    useSurfacePreferences().setTargetHidden(id, false)
}

describe('SettingsView formats', () => {
  beforeEach(() => {
    useLocale().setLocale('en')
    showEveryFormat()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    showEveryFormat()
  })

  it('offers one switch per catalog format under the words that say what it does', async () => {
    installFetch()
    const screen = await render(SettingsView)

    const group = screen.getByRole('group', {
      name: 'Offer when adding a format',
    })
    const keenetic = group.getByRole('switch', { name: 'Keenetic' })
    const singBox = group.getByRole('switch', { name: 'sing-box' })
    await expect.element(keenetic).toBeChecked()
    await expect.element(singBox).toBeChecked()

    await keenetic.click()
    await expect.element(keenetic).not.toBeChecked()
    await expect.element(singBox).toBeChecked()
    expect(window.localStorage.getItem('rv.hiddenTargets')).toBe('["keenetic"]')

    await keenetic.click()
    await expect.element(keenetic).toBeChecked()
    expect(window.localStorage.getItem('rv.hiddenTargets')).toBe('[]')
  })

  it('reads a format hidden earlier as hidden', async () => {
    useSurfacePreferences().setTargetHidden('singbox', true)
    installFetch()
    const screen = await render(SettingsView)
    await expect
      .element(screen.getByRole('switch', { name: 'sing-box' }))
      .not.toBeChecked()
    await expect
      .element(screen.getByRole('switch', { name: 'Keenetic' }))
      .toBeChecked()
  })

  it('states an unreadable catalog and retries it in place', async () => {
    let reads = 0
    installFetch(() => {
      reads += 1
      return Promise.resolve(
        reads === 1 ? json({ error: 'unavailable' }, 503) : json(TARGETS),
      )
    })
    const screen = await render(SettingsView)
    await expect
      .element(screen.getByText('The format catalog is unavailable'))
      .toBeVisible()
    await screen.getByRole('button', { name: 'Retry', exact: true }).click()
    await expect
      .element(screen.getByRole('switch', { name: 'Keenetic' }))
      .toBeVisible()
  })

  it('states the refresh schedule once, as a schedule', async () => {
    installFetch()
    const screen = await render(SettingsView)
    expect(
      screen.getByText('Source refresh', { exact: true }).elements(),
    ).toHaveLength(1)
    await expect
      .element(screen.getByRole('group', { name: 'Default' }))
      .toBeInTheDocument()
    await expect
      .element(
        screen.getByText(
          'The schedule for profiles that have none of their own.',
        ),
      )
      .toBeVisible()
  })
})
