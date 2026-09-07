import * as v from 'valibot'

import { decode, type Decoder, fields, getJSON, postJSON } from './http'
import { type RefreshInterval, refreshRule } from './profiles'

type DefaultSchedule = { refreshInterval: RefreshInterval }

export function loadSettings(): Promise<DefaultSchedule> {
  return getJSON('/v1/settings', parseSettings)
}

export function saveDefaultRefreshInterval(
  interval: RefreshInterval,
): Promise<DefaultSchedule> {
  return postJSON(
    '/v1/settings/update',
    { refresh_interval: interval },
    parseSettings,
  )
}

// The service-wide default answers the same question a list does, so it is read
// through the same rule.
const settingsSchema = v.pipe(
  fields({ refresh_interval: refreshRule }),
  v.transform((settings): DefaultSchedule => ({
    refreshInterval: settings.refresh_interval,
  })),
)

const parseSettings: Decoder<DefaultSchedule> = decode(settingsSchema)
