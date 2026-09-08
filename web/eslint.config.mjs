import { fileURLToPath } from 'node:url'

import withNuxt from './.nuxt/eslint.config.mjs'
import vitest from '@vitest/eslint-plugin'
import eslintConfigPrettier from 'eslint-config-prettier'
import boundaries from 'eslint-plugin-boundaries'
import playwright from 'eslint-plugin-playwright'
import preferArrowFunctions from 'eslint-plugin-prefer-arrow-functions'
import security from 'eslint-plugin-security'
import sonarjs from 'eslint-plugin-sonarjs'
import vuejsAccessibility from 'eslint-plugin-vuejs-accessibility'

// error = gate, warning = advice. `npm run lint` runs without `--max-warnings`,
// so an error fails the gate and a warning is printed advice. Every rule that
// has to catch something keeps error severity; only the advisory size and
// complexity signals below stay at `warn`. `gateWarnings` promotes every other
// `warn` a preset ships, so a preset upgrade cannot quietly downgrade a gate.
// A config whose `name` starts with this prefix is exempt from that promotion.
const ADVISORY_NAME_PREFIX = 'routevane/advisory-'

// The browser suites under `tests/`. Unit specs live in `tests/unit`, so the
// Playwright rules name their own folders instead of everything under `tests/`.
const BROWSER_SUITES = 'e2e,desktop,dev,update'

const playwrightRecommended = playwright.configs['flat/recommended']

// eslint-plugin-boundaries resolves an import before it classifies it, and the
// `@/` alias plus extensionless `.ts`/`.vue` imports only resolve through the
// TypeScript resolver reading the Nuxt-generated path mapping.
const typescriptProject = fileURLToPath(
  new URL('./tsconfig.json', import.meta.url),
)

// A module-level literal constant is written UPPER_CASE. The declarator forms
// below are the ones whose value is fixed at parse time: a literal, a template
// with no expression, a negated literal, and any of those behind a TypeScript
// `as`. `Program` is the root of a `<script setup>` block too, so both module
// forms a linted file can use are covered.
const literalInitializers = [
  '[init.type="Literal"]',
  '[init.type="TemplateLiteral"][init.expressions.length=0]',
  '[init.type="UnaryExpression"][init.argument.type="Literal"]',
  '[init.type="TSAsExpression"][init.expression.type="Literal"]',
  '[init.type="TSAsExpression"][init.expression.type="TemplateLiteral"][init.expression.expressions.length=0]',
  '[init.type="TSAsExpression"][init.expression.type="UnaryExpression"][init.expression.argument.type="Literal"]',
]

const moduleConstDeclarators = [
  'Program > VariableDeclaration[kind="const"] > VariableDeclarator',
  'Program > ExportNamedDeclaration > VariableDeclaration[kind="const"] > VariableDeclarator',
]

const lowercaseModuleLiteralConstant = moduleConstDeclarators
  .flatMap((declarator) =>
    literalInitializers.map(
      (initializer) => `${declarator}[id.name=/[a-z]/]${initializer}`,
    ),
  )
  .join(', ')

// A unit spec reaches the DOM the way a reader perceives it. These are the
// query methods a markup-coupled test reaches for, and a first argument that
// starts with `#` or `.` is an id or class selector on whichever receiver it is
// called on — `wrapper.find('.row')`, `panel.querySelectorAll('.row')`, a bare
// `locator('#id')`.
const NODE_QUERY_METHODS = 'find|findAll|querySelector|querySelectorAll|locator'

const idOrClassQuery = [
  `CallExpression[callee.property.name=/^(${NODE_QUERY_METHODS})$/][arguments.0.value=/^[#.]/]`,
  `CallExpression[callee.name=/^(${NODE_QUERY_METHODS})$/][arguments.0.value=/^[#.]/]`,
].join(', ')

/** @param {import('eslint').Linter.RuleEntry} entry */
const toErrorSeverity = (entry) => {
  if (Array.isArray(entry)) {
    return entry[0] === 'warn' || entry[0] === 1
      ? ['error', ...entry.slice(1)]
      : entry
  }
  return entry === 'warn' || entry === 1 ? 'error' : entry
}

