/**
 * The words the product ships.
 *
 * A suite states what it expects in the operator's own language, so the text
 * is read from the shipped dictionary rather than copied into the test: a
 * message the product renames fails the suite instead of passing silently.
 */
import { dictionaries } from '../../../src/shared/i18n/messages'

/** One language's dictionary, read by message key. */
export type Copy = (key: string) => string

// The locale suites read their expected text from the shipped dictionary, so a
// message the product renames fails the test instead of passing silently.
export const copyFor =
  (language: keyof typeof dictionaries): Copy =>
  (key) => {
    const value = dictionaries[language][key]
    if (typeof value !== 'string')
      throw new Error(`Not a string message: ${key}`)
    return value
  }

// The walkthroughs below run in English and name what they expect inline. The
// shared queries take a dictionary so the localized suites can hand them their
// own words, and default to this one so an English call site stays a call with
// one argument.
export const englishCopy = copyFor('en')
