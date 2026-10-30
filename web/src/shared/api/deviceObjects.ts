import * as v from 'valibot'

import {
  count,
  decode,
  type Decoder,
  fields,
  getJSON,
  optionalFlag,
  optionalText,
  text,
} from './http'

// What a format delivered and the registry still remembers, router by router
// (ADR 0060). Reading it contacts no device. A surface that cannot be cleaned
// says why: its connection is gone, it has no saved password, or its format
// cannot remove what it delivered.
export type DeviceObjectsBlock =
  'connection_missing' | 'credential_missing' | 'removal_unsupported'

export type DeviceObjectsSurface = {
  outputID: string
  targetID: string
  endpoint: string
  // The interface the surface's routes go through; empty for FQDN groups.
  interfaceName: string
  connectionName: string
  cleanable: boolean
  blockedBy: DeviceObjectsBlock | ''
  fqdnGroups: number
  // Routes this format alone placed, which removal takes away; routes another
  // format or the operator also holds stay.
  routes: number
}

export type DeviceObjects = {
  surfaces: DeviceObjectsSurface[]
  // Every surface can be cleaned; true for none at all.
  cleanable: boolean
}

const surfaceSchema = v.pipe(
  fields({
    output_id: text,
    target_id: text,
    endpoint: optionalText,
    interface: optionalText,
    connection: v.optional(fields({ name: optionalText })),
    cleanable: optionalFlag,
    blocked_by: v.optional(
      v.picklist([
        'connection_missing',
        'credential_missing',
        'removal_unsupported',
      ]),
    ),
    fqdn_groups: v.optional(v.array(text), []),
    routes: v.optional(fields({ removable: v.optional(count, 0) })),
  }),
  v.transform((surface): DeviceObjectsSurface => ({
    outputID: surface.output_id,
    targetID: surface.target_id,
    endpoint: surface.endpoint,
    interfaceName: surface.interface,
    connectionName: surface.connection?.name ?? '',
    cleanable: surface.cleanable,
    blockedBy: surface.blocked_by ?? '',
    fqdnGroups: surface.fqdn_groups.length,
    routes: surface.routes?.removable ?? 0,
  })),
)

const parseDeviceObjects: Decoder<DeviceObjects> = decode(
  v.pipe(
    fields({
      device_objects: v.array(surfaceSchema),
      cleanable: optionalFlag,
    }),
    v.transform((answer): DeviceObjects => ({
      surfaces: answer.device_objects,
      cleanable: answer.cleanable,
    })),
  ),
)

export const loadOutputDeviceObjects = (
  outputID: string,
): Promise<DeviceObjects> =>
  getJSON(`/v1/outputs/${outputID}/device-objects`, parseDeviceObjects)

export const loadProfileDeviceObjects = (
  profileID: string,
): Promise<DeviceObjects> =>
  getJSON(`/v1/profiles/${profileID}/device-objects`, parseDeviceObjects)

// The body of a retirement: empty keeps the routers as they are, the opt-in
// removes what the format delivered first and retires nothing if that fails.
export const retirementBody = (
  removeFromDevices: boolean,
): Record<string, boolean> =>
  removeFromDevices ? { remove_from_devices: true } : {}
