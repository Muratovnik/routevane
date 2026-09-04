import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'

import type { TargetForecast } from '@/shared/api/lists'
import { useLocale } from '@/shared/i18n/useLocale'

import CompositionOverlaps from './CompositionOverlaps.vue'

const forecast: TargetForecast = {
  targetID: 'singbox',
  maximumRules: 8192,
  projectedRules: 5,
  fits: true,
  perService: [],
  overlaps: {
    truncated: false,
    items: [
      {
        kind: 'duplicate',
        entry: {
          ruleKind: 'domain_suffix',
          value: 'shared.example',
          services: ['alpha', 'beta'],
        },
      },
      {
        kind: 'covered',
        entry: { ruleKind: 'ipv6', value: '2001:db8::1', services: ['beta'] },
        covering: {
          ruleKind: 'prefix6',
          value: '2001:db8::/48',
          services: ['alpha'],
        },
      },
    ],
  },
}
const props = {
  forecast,
  pending: false,
  targetTitle: 'sing-box',
}

describe('CompositionOverlaps', () => {
  afterEach(() => useLocale().setLocale('en'))

  it('explains automatic resolution without asking the operator to inspect every destination', async () => {
    const wrapper = mount(CompositionOverlaps, { props })
    const trigger = wrapper.get<HTMLButtonElement>('.rv-disclosure__summary')
    expect(trigger.attributes('aria-expanded')).toBe('false')
    expect(wrapper.get('.rv-disclosure__panel').attributes('inert')).toBe('')
    await trigger.trigger('click')
    expect(trigger.attributes('aria-expanded')).toBe('true')
    expect(wrapper.text()).toContain('sing-box: file forecast — 5 entries.')
    expect(wrapper.text()).toContain('Overlaps were found')
    expect(wrapper.text()).toContain('No manual cleanup is required')
    expect(wrapper.text()).toContain('higher priority')
    expect(wrapper.find('a').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('shared.example')
    useLocale().setLocale('ru')
    await wrapper.vm.$nextTick()
    expect(wrapper.text()).toContain('Как разрешаются пересечения')
    expect(wrapper.text()).toContain('Ничего разбирать вручную не нужно')
    wrapper.unmount()
  })

  it('marks a retained answer as stale and distinguishes failure from no overlaps', async () => {
    const wrapper = mount(CompositionOverlaps, { props })
    await wrapper.setProps({ pending: true })
    expect(wrapper.text()).toContain('previous result')
    await wrapper.setProps({ pending: false, forecast: null })
    expect(wrapper.text()).toContain('Overlaps are not known yet')
    const retry = wrapper
      .findAll('button')
      .find((button) => button.text().includes('Retry'))
    expect(retry).toBeDefined()
    await retry?.trigger('click')
    expect(wrapper.emitted('retry')).toHaveLength(1)
    await wrapper.setProps({
      forecast: { ...forecast, overlaps: { items: [], truncated: false } },
    })
    expect(wrapper.text()).toContain('No identical rules or containment')
    expect(wrapper.text()).not.toContain('Overlaps are not known yet')
    wrapper.unmount()
  })

  it('does not render the server detail cap as a cleanup queue', () => {
    const wrapper = mount(CompositionOverlaps, {
      props: {
        ...props,
        forecast: {
          ...forecast,
          overlaps: { items: forecast.overlaps!.items, truncated: true },
        },
      },
    })
    expect(wrapper.text()).toContain('file forecast — 5 entries')
    expect(wrapper.text()).toContain('resolved automatically')
    expect(wrapper.text()).not.toContain('Showing the first')
    wrapper.unmount()
  })
})
