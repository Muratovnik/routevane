import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const fixtureRoot = dirname(fileURLToPath(import.meta.url))
const webRoot = resolve(fixtureRoot, '..', '..', '..')

export default defineNuxtConfig({
  ssr: false,
  compatibilityDate: '2026-08-20',
  css: [
    resolve(webRoot, 'src/assets/styles/nuxt-ui.css'),
    resolve(webRoot, 'src/assets/styles/tokens.css'),
    resolve(webRoot, 'src/assets/styles/global.css'),
  ],
  devtools: { enabled: false },
  modules: [resolve(webRoot, 'node_modules/@nuxt/ui/dist/module.mjs')],
  ui: {
    colorMode: false,
    fonts: false,
  },
})
