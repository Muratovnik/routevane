import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { page } from 'vitest/browser'
import { render } from 'vitest-browser-vue'
import { defineComponent, h } from 'vue'

import { useLocale } from '@/shared/i18n/useLocale'
import type { OutputCard } from '@/shared/api/outputs'
import type { Schedule } from '@/shared/api/profiles'

import OutputsPanel from '@/features/view-profile/ui/OutputsPanel.vue'

const OUTPUT: OutputCard = {
  id: 'output-1',
  targetID: 'keenetic',
  deviceID: '',
  keepAddresses: false,
  keepAddressesAvailable: false,
  targetTitle: 'Keenetic',
  targetKind: 'router',
  fileExtension: 'bat',
  createdAt: '2026-09-03T09:00:00Z',
  latest: null,
  lastAttempt: null,
  lastDelivery: null,
  deviceName: '',
  latestDelivery: null,
  latestFailure: null,
}

const PUBLISHED = {
  ...OUTPUT,
  latest: {
    id: 'artifact-1',
    snapshotID: 'snapshot-1',
    sizeBytes: 10,
    contentType: 'text/plain',
    contentCreatedAt: '2026-09-03T09:00:00Z',
    builtAt: '2026-09-03T09:00:00Z',
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
  address: 'http://10.255.0.1',
  account: 'admin',
  interfaceName: 'Wireguard0',
  autoDeliver: true,
  credentialStored: true,
  deployable: true,
}

// A router link that is only its address: the menu resolves the link by name,
// where a template string would need the runtime compiler.
const LINK = defineComponent({
  props: { to: { type: String, default: '' } },
  setup:
    (props, { slots }) =>
    () =>
      h('a', { href: props.to }, slots.default?.()),
})

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
      components: { NuxtLink: LINK },
    },
  })

