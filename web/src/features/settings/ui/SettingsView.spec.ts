import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useLocale } from '@/shared/i18n/useLocale'

import SettingsView from './SettingsView.vue'

function json(payload: unknown, status = 200): Response {
  return new Response(JSON.stringify(payload), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

const fetchMock = vi.fn()

function mountSettings() {
  return mount(SettingsView, {
    attachTo: document.body,
    global: { stubs: { RvIcon: true } },
  })
}

describe('SettingsView prerequisite audit', () => {
  beforeEach(() => {
    useLocale().setLocale('en')
    fetchMock.mockReset()
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    document.body.innerHTML = ''
  })

  it('does not present Off as confirmed or save after a failed first read', async () => {
    fetchMock.mockRejectedValue(new TypeError('unavailable'))
    const wrapper = mountSettings()
    await flushPromises()

    const radios = wrapper.findAll<HTMLInputElement>(
      'input[name="rv-refresh-interval"]',
    )
    expect(radios).toHaveLength(3)
    expect(radios.some((radio) => radio.element.checked)).toBe(false)
    expect(wrapper.text()).toContain('The refresh rule could not be read.')
    expect(wrapper.text()).not.toContain('The refresh rule could not be saved.')
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(
      wrapper.findAll('button').some((button) => button.text() === 'Retry'),
    ).toBe(true)
    wrapper.unmount()
  })

  it('retries in the settings section and renders the server value', async () => {
    let reads = 0
    fetchMock.mockImplementation((input: string) => {
      if (input === '/v1/settings') {
        reads += 1
        return Promise.resolve(
          reads === 1
            ? json({ error: 'unavailable' }, 503)
            : json({ refresh_interval: 'daily' }),
        )
      }
      return Promise.reject(new Error(`unexpected request ${input}`))
    })
    const wrapper = mountSettings()
    await flushPromises()

    const retry = wrapper
      .findAll('button')
      .find((button) => button.text() === 'Retry')
    expect(retry).toBeDefined()
    await retry?.trigger('click')
    await flushPromises()

    const daily = wrapper
      .findAll<HTMLInputElement>('input[name="rv-refresh-interval"]')
      .find((radio) => radio.element.value === 'daily')
    expect(daily?.element.checked).toBe(true)
    expect(wrapper.text()).not.toContain('The refresh rule could not be read.')
    wrapper.unmount()
  })

  it.each([true, false])(
    'keeps native selection confirmed through saving (success=%s)',
    async (success) => {
      let release!: () => void
      const held = new Promise<void>((resolve) => {
        release = resolve
      })
      fetchMock.mockImplementation((input: string) => {
        if (input === '/v1/settings')
          return Promise.resolve(json({ refresh_interval: 'daily' }))
        if (input === '/v1/settings/update')
          return held.then(() =>
            success
              ? json({ refresh_interval: 'weekly' })
              : json({ error: 'unavailable' }, 503),
          )
        return Promise.reject(new Error(`unexpected request ${input}`))
      })
      const wrapper = mountSettings()
      await flushPromises()

      const radios = wrapper.findAll<HTMLInputElement>(
        'input[name="rv-refresh-interval"]',
      )
      const daily = radios.find((radio) => radio.element.value === 'daily')
      const weekly = radios.find((radio) => radio.element.value === 'weekly')
      const off = radios.find((radio) => radio.element.value === 'off')
      expect(daily?.element.checked).toBe(true)
      weekly!.element.click()
      await flushPromises()
      expect(daily?.element.checked).toBe(true)
      expect(weekly?.element.checked).toBe(false)
      expect(wrapper.text()).toContain('Saving the rule…')
      expect(
        fetchMock.mock.calls.filter(
          ([input]) => input === '/v1/settings/update',
        ),
      ).toHaveLength(1)

      off!.element.click()
      expect(
        fetchMock.mock.calls.filter(
          ([input]) => input === '/v1/settings/update',
        ),
      ).toHaveLength(1)
      release()
      await flushPromises()
      expect(weekly?.element.checked).toBe(success)
      expect(daily?.element.checked).toBe(!success)
      if (!success)
        expect(wrapper.text()).toContain('The refresh rule could not be saved.')
      wrapper.unmount()
    },
  )
})
