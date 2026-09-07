import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it } from 'vitest'

import { useLocale } from '@/shared/i18n/useLocale'

import ConfigTransferPanel from '@/features/settings/ui/ConfigTransferPanel.vue'

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

  it('explains the omitted custom-source boundary in Russian', () => {
    const locale = useLocale()
    locale.setLocale('ru')
    const wrapper = mount(ConfigTransferPanel, {
      global: {
        stubs: { RvButton: true, RvDialog: true, RvStateNotice: true },
      },
    })

    expect(wrapper.text()).toContain('адреса пользовательских источников')
    expect(wrapper.text()).toContain('Не больше 64 МБ.')
    expect(
      locale.t('configTransfer.warning.custom_sources_require_recreation'),
    ).toBe(
      'Пользовательские источники не входят в файл. После переноса добавьте их заново.',
    )
    wrapper.unmount()
  })
})
