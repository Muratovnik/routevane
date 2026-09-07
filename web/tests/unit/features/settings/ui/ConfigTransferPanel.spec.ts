import { beforeEach, describe, expect, it } from 'vitest'
import { render } from 'vitest-browser-vue'

import { useLocale } from '@/shared/i18n/useLocale'

import ConfigTransferPanel from '@/features/settings/ui/ConfigTransferPanel.vue'

const GLOBAL = {
  stubs: { RvButton: true, RvDialog: true, RvStateNotice: true },
}

describe('ConfigTransferPanel', () => {
  beforeEach(() => {
    useLocale().setLocale('en')
  })

  it('states that transfer is for an empty installation', async () => {
    const screen = await render(ConfigTransferPanel, { global: GLOBAL })

    await expect
      .element(
        screen.getByText('Import works only in an empty installation.', {
          exact: false,
        }),
      )
      .toBeVisible()
    await expect
      .element(
        screen.getByText('current configuration will be replaced', {
          exact: false,
        }),
      )
      .not.toBeInTheDocument()
  })

  it('explains the omitted custom-source boundary in Russian', async () => {
    const locale = useLocale()
    locale.setLocale('ru')
    const screen = await render(ConfigTransferPanel, { global: GLOBAL })

    await expect
      .element(
        screen.getByText('адреса пользовательских источников', {
          exact: false,
        }),
      )
      .toBeVisible()
    await expect
      .element(screen.getByText('Не больше 64 МБ.', { exact: false }))
      .toBeVisible()
    expect(
      locale.t('configTransfer.warning.custom_sources_require_recreation'),
    ).toBe(
      'Пользовательские источники не входят в файл. После переноса добавьте их заново.',
    )
  })
})
