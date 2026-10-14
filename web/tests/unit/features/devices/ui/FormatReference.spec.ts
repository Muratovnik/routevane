import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { render } from 'vitest-browser-vue'

import FormatReference from '@/features/devices/ui/FormatReference.vue'
import type { TargetOption } from '@/shared/api/catalog'
import { useLocale } from '@/shared/i18n/useLocale'

const STEPS = [
  'Open the router command line and run the commands from the file.',
  'On the DNS-Based Routes page choose the interface for each group.',
  'Make the router the DNS server of your clients.',
]

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

// The catalog serves the same instruction twice: as steps, and as the steps
// joined by spaces in the legacy one-line hint. Only the steps are read.
const several = target({
  id: 'several',
  manualInstallationHint: STEPS.join(' '),
  manualInstallationSteps: STEPS,
  title: 'Several steps',
})
const single = target({
  id: 'single',
  manualInstallationHint: 'Import the file.',
  manualInstallationSteps: ['Import the file.'],
  title: 'One step',
})

const renderReference = async (
  targets: TargetOption[],
  disclosure = /^Supported formats/,
) => {
  const screen = await render(FormatReference, {
    props: {
      deliverableIds: new Set<string>(),
      failure: 'unreachable',
      targets,
    },
  })
  await screen.getByRole('button', { name: disclosure }).click()
  return screen
}

describe('FormatReference installation informers', () => {
  beforeEach(() => {
    useLocale().setLocale('en')
  })
  afterEach(() => {
    useLocale().setLocale('en')
  })

  it('numbers the steps of a format that has several', async () => {
    const screen = await renderReference([several])

    await screen
      .getByRole('button', { name: 'How to install the file on Several steps' })
      .click()

    const panel = screen.getByRole('dialog', {
      name: 'How to install the file on Several steps',
    })
    await expect
      .element(panel.getByText('How to install by hand'))
      .toBeVisible()
    const list = panel.getByRole('list')
    expect(list.element().tagName).toBe('OL')
    expect(getComputedStyle(list.element()).listStyleType).toBe('decimal')
    const items = panel.getByRole('listitem').elements()
    expect(items.map((item) => item.textContent)).toEqual(STEPS)
    // Not the run-on paragraph the legacy hint would make.
    expect(panel.element().textContent).not.toContain(STEPS.join(' '))
  })

  it('keeps a format that gives one sentence as plain text', async () => {
    const screen = await renderReference([single])

    await screen
      .getByRole('button', { name: 'How to install the file on One step' })
      .click()

    const panel = screen.getByRole('dialog', {
      name: 'How to install the file on One step',
    })
    await expect.element(panel).toHaveTextContent('Import the file.')
    await expect.element(panel.getByRole('list')).not.toBeInTheDocument()
  })

  it('reads the steps in the language being read', async () => {
    useLocale().setLocale('ru')
    const translated = target({
      manualInstallationSteps: ['Первый шаг.', 'Второй шаг.'],
      manualInstallationStepsEn: ['First step.', 'Second step.'],
      title: 'Перевод',
    })
    const screen = await renderReference(
      [translated],
      /^Поддерживаемые форматы/,
    )

    await screen
      .getByRole('button', { name: 'Как установить файл на Перевод' })
      .click()

    const panel = screen.getByRole('dialog', {
      name: 'Как установить файл на Перевод',
    })
    expect(
      panel
        .getByRole('listitem')
        .elements()
        .map((item) => item.textContent),
    ).toEqual(['Первый шаг.', 'Второй шаг.'])
  })

  it('offers no informer for a format that gives no instruction', async () => {
    const screen = await renderReference([target({ title: 'Silent' })])

    await expect
      .element(
        screen.getByRole('button', {
          name: 'How to install the file on Silent',
        }),
      )
      .not.toBeInTheDocument()
  })
})
