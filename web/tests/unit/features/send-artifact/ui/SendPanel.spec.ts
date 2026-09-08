import { afterEach, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-vue'

import SendPanel from '@/features/send-artifact/ui/SendPanel.vue'
import { useLocale } from '@/shared/i18n/useLocale'

const json = (value: unknown) =>
  new Response(JSON.stringify(value), { status: 200 })
afterEach(() => {
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
      const body = JSON.parse(String(init?.body)) as { confirm: boolean }
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
