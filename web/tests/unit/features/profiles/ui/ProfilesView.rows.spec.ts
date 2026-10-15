import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-vue'

import ProfilesView from '@/features/profiles/ui/ProfilesView.vue'
import { invalidateCatalogCache } from '@/shared/api/catalog'
import { useLocale } from '@/shared/i18n/useLocale'

import {
  fakeOutput,
  fakeProfile,
  installShelfService,
} from '../support/shelfService'

const MIXED = 'a'.repeat(32)
const SOLO = 'b'.repeat(32)
const DRAFT = 'c'.repeat(32)
const BROKEN = 'd'.repeat(32)
const ORIGINAL_CLIPBOARD = Object.getOwnPropertyDescriptor(
  navigator,
  'clipboard',
)

const renderShelf = () =>
  render(ProfilesView, {
    global: {
      stubs: {
        NuxtLink: { props: ['to'], template: '<a :href="to"><slot /></a>' },
      },
    },
  })

type Screen = Awaited<ReturnType<typeof renderShelf>>

const menuOf = (screen: Screen, name: string) =>
  screen.getByRole('button', { name: `Actions for profile ${name}` })

const itemNames = (screen: Screen): string[] =>
  screen
    .getByRole('menuitem')
    .all()
    .map((item) => item.element().textContent.trim())

const mixed = () =>
  fakeProfile(MIXED, 'Mixed', [
    fakeOutput('keenetic-output', 'Keenetic', { deviceID: 'home' }),
    fakeOutput('singbox-output', 'sing-box'),
  ])

