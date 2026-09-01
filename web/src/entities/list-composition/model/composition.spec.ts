import { describe, expect, it } from 'vitest'
import { reactive } from 'vue'

import {
  categorySelectionState,
  compositionSignature,
  resolvedComposition,
  toggleCompositionCategory,
  toggleCompositionService,
} from './composition'

const categories = [
  {
    custom: false,
    id: 'communication',
    title: 'Communication',
    services: ['discord', 'telegram'],
  },
  {
    custom: false,
    id: 'video',
    title: 'Video',
    services: ['youtube'],
  },
]

describe('list composition selection', () => {
  it('selects a collection as one reference and resolves all of its members', () => {
    const selected = toggleCompositionCategory(
      {
        categories: [],
        exclusions: [],
        serviceDomains: {},
        services: [],
      },
      categories,
      'communication',
    )

    expect(selected).toEqual({
      categories: ['communication'],
      exclusions: [],
      serviceDomains: {},
      services: [],
    })
    expect(resolvedComposition(selected, categories)).toEqual([
      'discord',
      'telegram',
    ])
  })

  it('lets one service be removed from a selected collection', () => {
    const category = {
      categories: ['communication'],
      exclusions: [],
      serviceDomains: {},
      services: [],
    }

    const withoutDiscord = toggleCompositionService(
      category,
      categories,
      'discord',
    )

    expect(withoutDiscord.exclusions).toEqual(['discord'])
    expect(resolvedComposition(withoutDiscord, categories)).toEqual([
      'telegram',
    ])
    expect(
      toggleCompositionService(withoutDiscord, categories, 'discord'),
    ).toEqual(category)
  })

  it('keeps a hand-picked service independent of unrelated collections', () => {
    const named = toggleCompositionService(
      {
        categories: [],
        exclusions: [],
        serviceDomains: {},
        services: [],
      },
      categories,
      'youtube',
    )
    const withCollection = toggleCompositionCategory(
      named,
      categories,
      'communication',
    )

    expect(withCollection.services).toEqual(['youtube'])
    expect(resolvedComposition(withCollection, categories)).toEqual([
      'discord',
      'telegram',
      'youtube',
    ])
  })

  it('reports a mixed parent state for hand-picked members', () => {
    const composition = {
      categories: [],
      exclusions: [],
      serviceDomains: {},
      services: ['discord'],
    }

    expect(
      categorySelectionState(composition, categories, 'communication'),
    ).toBe('partial')
  })

  it('selects all members from a mixed state and clears all from a full state', () => {
    const mixed = {
      categories: [],
      exclusions: [],
      serviceDomains: {},
      services: ['discord'],
    }
    const full = toggleCompositionCategory(mixed, categories, 'communication')

    expect(full).toEqual({
      categories: ['communication'],
      exclusions: [],
      serviceDomains: {},
      services: [],
    })
    expect(categorySelectionState(full, categories, 'communication')).toBe(
      'all',
    )
    expect(
      toggleCompositionCategory(full, categories, 'communication'),
    ).toEqual({
      categories: [],
      exclusions: [],
      serviceDomains: {},
      services: [],
    })
  })
})

// What a composition selects is the fact; the order it was written down in is
// not. Everything that asks "has this changed?" has to agree with that, or a
// save control lights up for a rewrite of the same list.
describe('composition identity', () => {
  it('reads two writings of the same selection as one', () => {
    const written = {
      categories: ['video', 'communication'],
      exclusions: ['telegram', 'discord'],
      serviceDomains: {
        youtube: ['b.example', 'a.example'],
        discord: ['d.example'],
      },
      services: ['youtube', 'youtube'],
    }
    const rewritten = {
      categories: ['communication', 'video'],
      exclusions: ['discord', 'telegram'],
      serviceDomains: {
        discord: ['d.example'],
        youtube: ['a.example', 'b.example'],
      },
      services: ['youtube'],
    }

    expect(compositionSignature(written, categories)).toBe(
      compositionSignature(rewritten, categories),
    )
  })

  it('separates a selection that actually differs', () => {
    const base = {
      categories: ['communication'],
      exclusions: [],
      serviceDomains: {},
      services: [],
    }

    expect(compositionSignature(base, categories)).not.toBe(
      compositionSignature({ ...base, exclusions: ['discord'] }, categories),
    )
    expect(compositionSignature(base, categories)).not.toBe(
      compositionSignature(
        { ...base, serviceDomains: { discord: ['a.example'] } },
        categories,
      ),
    )
  })

  // Components hand these helpers reactive state, and the structured-clone
  // algorithm refuses a proxy outright.
  it('copies a composition a component owns', () => {
    const owned = reactive({
      categories: [] as string[],
      exclusions: [] as string[],
      serviceDomains: { youtube: ['a.example'] } as Record<string, string[]>,
      services: ['youtube'],
    })

    const next = toggleCompositionService(owned, categories, 'discord')

    expect(next.services).toEqual(['discord', 'youtube'])
    expect(next.serviceDomains).toEqual({ youtube: ['a.example'] })
    // The copy is a copy: editing it never reaches back into the component.
    next.services.push('telegram')
    expect(owned.services).toEqual(['youtube'])
  })
})
