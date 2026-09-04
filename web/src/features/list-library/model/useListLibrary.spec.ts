import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { invalidateCatalogCache } from '@/shared/api/catalog'

import { useListLibrary } from './useListLibrary'

type CategoryFixture = {
  custom: boolean
  id: string
  services: string[]
  title: string
}

const initialCategory: CategoryFixture = {
  custom: false,
  id: 'communication',
  services: ['discord'],
  title: 'Communication',
}

const services = [
  { categories: ['communication'], id: 'discord', title: 'Discord' },
  { categories: [], id: 'steam', title: 'Steam' },
]

function json(payload: unknown, status = 200): Response {
  return new Response(JSON.stringify(payload), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function catalogResponse(categories: CategoryFixture[]): Response {
  return json({
    categories,
    default_priority: services.map((service) => service.id),
    service_details: services,
    services: services.map((service) => service.id),
  })
}

function targetResponse(): Response {
  return json({ targets: [] })
}

function postCategoryResponse(category: CategoryFixture): Response {
  return json({ category }, 201)
}

describe('library audit: write/read reconciliation', () => {
  beforeEach(() => {
    invalidateCatalogCache()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    invalidateCatalogCache()
  })

  it('library audit: reports a committed stale category and recovers with GET only', async () => {
    const created: CategoryFixture = {
      custom: true,
      id: 'custom-created',
      services: [],
      title: 'Saved category',
    }
    let catalogReads = 0
    const calls: string[] = []
    vi.stubGlobal(
      'fetch',
      vi.fn((input: unknown, init?: RequestInit) => {
        const method = init?.method ?? 'GET'
        const path = String(input)
        calls.push(`${method} ${path}`)
        if (method === 'POST' && path === '/v1/categories')
          return Promise.resolve(postCategoryResponse(created))
        if (method === 'GET' && path === '/v1/services') {
          catalogReads += 1
          if (catalogReads === 2) return Promise.resolve(json({}, 503))
          return Promise.resolve(
            catalogResponse(
              catalogReads >= 3
                ? [initialCategory, created]
                : [initialCategory],
            ),
          )
        }
        if (method === 'GET' && path === '/v1/targets')
          return Promise.resolve(targetResponse())
        return Promise.resolve(json({ error: 'unrouted' }, 500))
      }),
    )

    const library = useListLibrary()
    await library.initialize()
    const result = await library.addCategory(created.title)

    expect(result.status).toBe('stale')
    expect(result.value?.id).toBe(created.id)
    expect(library.stale.value).toBe(true)
    expect(library.busy.value).toBe(false)
    expect(library.categories.value.map((category) => category.id)).toEqual([
      initialCategory.id,
    ])
    expect(calls.filter((call) => call === 'POST /v1/categories')).toHaveLength(
      1,
    )

    expect(await library.refresh()).toBe(true)
    expect(library.stale.value).toBe(false)
    expect(library.categories.value.map((category) => category.id)).toEqual([
      initialCategory.id,
      created.id,
    ])
    // Recovering the stale copy is a read, never a second mutation.
    expect(calls.filter((call) => call === 'POST /v1/categories')).toHaveLength(
      1,
    )
  })

  it('library audit: keeps stale data and does not remutate across repeated GET failures', async () => {
    const created: CategoryFixture = {
      custom: true,
      id: 'custom-created',
      services: [],
      title: 'Saved category',
    }
    let catalogReads = 0
    let failReads = false
    const calls: string[] = []
    vi.stubGlobal(
      'fetch',
      vi.fn((input: unknown, init?: RequestInit) => {
        const method = init?.method ?? 'GET'
        const path = String(input)
        calls.push(`${method} ${path}`)
        if (method === 'POST' && path === '/v1/categories')
          return Promise.resolve(postCategoryResponse(created))
        if (method === 'GET' && path === '/v1/services') {
          catalogReads += 1
          if (catalogReads > 1 && failReads)
            return Promise.resolve(json({}, 503))
          return Promise.resolve(catalogResponse([initialCategory]))
        }
        if (method === 'GET' && path === '/v1/targets')
          return Promise.resolve(targetResponse())
        return Promise.resolve(json({ error: 'unrouted' }, 500))
      }),
    )

    const library = useListLibrary()
    await library.initialize()
    failReads = true
    expect((await library.addCategory(created.title)).status).toBe('stale')
    const writes = calls.filter((call) => call === 'POST /v1/categories')

    expect(await library.refresh()).toBe(false)
    expect(await library.refresh()).toBe(false)
    expect(library.stale.value).toBe(true)
    expect(library.categories.value).toEqual([initialCategory])
    expect(calls.filter((call) => call === 'POST /v1/categories')).toEqual(
      writes,
    )
  })

  it('library audit: blocks stale membership edits without reading or writing', async () => {
    const widened: CategoryFixture = {
      ...initialCategory,
      services: ['discord', 'steam'],
    }
    let catalogReads = 0
    const calls: string[] = []
    vi.stubGlobal(
      'fetch',
      vi.fn((input: unknown, init?: RequestInit) => {
        const method = init?.method ?? 'GET'
        const path = String(input)
        calls.push(`${method} ${path}`)
        if (method === 'POST' && path === '/v1/categories/communication/update')
          return Promise.resolve(json({ category: widened }))
        if (method === 'GET' && path === '/v1/services') {
          catalogReads += 1
          if (catalogReads === 2) return Promise.resolve(json({}, 503))
          return Promise.resolve(
            catalogResponse(catalogReads >= 3 ? [widened] : [initialCategory]),
          )
        }
        if (method === 'GET' && path === '/v1/targets')
          return Promise.resolve(targetResponse())
        return Promise.resolve(json({ error: 'unrouted' }, 500))
      }),
    )

    const library = useListLibrary()
    await library.initialize()
    const category = library.categories.value[0]
    expect(category).toBeDefined()
    expect((await library.addList(category!, 'steam')).status).toBe('stale')

    const before = [...calls]
    const blocked = await library.detachList(category!, 'discord')
    expect(blocked.status).toBe('blocked')
    expect(calls).toEqual(before)
    expect(library.stale.value).toBe(true)
  })

  it('library audit: holds busy through reread and blocks a second write or refresh', async () => {
    const created: CategoryFixture = {
      custom: true,
      id: 'custom-delayed',
      services: [],
      title: 'Delayed category',
    }
    let catalogReads = 0
    let releaseRead!: (response: Response) => void
    const delayedRead = new Promise<Response>((resolve) => {
      releaseRead = resolve
    })
    const calls: string[] = []
    vi.stubGlobal(
      'fetch',
      vi.fn((input: unknown, init?: RequestInit) => {
        const method = init?.method ?? 'GET'
        const path = String(input)
        calls.push(`${method} ${path}`)
        if (method === 'POST' && path === '/v1/categories')
          return Promise.resolve(postCategoryResponse(created))
        if (method === 'GET' && path === '/v1/services') {
          catalogReads += 1
          if (catalogReads === 2) return delayedRead
          return Promise.resolve(
            catalogResponse(
              catalogReads >= 3
                ? [initialCategory, created]
                : [initialCategory],
            ),
          )
        }
        if (method === 'GET' && path === '/v1/targets')
          return Promise.resolve(targetResponse())
        return Promise.resolve(json({ error: 'unrouted' }, 500))
      }),
    )

    const library = useListLibrary()
    await library.initialize()
    const first = library.addCategory(created.title)
    await vi.waitFor(() => expect(catalogReads).toBe(2))
    expect(library.busy.value).toBe(true)

    const second = await library.addCategory('Should not be sent')
    expect(second.status).toBe('blocked')
    expect(await library.refresh()).toBe(false)
    expect(calls.filter((call) => call === 'POST /v1/categories')).toHaveLength(
      1,
    )
    expect(catalogReads).toBe(2)

    releaseRead(catalogResponse([initialCategory, created]))
    expect((await first).status).toBe('saved')
    expect(library.busy.value).toBe(false)
    expect(library.stale.value).toBe(false)
  })

  it('library audit: keeps input and route refusal when the write itself fails', async () => {
    const calls: string[] = []
    vi.stubGlobal(
      'fetch',
      vi.fn((input: unknown, init?: RequestInit) => {
        const method = init?.method ?? 'GET'
        const path = String(input)
        calls.push(`${method} ${path}`)
        if (method === 'GET' && path === '/v1/services')
          return Promise.resolve(catalogResponse([initialCategory]))
        if (method === 'GET' && path === '/v1/targets')
          return Promise.resolve(targetResponse())
        if (method === 'POST' && path === '/v1/categories/communication/update')
          return Promise.resolve(
            json(
              {
                error: 'category in use',
                lists: [{ id: 'route-a', title: 'Office' }],
              },
              409,
            ),
          )
        return Promise.resolve(json({ error: 'unrouted' }, 500))
      }),
    )

    const library = useListLibrary()
    await library.initialize()
    const result = await library.renameCategory('communication', 'Office')
    expect(result.status).toBe('failed')
    expect(library.refusal.value).toEqual({
      kind: 'inUse',
      routes: ['Office'],
    })
    expect(library.categories.value[0]?.title).toBe('Communication')
    expect(calls.filter((call) => call.includes('/v1/services'))).toHaveLength(
      1,
    )
  })

  it('saves only the complete library default order and updates it locally', async () => {
    const calls: { body: string | null; key: string }[] = []
    const saved = ['steam', 'discord']
    vi.stubGlobal(
      'fetch',
      vi.fn((input: unknown, init?: RequestInit) => {
        const key = `${init?.method ?? 'GET'} ${String(input)}`
        calls.push({ body: (init?.body as string | undefined) ?? null, key })
        if (key === 'GET /v1/services')
          return Promise.resolve(catalogResponse([initialCategory]))
        if (key === 'GET /v1/targets') return Promise.resolve(targetResponse())
        if (key === 'POST /v1/services/priority')
          return Promise.resolve(json({ default_priority: saved }))
        return Promise.resolve(json({ error: 'unrouted' }, 500))
      }),
    )

    const library = useListLibrary()
    await library.initialize()
    expect(library.defaultPriority.value).toEqual(['discord', 'steam'])
    expect((await library.setDefaultPriority(saved)).status).toBe('saved')
    expect(library.defaultPriority.value).toEqual(saved)
    expect(calls.at(-1)).toEqual({
      body: JSON.stringify({ default_priority: saved }),
      key: 'POST /v1/services/priority',
    })
    expect(calls.filter((call) => call.key.includes('/v1/lists'))).toHaveLength(
      0,
    )
  })

  it('merges multiple new lists in the same canonical order as the server', async () => {
    const calls: string[] = []
    const created = [
      {
        domains: ['later.example'],
        id: 'custom-z-later',
        title: 'Later',
      },
      {
        domains: ['earlier.example'],
        id: 'custom-a-earlier',
        title: 'Earlier',
      },
    ]
    let creation = 0
    vi.stubGlobal(
      'fetch',
      vi.fn((input: unknown, init?: RequestInit) => {
        const key = `${init?.method ?? 'GET'} ${String(input)}`
        calls.push(key)
        if (key === 'GET /v1/services')
          return Promise.resolve(catalogResponse([initialCategory]))
        if (key === 'GET /v1/targets') return Promise.resolve(targetResponse())
        if (key === 'POST /v1/services') {
          const service = created[creation]
          creation += 1
          return Promise.resolve(json({ service }, 201))
        }
        return Promise.resolve(json({ error: 'unrouted' }, 500))
      }),
    )

    const library = useListLibrary()
    await library.initialize()
    const first = created[0]
    const second = created[1]
    expect(first).toBeDefined()
    expect(second).toBeDefined()
    const firstResult = await library.addCustomList(
      first!.title,
      first!.domains,
    )
    const secondResult = await library.addCustomList(
      second!.title,
      second!.domains,
    )

    expect(firstResult.status).toBe('saved')
    expect(secondResult.status).toBe('saved')
    expect(library.defaultPriority.value).toEqual([
      'discord',
      'steam',
      'custom-a-earlier',
      'custom-z-later',
    ])
    expect(calls.filter((call) => call === 'GET /v1/services')).toHaveLength(1)
  })
})
