import { describe, expect, it } from 'vitest'
import { reactive } from 'vue'

import {
  applyDefaultPriority,
  categorySelectionState,
  compositionSignature,
  moveCompositionPriority,
  overlapListIDs,
  resolvedComposition,
  listIdentityLabels,
  setCompositionCategoryReference,
  toggleCompositionCategory,
  toggleCompositionList,
} from './composition'

const categories = [
  {
    custom: false,
    id: 'communication',
    title: 'Communication',
    lists: ['discord', 'telegram'],
  },
  {
    custom: false,
    id: 'video',
    title: 'Video',
    lists: ['youtube'],
  },
]

describe('list composition selection', () => {
  it('selects a collection as one reference and resolves all of its members', () => {
    const selected = toggleCompositionCategory(
      {
        categories: [],
        exclusions: [],
        listDomains: {},
        lists: [],
      },
      categories,
      'communication',
    )

    expect(selected).toEqual({
      categories: ['communication'],
      exclusions: [],
      priority: ['discord', 'telegram'],
      listDomains: {},
      lists: [],
    })
    expect(resolvedComposition(selected, categories)).toEqual([
      'discord',
      'telegram',
    ])
  })

  it('lets one list be removed from a selected collection', () => {
    const category = {
      categories: ['communication'],
      exclusions: [],
      priority: ['discord', 'telegram'],
      listDomains: {},
      lists: [],
    }

    const withoutDiscord = toggleCompositionList(
      category,
      categories,
      'discord',
    )

    expect(withoutDiscord.exclusions).toEqual(['discord'])
    expect(withoutDiscord.priority).toEqual(['telegram'])
    expect(resolvedComposition(withoutDiscord, categories)).toEqual([
      'telegram',
    ])
    expect(
      toggleCompositionList(withoutDiscord, categories, 'discord'),
    ).toEqual({ ...category, priority: ['telegram', 'discord'] })
  })

  it('keeps a hand-picked list independent of unrelated collections', () => {
    const named = toggleCompositionList(
      {
        categories: [],
        exclusions: [],
        priority: [],
        listDomains: {},
        lists: [],
      },
      categories,
      'youtube',
    )
    const withCollection = toggleCompositionCategory(
      named,
      categories,
      'communication',
    )

    expect(withCollection.lists).toEqual(['youtube'])
    expect(resolvedComposition(withCollection, categories)).toEqual([
      'youtube',
      'discord',
      'telegram',
    ])
  })

  it('reports a mixed parent state for hand-picked members', () => {
    const composition = {
      categories: [],
      exclusions: [],
      listDomains: {},
      lists: ['discord'],
    }

    expect(
      categorySelectionState(composition, categories, 'communication'),
    ).toBe('partial')
  })

  it('selects all members from a mixed state and clears all from a full state', () => {
    const mixed = {
      categories: [],
      exclusions: [],
      listDomains: {},
      lists: ['discord'],
    }
    const full = toggleCompositionCategory(mixed, categories, 'communication')

    expect(full).toEqual({
      categories: ['communication'],
      exclusions: [],
      priority: ['discord', 'telegram'],
      listDomains: {},
      lists: [],
    })
    expect(categorySelectionState(full, categories, 'communication')).toBe(
      'all',
    )
    expect(
      toggleCompositionCategory(full, categories, 'communication'),
    ).toEqual({
      categories: [],
      exclusions: [],
      priority: [],
      listDomains: {},
      lists: [],
    })
  })

  it('keeps a live category reference legible while individual exclusions change', () => {
    const followed = setCompositionCategoryReference(
      {
        categories: [],
        exclusions: [],
        listDomains: {},
        lists: ['discord'],
      },
      categories,
      'communication',
      true,
    )
    const partial = toggleCompositionList(followed, categories, 'discord')

    expect(partial.categories).toEqual(['communication'])
    expect(partial.exclusions).toEqual(['discord'])
    expect(
      setCompositionCategoryReference(
        partial,
        categories,
        'communication',
        false,
      ),
    ).toMatchObject({ categories: [], exclusions: [], lists: [] })
  })
})

