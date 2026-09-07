import { fileURLToPath, URL } from 'node:url'

import vue from '@vitejs/plugin-vue'
import { playwright } from '@vitest/browser-playwright'
import { defaultExclude, defineConfig } from 'vitest/config'

// `tools/dev.ps1` points Playwright at the repository cache so the unit gate,
// the end-to-end suites and the Go discovery tests exercise the one pinned
// revision. Default it here too, so `npm run test:unit` from `web/` finds the
// same browser without the wrapper.
process.env.PLAYWRIGHT_BROWSERS_PATH ??= fileURLToPath(
  new URL('../.cache/browsers', import.meta.url),
)

// A unit spec renders in the pinned Chromium by default, because that is where
// a component, browser storage, the document element and `shared/api`'s origin
// lookup actually behave. The list below is the opt-out: specs whose subject is
// a pure function or a file on disk, which a Node process runs for less.
//
// Naming the Node members rather than the browser members keeps the default
// safe — a new spec lands where a document exists instead of silently passing
// against a simulated one — and keeps one greppable answer to where a spec runs.
const NODE_SPECS = [
  // `structure` and `dev-proxy` read this repository from disk.
  'tests/unit/*.spec.ts',
  'tests/unit/assets/**/*.spec.ts',
  'tests/unit/shared/lib/**/*.spec.ts',
  // The model composables that never reach `shared/api`, which resolves the
  // service origin through `window.location`.
  'tests/unit/entities/profile-composition/model/composition.spec.ts',
  'tests/unit/features/{devices,lists,send-artifact,settings}/model/*.spec.ts',
]

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  test: {
    projects: [
      {
        test: {
          name: 'node',
          environment: 'node',
          include: NODE_SPECS,
        },
      },
      {
        test: {
          name: 'browser',
          include: ['tests/unit/**/*.spec.ts'],
          exclude: [...defaultExclude, ...NODE_SPECS],
          setupFiles: ['./tests/unit/setup.ts'],
          browser: {
            enabled: true,
            // A real browser reports the workstation's own language, time zone
            // and appearance. Pin all three: `Intl`, `navigator.languages` and
            // `prefers-color-scheme` are inputs to this surface, and a suite
            // whose result depends on the developer's operating system is not
            // the suite CI runs.
            provider: playwright({
              contextOptions: {
                colorScheme: 'light',
                locale: 'en-US',
                timezoneId: 'UTC',
              },
            }),
            headless: true,
            // A failure is read from the terminal. Writing an image would put a
            // generated artifact next to the spec that produced it.
            screenshotFailures: false,
            // Routevane is a desktop interface; the default 414px viewport is
            // not a window any of these components is laid out for.
            viewport: { width: 1280, height: 800 },
            instances: [{ browser: 'chromium' }],
          },
        },
      },
    ],
  },
})
