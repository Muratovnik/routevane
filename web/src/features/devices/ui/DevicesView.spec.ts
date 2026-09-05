import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useLocale } from '@/shared/i18n/useLocale'

import DevicesView from './DevicesView.vue'

function json(payload: unknown, status = 200): Response {
  return new Response(JSON.stringify(payload), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

const fetchMock = vi.fn()

const devicePayload = {
  devices: [
    {
      id: 'device-1',
      target_id: 'keenetic',
      target_title: 'Keenetic',
      name: 'Home router',
      address: 'http://192.168.1.1',
      account: 'admin',
      auto_deliver: false,
      deployable: true,
    },
  ],
  secret_store_available: true,
}

const catalogPayload = {
  services: [],
  service_details: [],
  categories: [],
}

const targetsPayload = {
  targets: [
    {
      id: 'keenetic',
      title: 'Keenetic',
      kind: 'router',
      profile_key: 'keenetic-bat-ipv4-v1',
      renderer_id: 'keenetic-route-bat',
      file_extension: 'bat',
      manual_installation_hint: 'Upload the file.',
    },
  ],
}

const requirementsPayload = {
  targets: [
    {
      target_id: 'keenetic',
      title: 'Keenetic',
      deployer_id: 'keenetic',
      requirements: {
        address_label: 'Device address',
        address_example: 'http://192.168.1.1',
        needs_credential: true,
        needs_interface: true,
        interface_label: 'Device interface',
      },
    },
  ],
}

function mountDevices() {
  return mount(DevicesView, {
    attachTo: document.body,
    global: { stubs: { RvIcon: true } },
  })
}

describe('DevicesView prerequisite audit', () => {
  beforeEach(() => {
    useLocale().setLocale('en')
    fetchMock.mockReset()
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    document.body.innerHTML = ''
  })

  it('keeps the form and devices visible while requirements fail, then retries in place', async () => {
    let requirementsReads = 0
    fetchMock.mockImplementation((input: string) => {
      if (input === '/v1/devices') return Promise.resolve(json(devicePayload))
      if (input === '/v1/services') return Promise.resolve(json(catalogPayload))
      if (input === '/v1/targets') return Promise.resolve(json(targetsPayload))
      if (input === '/v1/deployments/targets') {
        requirementsReads += 1
        return requirementsReads === 1
          ? Promise.reject(new TypeError('unavailable'))
          : Promise.resolve(json(requirementsPayload))
      }
      return Promise.reject(new Error(`unexpected request ${input}`))
    })
    const wrapper = mountDevices()
    await flushPromises()

    expect(wrapper.text()).toContain('Home router')
    expect(wrapper.text()).toContain('Connection requirements unavailable')
    expect(wrapper.find('#device-target').exists()).toBe(true)
    expect(
      wrapper
        .findAll('button')
        .some((button) => button.text() === 'Turn on automatic delivery'),
    ).toBe(false)

    await wrapper.get('#device-target').trigger('click')
    await flushPromises()
    const option = [
      ...document.body.querySelectorAll<HTMLElement>('[role="option"]'),
    ].find((candidate) => candidate.textContent?.includes('Keenetic'))
    expect(option).toBeDefined()
    option?.click()
    await flushPromises()

    expect(wrapper.text()).not.toContain('Fill in this field')

    await wrapper.get('#device-name').setValue('Draft router')
    await wrapper.get('#device-address').setValue('http://192.168.1.2')
    expect(
      wrapper.get<HTMLButtonElement>('button[type="submit"]').element.disabled,
    ).toBe(true)

    const retry = wrapper
      .findAll('button')
      .find((button) => button.text() === 'Retry')
    expect(retry).toBeDefined()
    await retry?.trigger('click')
    await flushPromises()

    expect(wrapper.get<HTMLInputElement>('#device-name').element.value).toBe(
      'Draft router',
    )
    expect(wrapper.get<HTMLInputElement>('#device-address').element.value).toBe(
      'http://192.168.1.2',
    )
    expect(wrapper.find('#device-account').exists()).toBe(true)
    expect(wrapper.find('#device-interface').exists()).toBe(true)
    await wrapper.get('#device-interface').setValue('Wireguard0')
    await wrapper.get('#device-account').trigger('blur')
    expect(
      wrapper.get<HTMLButtonElement>('button[type="submit"]').element.disabled,
    ).toBe(true)
    expect(wrapper.text()).toContain('Fill in this field')
    await wrapper.get('#device-account').setValue('admin')
    expect(
      wrapper.get<HTMLButtonElement>('button[type="submit"]').element.disabled,
    ).toBe(false)
    expect(requirementsReads).toBe(2)
    wrapper.unmount()
  })

  it('keeps a no-deployer target registerable without inventing credential fields', async () => {
    fetchMock.mockImplementation((input: string, init?: RequestInit) => {
      if (input === '/v1/devices' && init?.method === 'POST')
        return Promise.resolve(json({}))
      if (input === '/v1/devices')
        return Promise.resolve(
          json({ devices: [], secret_store_available: true }),
        )
      if (input === '/v1/services') return Promise.resolve(json(catalogPayload))
      if (input === '/v1/targets') return Promise.resolve(json(targetsPayload))
      if (input === '/v1/deployments/targets')
        return Promise.resolve(json({ targets: [] }))
      return Promise.reject(new Error(`unexpected request ${input}`))
    })
    const wrapper = mountDevices()
    await flushPromises()

    await wrapper.get('#device-target').trigger('click')
    await flushPromises()
    const option = [
      ...document.body.querySelectorAll<HTMLElement>('[role="option"]'),
    ].find((candidate) => candidate.textContent?.includes('Keenetic'))
    expect(option).toBeDefined()
    option?.click()
    await flushPromises()
    await wrapper.get('#device-name').setValue('Manual router')
    await wrapper.get('#device-address').setValue('file:///router.conf')

    expect(wrapper.find('#device-account').exists()).toBe(false)
    expect(wrapper.find('#device-interface').exists()).toBe(false)
    expect(
      wrapper.get<HTMLButtonElement>('button[type="submit"]').element.disabled,
    ).toBe(false)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(
      fetchMock.mock.calls.filter(
        ([input, request]) =>
          input === '/v1/devices' &&
          (request as RequestInit | undefined)?.method === 'POST',
      ),
    ).toHaveLength(1)
    wrapper.unmount()
  })
})