// Selection arrays are sets, while priority is an operator-owned order.
// Everything that asks "has this changed?" must preserve that distinction.
describe('composition identity', () => {
  it('reads two writings of the same selection as one', () => {
    const written = {
      categories: ['video', 'communication'],
      exclusions: ['telegram', 'discord'],
      listDomains: {
        youtube: ['b.example', 'a.example'],
        discord: ['d.example'],
      },
      lists: ['youtube', 'youtube'],
    }
    const rewritten = {
      categories: ['communication', 'video'],
      exclusions: ['discord', 'telegram'],
      listDomains: {
        discord: ['d.example'],
        youtube: ['a.example', 'b.example'],
      },
      lists: ['youtube'],
    }

    expect(compositionSignature(written, categories)).toBe(
      compositionSignature(rewritten, categories),
    )
  })

  it('separates a selection that actually differs', () => {
    const base = {
      categories: ['communication'],
      exclusions: [],
      listDomains: {},
      lists: [],
    }

    expect(compositionSignature(base, categories)).not.toBe(
      compositionSignature({ ...base, exclusions: ['discord'] }, categories),
    )
    expect(compositionSignature(base, categories)).not.toBe(
      compositionSignature(
        { ...base, listDomains: { discord: ['a.example'] } },
        categories,
      ),
    )
    expect(compositionSignature(base, categories)).not.toBe(
      compositionSignature(
        { ...base, priority: ['telegram', 'discord'] },
        categories,
      ),
    )
  })

  it('keeps explicit priority and appends a newly carried list', () => {
    const composition = {
      categories: ['communication'],
      exclusions: [],
      priority: ['telegram', 'discord'],
      listDomains: {},
      lists: ['youtube'],
    }

    expect(resolvedComposition(composition, categories)).toEqual([
      'telegram',
      'discord',
      'youtube',
    ])
    expect(
      moveCompositionPriority(composition, categories, 2, 0).priority,
    ).toEqual(['youtube', 'telegram', 'discord'])
  })

  it('orders a new draft by the library default without adding unselected lists', () => {
    const ordered = applyDefaultPriority(
      {
        categories: ['communication'],
        exclusions: [],
        priority: ['discord', 'telegram'],
        listDomains: {},
        lists: ['youtube'],
      },
      categories,
      ['youtube', 'telegram', 'discord', 'missing'],
    )

    expect(ordered.priority).toEqual(['youtube', 'telegram', 'discord'])
  })

  it('distinguishes an old forecast from a confirmed empty overlap row', () => {
    const base = {
      fits: true,
      maximumRules: 10,
      perList: [],
      projectedRules: 2,
      targetID: 'target',
    }
    expect(overlapListIDs(base, 'discord')).toBeNull()
    expect(
      overlapListIDs(
        {
          ...base,
          overlaps: {
            items: [],
            summary: [{ listID: 'discord', overlaps: [] }],
            truncated: false,
          },
        },
        'discord',
      ),
    ).toEqual([])
  })

  it('adds a stable disambiguator only when list titles collide', () => {
    const labels = listIdentityLabels([
      { categories: [], id: 'custom-alpha', title: 'Shared' },
      { categories: [], id: 'custom-beta', title: 'Shared' },
      { categories: [], id: 'unique', title: 'Unique' },
    ])

    expect(labels.get('custom-alpha')).toBe('Shared · custom-a…')
    expect(labels.get('custom-beta')).toBe('Shared · custom-b…')
    expect(labels.get('unique')).toBe('Unique')
  })

  // Components hand these helpers reactive state, and the structured-clone
  // algorithm refuses a proxy outright.
  it('copies a composition a component owns', () => {
    const owned = reactive({
      categories: [] as string[],
      exclusions: [] as string[],
      listDomains: { youtube: ['a.example'] } as Record<string, string[]>,
      lists: ['youtube'],
    })

    const next = toggleCompositionList(owned, categories, 'discord')

    expect(next.lists).toEqual(['discord', 'youtube'])
    expect(next.listDomains).toEqual({ youtube: ['a.example'] })
    // The copy is a copy: editing it never reaches back into the component.
    next.lists.push('telegram')
    expect(owned.lists).toEqual(['youtube'])
  })
})
