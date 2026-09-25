import { readFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'
import { ESLint, type Linter } from 'eslint'
import { describe, expect, it } from 'vitest'

// Vitest runs from the `web` package root, which is where ESLint finds
// `eslint.config.mjs`: these cases lint through the configuration the gate
// runs, not a copy of it.
const WEB_ROOT = process.cwd()
const SOURCE_ROOT = 'src'
const NATIVE_CONTROLS = 'vue/no-restricted-html-elements'
// A library tag is refused in the template, a library import in the script.
const LIBRARY_TAGS = 'vue/no-restricted-syntax'
const LIBRARY_IMPORTS = 'no-restricted-syntax'
// Loading the whole configuration — the Nuxt preset, every plugin and the
// TypeScript resolver behind the layer boundaries — can take seconds.
const LINT_TIMEOUT_MILLISECONDS = 60000

const eslint = new ESLint({ cwd: WEB_ROOT })

const toPosix = (path: string) => path.replaceAll('\\', '/')

/** What one rule says about one file, linted at the path it would live at. */
const reportsOf = async (
  linter: ESLint,
  ruleId: string,
  path: string,
  code: string,
): Promise<string[]> => {
  const [result] = await linter.lintText(code, {
    filePath: join(WEB_ROOT, path),
  })
  return (result?.messages ?? [])
    .filter((message) => message.ruleId === ruleId)
    .map((message) => message.message)
}

/** Whether a resolved rule entry reports at all; a rule the file never met is off. */
const isOn = (entry: Linter.RuleEntry | undefined): boolean => {
  const severity = Array.isArray(entry) ? entry[0] : entry
  return severity !== undefined && severity !== 0 && severity !== 'off'
}

const component = (template: string, script = '') =>
  `${script}<template>\n  <div>${template}</div>\n</template>\n`

const scriptImporting = (statement: string) =>
  `<script setup lang="ts">\n${statement}\n</script>\n`

const nativeControls = [
  {
    element: 'button',
    markup: '<button type="button">Save</button>',
    wrapper: 'RvButton',
  },
  {
    element: 'input',
    markup: '<input aria-label="Name" type="text" />',
    wrapper: 'RvTextInput',
  },
  {
    element: 'select',
    markup: '<select aria-label="Mode"><option>One</option></select>',
    wrapper: 'RvSelect',
  },
  {
    element: 'textarea',
    markup: '<textarea aria-label="Notes"></textarea>',
    wrapper: 'RvTextarea',
  },
]

const FEATURE_COMPONENT = 'src/features/probe/ui/Probe.vue'

const screens = [
  'src/pages/probe.vue',
  'src/widgets/probe/ui/Probe.vue',
  FEATURE_COMPONENT,
  'src/entities/probe/ui/Probe.vue',
]

const WRAPPER_HOME = 'src/shared/ui/RvProbe.vue'

describe(
  'native controls outside shared/ui',
  { timeout: LINT_TIMEOUT_MILLISECONDS },
  () => {
    it.each(nativeControls)(
      'refuses a native <$element> in a screen and names $wrapper',
      async ({ markup, wrapper }) => {
        for (const path of screens) {
          const reports = await reportsOf(
            eslint,
            NATIVE_CONTROLS,
            path,
            component(markup),
          )
          expect(reports, path).toHaveLength(1)
          expect(reports[0], path).toContain(wrapper)
        }
      },
    )

    it.each(nativeControls)(
      'accepts a native <$element> inside the wrapper that owns it',
      async ({ markup }) => {
        expect(
          await reportsOf(
            eslint,
            NATIVE_CONTROLS,
            WRAPPER_HOME,
            component(markup),
          ),
        ).toEqual([])
      },
    )

    it('lists only components that still draw a native control', async () => {
      // A listed file is exempt, so the gate says nothing about it. The same
      // rule, applied to every component, says whether it still needs to be.
      const enabled = (
        await eslint.calculateConfigForFile(join(WEB_ROOT, FEATURE_COMPONENT))
      ).rules[NATIVE_CONTROLS] as Linter.RuleEntry
      const unexempted = new ESLint({
        cwd: WEB_ROOT,
        overrideConfig: {
          files: ['src/**/*.vue'],
          rules: { [NATIVE_CONTROLS]: enabled },
        },
      })
      const components = readdirSync(join(WEB_ROOT, SOURCE_ROOT), {
        encoding: 'utf8',
        recursive: true,
      })
        .map((entry) => `${SOURCE_ROOT}/${toPosix(entry)}`)
        .filter(
          (path) => path.endsWith('.vue') && !path.startsWith('src/shared/ui/'),
        )
      const exempt: string[] = []
      for (const path of components) {
        const rules = (
          await eslint.calculateConfigForFile(join(WEB_ROOT, path))
        ).rules as Record<string, Linter.RuleEntry | undefined>
        if (!isOn(rules[NATIVE_CONTROLS])) exempt.push(path)
      }
      const stale: string[] = []
      for (const path of exempt) {
        const code = readFileSync(join(WEB_ROOT, path), 'utf8')
        const reports = await reportsOf(unexempted, NATIVE_CONTROLS, path, code)
        if (reports.length === 0) stale.push(path)
      }

      expect(isOn(enabled)).toBe(true)
      expect(exempt.length).toBeGreaterThan(0)
      expect(
        stale,
        `These files draw no native control any more; remove them from NATIVE_CONTROL_EXCEPTIONS in eslint.config.mjs: ${stale.join(', ')}`,
      ).toEqual([])
    })
  },
)

describe(
  'component libraries outside shared/ui',
  { timeout: LINT_TIMEOUT_MILLISECONDS },
  () => {
    const refused = [
      {
        case: 'a Nuxt UI tag',
        code: component('<UButton label="Save" />'),
        ruleId: LIBRARY_TAGS,
      },
      {
        case: 'a kebab-case Nuxt UI tag',
        code: component('<u-badge />'),
        ruleId: LIBRARY_TAGS,
      },
      {
        case: 'a Reka UI import',
        code: component(
          '<PopoverRoot />',
          scriptImporting("import { PopoverRoot } from 'reka-ui'"),
        ),
        ruleId: LIBRARY_IMPORTS,
      },
      {
        case: 'a Nuxt UI import',
        code: component(
          '<Button />',
          scriptImporting(
            "import Button from '@nuxt/ui/runtime/components/Button.vue'",
          ),
        ),
        ruleId: LIBRARY_IMPORTS,
      },
      {
        case: 'a Nuxt UI component from #components',
        code: component(
          '<Link />',
          scriptImporting("import { ULink as Link } from '#components'"),
        ),
        ruleId: LIBRARY_IMPORTS,
      },
    ]

    it.each(refused)('refuses $case in a screen', async ({ code, ruleId }) => {
      for (const path of screens) {
        expect(await reportsOf(eslint, ruleId, path, code), path).toHaveLength(
          1,
        )
      }
    })

    it.each(refused)(
      'accepts $case inside shared/ui',
      async ({ code, ruleId }) => {
        expect(await reportsOf(eslint, ruleId, WRAPPER_HOME, code)).toEqual([])
      },
    )

    it('accepts the shared wrapper in a screen and UApp at the application root', async () => {
      const wrapper = component(
        '<RvButton>Save</RvButton>',
        scriptImporting("import RvButton from '@/shared/ui/RvButton.vue'"),
      )
      const root =
        '<template>\n  <UApp :toaster="null"><NuxtPage /></UApp>\n</template>\n'
      for (const ruleId of [LIBRARY_TAGS, LIBRARY_IMPORTS]) {
        expect(
          await reportsOf(eslint, ruleId, FEATURE_COMPONENT, wrapper),
          ruleId,
        ).toEqual([])
        expect(
          await reportsOf(eslint, ruleId, 'src/app.vue', root),
          ruleId,
        ).toEqual([])
      }
    })

    it('keeps the constant-case rule the import ban restates', async () => {
      expect(
        await reportsOf(
          eslint,
          LIBRARY_IMPORTS,
          FEATURE_COMPONENT,
          component('', scriptImporting('const limit = 3')),
        ),
      ).toEqual(['Module-level literal constants are UPPER_CASE.'])
    })
  },
)
