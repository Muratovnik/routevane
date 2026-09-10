import { fileURLToPath } from 'node:url'

import { createDevProxy } from './dev-proxy'

// This interface's own modules, told from everything else by where they live.
// Written the way a bundler spells a path on either platform, and anchored to
// this file so a dependency that happens to carry a `src` folder of its own is
// never mistaken for one of ours.
const sourceRoot = fileURLToPath(new URL('src/', import.meta.url)).replaceAll(
  '\\',
  '/',
)

const ownModule = (id: string): boolean =>
  id.replaceAll('\\', '/').startsWith(sourceRoot)

const disableNuxtUIColorRuntime = (
  _options: Record<string, never>,
  nuxt: { options: { plugins: ({ src?: string } | string)[] } },
): void => {
  // Routevane's palette is static and token-owned. Nuxt UI otherwise injects
  // the same generated palette twice at runtime, which strict style-src blocks.
  nuxt.options.plugins = nuxt.options.plugins.filter((plugin) => {
    const source = typeof plugin === 'string' ? plugin : plugin.src
    return !/\/runtime\/plugins\/colors(?:\.js)?$/.test(
      source?.replaceAll('\\', '/') ?? '',
    )
  })
}

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
  css: [
    '~/assets/styles/nuxt-ui.css',
    '~/assets/styles/tokens.css',
    '~/assets/styles/global.css',
  ],
  devtools: { enabled: false },
  experimental: {
    // The entry import map makes every chunk reference "#entry", which the CSP
    // preparation step then rewrites inside content-hashed files. A chunk whose
    // bytes change under an unchanged name breaks the immutable asset cache and
    // makes browsers mix chunks of different builds. Direct entry references
    // keep every _nuxt file content-addressed.
    entryImportMap: false,
    // Nuxt's own answer to a section that will not load is a page reload, and
    // it is armed by a browser event that only the built product ever fires:
    // during development it never runs at all, and against a service that has
    // stopped it replaces the interface with an empty window. `manual` keeps
    // Nuxt stating the fact and leaves the decision to
    // `plugins/navigation.client.ts`, which asks the service first.
    emitRouteChunkError: 'manual',
  },
  modules: ['@nuxt/eslint', '@nuxt/ui', disableNuxtUIColorRuntime],
  ui: {
    // Routevane ships its own local font stack and theme switch. Do not let a
    // component dependency add network font metadata or a second color owner.
    fonts: false,
    colorMode: false,
  },
  srcDir: 'src/',
  vite: {
    build: {
      rollupOptions: {
        output: {
          // Opening a section must not depend on the network. Every module of
          // this interface is bundled with the entry, so the dynamic import a
          // section link performs resolves inside code the browser already
          // holds, and a service that stops answering can no longer make a
          // link quietly do nothing. Only our own modules are named: an entry
          // cannot be assigned to a manual chunk, and dependencies keep the
          // splitting the bundler chose for them.
          manualChunks: (id: string) =>
            ownModule(id) ? 'routevane' : undefined,
        },
      },
    },
  },
  typescript: {
    strict: true,
    // Production scripts run `nuxt typecheck` explicitly before generation.
    // The embedded checker's shell command splits tsconfig paths with spaces.
    typeCheck: process.env.NODE_ENV === 'development',
    // Every suite — the unit specs and the browser ones — lives in `tests/`,
    // outside `srcDir`, and a file the checker never reads can carry a missing
    // import or a stale type for as long as nobody runs it. `include` in an
    // extending tsconfig replaces the inherited list rather than adding to it,
    // so the entry is appended here and Nuxt keeps owning the rest of the
    // generated set.
    tsConfig: { include: ['../tests/**/*'] },
  },
})
