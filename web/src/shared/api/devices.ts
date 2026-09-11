import * as v from 'valibot'

import {
  acknowledged,
  decode,
  type Decoder,
  fields,
  getJSON,
  optionalFlag,
  optionalText,
  postJSON,
  text,
} from './http'

// A device the operator registered: an instance of a catalog target on their
// own network. The credential is deliberately absent from every shape here --
// it is written once, to the operating system, and never read back.
export type DeviceCard = {
  id: string
  targetID: string
  targetTitle: string
  name: string
  address: string
  account: string
  interfaceName: string
  autoDeliver: boolean
  deployable: boolean
}

type DeviceRegistry = {
  devices: DeviceCard[]
  secretStoreAvailable: boolean
}

export const loadDevices = async (): Promise<DeviceRegistry> =>
  getJSON('/v1/devices', parseDevices)

export const registerDevice = (
  targetID: string,
  name: string,
  address: string,
  account: string,
  interfaceName: string,
): Promise<string> =>
  postJSON(
    '/v1/devices',
    { target_id: targetID, name, address, account, interface: interfaceName },
    parseDeviceID,
  )

// The target is identity and is not part of this call. Changing the address,
// the account or the interface revokes automatic delivery and removes the
// stored credential on the server, because consent named one exact destination
// and account; the credential itself never travels here.
export const updateDevice = (
  id: string,
  name: string,
  address: string,
  account: string,
  interfaceName: string,
): Promise<string> =>
  postJSON(
    `/v1/devices/${id}/update`,
    { name, address, account, interface: interfaceName },
    parseDeviceID,
  )

export const forgetDevice = (id: string): Promise<true> =>
  postJSON(`/v1/devices/${id}/forget`, {}, parseAcknowledgement)

// The credential leaves this tab once and is never read back. Nothing here
// keeps it, and no reply carries it.
export const enableAutoDelivery = (
  id: string,
  credential: string,
): Promise<true> =>
  postJSON(
    `/v1/devices/${id}/auto-delivery`,
    { enabled: true, credential },
    parseAcknowledgement,
  )

export const disableAutoDelivery = (id: string): Promise<true> =>
  postJSON(
    `/v1/devices/${id}/auto-delivery`,
    { enabled: false },
    parseAcknowledgement,
  )

const deviceSchema = v.pipe(
  fields({
    id: text,
    target_id: text,
    target_title: text,
    name: text,
    address: text,
    account: optionalText,
    interface: optionalText,
    auto_deliver: optionalFlag,
    deployable: optionalFlag,
  }),
  v.transform((device): DeviceCard => ({
    id: device.id,
    targetID: device.target_id,
    targetTitle: device.target_title,
    name: device.name,
    address: device.address,
    account: device.account,
    interfaceName: device.interface,
    autoDeliver: device.auto_deliver,
    deployable: device.deployable,
  })),
)

// Whether the operating system offers a secret store at all is the server's
// answer. A build that cannot say so is read as one that has none, so the form
// never offers to keep a credential nothing would hold.
const devicesSchema = v.pipe(
  fields({
    devices: v.array(deviceSchema),
    secret_store_available: optionalFlag,
  }),
  v.transform((registry): DeviceRegistry => ({
    devices: registry.devices,
    secretStoreAvailable: registry.secret_store_available,
  })),
)

const parseDevices: Decoder<DeviceRegistry> = decode(devicesSchema)
// Registration and a parameter change both answer with the device they wrote,
// and both are read for the same one fact: which device this reply is about.
const parseDeviceID: Decoder<string> = decode(
  v.pipe(
    fields({ device: fields({ id: text }) }),
    v.transform(({ device }) => device.id),
  ),
)
const parseAcknowledgement: Decoder<true> = decode(acknowledged)