describe('ProfilesView rows', () => {
  beforeEach(() => {
    invalidateCatalogCache()
    useLocale().setLocale('en')
    vi.stubGlobal('useRoute', () => ({ hash: '' }))
    vi.stubGlobal('useRouter', () => ({ push: vi.fn(), replace: vi.fn() }))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    if (ORIGINAL_CLIPBOARD === undefined)
      Reflect.deleteProperty(navigator, 'clipboard')
    else Object.defineProperty(navigator, 'clipboard', ORIGINAL_CLIPBOARD)
  })

  describe('row menu', () => {
    it('lists exactly what can happen to a live profile', async () => {
      installShelfService({
        deployable: ['keenetic', 'sing-box'],
        profiles: [
          fakeProfile(SOLO, 'Solo', [fakeOutput('solo-output', 'Keenetic')]),
        ],
      })
      const screen = await renderShelf()
      await menuOf(screen, 'Solo').click()

      expect(itemNames(screen)).toEqual([
        'Open profile',
        'Export to file',
        'Send',
        'Add a format…',
        'Copy contents · Keenetic',
        'Rebuild files',
        'Archive',
      ])
    })

    // Every item of a menu panel carries a glyph, a submenu's items included.
    const unmarked = (screen: Screen): string[] =>
      screen
        .getByRole('menuitem')
        .all()
        .filter(
          (item) =>
            item.element().querySelector('[data-testid="rv-icon"]') === null,
        )
        .map((item) => item.element().textContent.trim())

    it('draws a glyph on every item of the menu', async () => {
      installShelfService({
        deployable: ['keenetic', 'sing-box'],
        profiles: [mixed()],
      })
      const screen = await renderShelf()
      await menuOf(screen, 'Mixed').click()

      expect(itemNames(screen)).toContain('Add a format…')
      expect(unmarked(screen)).toEqual([])
    })

    it.each(['Send', 'Copy contents'])(
      'draws a glyph on every item of the %s submenu',
      async (group) => {
        installShelfService({
          deployable: ['keenetic', 'sing-box'],
          profiles: [mixed()],
        })
        const screen = await renderShelf()
        await menuOf(screen, 'Mixed').click()
        await screen.getByRole('menuitem', { name: group, exact: true }).click()

        await expect
          .element(
            screen.getByRole('menuitem', { name: 'sing-box', exact: true }),
          )
          .toBeVisible()
        expect(unmarked(screen)).toEqual([])
      },
    )

    it('offers only what a profile with no published file can do', async () => {
      installShelfService({
        deployable: ['keenetic'],
        profiles: [fakeProfile(DRAFT, 'Draft')],
      })
      const screen = await renderShelf()
      await menuOf(screen, 'Draft').click()

      expect(itemNames(screen)).toEqual([
        'Open profile',
        'Export to file',
        'Add a format…',
        'Archive',
      ])
    })

    it('names the connection a format is sent to and nothing it is not bound to', async () => {
      installShelfService({
        deployable: ['keenetic', 'sing-box'],
        devices: [{ id: 'home', name: 'Home router' }],
        profiles: [mixed()],
      })
      const screen = await renderShelf()
      await menuOf(screen, 'Mixed').click()
      await screen.getByRole('menuitem', { name: 'Send' }).click()

      await expect
        .element(
          screen.getByRole('menuitem', { name: 'Keenetic · Home router' }),
        )
        .toBeVisible()
      await expect
        .element(
          screen.getByRole('menuitem', { name: 'sing-box', exact: true }),
        )
        .toBeVisible()
    })

    it('does not offer a rebuild for a profile that has a format never built', async () => {
      installShelfService({
        profiles: [
          fakeProfile(DRAFT, 'Draft', [
            fakeOutput('draft-output', 'Keenetic', { published: false }),
          ]),
        ],
      })
      const screen = await renderShelf()
      await menuOf(screen, 'Draft').click()

      expect(itemNames(screen)).not.toContain('Rebuild files')
    })

    it('opens the publishing tab to add a format', async () => {
      installShelfService({ profiles: [fakeProfile(DRAFT, 'Draft')] })
      const screen = await renderShelf()
      await menuOf(screen, 'Draft').click()

      await expect
        .element(screen.getByRole('menuitem', { name: 'Add a format…' }))
        .toHaveAttribute('href', expect.stringContaining(`/profiles/${DRAFT}`))
      await expect
        .element(screen.getByRole('menuitem', { name: 'Add a format…' }))
        .toHaveAttribute('href', expect.stringContaining('outputs'))
    })
  })

  describe('copy contents', () => {
    const installClipboard = () => {
      const writeText = vi.fn(async () => {})
      Object.defineProperty(navigator, 'clipboard', {
        configurable: true,
        value: { writeText },
      })
      return writeText
    }

    // The old row copied the first published format whatever the operator
    // meant. With two formats the choice is theirs, and the second one is what
    // proves it.
    it('copies the format chosen from the submenu, not the first one', async () => {
      installShelfService({ profiles: [mixed()] })
      const writeText = installClipboard()
      const screen = await renderShelf()
      await menuOf(screen, 'Mixed').click()
      await screen.getByRole('menuitem', { name: 'Copy contents' }).click()
      await screen
        .getByRole('menuitem', { name: 'sing-box', exact: true })
        .click()

      await expect.element(screen.getByText('Contents copied')).toBeVisible()
      expect(writeText).toHaveBeenCalledExactlyOnceWith(
        'bytes of artifact-singbox-output',
      )
    })

    it('names the only published format in one direct item', async () => {
      installShelfService({
        profiles: [
          fakeProfile(SOLO, 'Solo', [fakeOutput('solo-output', 'Keenetic')]),
        ],
      })
      const writeText = installClipboard()
      const screen = await renderShelf()
      await menuOf(screen, 'Solo').click()
      await screen
        .getByRole('menuitem', { name: 'Copy contents · Keenetic' })
        .click()

      await expect.element(screen.getByText('Contents copied')).toBeVisible()
      expect(writeText).toHaveBeenCalledExactlyOnceWith(
        'bytes of artifact-solo-output',
      )
    })
  })

  describe('rebuild', () => {
    it('refreshes the profile once and rebuilds each format, then says so', async () => {
      const service = installShelfService({
        profiles: [
          fakeProfile(MIXED, 'Mixed', [
            fakeOutput('keenetic-output', 'Keenetic'),
            fakeOutput('singbox-output', 'sing-box'),
          ]),
        ],
      })
      const screen = await renderShelf()
      await menuOf(screen, 'Mixed').click()
      await screen.getByRole('menuitem', { name: 'Rebuild files' }).click()

      await expect
        .element(screen.getByText('The files of profile Mixed were rebuilt'))
        .toBeVisible()
      expect(service.posts()).toEqual([
        `/v1/profiles/${MIXED}/refresh`,
        '/v1/outputs/keenetic-output/build',
        '/v1/outputs/singbox-output/build',
      ])
    })

    it('says that earlier files were left as they were when a build fails', async () => {
      installShelfService({
        buildFails: true,
        profiles: [
          fakeProfile(SOLO, 'Solo', [fakeOutput('solo-output', 'Keenetic')]),
        ],
      })
      const screen = await renderShelf()
      await menuOf(screen, 'Solo').click()
      await screen.getByRole('menuitem', { name: 'Rebuild files' }).click()

      await expect
        .element(
          screen.getByText(
            'The files of profile Solo were not rebuilt. Files published earlier are unchanged.',
          ),
        )
        .toBeVisible()
    })
  })

  describe('row state', () => {
    // A first build that failed left no file and one failed attempt. The row
    // used to say only that no file was published, in grey.
    it('marks a profile whose last build failed instead of calling it unpublished', async () => {
      installShelfService({
        profiles: [
          fakeProfile(BROKEN, 'Broken', [
            fakeOutput('broken-output', 'Keenetic', {
              failed: true,
              published: false,
            }),
          ]),
          fakeProfile(DRAFT, 'Fresh', [
            fakeOutput('fresh-output', 'Keenetic', { published: false }),
          ]),
        ],
      })
      const screen = await renderShelf()

      const broken = screen.getByRole('row', { name: /Broken/ })
      await expect.element(broken.getByText('Build failed')).toBeVisible()
      await expect
        .element(broken.getByText('No published file'))
        .not.toBeInTheDocument()
      const fresh = screen.getByRole('row', { name: /Fresh/ })
      await expect.element(fresh.getByText('No published file')).toBeVisible()
      await expect
        .element(fresh.getByText('Build failed'))
        .not.toBeInTheDocument()
    })

    it('keeps the time of the file still published beside a failed rebuild', async () => {
      installShelfService({
        profiles: [
          fakeProfile(SOLO, 'Solo', [
            fakeOutput('solo-output', 'Keenetic', { failed: true }),
          ]),
        ],
      })
      const screen = await renderShelf()
      const row = screen.getByRole('row', { name: /Solo/ })

      await expect.element(row.getByText('Build failed')).toBeVisible()
      await expect
        .element(row.getByText('No published file'))
        .not.toBeInTheDocument()
      await expect.element(row.getByText(/2026/)).toBeVisible()
    })

    it('shows each format of a profile with its icon', async () => {
      installShelfService({ profiles: [mixed()] })
      const screen = await renderShelf()

      const items = screen
        .getByRole('row', { name: /Mixed/ })
        .getByRole('listitem')
      await expect.element(items.first()).toBeVisible()
      const formats = items.all()
      expect(formats.map((item) => item.element().textContent.trim())).toEqual([
        'Keenetic',
        'sing-box',
      ])
      for (const item of formats) {
        expect(
          item.element().querySelector('[data-testid="rv-icon"]'),
        ).not.toBeNull()
      }
    })

    it('names the formats in a column of that name', async () => {
      installShelfService({ profiles: [mixed()] })
      const screen = await renderShelf()

      await expect
        .element(screen.getByRole('columnheader', { name: 'Formats' }))
        .toBeVisible()
      await expect
        .element(screen.getByRole('columnheader', { name: 'Connected to' }))
        .not.toBeInTheDocument()
    })

    // The page header owns the one primary action; the empty shelf used to
    // repeat it as a second, identical button.
    it('offers one build action on an empty shelf', async () => {
      installShelfService({ profiles: [] })
      const screen = await renderShelf()

      await expect.element(screen.getByText('No profiles yet')).toBeVisible()
      expect(
        screen.getByRole('link', { name: 'Build a profile' }).all(),
      ).toHaveLength(1)
    })
  })
})