describe('OutputsPanel connection cell', () => {
  beforeEach(() => useLocale().setLocale('en'))

  it('keeps the refresh rule above the table and the connection field in the row', async () => {
    const screen = await renderPanel({
      devices: [{ ...CONNECTED_DEVICE, autoDeliver: false }],
    })

    await expect
      .element(screen.getByRole('combobox', { name: 'Source refresh' }))
      .toBeVisible()
    await expect
      .element(screen.getByLabelText('Connection for Keenetic'))
      .toHaveTextContent('None')
  })

  // The cell is the choice and nothing else: the state of a connection is the
  // option's note, and the connection page owns it.
  it.each([
    [
      'unbound, a connection of its format sends automatically',
      '',
      'available',
      true,
    ],
    ['bound, delivery is off', 'device-1', 'available', false],
    ['bound, delivery is unconfirmed', 'device-1', 'unknown', true],
    [
      'bound, the system has no password store',
      'device-1',
      'unavailable',
      true,
    ],
  ] as const)(
    'states no delivery status in the cell when the row is %s',
    async (_case, deviceID, capability, autoDeliver) => {
      const screen = await renderPanel({
        automaticDelivery: () => capability,
        devices: [{ ...CONNECTED_DEVICE, autoDeliver }],
        outputs: [{ ...OUTPUT, deviceID }],
        schedule: { ...SCHEDULE, effective: 'daily' },
      })

      const cell = screen.getByRole('cell', { name: /Home router|None/ })
      await expect.element(cell).toBeVisible()
      expect(cell.element().querySelectorAll('a')).toHaveLength(0)
      expect(cell.element().textContent).not.toMatch(
        /automatic|Automatic|password store|Choose a connection|Open the connection/,
      )
    },
  )

  it('names a connection by itself and its delivery state in the option note', async () => {
    const screen = await renderPanel({
      devices: [{ ...CONNECTED_DEVICE, autoDeliver: false }],
      outputs: [{ ...OUTPUT, deviceID: 'device-1' }],
    })

    await expect
      .element(screen.getByLabelText('Connection for Keenetic'))
      .toHaveTextContent('Home router')
    await expect
      .element(screen.getByLabelText('Connection for Keenetic'))
      .not.toHaveTextContent('·')
  })

  it('offers None as the first choice and unbinds through it', async () => {
    const screen = await renderPanel({
      devices: [{ ...CONNECTED_DEVICE, autoDeliver: false }],
      outputs: [{ ...OUTPUT, deviceID: 'device-1' }],
    })

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

  it('states a manual-only output as a fact without offering a connection', async () => {
    const screen = await renderPanel({ deployable: () => false })

    await expect
      .element(screen.getByText('Manual download', { exact: false }))
      .toBeVisible()
    await expect
      .element(screen.getByLabelText('Connection for Keenetic'))
      .not.toBeInTheDocument()
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
      .element(screen.getByRole('combobox', { name: 'Source refresh' }))
      .not.toBeInTheDocument()
    await expect
      .element(screen.getByRole('button', { name: 'Add a format' }))
      .not.toBeInTheDocument()
  })

  // An empty picker has two causes, and they ask for different things: all
  // formats added needs nothing, formats hidden in Settings are one switch away.
  it.each([
    [0, 'Every available format is already added.'],
    [2, 'The remaining formats are hidden in Settings.'],
  ])(
    'says why the format picker is empty with %i hidden formats',
    async (hiddenTargetCount, words) => {
      const screen = await renderPanel({ hiddenTargetCount })

      await expect.element(screen.getByText(words)).toBeVisible()
      // The control is disabled and carries that reason as its description.
      const add = screen.getByRole('button', { name: 'Add a format' })
      await expect.element(add).toBeDisabled()
      await expect.element(add).toHaveAccessibleDescription(words)
    },
  )

  it('keeps Add format enabled and undescribed while there is a format to add', async () => {
    const screen = await renderPanel({
      targetGroups: [
        {
          kind: 'router',
          targets: [
            {
              id: 'mikrotik',
              title: 'MikroTik',
              fileExtension: 'rsc',
            },
          ],
        },
      ],
    })

    const add = screen.getByRole('button', { name: 'Add a format' })
    await expect.element(add).toBeEnabled()
    await expect.element(add).not.toHaveAccessibleDescription(/\S/)
  })
})

describe('OutputsPanel row actions', () => {
  beforeEach(() => useLocale().setLocale('en'))
  afterEach(() => vi.unstubAllGlobals())

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
    expect(
      screen
        .getByRole('columnheader')
        .all()
        .map((header) => header.element().textContent?.trim()),
    ).toEqual(['Format', 'Connection', 'Updated', 'Last sent', 'Actions'])
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
    // Every item of a menu panel carries a glyph.
    for (const item of screen.getByRole('menuitem').all()) {
      expect(
        item.element().querySelector('[data-testid="rv-icon"]'),
      ).not.toBeNull()
    }
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

  // The setting belongs to the formats that take names and addresses, so the
  // entry follows what the card says about its format, not a list of formats.
  it.each([
    ['holds the keep-addresses setting', true, true],
    ['cannot hold it', false, false],
  ] as const)(
    'offers format settings to a format that %s only when it can',
    async (_case, keepAddressesAvailable, offered) => {
      const screen = await renderPanel({
        outputs: [
          {
            ...PUBLISHED,
            targetID: keepAddressesAvailable ? 'mikrotik' : 'openwrt',
            targetTitle: keepAddressesAvailable ? 'MikroTik' : 'OpenWrt',
            keepAddressesAvailable,
          },
        ],
      })

      await screen
        .getByRole('button', {
          name: `Actions for ${keepAddressesAvailable ? 'MikroTik' : 'OpenWrt'}`,
        })
        .click()
      const items = screen
        .getByRole('menuitem')
        .all()
        .map((item) => item.element().textContent?.trim())
      expect(items.includes('Format settings…')).toBe(offered)
    },
  )

  it('opens the settings of a format that is not domain-based Keenetic without a prefix field, and shows the choice stored on its card', async () => {
    const screen = await renderPanel({
      outputs: [
        {
          ...PUBLISHED,
          targetID: 'mikrotik',
          targetTitle: 'MikroTik',
          keepAddresses: true,
          keepAddressesAvailable: true,
        },
      ],
    })

    await screen.getByRole('button', { name: 'Actions for MikroTik' }).click()
    await screen.getByRole('menuitem', { name: 'Format settings…' }).click()
    await expect
      .element(
        screen.getByRole('switch', { name: 'Keep addresses beside domains' }),
      )
      .toBeChecked()
    await expect
      .element(screen.getByRole('textbox', { name: 'FQDN group prefix' }))
      .not.toBeInTheDocument()
  })

  it('shows a setting saved a moment ago when the card is read again, and asks the page to read it', async () => {
    const card: OutputCard = {
      ...PUBLISHED,
      targetID: 'mikrotik',
      targetTitle: 'MikroTik',
      keepAddresses: false,
      keepAddressesAvailable: true,
    }
    const fetchMock = vi.fn().mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          output: {
            id: 'output-1',
            list_id: 'profile-1',
            target_id: 'mikrotik',
            keep_addresses: true,
          },
        }),
      ),
    )
    vi.stubGlobal('fetch', fetchMock)
    const screen = await renderPanel({ outputs: [card] })

    await screen.getByRole('button', { name: 'Actions for MikroTik' }).click()
    await screen.getByRole('menuitem', { name: 'Format settings…' }).click()
    const keep = screen.getByRole('switch', {
      name: 'Keep addresses beside domains',
    })
    await keep.click()
    await screen.getByRole('button', { name: 'Save', exact: true }).click()
    await expect.poll(() => screen.emitted('settingsSaved')).toEqual([[]])
    await screen.rerender({ outputs: [{ ...card, keepAddresses: true }] })
    await screen.getByRole('button', { name: 'Actions for MikroTik' }).click()
    await screen.getByRole('menuitem', { name: 'Format settings…' }).click()
    await expect.element(keep).toBeChecked()
  })

  it('offers Download and Send as icon buttons named for the format, each explained by a tooltip', async () => {
    const screen = await renderPanel({ outputs: [PUBLISHED] })

    const download = screen.getByRole('link', {
      name: 'Download the file for Keenetic',
    })
    // Sending opens over the page, so it is an act rather than a way out.
    const send = screen.getByRole('button', { name: 'Send Keenetic' })
    await expect
      .element(download)
      .toHaveAttribute('href', '/v1/artifacts/artifact-1')

    // The glyph is the whole face: no word stands beside it.
    expect(download.element().textContent?.trim()).toBe('')
    expect(send.element().textContent?.trim()).toBe('')

    await send.hover()
    await expect.element(page.getByTestId('rv-tooltip')).toBeVisible()
    expect(page.getByTestId('rv-tooltip').element().textContent).toContain(
      'Send Keenetic',
    )

    await send.click()
    expect(screen.emitted('send')).toEqual([['output-1']])
  })

  it('offers no Send for a format that cannot be sent and no Download before a file exists', async () => {
    const screen = await renderPanel({ deployable: () => false })

    await expect
      .element(screen.getByRole('button', { name: /^Send/ }))
      .not.toBeInTheDocument()
    await expect
      .element(screen.getByRole('link', { name: /^Download/ }))
      .not.toBeInTheDocument()
  })

  it('keeps the actions in the row at a narrow panel instead of moving them into the menu', async () => {
    const screen = await renderPanel({ outputs: [PUBLISHED] })

    await screen.getByRole('button', { name: 'Actions for Keenetic' }).click()
    const items = screen
      .getByRole('menuitem')
      .all()
      .map((item) => item.element().textContent?.trim())
    expect(items).not.toContain('Download')
    expect(items).not.toContain('Send')
  })

  describe('open connection', () => {
    it('leads from the menu to the bound connection, selected', async () => {
      const screen = await renderPanel({
        devices: [CONNECTED_DEVICE],
        outputs: [{ ...OUTPUT, deviceID: 'device-1' }],
      })

      await screen.getByRole('button', { name: 'Actions for Keenetic' }).click()
      const open = screen.getByRole('menuitem', { name: 'Open the connection' })
      await expect
        .element(open)
        .toHaveAttribute('href', '/connections#connection=device-1')
      expect(
        open.element().querySelector('[data-testid="rv-icon"]'),
      ).not.toBeNull()
    })

    it('is not offered while no connection is bound', async () => {
      const screen = await renderPanel({ devices: [CONNECTED_DEVICE] })

      await screen.getByRole('button', { name: 'Actions for Keenetic' }).click()
      await expect
        .element(screen.getByRole('menuitem', { name: 'Open the connection' }))
        .not.toBeInTheDocument()
    })
  })

  describe('archived profile', () => {
    it('keeps Download, withdraws Send and offers only the way to the bound connection', async () => {
      const screen = await renderPanel({
        archived: true,
        devices: [CONNECTED_DEVICE],
        outputs: [{ ...PUBLISHED, deviceID: 'device-1' }],
      })

      await expect
        .element(
          screen.getByRole('link', { name: 'Download the file for Keenetic' }),
        )
        .toBeVisible()
      await expect
        .element(screen.getByRole('button', { name: 'Send Keenetic' }))
        .not.toBeInTheDocument()
      await screen.getByRole('button', { name: 'Actions for Keenetic' }).click()
      expect(
        screen
          .getByRole('menuitem')
          .all()
          .map((item) => item.element().textContent?.trim()),
      ).toEqual(['Open the connection'])
    })

    it('has no menu on a row with no connection to open', async () => {
      const screen = await renderPanel({
        archived: true,
        outputs: [PUBLISHED],
      })

      await expect
        .element(screen.getByRole('button', { name: 'Actions for Keenetic' }))
        .not.toBeInTheDocument()
    })
  })
})

