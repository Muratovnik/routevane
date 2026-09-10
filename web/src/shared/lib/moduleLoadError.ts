/**
 * Whether a value is a browser refusing to load a JavaScript module.
 *
 * A section of this interface is a module of its own, fetched when the operator
 * opens it. When that fetch fails the browser reports an ordinary rejection and
 * every engine words it differently, so the wording is the only thing that
 * identifies the failure — there is no code and no type to read. The phrases
 * below are the ones the engines this product runs in actually produce, plus
 * the one the bundler's own preload helper writes for the stylesheet a module
 * carries.
 *
 * The question is asked of a value rather than of the document, so the router,
 * a stray rejection and a preload event can all ask the same one.
 */

const moduleLoadMessages = [
  // Chromium.
  'failed to fetch dynamically imported module',
  // Firefox, and Chromium for a module that answered but did not parse.
  'error loading dynamically imported module',
  // Safari.
  'importing a module script failed',
  // Vite's preload helper, for the stylesheet of a module chunk.
  'unable to preload css',
]

/**
 * The message a thrown value carries. A rejection reaches this from three
 * places — a router error, an unhandled rejection and a preload event — and
 * only one of them promises an `Error`, so anything that states a message is
 * read and anything else states nothing.
 */
const messageOf = (value: unknown): string => {
  if (typeof value === 'string') return value
  if (typeof value !== 'object' || value === null) return ''
  const { message } = value as { message?: unknown }
  return typeof message === 'string' ? message : ''
}

export const isModuleLoadError = (value: unknown): boolean => {
  const message = messageOf(value).toLowerCase()
  return moduleLoadMessages.some((phrase) => message.includes(phrase))
}
