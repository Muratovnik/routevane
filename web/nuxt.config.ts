export default defineNuxtConfig({
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
    typeCheck: true,
  },
})
