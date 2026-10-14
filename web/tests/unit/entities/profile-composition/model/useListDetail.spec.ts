import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-vue'
import { defineComponent, h, nextTick, reactive } from 'vue'

import {
  useListDetail,
  type ListDetailProps,
} from '@/entities/profile-composition/model/useListDetail'
import { invalidateCatalogCache } from '@/shared/api/catalog'
import { useLocale } from '@/shared/i18n/useLocale'

const BYTE_LIMIT = 1024 * 1024
const json = (payload: unknown, status = 200): Response =>
  new Response(JSON.stringify(payload), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })

const contents = (listID = 'discord', observed = true) => ({
  list_id: listID,
  rows: [],
  sources: observed
    ? []
    : [{ id: 'feed', type: 'http', custom: false, enabled: true }],
  observed,
})

const mount = async (initial?: ListDetailProps) => {
  const props = reactive<ListDetailProps>(
    initial ?? {
      mode: 'library',
      list: { id: 'discord', title: 'Discord', categories: [] },
    },
  )
  const report = {
    close: vi.fn(),
    created: vi.fn(),
    updated: vi.fn(),
    remove: vi.fn(),
  }
  let model: ReturnType<typeof useListDetail> | undefined
  const screen = await render(
    defineComponent({
      setup: () => {
        model = useListDetail(props, report)
        return () => h('div')
      },
    }),
  )
  return { model: model!, props, report, unmount: screen.unmount }
}

// A response body is read on the browser's task queue. Let it settle before a
// negative assertion about a follow-up request; no wall-clock delay is needed.
const browserTurn = (): Promise<void> =>
  new Promise((resolve) => {
    const channel = new MessageChannel()
    channel.port1.addEventListener(
      'message',
      () => {
        channel.port1.close()
        channel.port2.close()
        resolve()
      },
      { once: true },
    )
    channel.port1.start()
    channel.port2.postMessage(null)
  })

beforeEach(() => {
  useLocale().setLocale('en')
  invalidateCatalogCache()
})

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

it('rejects an oversized file before reading it and preserves the typed draft', async () => {
  const fetchMock = vi.fn(async () => json(contents()))
  vi.stubGlobal('fetch', fetchMock)
  const { model } = await mount()
  await vi.waitFor(() => expect(model.contentsState.value).toBe('ready'))
  model.addValuesDraft.value = 'typed.example.test'
  model.onAddValuesOpen(true)
  const file = new File(['x'.repeat(BYTE_LIMIT + 1)], 'large.txt')
  const read = vi.spyOn(file, 'text')

  await model.onImportFile(file)

  expect(read).not.toHaveBeenCalled()
  expect(fetchMock).toHaveBeenCalledTimes(1)
  expect(model.addError.value).toBe(useLocale().t('listCard.import.tooLarge'))
  expect(model.addValuesDraft.value).toBe('typed.example.test')
  expect(model.addValuesOpen.value).toBe(true)
  expect(model.saving.value).toBe(false)
})

it('accepts exactly 1 MiB and sends parsed entries through the normal endpoint', async () => {
  const fetchMock = vi.fn(async (_input: unknown, _init?: RequestInit) =>
    json(contents()),
  )
  vi.stubGlobal('fetch', fetchMock)
  const { model } = await mount()
  await vi.waitFor(() => expect(model.contentsState.value).toBe('ready'))
  const prefix = 'example.test\n#'
  const file = new File(
    [prefix, 'x'.repeat(BYTE_LIMIT - prefix.length)],
    'exact.txt',
  )
  const read = vi.spyOn(file, 'text')

  await model.onImportFile(file)

  expect(file.size).toBe(BYTE_LIMIT)
  expect(read).toHaveBeenCalledOnce()
  expect(fetchMock).toHaveBeenCalledTimes(2)
  expect(fetchMock.mock.calls[1]?.[0]).toBe('/v1/lists/discord/domains')
  expect(JSON.parse(String(fetchMock.mock.calls[1]?.[1]?.body))).toEqual({
    values: ['example.test'],
    verdict: 'include',
  })
  expect(model.addError.value).toBe('')
  expect(model.saving.value).toBe(false)
})

