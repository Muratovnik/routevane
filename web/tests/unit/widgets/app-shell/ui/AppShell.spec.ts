import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-vue'
import { nextTick, toValue, watchEffect, type MaybeRefOrGetter } from 'vue'

import { useLocale } from '@/shared/i18n/useLocale'
import {
  checkService,
  clearSectionFailure,
  onSectionRecovery,
  reportDevDisconnected,
  reportReachable,
  reportSectionFailure,
  reportTransportFailure,
} from '@/shared/model/useServiceHealth'
import AppShell from '@/widgets/app-shell/ui/AppShell.vue'

type HeadInput = { htmlAttrs?: { lang?: MaybeRefOrGetter<string> } }

/**
 * The head manager, reduced to the one thing this asserts: it applies what it
 * is given and keeps applying it. The real one is Nuxt's; what belongs to this
 * repository is which value the shell hands it and whether that value tracks
 * the operator's choice.
 */
const stubNuxtContext = (): void => {
  vi.stubGlobal('useHead', (input: HeadInput) => {
    watchEffect(() => {
      const lang = input.htmlAttrs?.lang
      if (lang !== undefined) document.documentElement.lang = toValue(lang)
    })
  })
  vi.stubGlobal('useRoute', () => ({ path: '/' }))
}

const draw = async () =>
  render(AppShell, {
    global: {
      stubs: { NuxtLink: { props: ['to'], template: '<a><slot /></a>' } },
    },
  })

const stubService = (respond: (path: string) => Promise<Response>) => {
  const service = vi.fn(respond)
  vi.stubGlobal('fetch', service)
  return service
}

/** Drives the surface to a service that does not answer, the way one does. */
const loseTheService = async (): Promise<void> => {
  stubService(() => Promise.reject(new TypeError('offline')))
  reportTransportFailure()
  await checkService()
}

describe('AppShell', () => {
  beforeEach(() => {
    stubNuxtContext()
    document.documentElement.lang = 'xx'
    useLocale().setLocale('en')
  })

  afterEach(() => {
    // The surface's own state is one value for every screen, so a case hands it
    // back answering and quiet rather than leaving it to the next one — the
    // registered recovery included, which the application installs once and a
    // case replaces to watch what the shell asks for.
    reportReachable()
    clearSectionFailure()
    reportDevDisconnected(false)
    onSectionRecovery(async () => undefined)
    vi.unstubAllGlobals()
  })

  // A screen reader told `en` pronounces Russian copy as English, so the
  // declared language and the words on the page are one statement — stated when
  // the shell first renders and restated on every switch.
  it('declares the language at start and again on a switch', async () => {
    const { setLocale } = useLocale()
    setLocale('en')

    const screen = await draw()
    expect(document.documentElement.lang).toBe('en')
    await expect.element(screen.getByText('Routevane')).toBeVisible()
    // The mark beside the product name is decoration, so it is kept out of the
    // accessibility tree rather than read out as a second product name.
    await expect
      .element(screen.getByTestId('rv-shell-product-mark'))
      .toHaveAttribute('aria-hidden', 'true')

    setLocale('ru')
    await nextTick()
    expect(document.documentElement.lang).toBe('ru')
    await expect.element(screen.getByText('Профили')).toBeVisible()

    // The head declaration survives re-renders; the locale module writes the
    // attribute synchronously so a reader never lands between the render and
    // the head manager's own patch. Both writers state the same value, and the
    // immediate one answers even with the shell gone.
    await screen.unmount()
    setLocale('en')
    await nextTick()
    expect(document.documentElement.lang).toBe('en')
  })

  it('says nothing about the surface while the service answers', async () => {
    const screen = await draw()

    expect(screen.getByRole('status').all()).toEqual([])
  })

  /**
   * All three facts can be true at once, and stating all three would say one
   * thing three times: a service that is not answering is also why the section
   * did not open and why the development server went quiet. So there is one
   * message, and it is the one that explains the most.
   */
  it('states one thing at a time, the most explaining first', async () => {
    const screen = await draw()
    reportSectionFailure('/lists')
    reportDevDisconnected(true)
    await loseTheService()
    await nextTick()

    const notice = screen.getByRole('status')
    expect(notice.all()).toHaveLength(1)
    await expect
      .element(screen.getByText('Routevane is not responding'))
      .toBeVisible()
    await expect
      .element(screen.getByRole('button', { name: 'Retry' }))
      .toBeVisible()

    // The service answers again; what is left is the section that never opened.
    stubService(async () => new Response('{}'))
    await checkService()
    await nextTick()
    expect(notice.all()).toHaveLength(1)
    await expect
      .element(screen.getByText('This section did not load'))
      .toBeVisible()

    // And under that, the fact only a development build can report.
    clearSectionFailure()
    await nextTick()
    expect(notice.all()).toHaveLength(1)
    await expect
      .element(screen.getByText('Development server disconnected'))
      .toBeVisible()

    reportDevDisconnected(false)
    await nextTick()
    expect(screen.getByRole('status').all()).toEqual([])
  })

  it('asks for the section it named when that is the message', async () => {
    // What recovering a section takes belongs to the application, which
    // registers it. The shell's part is asking for the section it named, and
    // the message stays until the recovery answers it.
    const recover = vi.fn(async (_path: string) => undefined)
    onSectionRecovery(recover)
    const screen = await draw()
    reportSectionFailure('/profiles/new')
    await nextTick()

    await screen.getByRole('button', { name: 'Retry' }).click()

    expect(recover).toHaveBeenCalledWith('/profiles/new')
  })

  it('asks the service when the service is the message', async () => {
    const screen = await draw()
    await loseTheService()
    await nextTick()
    const service = stubService(async () => new Response('{}'))

    await screen.getByRole('button', { name: 'Retry' }).click()

    await vi.waitFor(() => expect(service).toHaveBeenCalledTimes(1))
    expect(service.mock.calls[0]?.[0]).toBe('/health')
    await expect.element(screen.getByRole('status')).not.toBeInTheDocument()
  })
})
