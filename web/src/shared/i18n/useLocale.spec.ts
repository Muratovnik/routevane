import { beforeEach, describe, expect, it, vi } from 'vitest'

import { dictionaries, locales } from '@/shared/i18n/messages'
import {
  formatMessage,
  resolveLocale,
  useLocale,
} from '@/shared/i18n/useLocale'

describe('locale resolution', () => {
  it('prefers a stored choice over the browser preference', () => {
    expect(resolveLocale('en', ['ru-RU'])).toBe('en')
  })

  it('falls back to the browser preference, ignoring the region', () => {
    expect(resolveLocale(null, ['en-GB', 'ru'])).toBe('en')
  })

  it('ignores a stored value that is not a locale this build speaks', () => {
    expect(resolveLocale('klingon', ['de-DE'])).toBe('en')
  })
})

describe('message formatting', () => {
  it('substitutes named parameters', () => {
    expect(
      formatMessage('Собрано {count} из {total}', { count: 2, total: 5 }),
    ).toBe('Собрано 2 из 5')
  })

  it('leaves an unknown placeholder visible rather than printing undefined', () => {
    expect(formatMessage('Файл {name}', {})).toBe('Файл {name}')
  })
})

describe('dictionaries', () => {
  it('translates and pluralises in the selected locale', () => {
    const { setLocale, t, tc } = useLocale()
    setLocale('ru')
    expect(tc('profile.rules', 1)).toBe('1 правило')
    expect(tc('profile.rules', 3)).toBe('3 правила')
    expect(tc('profile.rules', 14)).toBe('14 правил')
    setLocale('en')
    expect(tc('profile.rules', 1)).toBe('1 rule')
    expect(tc('profile.rules', 14)).toBe('14 rules')
    expect(t('shell.nav.profiles')).toBe('Profiles')
    setLocale('ru')
    expect(t('profile.diagnostics.reason.unsupported_by_target')).toBe(
      'формат это не поддерживает',
    )
  })

  // A key present in one locale and missing in another is a screen that speaks
  // half a language, which is why the sets are compared rather than sampled.
  it('carries the same keys in every locale', () => {
    const reference = Object.keys(dictionaries.ru).sort()
    for (const locale of locales) {
      expect(Object.keys(dictionaries[locale]).sort()).toEqual(reference)
    }
  })

  it('never leaves a message empty', () => {
    for (const locale of locales) {
      for (const [key, message] of Object.entries(dictionaries[locale])) {
        const values =
          typeof message === 'string' ? [message] : Object.values(message)
        expect(values.length, `${locale}:${key}`).toBeGreaterThan(0)
        for (const value of values)
          expect(value, `${locale}:${key}`).not.toBe('')
      }
    }
  })
})

// Digits are read in the reader's language too: a four-figure rule count is
// grouped, and a file is stated in the unit that language names rather than in
// six figures of bytes.
describe('numbers and sizes', () => {
  it('groups a count the way the locale reads it', () => {
    const { formatNumber, setLocale, t, tc } = useLocale()

    setLocale('en')
    expect(formatNumber(1024)).toBe('1,024')
    expect(tc('profile.rules', 8192)).toBe('8,192 rules')
    expect(
      t('profile.forecast.overflow', {
        count: formatNumber(12000),
        max: formatNumber(1024),
        target: 'Keenetic',
      }),
    ).toBe('Keenetic: ≈ 12,000 of 1,024 — will not fit')

    setLocale('ru')
    expect(formatNumber(1024)).toBe('1 024')
    expect(tc('profile.rules', 8192)).toBe('8 192 правила')
  })

  it('scales a size into the unit the locale names', () => {
    const { formatBytes, setLocale } = useLocale()

    setLocale('en')
    expect(formatBytes(0)).toBe('0 bytes')
    expect(formatBytes(1)).toBe('1 byte')
    expect(formatBytes(950)).toBe('950 bytes')
    expect(formatBytes(12_300)).toBe('12.3 kB')
    expect(formatBytes(3_456_000)).toBe('3.5 MB')

    setLocale('ru')
    expect(formatBytes(950)).toBe('950 байт')
    expect(formatBytes(12_300)).toBe('12,3 кБ')
    expect(formatBytes(3_456_000)).toBe('3,5 МБ')
  })
})

// Storage is a convenience: it carries the choice between visits, it does not
// decide what a locale is, and it is never written on the operator's behalf.
describe('the remembered choice', () => {
  beforeEach(() => {
    vi.resetModules()
    window.localStorage.clear()
  })

  it('takes a stored locale this build speaks', async () => {
    window.localStorage.setItem('rv.locale', 'ru')
    const module = await import('@/shared/i18n/useLocale')
    expect(module.useLocale().locale.value).toBe('ru')
  })

  it('falls back to the browser when the stored value is not one', async () => {
    window.localStorage.setItem('rv.locale', 'klingon')
    const module = await import('@/shared/i18n/useLocale')
    expect(module.useLocale().locale.value).toBe('en')
  })

  it('records a switch under the key an earlier build reads', async () => {
    const module = await import('@/shared/i18n/useLocale')
    module.useLocale().setLocale('ru')
    expect(window.localStorage.getItem('rv.locale')).toBe('ru')
  })

  // Following the browser is not a choice the operator made, so it is not
  // stored as one: a laptop switched to Russian next month still switches the
  // surface with it.
  it('leaves the browser preference unrecorded until one is chosen', async () => {
    const module = await import('@/shared/i18n/useLocale')
    expect(module.useLocale().locale.value).toBe('en')
    expect(window.localStorage.getItem('rv.locale')).toBeNull()
  })
})
