import { createDevProxy } from './dev-proxy'

export default defineNuxtConfig({
  $development: {
    devServer: { host: '127.0.0.1', port: 8765 },
    vite: {
      server: {
        cors: false,
        proxy: process.env.ROUTEVANE_DEV_API_ORIGIN
          ? createDevProxy(
              process.env.ROUTEVANE_DEV_API_ORIGIN,
              process.env.ROUTEVANE_DEV_UI_ORIGIN ?? 'http://127.0.0.1:8765',
            )
          : {},
      },
    },
  },
  ssr: false,
  app: {
    head: {
      htmlAttrs: { lang: 'en' },
      link: [{ href: '/favicon.svg', rel: 'icon', type: 'image/svg+xml' }],
    },
  },
  compatibilityDate: '2026-08-20',
  components: [{ path: '~/shared/ui', pathPrefix: false }],
  css: ['~/assets/styles/tokens.css', '~/assets/styles/global.css'],
  devtools: { enabled: false },
  experimental: {
    // The entry import map makes every chunk reference "#entry", which the CSP
    // preparation step then rewrites inside content-hashed files. A chunk whose
    // bytes change under an unchanged name breaks the immutable asset cache and
    // makes browsers mix chunks of different builds. Direct entry references
    // keep every _nuxt file content-addressed.
    entryImportMap: false,
  },
  modules: ['@nuxt/eslint'],
  srcDir: 'src/',
  typescript: {
    strict: true,
    // Production scripts run `nuxt typecheck` explicitly before generation.
    // The embedded checker's shell command splits tsconfig paths with spaces.
    typeCheck: process.env.NODE_ENV === 'development',
  },
})
