import { beforeEach, describe, expect, it } from 'vitest'
import { render } from 'vitest-browser-vue'

import DeviceCleanupChoice from '@/entities/device-cleanup/ui/DeviceCleanupChoice.vue'
import type { DeviceObjects } from '@/entities/device-cleanup/model/types'
import { useLocale } from '@/shared/i18n/useLocale'

const HOME = {
  outputID: 'output-1',
  targetID: 'keenetic-dns',
  endpoint: 'http://192.0.2.1',
  interfaceName: '',
  connectionName: 'Home router',
  cleanable: true,
  blockedBy: '' as const,
  fqdnGroups: 2,
  routes: 0,
}

const renderChoice = (objects: DeviceObjects | null, state = 'ready') =>
  render(DeviceCleanupChoice, {
    props: {
      modelValue: false,
      objects,
      state: state as 'loading' | 'ready' | 'failed',
    },
  })

describe('DeviceCleanupChoice', () => {
  beforeEach(() => useLocale().setLocale('en'))

  it('offers to remove what the format left, router by router', async () => {
    const screen = await renderChoice({ cleanable: true, surfaces: [HOME] })

    const choice = screen.getByRole('switch', {
      name: 'Also remove from the router',
    })
    await expect.element(choice).toBeEnabled()
    await expect
      .element(screen.getByText('“Home router” (http://192.0.2.1)'))
      .toBeVisible()
    await expect.element(screen.getByText('2 FQDN groups')).toBeVisible()
    await choice.click()
    expect(screen.emitted('update:modelValue')).toEqual([[true]])
  })

  it('withdraws the choice and says why when a router cannot be cleaned', async () => {
    const screen = await renderChoice({
      cleanable: false,
      surfaces: [
        HOME,
        {
          ...HOME,
          outputID: 'output-2',
          endpoint: 'http://192.0.2.2',
          connectionName: '',
          cleanable: false,
          blockedBy: 'credential_missing',
          fqdnGroups: 0,
        },
      ],
    })

    await expect
      .element(
        screen.getByRole('switch', { name: 'Also remove from the router' }),
      )
      .toBeDisabled()
    await expect
      .element(screen.getByText('the connection has no saved password'))
      .toBeVisible()
  })

  it('offers nothing for a format that left nothing on any router', async () => {
    const screen = await renderChoice({ cleanable: true, surfaces: [] })

    await expect
      .element(
        screen.getByRole('switch', { name: 'Also remove from the router' }),
      )
      .not.toBeInTheDocument()
  })

  it('says it could not read what was left, without offering the choice', async () => {
    const screen = await renderChoice(null, 'failed')

    await expect.element(screen.getByText(/could not be read/)).toBeVisible()
    await expect.element(screen.getByRole('switch')).not.toBeInTheDocument()
  })
})