it('keeps the draft and panel after a file read failure so another file can be chosen', async () => {
  const fetchMock = vi.fn(async () => json(contents()))
  vi.stubGlobal('fetch', fetchMock)
  const { model } = await mount()
  await vi.waitFor(() => expect(model.contentsState.value).toBe('ready'))
  model.addValuesDraft.value = 'typed.example.test'
  model.onAddValuesOpen(true)
  const file = new File(['example.test'], 'unreadable.txt')
  vi.spyOn(file, 'text').mockRejectedValue(new Error('read failed'))

  await model.onImportFile(file)

  expect(model.addError.value).toBe(useLocale().t('listCard.action.failed'))
  expect(model.addValuesDraft.value).toBe('typed.example.test')
  expect(model.addValuesOpen.value).toBe(true)
  expect(model.saving.value).toBe(false)
  expect(fetchMock).toHaveBeenCalledTimes(1)
})

it('does not start source work after the card unmounts with an unread contents request', async () => {
  const held = Promise.withResolvers<Response>()
  const fetchMock = vi.fn(() => held.promise)
  vi.stubGlobal('fetch', fetchMock)
  const { unmount } = await mount()
  expect(fetchMock).toHaveBeenCalledOnce()

  await unmount()
  held.resolve(json(contents('discord', false)))
  await browserTurn()
  await browserTurn()

  expect(fetchMock).toHaveBeenCalledOnce()
})

it('does not send a file that finishes reading after its card unmounts', async () => {
  const fetchMock = vi.fn(async () => json(contents()))
  vi.stubGlobal('fetch', fetchMock)
  const { model, unmount } = await mount()
  await vi.waitFor(() => expect(model.contentsState.value).toBe('ready'))
  const held = Promise.withResolvers<string>()
  const file = new File(['example.test'], 'held.txt')
  vi.spyOn(file, 'text').mockReturnValue(held.promise)
  const pending = model.onImportFile(file)
  await unmount()
  held.resolve('example.test')
  await pending
  expect(fetchMock).toHaveBeenCalledOnce()
})

it('stops issuing import batches when the card unmounts during a write', async () => {
  const held = Promise.withResolvers<Response>()
  const fetchMock = vi.fn((input: unknown) =>
    String(input).endsWith('/domains')
      ? held.promise
      : Promise.resolve(json(contents())),
  )
  vi.stubGlobal('fetch', fetchMock)
  const { model, unmount } = await mount()
  await vi.waitFor(() => expect(model.contentsState.value).toBe('ready'))
  const file = new File(
    [
      Array.from({ length: 1025 }, (_, index) => `entry-${index}.test`).join(
        '\n',
      ),
    ],
    'batch.txt',
  )
  const pending = model.onImportFile(file)
  await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
  await unmount()
  held.resolve(json(contents()))
  await pending
  expect(fetchMock).toHaveBeenCalledTimes(2)
})

it('does not clear a new card draft when the previous card write completes', async () => {
  const held = Promise.withResolvers<Response>()
  const fetchMock = vi.fn((input: unknown) => {
    const path = String(input)
    if (path.endsWith('/domains')) return held.promise
    return Promise.resolve(
      json(contents(path.includes('youtube') ? 'youtube' : 'discord')),
    )
  })
  vi.stubGlobal('fetch', fetchMock)
  const { model, props } = await mount()
  await vi.waitFor(() => expect(model.contentsState.value).toBe('ready'))
  model.addValuesDraft.value = 'old.example.test'
  const pending = model.onAddValues()
  props.list = { id: 'youtube', title: 'YouTube', categories: [] }
  await nextTick()
  await vi.waitFor(() => expect(model.contents.value?.listID).toBe('youtube'))
  model.addValuesDraft.value = 'new.example.test'
  model.onAddValuesOpen(true)
  held.resolve(json(contents()))
  await pending
  expect(model.contents.value?.listID).toBe('youtube')
  expect(model.addValuesDraft.value).toBe('new.example.test')
  expect(model.addValuesOpen.value).toBe(true)
})

