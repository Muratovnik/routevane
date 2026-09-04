import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it } from 'vitest'

import { useLocale } from '@/shared/i18n/useLocale'

import ConfigTransferPanel from './ConfigTransferPanel.vue'

describe('ConfigTransferPanel', () => {
  beforeEach(() => {
    useLocale().setLocale('en')
  })

  it('states that transfer is for an empty installation', () => {
    const wrapper = mount(ConfigTransferPanel, {
      global: {
        stubs: { RvButton: true, RvDialog: true, RvStateNotice: true },
      },
    })

    expect(wrapper.text()).toContain(
      'Import works only in an empty installation.',
    )
    expect(wrapper.text()).not.toContain(
      'current configuration will be replaced',
    )
    wrapper.unmount()
  })
})
