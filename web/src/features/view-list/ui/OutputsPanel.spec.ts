import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it } from 'vitest'

import { useLocale } from '@/shared/i18n/useLocale'
import type { Schedule } from '@/shared/api/lists'

import OutputsPanel from './OutputsPanel.vue'

const output = {
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

const schedule: Schedule = {
  interval: '',
  effective: 'off' as const,
  followsDefault: true,
  lastRefreshedAt: '',
  lastRefreshFailed: false,
  nextRefreshAt: '',
}

function render(overrides: Record<string, unknown> = {}) {
  return mount(OutputsPanel, {
    props: {
      archived: false,
      busy: false,
      deployable: () => true,
      devices: [],
      listId: 'route-1',
      outputs: [output],
      schedule,
      selectedId: '',
      targetGroups: [],
      targetTitle: (_id: string, fallback?: string) => fallback ?? 'Keenetic',
      ...overrides,
    },
    global: { stubs: { RvIcon: true } },
  })
}

describe('OutputsPanel connection readiness', () => {
  beforeEach(() => useLocale().setLocale('en'))

  it('states the next unmet condition for an unbound output', () => {
    const wrapper = render()
    expect(wrapper.text()).toContain('Choose a connection')
    expect(wrapper.find('#list-schedule-select').exists()).toBe(true)
    wrapper.unmount()
  })

  it('states when automatic delivery is on but route refresh is off', () => {
    const wrapper = render({
      devices: [
        {
          id: 'device-1',
          targetID: 'keenetic',
          targetTitle: 'Keenetic',
          name: 'Home router',
          address: 'http://192.168.1.1',
          account: 'admin',
          interfaceName: 'Wireguard0',
          autoDeliver: true,
          deployable: true,
        },
      ],
      outputs: [{ ...output, deviceID: 'device-1' }],
    })
    expect(wrapper.text()).toContain('Turn on route refresh')
    wrapper.unmount()
  })

  it('does not invent automatic readiness for a manual-only output', () => {
    const wrapper = render({ deployable: () => false })
    expect(wrapper.text()).not.toContain('Choose a connection')
    expect(wrapper.text()).not.toContain('Automatic delivery configured')
    wrapper.unmount()
  })

  it('does not report readiness on an archived route', () => {
    const wrapper = render({
      archived: true,
      devices: [
        {
          id: 'device-1',
          targetID: 'keenetic',
          targetTitle: 'Keenetic',
          name: 'Home router',
          address: 'http://192.168.1.1',
          account: 'admin',
          interfaceName: 'Wireguard0',
          autoDeliver: true,
          deployable: true,
        },
      ],
      outputs: [{ ...output, deviceID: 'device-1' }],
    })
    expect(wrapper.text()).not.toContain('Automatic delivery configured')
    expect(wrapper.text()).not.toContain('Ready: new versions')
    wrapper.unmount()
  })
})