/** @param {import('eslint').Linter.Config[]} configs */
const gateWarnings = (configs) =>
  configs.map((config) => {
    if (
      !config.rules ||
      String(config.name ?? '').startsWith(ADVISORY_NAME_PREFIX)
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

export default withNuxt(
  {
    name: 'routevane/application-rules',
    plugins: {
      'prefer-arrow-functions': preferArrowFunctions,
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
      // A function is an arrow expression. The `function` keyword is left only
      // where an arrow cannot express the same behaviour, which the plugin
      // detects for itself: `this`, `arguments`, `super`, `new.target`, a
      // generator, an overload signature, and — through
      // `allowObjectProperties` — a method shorthand that an Options-API stub
      // needs. `returnStyle: 'unchanged'` keeps every existing body as it is.
      'prefer-arrow-functions/prefer-arrow-functions': [
        'error',
        {
          allowedNames: [],
          allowNamedFunctions: false,
          allowObjectProperties: true,
          classPropertiesAllowed: false,
          disallowPrototype: false,
          returnStyle: 'unchanged',
          singleReturnOnly: false,
        },
      ],
      'func-style': ['error', 'expression'],
      'prefer-arrow-callback': 'error',
      // An arrow constant is not hoisted the way a function declaration was.
      // `variables: false` reports the reference that would actually throw —
      // one evaluated at module scope above its own definition — while still
      // allowing the mutual references that live inside function bodies.
      '@typescript-eslint/no-use-before-define': [
        'error',
        {
          functions: true,
          classes: true,
          variables: false,
          allowNamedExports: false,
          ignoreTypeReferences: true,
        },
      ],
      'no-restricted-syntax': [
        'error',
        {
          selector: lowercaseModuleLiteralConstant,
          message: 'Module-level literal constants are UPPER_CASE.',
        },
      ],
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
    files: ['tests/unit/**/*.spec.ts'],
    plugins: { vitest },
    rules: {
      ...vitest.configs.recommended.rules,
      // Vitest's own `expect(value, message)` names the case a loop is on.
      'vitest/valid-expect': ['error', { maxArgs: 2 }],
      // `no-restricted-syntax` is replaced rather than merged, so the
      // application rule above is restated here alongside the spec ones.
      'no-restricted-syntax': [
        'error',
        {
          selector: lowercaseModuleLiteralConstant,
          message: 'Module-level literal constants are UPPER_CASE.',
        },
        {
          selector: 'Program > VariableDeclaration[kind="let"]',
          message:
            'Specs keep no mutable module state; build a scenario per test.',
        },
        {
          selector: idOrClassQuery,
          message:
            'Query by role, label, or text; data-testid only as a last resort.',
        },
      ],
    },
  },
  {
    name: 'routevane/playwright-tests',
    files: [`tests/{${BROWSER_SUITES}}/**/*.ts`],
    plugins: playwrightRecommended.plugins,
    languageOptions: playwrightRecommended.languageOptions,
    rules: {
      ...playwrightRecommended.rules,
      // A platform-specific case is gated by `test.skip(condition, reason)`,
      // which is Playwright's own mechanism. An unconditional skip still fails.
      'playwright/no-skipped-test': ['error', { allowConditional: true }],
      // A browser suite locates what an operator perceives: a role, an
      // accessible name, a label, or the words on screen. Three layers have no
      // such handle and are named here instead. The modal scrim: RvDialog
      // hooks the layer it owns itself, but the sheet's belongs to USlideover,
      // which takes no attributes for it, so the class both variants carry is
      // the only handle that reaches a whole stack. SortableJS's drag clone,
      // which copies the row it is dragged from and carries no role or name of
      // its own. And the design system's own button class, which is what tells
      // a product action rendered as a link from a link inside a sentence —
      // a distinction the target-size sweep needs and no role expresses. Each
      // is written once, in `tests/e2e/support/queries.ts`.
      'playwright/no-raw-locators': [
        'error',
        {
          allowed: ['.rv-dialog__scrim', '.sortable-fallback', 'a.rv-button'],
        },
      ],
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
    // An entity's `ui` presents what it is given. The requests a slice makes
    // live in that slice's `model`, and a feature orchestrates across slices;
    // either way the transport never reaches a component. A gate, not advice.
    name: 'routevane/entities-api-orchestration',
    files: ['src/entities/**/ui/**'],
    rules: {
      'no-restricted-imports': [
        'error',
        {
          patterns: [
            {
              group: ['@/shared/api/*'],
              message:
                "An entity's ui presents what its model hands it. Read the API in the slice's model/useX.ts and pass the result in.",
            },
          ],
        },
      ],
    },
  },
).onResolved(gateWarnings)
