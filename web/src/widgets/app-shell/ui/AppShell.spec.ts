import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick, toValue, watchEffect, type MaybeRefOrGetter } from 'vue'

import { useLocale } from '@/shared/i18n/useLocale'
import AppShell from '@/widgets/app-shell/ui/AppShell.vue'

type HeadInput = { htmlAttrs?: { lang?: MaybeRefOrGetter<string> } }

/**
 * The head manager, reduced to the one thing this asserts: it applies what it
 * is given and keeps applying it. The real one is Nuxt's; what belongs to this
 * repository is which value the shell hands it and whether that value tracks
 * the operator's choice.
 */
function stubNuxtContext(): void {
  vi.stubGlobal('useHead', (input: HeadInput) => {
    watchEffect(() => {
      const lang = input.htmlAttrs?.lang
      if (lang !== undefined) document.documentElement.lang = toValue(lang)
    })
  })
  vi.stubGlobal('useRoute', () => ({ path: '/' }))
}

describe('AppShell', () => {
  beforeEach(() => {
    stubNuxtContext()
    document.documentElement.lang = 'xx'
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  // A screen reader told `en` pronounces Russian copy as English, so the
  // declared language and the words on the page are one statement — stated when
  // the shell first renders and restated on every switch.
  it('declares the language at start and again on a switch', async () => {
    const { setLocale } = useLocale()
    setLocale('en')

    const wrapper = mount(AppShell, {
      global: {
        stubs: {
          NuxtLink: { props: ['to'], template: '<a><slot /></a>' },
          RvIcon: true,
        },
      },
    })
    expect(document.documentElement.lang).toBe('en')

    setLocale('ru')
    await nextTick()
    expect(document.documentElement.lang).toBe('ru')
    expect(wrapper.text()).toContain('Маршруты')

    // The head declaration survives re-renders; the locale module writes the
    // attribute synchronously so a reader never lands between the render and
    // the head manager's own patch. Both writers state the same value, and the
    // immediate one answers even with the shell gone.
    wrapper.unmount()
    setLocale('en')
    await nextTick()
    expect(document.documentElement.lang).toBe('en')
  })
})
