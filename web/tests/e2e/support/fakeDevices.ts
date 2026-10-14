/**
 * A stand-in for the device registry's saved-password routes.
 *
 * A suite that needs a registry state it cannot reach in one step, or a
 * refusal on demand, states it here instead of building it on the product.
 * (The product's own password routes, over the test build's in-memory store,
 * are walked in `test-build-device.spec.ts`.) This answers the registry reads
 * and the password and delivery writes of ADR 0050 from memory, with the
 * contract the real routes keep: saving replaces
 * the password; removing it also turns delivery off for a format that needs
 * one; delivery cannot be turned on without a saved password for such a format;
 * and changing the address, login or interface revokes both.
 *
 * Only the `/v1/devices` routes are answered here. The catalog, the formats and
 * the deployment requirements still come from the running product.
 */
import type { Page, Request, Route } from '@playwright/test'

export interface FakeDevice {
  id: string
  target_id: string
  target_title: string
  name: string
  address: string
  account: string
  interface: string
  credential_stored: boolean
  auto_deliver: boolean
  deployable: boolean
}

export interface FakeDeviceCall {
  path: string
  body: unknown
}

export interface FakeRegistry {
  /** Every write the page made, in order. */
  readonly writes: FakeDeviceCall[]
  device(id: string): FakeDevice
  /** Answer the next update with the generic refusal the product gives. */
  refuseNextUpdate(): void
}

export const routerDevice = (
  overrides: Partial<FakeDevice> = {},
): FakeDevice => ({
  id: 'fake-router',
  target_id: 'keenetic',
  target_title: 'Keenetic',
  name: 'Home router',
  address: 'http://192.168.1.1',
  account: 'admin',
  interface: 'Wireguard0',
  credential_stored: false,
  auto_deliver: false,
  deployable: true,
  ...overrides,
})

const json = (route: Route, status: number, body: unknown) =>
  route.fulfill({
    status,
    contentType: 'application/json',
    body: JSON.stringify(body),
  })

export const routeFakeDevices = async (
  page: Page,
  seed: FakeDevice[],
  options: { secretStoreAvailable?: boolean } = {},
): Promise<FakeRegistry> => {
  const devices = new Map(seed.map((device) => [device.id, { ...device }]))
  const writes: FakeDeviceCall[] = []
  let refuseUpdate = false

  const answer = async (route: Route, request: Request) => {
    const [, , scope, id, ...rest] = new URL(request.url()).pathname.split('/')
    if (scope !== 'devices') return route.fallback()
    const action = rest.join('/') || undefined
    if (request.method() === 'GET' && id === undefined)
      return json(route, 200, {
        devices: [...devices.values()],
        secret_store_available: options.secretStoreAvailable ?? true,
      })
    if (request.method() !== 'POST' || id === undefined || action === undefined)
      return route.fallback()
    const body = (request.postDataJSON() ?? {}) as Record<string, unknown>
    writes.push({ path: `${id}/${action}`, body })
    const device = devices.get(id)
    if (device === undefined) return json(route, 404, { error: 'not found' })
    const needsPassword = device.target_id !== 'singbox'
    if (action === 'credential') {
      device.credential_stored = true
    } else if (action === 'credential/remove') {
      device.credential_stored = false
      if (needsPassword) device.auto_deliver = false
    } else if (action === 'auto-delivery') {
      if (body.enabled === true) {
        if (needsPassword && !device.credential_stored)
          return json(route, 409, { error: 'credential_required' })
        device.auto_deliver = true
      } else {
        device.auto_deliver = false
      }
    } else if (action === 'update') {
      if (refuseUpdate) {
        refuseUpdate = false
        return json(route, 422, { error: 'operation failed' })
      }
      const changed =
        body.address !== device.address ||
        body.account !== device.account ||
        body.interface !== device.interface
      if (changed) {
        device.credential_stored = false
        device.auto_deliver = false
      }
      device.name = String(body.name)
      device.address = String(body.address)
      device.account = String(body.account)
      device.interface = String(body.interface)
    } else {
      return route.fallback()
    }
    return json(route, 200, { device })
  }

  await page.route('**/v1/devices**', answer)
  return {
    writes,
    device: (id) => {
      const device = devices.get(id)
      if (device === undefined) throw new Error(`no fake device ${id}`)
      return device
    },
    refuseNextUpdate: () => {
      refuseUpdate = true
    },
  }
}
