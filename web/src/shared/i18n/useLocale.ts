import { useLocalStorage } from '@vueuse/core'
import { computed, watch } from 'vue'

import {
  dictionaries,
  locales,
  type Locale,
  type PluralMessage,
} from '@/shared/i18n/messages'

const STORAGE_KEY = 'rv.locale'

// Sizes are stated in the decimal units CLDR names — 1 kB is 1000 B — because
// the unit word is printed by the same table that groups the digits. A scale
// invented here would disagree with the word Intl puts beside the number.
const BYTE_SCALE = 1000
const byteUnits = [
  'byte',
  'kilobyte',
  'megabyte',
  'gigabyte',
  'terabyte',
  'petabyte',
] as const

const isLocale = (value: string | null | undefined): value is Locale =>
  value !== null && value !== undefined && locales.includes(value as Locale)

const preferredLanguages = (): readonly string[] => {
  if (typeof navigator === 'undefined') return []
  return navigator.languages ?? [navigator.language]
}

export const resolveLocale = (
  stored: string | null,
  preferred: readonly string[],
): Locale => {
  if (isLocale(stored)) return stored
  for (const candidate of preferred) {
    const base = candidate.toLowerCase().split('-')[0]
    if (isLocale(base)) return base
  }
  return 'en'
}

export const formatMessage = (
  template: string,
  params: Record<string, string | number> = {},
): string =>
  template.replaceAll(/\{(\w+)\}/g, (match, name: string) => {
    const value = params[name]
    return value === undefined ? match : String(value)
  })

const initialLocale = (): Locale => resolveLocale(null, preferredLanguages())

const noteStorageRefusal = (): void => {
  // A refusal to remember the choice is not a reason to reject it, and not a
  // failure the operator can act on either.
}

/**
 * One locale for the whole surface: module scope, not per component, so a
 * switch in the shell reaches every screen without a provider chain.
 *
 * Storage is a convenience, never a requirement: a browser that refuses it
 * still gets a working surface in the operating system's language, and a value
 * found there is resolved rather than trusted, because anything on this origin
 * can write to it. The stored choice is also the same choice in a second tab —
 * the browser's storage event carries it across without either tab asking.
 */
const current = useLocalStorage<Locale>(STORAGE_KEY, initialLocale, {
  flush: 'sync',
  onError: noteStorageRefusal,
  serializer: {
    read: (raw) => resolveLocale(raw, preferredLanguages()),
    write: (value) => value,
  },
  // The first visit follows the browser, and keeps following it: writing the
  // negotiated locale back would freeze today's browser language into a choice
  // the operator never made.
  writeDefaults: false,
})

// The shell declares `lang` in the head, which survives a re-render. This
// write is the immediate one: the head manager patches the DOM on its own
// schedule, and a reader between the render and that patch must not hear the
// previous language.
if (typeof document !== 'undefined') {
  watch(
    current,
    (locale) => {
      document.documentElement.lang = locale
    },
    { flush: 'sync', immediate: true },
  )
}

const numberFormat = computed(() => new Intl.NumberFormat(current.value))

// Plain bytes are spelled out because CLDR's abbreviation of `byte` in English
// is the ungrammatical "950 byte"; its written form, "950 bytes", is the one
// English actually uses and Russian's "950 байт" declines correctly with it.
// From kilobytes up every locale writes the symbol, so the scaled steps do.
const byteFormats = computed(() =>
  byteUnits.map(
    (unit, index) =>
      new Intl.NumberFormat(current.value, {
        maximumFractionDigits: index === 0 ? 0 : 1,
        style: 'unit',
        unit,
        unitDisplay: index === 0 ? 'long' : 'short',
      }),
  ),
)

export const useLocale = () => {
  const translate = (
    key: string,
    params: Record<string, string | number> = {},
  ): string => {
    const message = dictionaries[current.value][key]
    if (typeof message !== 'string') return key
    return formatMessage(message, params)
  }

  const translatePlural = (
    key: string,
    count: number,
    params: Record<string, string | number> = {},
  ): string => {
    const message = dictionaries[current.value][key]
    if (message === undefined || typeof message === 'string') return key
    // The category is chosen by the count itself and the digits are printed by
    // the locale, so a four-figure rule count is grouped the way the sentence
    // around it is read.
    return formatMessage(selectPlural(message, count, current.value), {
      ...params,
      count: formatNumber(count),
    })
  }

  /**
   * translateOr answers with the catalog's own wording when the surface has no
   * translation of its own. Catalog content is extensible — an operator adds a
   * local category and it must still be readable — so a missing key is a fact
   * about our dictionary, not an error to show as a raw key.
   */
  const translateOr = (
    key: string,
    fallback: string,
    params: Record<string, string | number> = {},
  ): string => {
    const translated = translate(key, params)
    return translated === key ? fallback : translated
  }

  const setLocale = (next: Locale): void => {
    current.value = next
  }

  return {
    available: locales,
    dateTime: computed(
      () =>
        new Intl.DateTimeFormat(current.value, {
          dateStyle: 'medium',
          timeStyle: 'medium',
        }),
    ),
    formatBytes,
    formatNumber,
    locale: computed(() => current.value),
    setLocale,
    t: translate,
    tc: translatePlural,
    tor: translateOr,
  }
}

/**
 * A count in the reader's digits: «1 024», "1,024". Group separators are a
 * property of the language, not of the sentence, so the dictionary states the
 * sentence and this states the number.
 */
export const formatNumber = (value: number): string =>
  numberFormat.value.format(value)

/**
 * A size in the units the reader's locale names — «12,3 кБ», "12.3 kB". The
 * unit comes from the same table as the digits, so no dictionary entry has to
 * spell a byte prefix in two languages, and a file stops being reported as six
 * figures of bytes.
 */
export const formatBytes = (value: number): string => {
  let amount = Math.max(0, value)
  let unit = 0
  while (amount >= BYTE_SCALE && unit < byteUnits.length - 1) {
    amount /= BYTE_SCALE
    unit += 1
  }
  const format = byteFormats.value[unit] ?? byteFormats.value[0]
  return format === undefined ? String(value) : format.format(amount)
}

const selectPlural = (
  message: PluralMessage,
  count: number,
  locale: Locale,
): string => {
  const category = new Intl.PluralRules(locale).select(count)
  return message[category] ?? message.other ?? ''
}
