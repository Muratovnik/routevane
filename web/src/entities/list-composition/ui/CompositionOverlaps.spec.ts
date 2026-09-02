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
  services: [
    { id: 'alpha', title: 'Alpha list' },
    { id: 'beta', title: 'Beta list' },
  ],
}

describe('CompositionOverlaps', () => {
  afterEach(() => useLocale().setLocale('en'))

  it('keeps destinations behind deliberate inspection and names the format and owners', async () => {
    const wrapper = mount(CompositionOverlaps, { props })
    expect(wrapper.get('details').element.open).toBe(false)
    expect(wrapper.text()).toContain('sing-box: file forecast — 5 entries.')
    expect(wrapper.text()).toContain('Identical rule')
    expect(wrapper.text()).toContain('Covered by another list')
    expect(wrapper.text()).toContain('IPv6 network')
    const owner = wrapper.get('a[href="/library#list=alpha"]')
    expect(owner.text()).toBe('Alpha list')
    expect(owner.attributes('target')).toBe('_blank')
    expect(wrapper.get('ul').attributes('tabindex')).toBe('0')
    useLocale().setLocale('ru')
    await wrapper.vm.$nextTick()
    expect(wrapper.text()).toContain('Пересечения списков')
    expect(wrapper.text()).toContain('IPv6-сеть')
    wrapper.unmount()
  })

  it('marks a retained answer as stale and distinguishes failure from no overlaps', async () => {
    const wrapper = mount(CompositionOverlaps, { props })
    await wrapper.setProps({ pending: true })
    expect(wrapper.text()).toContain('previous result')
    expect(wrapper.text()).toContain('shared.example')
    await wrapper.setProps({ pending: false, forecast: null })
    expect(wrapper.text()).toContain('Overlaps are not known yet')
    expect(wrapper.text()).not.toContain('shared.example')
    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('retry')).toHaveLength(1)
    await wrapper.setProps({
      forecast: { ...forecast, overlaps: { items: [], truncated: false } },
    })
    expect(wrapper.text()).toContain('No identical rules or containment')
    expect(wrapper.text()).not.toContain('Overlaps are not known yet')
    wrapper.unmount()
  })

  it('states the detail bound without inventing a total or savings', () => {
    const wrapper = mount(CompositionOverlaps, {
      props: {
        ...props,
        forecast: {
          ...forecast,
          overlaps: { items: forecast.overlaps!.items, truncated: true },
        },
      },
    })
    expect(wrapper.text()).toContain(
      'Showing the first 2 overlaps. There are more',
    )
    expect(wrapper.text()).toContain('file forecast — 5 entries')
    expect(wrapper.text()).not.toContain('saved')
    wrapper.unmount()
  })
})