it('reports a newly created list and closes after the write has finished', async () => {
  const fetchMock = vi.fn(async () =>
    json({
      list: { id: 'custom-list', title: 'My list', domains: ['example.test'] },
    }),
  )
  vi.stubGlobal('fetch', fetchMock)
  const { model, report } = await mount({
    mode: 'library',
    creating: true,
    list: null,
  })
  model.titleDraft.value = 'My list'
  model.domainsDraft.value = 'example.test'
  await model.onCreate()
  expect(report.created).toHaveBeenCalledExactlyOnceWith(
    expect.objectContaining({ id: 'custom-list', title: 'My list' }),
  )
  expect(report.close).toHaveBeenCalledOnce()
  expect(model.saving.value).toBe(false)
})

it('does not emit a late creation result into an unmounted card', async () => {
  const held = Promise.withResolvers<Response>()
  vi.stubGlobal(
    'fetch',
    vi.fn(() => held.promise),
  )
  const { model, report, unmount } = await mount({
    mode: 'library',
    creating: true,
    list: null,
  })
  model.titleDraft.value = 'My list'
  model.domainsDraft.value = 'example.test'
  const pending = model.onCreate()
  await unmount()
  held.resolve(
    json({
      list: { id: 'custom-list', title: 'My list', domains: ['example.test'] },
    }),
  )
  await pending
  expect(report.created).not.toHaveBeenCalled()
  expect(report.close).not.toHaveBeenCalled()
})

// A write that did not land says what happened: the service answered with an
// error, or nothing answered and the service is the thing to check.
const NO_ANSWER =
  'The change could not be saved. The local service did not answer. Check that Routevane is running.'
const ERROR_ANSWER =
  'The change could not be saved. The service received the request but answered with an error. Try again.'

const failedWrite = {
  'an error answer': () => Promise.resolve(json({ error: 'refused' }, 500)),
  'a lost request': () => Promise.reject(new TypeError('Failed to fetch')),
} as const

it.each([
  ['an error answer', ERROR_ANSWER],
  ['a lost request', NO_ANSWER],
] as const)(
  'words a failed creation by what happened: %s',
  async (kind, words) => {
    vi.stubGlobal('fetch', vi.fn(failedWrite[kind]))
    const { model, report } = await mount({
      mode: 'library',
      creating: true,
      list: null,
    })
    model.titleDraft.value = 'My list'
    model.domainsDraft.value = 'example.test'

    await model.onCreate()

    expect(model.domainsError.value).toBe(words)
    expect(report.created).not.toHaveBeenCalled()
    expect(model.saving.value).toBe(false)
  },
)

it.each([
  ['an error answer', ERROR_ANSWER],
  ['a lost request', NO_ANSWER],
] as const)(
  'words a failed rename by what happened: %s',
  async (kind, words) => {
    vi.stubGlobal(
      'fetch',
      vi.fn((_input: unknown, init?: RequestInit) =>
        init?.method === 'POST'
          ? failedWrite[kind]()
          : Promise.resolve(json(contents())),
      ),
    )
    const { model, report } = await mount({
      mode: 'library',
      list: { id: 'mine', title: 'Mine', categories: [], custom: true },
    })
    await vi.waitFor(() => expect(model.contentsState.value).toBe('ready'))
    model.titleDraft.value = 'Renamed'

    await model.onRenameCustom()

    expect(model.titleError.value).toBe(words)
    expect(report.updated).not.toHaveBeenCalled()
  },
)
