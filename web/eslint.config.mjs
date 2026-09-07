import { fileURLToPath } from 'node:url'

import withNuxt from './.nuxt/eslint.config.mjs'
import vitest from '@vitest/eslint-plugin'
import eslintConfigPrettier from 'eslint-config-prettier'
import boundaries from 'eslint-plugin-boundaries'
import playwright from 'eslint-plugin-playwright'
import security from 'eslint-plugin-security'
import sonarjs from 'eslint-plugin-sonarjs'
import vuejsAccessibility from 'eslint-plugin-vuejs-accessibility'

// error = gate, warning = advice. `npm run lint` runs without `--max-warnings`,
// so an error fails the gate and a warning is printed advice. Every rule that
// has to catch something keeps error severity; only the advisory size and
// complexity signals below stay at `warn`. `gateWarnings` promotes every other
// `warn` a preset ships, so a preset upgrade cannot quietly downgrade a gate.
// A config whose `name` starts with this prefix is exempt from that promotion.
const advisoryNamePrefix = 'routevane/advisory-'

const playwrightRecommended = playwright.configs['flat/recommended']

// eslint-plugin-boundaries resolves an import before it classifies it, and the
// `@/` alias plus extensionless `.ts`/`.vue` imports only resolve through the
// TypeScript resolver reading the Nuxt-generated path mapping.
const typescriptProject = fileURLToPath(
  new URL('./tsconfig.json', import.meta.url),
)

/** @param {import('eslint').Linter.RuleEntry} entry */
function toErrorSeverity(entry) {
  if (Array.isArray(entry)) {
    return entry[0] === 'warn' || entry[0] === 1
      ? ['error', ...entry.slice(1)]
      : entry
  }
  return entry === 'warn' || entry === 1 ? 'error' : entry
}

/** @param {import('eslint').Linter.Config[]} configs */
function gateWarnings(configs) {
  return configs.map((config) => {
    if (
      !config.rules ||
      String(config.name ?? '').startsWith(advisoryNamePrefix)
    ) {
      return config
    }
    return {
      ...config,
      rules: Object.fromEntries(
        Object.entries(config.rules).map(([rule, entry]) => [
          rule,
          toErrorSeverity(entry),
        ]),
      ),
    }
  })
}

