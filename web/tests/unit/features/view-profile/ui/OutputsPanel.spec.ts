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

const PUBLISHED = {
  ...OUTPUT,
  latest: {
    id: 'artifact-1',
    snapshotID: 'snapshot-1',
    sizeBytes: 10,
    contentType: 'text/plain',
    contentCreatedAt: '2026-09-03T09:00:00Z',
  },
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
  credentialStored: true,
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
    // The refresh rule is one field above the table, named by its own label.
    await expect
      .element(screen.getByRole('combobox', { name: 'Profile refresh' }))
      .toBeVisible()
  })

  it('states when automatic delivery is on but profile refresh is off', async () => {
    const screen = await renderPanel({
      devices: [CONNECTED_DEVICE],
      outputs: [{ ...OUTPUT, deviceID: 'device-1' }],
    })

    await expect
      .element(screen.getByText('Profile refresh is off', { exact: true }))
      .toBeVisible()
  })

  it.each([false, true])(
    'states a system without a password store as a fact, automatic delivery %s',
    async (autoDeliver) => {
      const screen = await renderPanel({
        automaticDelivery: () => 'unavailable',
        devices: [{ ...CONNECTED_DEVICE, autoDeliver }],
        outputs: [{ ...OUTPUT, deviceID: 'device-1' }],
        schedule: { ...SCHEDULE, effective: 'daily' },
      })

      // The choice is named by the connection alone; its state is the
      // option's note and the status line, never part of the name.
      await expect
        .element(screen.getByLabelText('Connection for Keenetic'))
        .toHaveTextContent('Home router')
      await expect
        .element(screen.getByLabelText('Connection for Keenetic'))
        .not.toHaveTextContent('·')
      await expect
        .element(
          screen.getByText(
            'This system has no password store: manual sending only',
            { exact: true },
          ),
        )
        .toBeVisible()
      await expect
        .element(screen.getByRole('link', { name: 'Send', exact: true }))
        .toHaveAttribute('href', '/profiles/profile-1/send/output-1')
      await expect
        .element(screen.getByRole('link', { name: 'Open the connection' }))
        .not.toBeInTheDocument()
    },
  )

  it.each([
    ['unknown', true, 'Automatic delivery unconfirmed'],
    ['available', false, 'Automatic delivery is off'],
  ] as const)(
    'states %s delivery once, with the way to that connection',
    async (capability, autoDeliver, status) => {
      const screen = await renderPanel({
        automaticDelivery: () => capability,
        devices: [{ ...CONNECTED_DEVICE, autoDeliver }],
        outputs: [{ ...OUTPUT, deviceID: 'device-1' }],
        schedule: { ...SCHEDULE, effective: 'daily' },
      })

      await expect
        .element(screen.getByLabelText('Connection for Keenetic'))
        .toHaveTextContent('Home router')
      await expect
        .element(screen.getByLabelText('Connection for Keenetic'))
        .not.toHaveTextContent('·')
      await expect
        .element(screen.getByText(status, { exact: true }))
        .toBeVisible()
      await expect
        .element(screen.getByRole('link', { name: 'Open the connection' }))
        .toHaveAttribute('href', '/connections#connection=device-1')
      await expect
        .element(screen.getByText('Ready: new versions', { exact: false }))
        .not.toBeInTheDocument()
    },
  )

  it('offers None as the first choice and unbinds through it', async () => {
    const screen = await renderPanel({
      devices: [{ ...CONNECTED_DEVICE, autoDeliver: false }],
      outputs: [{ ...OUTPUT, deviceID: 'device-1' }],
    })

    await expect
      .element(screen.getByRole('button', { name: 'Detach' }))
      .not.toBeInTheDocument()
    await screen.getByLabelText('Connection for Keenetic').click()
    const options = screen.getByRole('option').all()
    expect(
      options.map((option) => option.element().textContent?.trim()),
    ).toEqual([
      'None',
      expect.stringMatching(/^Home router\s*automatic delivery off$/),
    ])
    await screen.getByRole('option', { name: 'None' }).click()
    expect(screen.emitted('bindDevice')).toEqual([['output-1', '']])
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

  it('states the bound connection of an archived profile instead of offering a choice', async () => {
    const screen = await renderPanel({
      archived: true,
      devices: [CONNECTED_DEVICE],
      outputs: [{ ...PUBLISHED, deviceID: 'device-1' }],
    })

    await expect
      .element(screen.getByRole('cell', { name: 'Home router' }))
      .toBeVisible()
    await expect
      .element(screen.getByLabelText('Connection for Keenetic'))
      .not.toBeInTheDocument()
    await expect
      .element(screen.getByRole('combobox', { name: 'Profile refresh' }))
      .not.toBeInTheDocument()
    await expect
      .element(screen.getByRole('button', { name: 'Add a format' }))
      .not.toBeInTheDocument()
    await expect
      .element(
        screen.getByText('Automatic delivery configured', { exact: false }),
      )
      .not.toBeInTheDocument()
    // Reading and downloading stay; nothing that changes the format is offered.
    await expect
      .element(screen.getByRole('button', { name: 'Actions for Keenetic' }))
      .not.toBeInTheDocument()
  })
})

describe('OutputsPanel row actions', () => {
  beforeEach(() => useLocale().setLocale('en'))

  it('puts the extension in the format cell and every act on a format in its menu', async () => {
    const screen = await renderPanel({
      outputs: [
        {
          ...PUBLISHED,
          id: 'output-dns',
          targetID: 'keenetic-dns',
          targetTitle: 'Keenetic (domain-based)',
          fileExtension: 'txt',
        },
      ],
    })

    const format = screen
      .getByRole('cell', { name: /^Keenetic \(domain-based\)/ })
      .first()
    await expect.element(format).toBeVisible()
    expect(format.element().textContent).toMatch(
      /Keenetic \(domain-based\)\s*\.txt/,
    )
    expect(screen.getByRole('columnheader').all()).toHaveLength(4)
    await screen
      .getByRole('button', { name: 'Actions for Keenetic (domain-based)' })
      .click()
    expect(
      screen
        .getByRole('menuitem')
        .all()
        .map((item) => item.element().textContent?.trim()),
    ).toEqual([
      'Format settings…',
      'Rebuild',
      'Issue a new link…',
      'Remove format…',
    ])
    await screen.getByRole('menuitem', { name: 'Remove format…' }).click()
    expect(screen.emitted('remove')).toEqual([['output-dns']])
  })

  it('offers no new link for a format that never published', async () => {
    const screen = await renderPanel()

    await screen.getByRole('button', { name: 'Actions for Keenetic' }).click()
    expect(
      screen
        .getByRole('menuitem')
        .all()
        .map((item) => item.element().textContent?.trim()),
    ).toEqual(['Rebuild', 'Remove format…'])
  })
})
