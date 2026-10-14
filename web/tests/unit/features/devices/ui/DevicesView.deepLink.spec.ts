import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-vue'

import DevicesView from '@/features/devices/ui/DevicesView.vue'
import { useLocale } from '@/shared/i18n/useLocale'

const json = (payload: unknown, status = 200): Response =>
  new Response(JSON.stringify(payload), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })

const wire = (id: string, name: string) => ({
  id,
  target_id: 'keenetic',
  target_title: 'Keenetic',
  name,
  address: 'http://192.0.2.1',
  account: 'admin',
  interface: 'Wireguard0',
  credential_stored: false,
  auto_deliver: false,
  deployable: true,
})

// `failFirstRead` makes the first read of the registry fail the way a service
// that is not up yet does, and every later one succeed.
const installFetch = (failFirstRead = false) => {
  let registryReads = 0
  return vi.stubGlobal(
    'fetch',
    vi.fn((input: unknown) => {
      switch (String(input)) {
        case '/v1/devices':
          if (failFirstRead && registryReads++ === 0)
            return Promise.reject(new TypeError('Failed to fetch'))
          return Promise.resolve(
            json({
              devices: [
                wire('device-1', 'First router'),
                wire('device-2', 'Second router'),
              ],
              secret_store_available: false,
            }),
          )
        case '/v1/lists':
          return Promise.resolve(
            json({ lists: [], list_details: [], categories: [] }),
          )
        case '/v1/targets':
          return Promise.resolve(json({ targets: [] }))
        case '/v1/deployments/targets':
          return Promise.resolve(json({ targets: [] }))
        default:
          return Promise.reject(
            new Error(`unexpected request ${String(input)}`),
          )
      }
    }),
  )
}

beforeEach(() => useLocale().setLocale('en'))
afterEach(() => vi.unstubAllGlobals())

describe('DevicesView arrival from a link to one connection', () => {
  it('opens the named connection and puts the keyboard on its heading', async () => {
    installFetch()
    const screen = await render(DevicesView, {
      props: { initialConnection: 'device-2' },
    })

    const heading = screen.getByRole('heading', { name: 'Second router' })
    await expect.element(heading).toBeVisible()
    await expect.element(heading).toHaveFocus()
    await expect
      .element(screen.getByRole('heading', { name: 'First router' }))
      .not.toBeInTheDocument()
    await expect
      .element(
        screen.getByRole('button', { name: /Second router/, pressed: true }),
      )
      .toBeVisible()
  })

  it('opens the first connection when the named one does not exist', async () => {
    installFetch()
    const screen = await render(DevicesView, {
      props: { initialConnection: 'device-gone' },
    })

    await expect
      .element(screen.getByRole('heading', { name: 'First router' }))
      .toBeVisible()
    await expect
      .element(screen.getByRole('heading', { name: 'Second router' }))
      .not.toBeInTheDocument()
  })

  it('opens the first connection when no link named one', async () => {
    installFetch()
    const screen = await render(DevicesView)

    await expect
      .element(screen.getByRole('heading', { name: 'First router' }))
      .toBeVisible()
  })

  it('reports the connection the operator picks, so the address can follow', async () => {
    installFetch()
    const picked: string[] = []
    const screen = await render(DevicesView, {
      props: { onSelect: (id: string) => picked.push(id) },
    })

    await screen.getByRole('button', { name: /Second router/ }).click()
    await expect
      .element(screen.getByRole('heading', { name: 'Second router' }))
      .toBeVisible()
    expect(picked).toEqual(['device-2'])
  })
})

// The first read of the registry may fail; the operator then presses Retry. The
// connection they land on is the one arrival would have chosen, so the screen
// does not answer a successful retry with the form for a new connection.
describe('DevicesView after a failed first read', () => {
  it('opens the first connection once Retry reads the registry', async () => {
    installFetch(true)
    const screen = await render(DevicesView)

    await screen.getByRole('button', { name: 'Retry', exact: true }).click()

    await expect
      .element(screen.getByRole('heading', { name: 'First router' }))
      .toBeVisible()
    await expect
      .element(
        screen.getByRole('button', { name: /First router/, pressed: true }),
      )
      .toBeVisible()
  })

  it('opens the connection a link named, with the keyboard on its heading', async () => {
    installFetch(true)
    const screen = await render(DevicesView, {
      props: { initialConnection: 'device-2' },
    })

    await screen.getByRole('button', { name: 'Retry', exact: true }).click()

    const heading = screen.getByRole('heading', { name: 'Second router' })
    await expect.element(heading).toBeVisible()
    await expect.element(heading).toHaveFocus()
    await expect
      .element(screen.getByRole('heading', { name: 'First router' }))
      .not.toBeInTheDocument()
  })
})