export default withNuxt(
  {
    name: 'routevane/application-rules',
    plugins: {
      security,
      sonarjs,
      'vuejs-accessibility': vuejsAccessibility,
    },
    settings: sonarjs.configs.recommended.settings,
    rules: {
      ...sonarjs.configs.recommended.rules,
      ...security.configs.recommended.rules,
      // Every bracket read here is a keyed lookup in the application's own
      // catalog, forecast or message table; the rule cannot tell that from an
      // untrusted index.
      'security/detect-object-injection': 'off',
      // The repeated literals are message keys and fixture values, not a
      // missing constant.
      'sonarjs/no-duplicate-string': 'off',
      'no-multi-assign': 'error',
      'vuejs-accessibility/anchor-has-content': 'error',
      'vuejs-accessibility/aria-props': 'error',
      'vuejs-accessibility/aria-role': 'error',
      'vuejs-accessibility/form-control-has-label': 'error',
      'vuejs-accessibility/heading-has-content': 'error',
    },
  },
  {
    // Feature-Sliced layer order. `src/pages` is one element because a Nuxt
    // route tree is files, not slices, and an element descriptor matches
    // folders. `src/app.vue` is classified as a file category for the same
    // reason. Every other layer is one element per slice folder, so a
    // same-layer import between two slices has no policy and is rejected.
    name: 'routevane/layer-boundaries',
    files: ['src/**/*.{ts,mts,vue}'],
    plugins: { boundaries },
    settings: {
      'import/resolver': {
        typescript: {
          project: typescriptProject,
          extensions: [
            '.ts',
            '.mts',
            '.tsx',
            '.d.ts',
            '.vue',
            '.mjs',
            '.js',
            '.json',
          ],
        },
      },
      'boundaries/files': [{ category: 'app-root', pattern: 'src/app.vue' }],
      'boundaries/elements': [
        { type: 'pages', pattern: 'src/pages' },
        { type: 'widgets', pattern: 'src/widgets/*' },
        { type: 'features', pattern: 'src/features/*' },
        { type: 'entities', pattern: 'src/entities/*' },
        { type: 'shared', pattern: 'src/shared/*' },
      ],
    },
    rules: {
      'boundaries/dependencies': [
        'error',
        {
          default: 'disallow',
          policies: [
            {
              from: { file: { categories: 'app-root' } },
              allow: {
                to: {
                  element: {
                    types: {
                      anyOf: [
                        'pages',
                        'widgets',
                        'features',
                        'entities',
                        'shared',
                      ],
                    },
                  },
                },
              },
            },
            {
              from: { element: { type: 'pages' } },
              allow: {
                to: {
                  element: {
                    types: {
                      anyOf: ['widgets', 'features', 'entities', 'shared'],
                    },
                  },
                },
              },
            },
            {
              from: { element: { type: 'widgets' } },
              allow: {
                to: {
                  element: {
                    types: { anyOf: ['features', 'entities', 'shared'] },
                  },
                },
              },
            },
            {
              from: { element: { type: 'features' } },
              allow: {
                to: { element: { types: { anyOf: ['entities', 'shared'] } } },
              },
            },
            {
              from: { element: { type: 'entities' } },
              allow: { to: { element: { type: 'shared' } } },
            },
            {
              from: { element: { type: 'shared' } },
              allow: { to: { element: { type: 'shared' } } },
            },
          ],
        },
      ],
    },
  },
  {
    name: 'routevane/vitest-unit-tests',
    files: ['src/**/*.spec.ts', 'dev-proxy.spec.ts'],
    plugins: { vitest },
    rules: {
      ...vitest.configs.recommended.rules,
      // Vitest's own `expect(value, message)` names the case a loop is on.
      'vitest/valid-expect': ['error', { maxArgs: 2 }],
    },
  },
  {
    name: 'routevane/playwright-tests',
    files: ['tests/**/*.ts'],
    plugins: playwrightRecommended.plugins,
    languageOptions: playwrightRecommended.languageOptions,
    rules: {
      ...playwrightRecommended.rules,
      // A platform-specific case is gated by `test.skip(condition, reason)`,
      // which is Playwright's own mechanism. An unconditional skip still fails.
      'playwright/no-skipped-test': ['error', { allowConditional: true }],
    },
  },
  {
    // A test file authors its own inputs, owns its fixture processes and
    // parameterizes one body over several surfaces. These rules read that
    // structure as a defect, so each is off here and stays on for product code.
    name: 'routevane/test-file-rules',
    files: ['**/*.spec.ts', 'tests/**'],
    rules: {
      // A test composes its own fixture paths and pattern strings.
      'security/detect-non-literal-fs-filename': 'off',
      'security/detect-non-literal-regexp': 'off',
      // Loopback origins, device endpoints and CIDRs are the fixture data of a
      // product whose subject is routing to addresses.
      'sonarjs/no-clear-text-protocols': 'off',
      'sonarjs/no-hardcoded-ip': 'off',
      // A browser callback is serialized into the page, so it cannot be lifted
      // out to a named helper.
      'sonarjs/no-nested-functions': 'off',
      // `sonarjs/no-skipped-tests` does not accept Playwright's reason
      // argument; `playwright/no-skipped-test` above gates the real case.
      'sonarjs/no-skipped-tests': 'off',
      // The Windows lifecycle test reclaims its own child processes through
      // `taskkill` and `powershell` on PATH.
      'sonarjs/no-os-command-from-path': 'off',
      // Clicking a disabled control is how a test proves the click is refused.
      'playwright/no-force-option': 'off',
      'sonarjs/no-forced-browser-interaction': 'off',
      // A branch on a loop parameter, the platform, or a `finally` cleanup is
      // not a branch on the outcome being asserted. Deferred rather than
      // exempt: the e2e suites still deserve a structural pass of their own.
      'playwright/no-conditional-in-test': 'off',
      'playwright/no-conditional-expect': 'off',
      // The drag and view-transition paths wait out a renderer frame, which is
      // not an observable condition. Also deferred, not settled.
      'playwright/no-wait-for-timeout': 'off',
      'sonarjs/no-fixed-wait-in-tests': 'off',
    },
  },
  eslintConfigPrettier,
  {
    // Advisory only. These numbers describe the size and branching a reviewer
    // should look at, not a limit that blocks a change. Do not split a
    // component to silence one.
    name: 'routevane/advisory-size-and-complexity',
    rules: {
      complexity: ['warn', 15],
      'max-lines': [
        'warn',
        { max: 400, skipBlankLines: true, skipComments: true },
      ],
      'max-lines-per-function': [
        'warn',
        { max: 60, skipBlankLines: true, skipComments: true, IIFEs: true },
      ],
      'sonarjs/cognitive-complexity': 'warn',
      'vue/max-lines-per-block': [
        'warn',
        { script: 250, template: 250, style: 250, skipBlankLines: true },
      ],
    },
  },
  {
    // A test file states a case per branch, so length carries less signal.
    name: 'routevane/advisory-size-and-complexity-tests',
    files: ['**/*.spec.ts', 'tests/**'],
    rules: {
      'max-lines': [
        'warn',
        { max: 800, skipBlankLines: true, skipComments: true },
      ],
    },
  },
  {
    // A raw id or class locator couples a test to markup. Advisory until the
    // existing suites are rewritten onto roles and accessible names.
    name: 'routevane/advisory-playwright-locators',
    files: ['tests/**/*.ts'],
    rules: {
      'playwright/no-raw-locators': 'warn',
    },
  },
  {
    // Entities present what they are given; features orchestrate the API.
    name: 'routevane/advisory-entities-api-orchestration',
    files: ['src/entities/**/ui/**'],
    rules: {
      'no-restricted-imports': [
        'warn',
        {
          patterns: [
            {
              group: ['@/shared/api/*'],
              message:
                'Entities present data and features orchestrate the API. Read this in a feature model and pass the result in as props.',
            },
          ],
        },
      ],
    },
  },
).onResolved(gateWarnings)
