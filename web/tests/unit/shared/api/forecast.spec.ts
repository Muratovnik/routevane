/** What a forecast says about the entries two lists have in common. */
import { beforeEach, describe, expect, it } from 'vitest'

import { RoutevaneAPIError } from '@/shared/api/http'
import { previewComposition } from '@/shared/api/profiles'

import { answer, draft, forecastPayload, stubFetch } from './support/fixtures'

beforeEach(stubFetch)

describe('forecast overlap contract', () => {
  const entry = {
    rule_kind: 'domain_suffix',
    value: 'shared.example',
    lists: ['alpha', 'beta'],
  }
  it('keeps typed ownership and the explicit detail limit', async () => {
    answer(
      forecastPayload({
        overlaps: { items: [{ kind: 'duplicate', entry }], truncated: true },
      }),
    )
    const result = await previewComposition(draft)
    expect(result[0]?.overlaps).toEqual({
      items: [
        {
          kind: 'duplicate',
          entry: {
            ruleKind: 'domain_suffix',
            value: 'shared.example',
            lists: ['alpha', 'beta'],
          },
        },
      ],
      truncated: true,
    })
  })
  it('decodes complete list adjacency separately from capped details', async () => {
    answer(
      forecastPayload({
        overlaps: {
          items: [],
          truncated: true,
          summary: [
            { list_id: 'alpha', overlaps: ['beta'] },
            { list_id: 'beta', overlaps: ['alpha'] },
            { list_id: 'solo', overlaps: [] },
          ],
        },
      }),
    )
    const result = await previewComposition(draft)
    expect(result[0]?.overlaps?.summary).toEqual([
      { listID: 'alpha', overlaps: ['beta'] },
      { listID: 'beta', overlaps: ['alpha'] },
      { listID: 'solo', overlaps: [] },
    ])
  })
  it('refuses missing truncation, self-coverage and malformed relations', async () => {
    for (const overlaps of [
      { items: [] },
      { items: [{ kind: 'similar', entry }], truncated: false },
      {
        items: [{ kind: 'duplicate', entry: { ...entry, lists: ['alpha'] } }],
        truncated: false,
      },
      {
        items: [
          {
            kind: 'covered',
            entry: { ...entry, lists: ['alpha'] },
            covering: entry,
          },
        ],
        truncated: false,
      },
      { items: [{ kind: 'covered', entry }], truncated: false },
      {
        items: Array.from({ length: 101 }, () => ({
          kind: 'duplicate',
          entry,
        })),
        truncated: true,
      },
      {
        items: [],
        truncated: false,
        summary: [{ list_id: 'alpha', overlaps: ['alpha'] }],
      },
      {
        items: [],
        truncated: false,
        summary: [{ list_id: 'alpha', overlaps: ['beta', 'beta'] }],
      },
      {
        items: [],
        truncated: false,
        summary: [{ list_id: 'alpha', overlaps: ['zeta', 'beta'] }],
      },
      {
        items: [],
        truncated: false,
        summary: [
          { list_id: 'alpha', overlaps: ['beta'] },
          { list_id: 'beta', overlaps: [] },
        ],
      },
      {
        items: [],
        truncated: false,
        summary: [
          { list_id: 'beta', overlaps: [] },
          { list_id: 'beta', overlaps: [] },
        ],
      },
    ]) {
      answer(forecastPayload({ overlaps }))
      await expect(previewComposition(draft)).rejects.toBeInstanceOf(
        RoutevaneAPIError,
      )
    }
  })
})
