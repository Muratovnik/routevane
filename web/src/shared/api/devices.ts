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
// own network. The credential itself is deliberately absent from every shape
// here -- it is written to the operating system, never read back, and the
// registry only reports whether one is saved (ADR 0050).
export type DeviceCard = {
  id: string
  targetID: string
  targetTitle: string
  name: string
  address: string
  account: string
  interfaceName: string
  credentialStored: boolean
  autoDeliver: boolean
  deployable: boolean
  // Every live format bound to this connection, so a change to the connection
  // can say what it affects. Empty from a server that does not report it.
  usedBy: DeviceUse[]
}

export type DeviceUse = {
  profileID: string
  profileName: string
  outputID: string
  targetID: string
  archived: boolean
}

// What saving a password learned by signing in with it (ADR 0059). A refused
// password is not saved and answers with an error instead; every result here
// means the password is kept. `code` names why it is kept unchecked, or what
// the router lacks.
export type CredentialCheck = {
  result: 'verified' | 'interface_missing' | 'unverified' | 'not_applicable'
  code: string
}

// Writes answer with the stored device, while catalog-derived card fields are
// only present on the registry read.
export type DeviceWrite = {
  id: string
  targetID?: string
  name?: string
  address?: string
  account?: string
  interfaceName?: string
  credentialStored?: boolean
  autoDeliver?: boolean
  // Present on the answer to saving a password.
  verification?: CredentialCheck
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
): Promise<DeviceWrite> =>
  postJSON(
    '/v1/devices',
    { target_id: targetID, name, address, account, interface: interfaceName },
    parseDeviceWrite,
  )

// The target is identity and is not part of this call. Changing the address,
// the account or the interface revokes automatic delivery and removes the
// stored credential on the server, because consent named one exact destination
// and account; the credential itself never travels here. A refusal carries no
// field name, so a screen can only say which field it suspects.
export const updateDevice = (
  id: string,
  name: string,
  address: string,
  account: string,
  interfaceName: string,
): Promise<DeviceWrite> =>
  postJSON(
    `/v1/devices/${id}/update`,
    { name, address, account, interface: interfaceName },
    parseDeviceWrite,
  )

export const forgetDevice = (id: string): Promise<true> =>
  postJSON(`/v1/devices/${id}/forget`, {}, parseAcknowledgement)

// Saving a password is its own act on a connection and says nothing about
// automatic delivery. The credential leaves this tab once, to the server, which
// hands it to the operating system; nothing here keeps it and no reply carries
// it. Saving again replaces the stored one.
export const saveDeviceCredential = (
  id: string,
  credential: string,
): Promise<DeviceWrite> =>
  postJSON(`/v1/devices/${id}/credential`, { credential }, parseDeviceWrite)

// Removing it also turns delivery off when the format needs a password, and the
// reply carries the state that results.
export const removeDeviceCredential = (id: string): Promise<DeviceWrite> =>
  postJSON(`/v1/devices/${id}/credential/remove`, {}, parseDeviceWrite)

// Enabling a format that needs a password is refused with 409 until one is
// saved; disabling keeps the saved password.
export const enableAutoDelivery = (id: string): Promise<DeviceWrite> =>
  postJSON(
    `/v1/devices/${id}/auto-delivery`,
    { enabled: true },
    parseDeviceWrite,
  )

export const disableAutoDelivery = (id: string): Promise<DeviceWrite> =>
  postJSON(
    `/v1/devices/${id}/auto-delivery`,
    { enabled: false },
    parseDeviceWrite,
  )

const deviceUseSchema = v.pipe(
  fields({
    profile_id: text,
    profile_name: text,
    output_id: text,
    target_id: text,
    archived: optionalFlag,
  }),
  v.transform((use): DeviceUse => ({
    profileID: use.profile_id,
    profileName: use.profile_name,
    outputID: use.output_id,
    targetID: use.target_id,
    archived: use.archived,
  })),
)

// A result this build does not know reads as no check at all, never as a
// verified one.
const verificationSchema = v.fallback(
  v.optional(
    fields({
      result: v.picklist([
        'verified',
        'interface_missing',
        'unverified',
        'not_applicable',
      ]),
      code: optionalText,
    }),
  ),
  undefined,
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
    credential_stored: optionalFlag,
    auto_deliver: optionalFlag,
    deployable: optionalFlag,
    used_by: v.optional(v.array(deviceUseSchema), []),
  }),
  v.transform((device): DeviceCard => ({
    id: device.id,
    targetID: device.target_id,
    targetTitle: device.target_title,
    name: device.name,
    address: device.address,
    account: device.account,
    interfaceName: device.interface,
    credentialStored: device.credential_stored,
    autoDeliver: device.auto_deliver,
    deployable: device.deployable,
    usedBy: device.used_by,
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
// Keep absent fields distinct from explicit empty values. The model can fill
// absent fields from the submitted operation without undoing intentional clears.
const parseDeviceWrite: Decoder<DeviceWrite> = decode(
  v.pipe(
    fields({
      device: fields({
        id: text,
        target_id: v.optional(optionalText),
        name: v.optional(optionalText),
        address: v.optional(optionalText),
        account: v.optional(optionalText),
        interface: v.optional(optionalText),
        credential_stored: v.optional(optionalFlag),
        auto_deliver: v.optional(optionalFlag),
      }),
      verification: verificationSchema,
    }),
    v.transform(({ device, verification }): DeviceWrite => ({
      id: device.id,
      targetID: device.target_id,
      name: device.name,
      address: device.address,
      account: device.account,
      interfaceName: device.interface,
      credentialStored: device.credential_stored,
      autoDeliver: device.auto_deliver,
      ...(verification === undefined ? {} : { verification }),
    })),
  ),
)
const parseAcknowledgement: Decoder<true> = decode(acknowledged)
