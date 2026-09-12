import { afterEach, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-vue'

import SendPanel from '@/features/send-artifact/ui/SendPanel.vue'
import { useLocale } from '@/shared/i18n/useLocale'

const json = (value: unknown) =>
  new Response(JSON.stringify(value), { status: 200 })
afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  window.sessionStorage.clear()
})

it.each([
  ['en', 'Managed object ownership', 'Device operation'],
  ['ru', 'Учёт управляемых объектов', 'Операция с устройством'],
] as const)(
  'previews exact DNS changes at IPv6 and translates the ownership trail in %s',
  async (language, ownership, fallback) => {
    const { setLocale, t } = useLocale()
    setLocale(language)
    let previews = 0
    const fetchMock = vi.fn(async (input: unknown, init?: RequestInit) => {
      if (String(input) === '/v1/deployments/targets')
        return json({
          targets: [
            {
              target_id: 'keenetic-dns',
              title: 'Keenetic DNS',
              deployer_id: 'keenetic-dns',
              requirements: {
                address_label: 'Router address',
                address_example: 'http://[fd00::1]',
                needs_credential: false,
                needs_interface: false,
              },
            },
          ],
        })
      const body = JSON.parse(String(init?.body)) as {
        attempt_id?: string
        confirm: boolean
      }
      if (!body.confirm) {
        previews += 1
        return json({
          plan: {
            artifact_id: 'artifact',
            artifact_hash: 'fixture-hash',
            target_id: 'keenetic-dns',
            title: 'Keenetic DNS',
            deployer_id: 'keenetic-dns',
            size_bytes: 12,
            fqdn_changes:
              previews === 1
                ? [
                    'no ip hotspot profile owned-old',
                    'ip hotspot profile owned-new',
                  ]
                : [],
          },
        })
      }
      return json({
        attempt_id: body.attempt_id ?? '',
        artifact_id: 'artifact',
        status: 'succeeded',
        result: {
          applied: true,
          rolled_back: false,
          device: {},
          events: [
            { step: 'ownership', outcome: 'success' },
            { step: 'future-step', outcome: 'success' },
          ],
        },
      })
    })
    vi.stubGlobal('fetch', fetchMock)
    const screen = await render(SendPanel, {
      props: {
        artifactId: 'artifact',
        profileId: 'profile',
        profileName: 'Profile',
        targetId: 'keenetic-dns',
        target: null,
      },
    })
    const address = screen.getByRole('textbox', {
      name: t('deploy.field.address.keenetic-dns'),
    })
    await address.fill('http://[fd00::1]:8080')
    await screen
      .getByRole('button', { name: t('send.review'), exact: true })
      .click()
    await expect
      .element(
        screen.getByText('no ip hotspot profile owned-old', { exact: false }),
      )
      .toBeVisible()
    await address.fill('http://[fd00::2]:8080')
    await expect
      .element(
        screen.getByRole('button', { name: t('send.apply'), exact: true }),
      )
      .not.toBeInTheDocument()
    await screen
      .getByRole('button', { name: t('send.review'), exact: true })
      .click()
    await expect
      .element(screen.getByText(t('send.plan.fqdn.unchanged')))
      .toBeVisible()
    await address.fill('http://[fd00::3]:8080')
    await expect
      .element(
        screen.getByRole('button', { name: t('send.apply'), exact: true }),
      )
      .not.toBeInTheDocument()
    await screen
      .getByRole('button', { name: t('send.review'), exact: true })
      .click()
    await screen
      .getByRole('button', { name: t('send.apply'), exact: true })
      .click()
    await expect
      .element(screen.getByText(ownership, { exact: true }))
      .toBeVisible()
    await expect
      .element(screen.getByText(fallback, { exact: true }))
      .toBeVisible()
    await expect
      .element(screen.getByText('send.step.ownership'))
      .not.toBeInTheDocument()
    expect(
      fetchMock.mock.calls.filter(([input]) =>
        String(input).endsWith('/deploy'),
      ),
    ).toHaveLength(4)
  },
)

