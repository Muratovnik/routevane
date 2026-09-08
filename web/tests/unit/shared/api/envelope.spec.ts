/**
 * What every reply has to be before any endpoint's own fields are read: a
 * value the screen can print, and an envelope that separates a broken reply
 * from a refusal.
 */
import { beforeEach, describe, expect, it } from 'vitest'

import { loadListContents, refreshList } from '@/shared/api/catalog'
import { RoutevaneAPIError } from '@/shared/api/http'
import { addOutput } from '@/shared/api/outputs'
import { loadProfiles, previewComposition } from '@/shared/api/profiles'

import {
  answer,
  draft,
  fetchMock,
  forecastPayload,
  profileID,
  profilePayload,
  stubFetch,
} from './support/fixtures'

beforeEach(stubFetch)

describe('the shape of a value', () => {
  // A count is a whole, non-negative, exactly representable number. Everything
  // else is a claim the screen cannot print, whatever it looks like.
  it('admits only a safe non-negative integer as a count', async () => {
    answer(forecastPayload({ maximum_rules: 0 }))
    await expect(previewComposition(draft)).resolves.toMatchObject([
      { maximumRules: 0 },
    ])

    for (const value of [12.5, -1, 2 ** 53, '1024', null, Number.NaN]) {
      answer(forecastPayload({ maximum_rules: value }))
      await expect(previewComposition(draft)).rejects.toBeInstanceOf(
        RoutevaneAPIError,
      )
    }
  })

  // An empty string is how this API says "absent", so it is never a stated
  // value.
  it('refuses an empty string where a value is required', async () => {
    answer(profilePayload({ name: '' }))
    await expect(loadProfiles()).rejects.toBeInstanceOf(RoutevaneAPIError)

    answer(profilePayload({ name: 42 }))
    await expect(loadProfiles()).rejects.toBeInstanceOf(RoutevaneAPIError)
  })

  // A moment is admitted by being placeable on a clock and kept in the
  // server's own spelling, so what round-trips is what was published.
  it('keeps a timestamp as stated and refuses one no clock can place', async () => {
    answer(profilePayload({ created_at: '2026-08-20T12:00:00+03:00' }))
    await expect(loadProfiles()).resolves.toMatchObject([
      { createdAt: '2026-08-20T12:00:00+03:00' },
    ])

    answer(profilePayload({ created_at: 'whenever' }))
    await expect(loadProfiles()).rejects.toBeInstanceOf(RoutevaneAPIError)
  })

  // An array is never a record here, and a schema of optional fields must not
  // quietly accept one.
  it('refuses an array where the contract states an object', async () => {
    answer([])
    await expect(loadListContents('discord')).rejects.toBeInstanceOf(
      RoutevaneAPIError,
    )

    answer([])
    await expect(refreshList('discord')).rejects.toBeInstanceOf(
      RoutevaneAPIError,
    )

    answer({ refresh: {} })
    await expect(refreshList('discord')).resolves.toEqual({
      skippedEntries: 0,
    })
  })

  it('preserves skipped source entries and refuses malformed counts', async () => {
    answer({ refresh: { skipped_entries: 2 } })
    await expect(refreshList('kinopub')).resolves.toEqual({
      skippedEntries: 2,
    })
    for (const payload of [
      {},
      { refresh: [] },
      { refresh: { skipped_entries: -1 } },
      { refresh: { skipped_entries: 1.5 } },
      { refresh: { skipped_entries: '1' } },
    ]) {
      answer(payload)
      await expect(refreshList('kinopub')).rejects.toBeInstanceOf(
        RoutevaneAPIError,
      )
    }
  })
})

describe('the envelope every endpoint shares', () => {
  it('separates a broken reply, a refusal and a reply outside the contract', async () => {
    fetchMock.mockResolvedValueOnce(new Response('not json', { status: 200 }))
    await expect(loadProfiles()).rejects.toThrow(
      'Сервер вернул некорректный ответ.',
    )

    answer(
      {
        error: 'operation failed',
        code: 'rule_limit',
        projected_rules: 1740,
        maximum_rules: 1024,
      },
      422,
    )
    const refused = await addOutput(profileID, 'keenetic').catch(
      (reason: unknown) => reason,
    )
    expect(refused).toMatchObject({
      message: 'operation failed',
      code: 'rule_limit',
      details: { projected_rules: 1740, maximum_rules: 1024 },
      status: 422,
    })

    answer({ profiles: [{ id: profileID }] })
    await expect(loadProfiles()).rejects.toThrow(
      'Сервер вернул ответ вне контракта.',
    )
  })

  // A refusal that states one of its counts badly still delivers the rest.
  it('reads each part of a refusal on its own', async () => {
    answer({ error: 'too many', code: 'rule_limit', projected_rules: -1 }, 422)
    const refused = await addOutput(profileID, 'keenetic').catch(
      (reason: unknown) => reason,
    )
    expect(refused).toMatchObject({ code: 'rule_limit', details: {} })
  })
})
