import { beforeEach, describe, expect, it } from 'vitest'
import { render } from 'vitest-browser-vue'

import { useLocale } from '@/shared/i18n/useLocale'
import type { Schedule } from '@/shared/api/profiles'

import OutputsPanel from '@/features/view-profile/ui/OutputsPanel.vue'

const OUTPUT = {
  id: 'output-1',
  targetID: 'keenetic',
  deviceID: '',
  targetTitle: 'Keenetic',
  targetKind: 'router',
  fileExtension: 'bat',
  createdAt: '2026-09-03T09:00:00Z',
  latest: null,
  lastAttempt: null,
}

const SCHEDULE: Schedule = {
  interval: '',
  effective: 'off' as const,
  followsDefault: true,
  lastRefreshedAt: '',
  lastRefreshFailed: false,
  nextRefreshAt: '',
}

const CONNECTED_DEVICE = {
  id: 'device-1',
  targetID: 'keenetic',
  targetTitle: 'Keenetic',
  name: 'Home router',
  address: 'http://192.168.1.1',
  account: 'admin',
  interfaceName: 'Wireguard0',
  autoDeliver: true,
  deployable: true,
}

const renderPanel = (overrides: Record<string, unknown> = {}) =>
  render(OutputsPanel, {
    props: {
      archived: false,
      automaticDelivery: () => 'available' as const,
      busy: false,
      deployable: () => true,
      devices: [],
      profileId: 'profile-1',
      outputs: [OUTPUT],
      schedule: SCHEDULE,
      selectedId: '',
      targetGroups: [],
      targetTitle: (_id: string, fallback?: string) => fallback ?? 'Keenetic',
      ...overrides,
    },
    global: {
      stubs: {
        NuxtLink: { props: ['to'], template: '<a :href="to"><slot /></a>' },
      },
    },
  })

describe('OutputsPanel connection readiness', () => {
  beforeEach(() => useLocale().setLocale('en'))

  it('states the next unmet condition for an unbound output', async () => {
    const screen = await renderPanel()

    await expect
      .element(screen.getByText('Add a connection', { exact: false }).first())
      .toBeVisible()
    await expect
      .element(screen.getByText('Connection for Keenetic —', { exact: false }))
      .not.toBeInTheDocument()
    // The refresh choice is named by the section heading above it, which is how
    // a reader knows what the control changes.
    await expect
      .element(screen.getByRole('combobox', { name: 'Refresh' }))
      .toBeVisible()
  })

  it('states when automatic delivery is on but profile refresh is off', async () => {
    const screen = await renderPanel({
      devices: [CONNECTED_DEVICE],
      outputs: [{ ...OUTPUT, deviceID: 'device-1' }],
    })

    await expect
      .element(screen.getByText('Turn on profile refresh', { exact: false }))
      .toBeVisible()
  })

  it.each([false, true])(
    'offers a manual send when automatic delivery is unavailable, stored consent %s',
    async (autoDeliver) => {
      const screen = await renderPanel({
        automaticDelivery: () => 'unavailable',
        devices: [{ ...CONNECTED_DEVICE, autoDeliver }],
        outputs: [{ ...OUTPUT, deviceID: 'device-1' }],
        schedule: { ...SCHEDULE, effective: 'daily' },
      })

      await expect
        .element(screen.getByLabelText('Connection for Keenetic'))
        .toHaveTextContent('Home router · manual send only')
      await expect
        .element(
          screen.getByText(
            'Automatic delivery is unavailable on this system. Use Send with a password for each delivery.',
          ),
        )
        .toBeVisible()
      await expect
        .element(screen.getByRole('link', { name: 'Send', exact: true }))
        .toHaveAttribute('href', '/profiles/profile-1/send/output-1')
      await expect
        .element(screen.getByText('Turn on automatic delivery in Connections'))
        .not.toBeInTheDocument()
      await expect
        .element(screen.getByText('Ready: new versions', { exact: false }))
        .not.toBeInTheDocument()
    },
  )

  it('does not declare stored consent ready when requirements are unknown', async () => {
    const screen = await renderPanel({
      automaticDelivery: () => 'unknown',
      devices: [CONNECTED_DEVICE],
      outputs: [{ ...OUTPUT, deviceID: 'device-1' }],
      schedule: { ...SCHEDULE, effective: 'daily' },
    })

    await expect
      .element(screen.getByLabelText('Connection for Keenetic'))
      .toHaveTextContent('Home router · automatic delivery unconfirmed')
    await expect
      .element(
        screen.getByText(
          'Automatic delivery is unconfirmed. Check the connection in Connections.',
        ),
      )
      .toBeVisible()
    await expect
      .element(screen.getByText('Ready: new versions', { exact: false }))
      .not.toBeInTheDocument()
  })

  it('offers consent when automatic delivery is available but still off', async () => {
    const screen = await renderPanel({
      devices: [{ ...CONNECTED_DEVICE, autoDeliver: false }],
      outputs: [{ ...OUTPUT, deviceID: 'device-1' }],
    })

    await expect
      .element(screen.getByText('Turn on automatic delivery in Connections'))
      .toBeVisible()
  })

  it('reports a configured password-free local target without a router promise', async () => {
    const screen = await renderPanel({
      devices: [
        {
          ...CONNECTED_DEVICE,
          targetID: 'singbox',
          targetTitle: 'sing-box',
          address: '/data/routevane.json',
          account: '',
          interfaceName: '',
        },
      ],
      outputs: [
        {
          ...OUTPUT,
          deviceID: 'device-1',
          targetID: 'singbox',
          targetTitle: 'sing-box',
          targetKind: 'app',
        },
      ],
      schedule: { ...SCHEDULE, effective: 'daily' },
    })

    await expect
      .element(screen.getByText('Automatic delivery configured'))
      .toBeVisible()
    await expect
      .element(screen.getByText('Ready: new versions', { exact: false }))
      .not.toBeInTheDocument()
  })

  it('does not invent automatic readiness for a manual-only output', async () => {
    const screen = await renderPanel({ deployable: () => false })

    await expect
      .element(screen.getByText('Choose a connection', { exact: false }))
      .not.toBeInTheDocument()
    await expect
      .element(
        screen.getByText('Automatic delivery configured', { exact: false }),
      )
      .not.toBeInTheDocument()
    await expect
      .element(screen.getByText('Manual download', { exact: false }))
      .toBeVisible()
  })

  it('does not report readiness on an archived profile', async () => {
    const screen = await renderPanel({
      archived: true,
      devices: [CONNECTED_DEVICE],
      outputs: [{ ...OUTPUT, deviceID: 'device-1' }],
    })

    await expect
      .element(
        screen.getByText('Automatic delivery configured', { exact: false }),
      )
      .not.toBeInTheDocument()
    await expect
      .element(screen.getByText('Ready: new versions', { exact: false }))
      .not.toBeInTheDocument()
  })
})