it.each(['dropped', 'mismatched'] as const)(
  'recovers a %s apply response without repeating the device write',
  async (responseMode) => {
    const { t } = useLocale()
    let attemptID = ''
    let statusReads = 0
    let applyCalls = 0
    const outcome = (status: 'outcome_unknown' | 'succeeded') => ({
      attempt_id: attemptID,
      artifact_id: 'artifact',
      status,
      result: {
        applied: status === 'succeeded',
        rolled_back: false,
        device: {},
        events: [],
      },
    })
    const fetchMock = vi.fn(async (input: unknown, init?: RequestInit) => {
      if (String(input) === '/v1/deployments/targets')
        return json({
          targets: [
            {
              target_id: 'keenetic-dns',
              title: 'Keenetic DNS',
              deployer_id: 'keenetic-dns',
              requirements: {
                address_label: 'Router address',
                address_example: 'http://[fd00::1]',
                needs_credential: false,
                needs_interface: false,
              },
            },
          ],
        })
      if (String(input).startsWith('/v1/deployment-attempts/')) {
        statusReads += 1
        return responseMode === 'dropped' && statusReads === 1
          ? new Response(JSON.stringify({ error: 'not found' }), {
              status: 404,
            })
          : json(outcome('succeeded'))
      }
      const body = JSON.parse(String(init?.body)) as {
        attempt_id?: string
        confirm: boolean
      }
      if (!body.confirm)
        return json({
          plan: {
            artifact_id: 'artifact',
            artifact_hash: 'd'.repeat(64),
            target_id: 'keenetic-dns',
            title: 'Keenetic DNS',
            deployer_id: 'keenetic-dns',
            size_bytes: 12,
          },
        })
      applyCalls += 1
      attemptID = body.attempt_id ?? ''
      if (responseMode === 'mismatched')
        return json({ ...outcome('succeeded'), attempt_id: 'c'.repeat(32) })
      throw new TypeError('response was lost')
    })
    vi.stubGlobal('fetch', fetchMock)
    const props = {
      artifactId: 'artifact',
      profileId: 'profile',
      profileName: 'Profile',
      targetId: 'keenetic-dns',
      target: null,
    }
    const screen = await render(SendPanel, { props })
    await screen
      .getByRole('textbox', { name: t('deploy.field.address.keenetic-dns') })
      .fill('http://[fd00::1]')
    await screen.getByRole('button', { name: t('send.review') }).click()
    await screen.getByRole('button', { name: t('send.apply') }).click()
    await expect
      .element(screen.getByText(t('send.outcomeUnknown.title')))
      .toBeVisible()
    await expect
      .element(screen.getByRole('button', { name: t('send.apply') }))
      .not.toBeInTheDocument()
    expect(applyCalls).toBe(1)
    expect(attemptID).toMatch(/^[a-f0-9]{32}$/)

    // The in-memory identity remains authoritative when tab storage is absent.
    window.sessionStorage.removeItem(`rv.deploy.attempt.artifact`)
    await screen
      .getByRole('button', { name: t('send.outcomeUnknown.check') })
      .click()
    await expect.element(screen.getByText(t('send.applied'))).toBeVisible()
    expect(applyCalls).toBe(1)

    window.sessionStorage.setItem(`rv.deploy.attempt.artifact`, attemptID)
    await screen.unmount()
    const reloaded = await render(SendPanel, { props })
    await expect.element(reloaded.getByText(t('send.applied'))).toBeVisible()
    expect(applyCalls).toBe(1)
    expect(
      [...Array(window.sessionStorage.length).keys()]
        .map((index) =>
          window.sessionStorage.getItem(window.sessionStorage.key(index) ?? ''),
        )
        .filter(Boolean),
    ).not.toContain('response was lost')
  },
)