describe('OutputsPanel last sent column', () => {
  // Five minutes before the test runs, so the cell reads as a distance.
  let COMPLETED_AT = ''
  beforeEach(() => {
    useLocale().setLocale('en')
    COMPLETED_AT = new Date(Date.now() - 5 * 60_000).toISOString()
  })

  const sentCell = (screen: Awaited<ReturnType<typeof renderPanel>>) =>
    screen.getByRole('row', { name: /^Keenetic/ })
  const delivery = (deviceID: string, artifactID = 'artifact-1') => ({
    deviceID,
    artifactID,
    completedAt: COMPLETED_AT,
  })
  const OFFICE = { ...CONNECTED_DEVICE, id: 'device-2', name: 'Office router' }

  it('says how long ago the file reached the bound connection', async () => {
    const screen = await renderPanel({
      devices: [CONNECTED_DEVICE],
      outputs: [
        {
          ...PUBLISHED,
          deviceID: 'device-1',
          deviceName: 'Home router',
          latestDelivery: delivery('device-1'),
        },
      ],
    })

    await expect
      .element(screen.getByText('5 minutes ago', { exact: true }))
      .toBeVisible()
    // The bound connection is named in its own cell, not again here.
    expect(
      sentCell(screen)
        .element()
        .textContent?.match(/Home router/g),
    ).toHaveLength(1)
  })

  // A send by hand to another saved connection is still where the file went
  // last; the row names that connection because it is not the bound one.
  it('names a connection other than the bound one, and a removed one', async () => {
    const screen = await renderPanel({
      devices: [CONNECTED_DEVICE, OFFICE],
      outputs: [
        {
          ...PUBLISHED,
          deviceID: 'device-1',
          latestDelivery: delivery('device-2'),
        },
      ],
    })
    await expect
      .element(screen.getByText('to “Office router”', { exact: true }))
      .toBeVisible()

    await screen.rerender({
      outputs: [
        { ...PUBLISHED, deviceID: '', latestDelivery: delivery('device-9') },
      ],
    })
    await expect
      .element(screen.getByText('to “a removed connection”', { exact: true }))
      .toBeVisible()
  })

  it('says an earlier file reached the router than the format publishes now', async () => {
    const screen = await renderPanel({
      devices: [CONNECTED_DEVICE],
      outputs: [
        {
          ...PUBLISHED,
          deviceID: 'device-1',
          latestDelivery: delivery('device-1', 'artifact-0'),
        },
      ],
    })

    await expect
      .element(screen.getByText('an earlier file was sent', { exact: true }))
      .toBeVisible()
  })

  it('states a send that failed after the last delivery, and why on request', async () => {
    const screen = await renderPanel({
      devices: [CONNECTED_DEVICE],
      outputs: [
        {
          ...PUBLISHED,
          deviceID: 'device-1',
          deviceName: 'Home router',
          latestDelivery: delivery('device-1'),
          latestFailure: {
            deviceID: 'device-1',
            code: 'device_auth_rejected',
            step: 'probe',
            completedAt: COMPLETED_AT,
          },
        },
      ],
    })

    await expect
      .element(screen.getByText('Not sent', { exact: true }))
      .toBeVisible()
    await screen
      .getByRole('button', { name: 'Why Keenetic was not sent' })
      .click()
    await expect
      .element(page.getByText(/“Home router”: The router did not accept/))
      .toBeVisible()
  })

  it('shows a dash when nothing was ever sent', async () => {
    const screen = await renderPanel({
      devices: [CONNECTED_DEVICE],
      outputs: [{ ...PUBLISHED, deviceID: 'device-1' }],
    })

    await expect.element(screen.getByText('—', { exact: true })).toBeVisible()
  })
})

describe('OutputsPanel updated column', () => {
  beforeEach(() => useLocale().setLocale('ru'))

  // The column is sized for a date. The words that stand in for a date when
  // nothing was published are longer in Russian, and cut to an ellipsis they
  // no longer say there is no file.
  it('shows the whole words for a format that never published', async () => {
    const screen = await renderPanel()

    const none = screen.getByText('Файл не опубликован', { exact: true })
    await expect.element(none).toBeVisible()
    const element = none.element() as HTMLElement
    expect(element.scrollWidth).toBeLessThanOrEqual(element.clientWidth)
  })
})
