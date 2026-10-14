import { afterEach, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-vue'

import SendPanel from '@/features/send-artifact/ui/SendPanel.vue'
import type { TargetOption } from '@/shared/api/catalog'
import { useLocale } from '@/shared/i18n/useLocale'

const json = (payload: unknown): Response =>
  new Response(JSON.stringify(payload), { status: 200 })

const target = (overrides: Partial<TargetOption>): TargetOption => ({
  fileExtension: 'txt',
  formatKey: 'fixture-v1',
  id: 'fixture',
  kind: 'router',
  manualInstallationHint: '',
  manualInstallationHintEn: '',
  manualInstallationSteps: [],
  manualInstallationStepsEn: [],
  rendererID: 'fixture-renderer',
  title: 'Fixture',
  titleEn: '',
  ...overrides,
})

const open = async (option: TargetOption) => {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: unknown) =>
      String(input) === '/v1/devices'
        ? json({ devices: [], secret_store_available: false })
        : json({ targets: [] }),
    ),
  )
  const screen = await render(SendPanel, {
    global: {
      stubs: {
        NuxtLink: { props: ['to'], template: '<a :href="to"><slot /></a>' },
      },
    },
    props: {
      artifactId: 'artifact',
      breadcrumbs: [],
      groupPrefix: '',
      heading: 'Send · Fixture',
      target: option,
      targetId: option.id,
    },
  })
  await screen
    .getByRole('button', { name: useLocale().t('send.manual.howto') })
    .click()
  return screen
}

afterEach(() => {
  vi.unstubAllGlobals()
  useLocale().setLocale('en')
})

// The catalog supplies the steps as a list, so the page numbers them as it
// receives them and takes no line apart, whatever digits a step contains.
it.each([
  ['en', ['KeeneticOS 5.0 or newer is required.', 'Choose the interface.']],
  ['ru', ['Нужна KeeneticOS 5.0 или новее.', 'Выберите интерфейс.']],
] as const)(
  'lists the catalog steps in order in %s',
  async (language, steps) => {
    useLocale().setLocale(language)
    const screen = await open(
      target({
        manualInstallationSteps: [
          'Нужна KeeneticOS 5.0 или новее.',
          'Выберите интерфейс.',
        ],
        manualInstallationStepsEn: [
          'KeeneticOS 5.0 or newer is required.',
          'Choose the interface.',
        ],
      }),
    )

    const items = screen.getByRole('listitem')
    await expect.element(items.first()).toHaveTextContent(steps[0])
    await expect.element(items.nth(1)).toHaveTextContent(steps[1])
    await expect.element(items).toHaveLength(steps.length)
  },
)

it('shows a single-sentence instruction as text and not as a list of one', async () => {
  const screen = await open(
    target({ manualInstallationSteps: ['Import the file.'] }),
  )

  await expect.element(screen.getByText('Import the file.')).toBeVisible()
  await expect.element(screen.getByRole('listitem')).not.toBeInTheDocument()
})
