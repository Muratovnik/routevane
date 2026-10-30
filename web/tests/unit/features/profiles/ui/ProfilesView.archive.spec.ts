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

const NEWEST = 'a'.repeat(32)
const OLDER = 'b'.repeat(32)
const OLD_ARCHIVED = 'c'.repeat(32)
const SECOND_ARCHIVED = 'd'.repeat(32)

const live = (id: string, name: string) =>
  fakeProfile(id, name, [fakeOutput(`output-${id}`, 'Keenetic')])
const archived = (id: string, name: string) =>
  fakeProfile(id, name, [fakeOutput(`output-${id}`, 'Keenetic')], true)

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

describe('ProfilesView archive', () => {
  beforeEach(() => {
    invalidateCatalogCache()
    useLocale().setLocale('en')
    vi.stubGlobal('useRoute', () => ({ hash: '' }))
    vi.stubGlobal('useRouter', () => ({ push: vi.fn(), replace: vi.fn() }))
  })

  afterEach(() => vi.unstubAllGlobals())

  // Archiving runs at once, so the way back is offered where the operator is
  // looking: the shelf says what moved and puts it back with one action.
  it('offers to undo an archive and puts the profile back on the shelf', async () => {
    const service = installShelfService({
      profiles: [live(NEWEST, 'Newest'), live(OLDER, 'Older')],
    })
    const screen = await renderShelf()

    await menuOf(screen, 'Newest').click()
    await screen.getByRole('menuitem', { name: 'Archive' }).click()

    await expect
      .element(screen.getByText('Profile “Newest” moved to the archive'))
      .toBeVisible()
    await expect
      .element(screen.getByRole('link', { name: 'Newest' }))
      .not.toBeInTheDocument()
    await expect
      .element(screen.getByRole('radio', { name: 'Archive 1' }))
      .toBeInTheDocument()

    await screen.getByRole('button', { name: 'Undo' }).click()

    await expect
      .element(screen.getByRole('link', { name: 'Newest' }))
      .toBeVisible()
    await expect
      .element(screen.getByText('Profile “Newest” moved to the archive'))
      .not.toBeInTheDocument()
    expect(service.posts()).toEqual([
      `/v1/profiles/${NEWEST}/archive`,
      `/v1/profiles/${NEWEST}/restore`,
    ])
    // The restored profile is back above the older one, where its age puts it.
    const links = screen.getByRole('link').all()
    expect(
      links
        .map((link) => link.element().textContent.trim())
        .filter((text) => text === 'Newest' || text === 'Older'),
    ).toEqual(['Newest', 'Older'])
  })

  // The count belongs to the server. Here the first page of the active shelf
  // holds one profile and the library holds three archived ones, two of which
  // this tab has never loaded.
  it('counts the archive on the server and lists it in the same table with its date', async () => {
    const service = installShelfService({
      archivedElsewhere: 0,
      profiles: [
        live(NEWEST, 'Newest'),
        archived(OLD_ARCHIVED, 'Old media'),
        archived(SECOND_ARCHIVED, 'Older media'),
      ],
    })
    const screen = await renderShelf()

    await expect
      .element(screen.getByRole('radio', { name: 'Archive 2' }))
      .toBeInTheDocument()
    await expect
      .element(screen.getByRole('radio', { name: 'Active' }))
      .toBeChecked()
    await expect
      .element(screen.getByRole('columnheader', { name: 'Archived since' }))
      .not.toBeInTheDocument()

    await screen.getByText('Archive 2', { exact: true }).click()

    await expect
      .element(screen.getByRole('link', { name: 'Old media' }))
      .toBeVisible()
    await expect
      .element(screen.getByRole('link', { name: 'Older media' }))
      .toBeVisible()
    await expect
      .element(screen.getByRole('link', { name: 'Newest' }))
      .not.toBeInTheDocument()
    // The same columns as the active shelf, plus when each profile left it.
    for (const name of ['Profile', 'Formats', 'Content from', 'Archived since'])
      await expect
        .element(screen.getByRole('columnheader', { name }))
        .toBeVisible()
    expect(service.calls.map((call) => call.path)).toContain(
      '/v1/profile-shelf/archived',
    )
  })

  it('says what the archive means from behind an informer instead of a paragraph', async () => {
    installShelfService({
      profiles: [live(NEWEST, 'Newest'), archived(OLD_ARCHIVED, 'Old media')],
    })
    const screen = await renderShelf()

    await expect
      .element(screen.getByRole('radio', { name: 'Archive 1' }))
      .toBeInTheDocument()
    await expect
      .element(screen.getByText(/An archived profile is frozen/))
      .not.toBeInTheDocument()
    await screen.getByRole('button', { name: 'About the archive' }).click()
    await expect
      .element(screen.getByText(/An archived profile is frozen/))
      .toBeVisible()
  })

  // Leaving an archive that just emptied is the shelf's own move. It must not
  // withdraw the report of what the operator did to cause it.
  it('returns to the shelf and still says what was done when the last archived profile leaves', async () => {
    installShelfService({
      profiles: [live(NEWEST, 'Newest'), archived(OLD_ARCHIVED, 'Old media')],
    })
    const screen = await renderShelf()
    await screen.getByText('Archive 1', { exact: true }).click()
    await menuOf(screen, 'Old media').click()
    await screen.getByRole('menuitem', { name: 'Restore' }).click()

    await expect
      .element(
        screen.getByText('Profile “Old media” restored from the archive'),
      )
      .toBeVisible()
    await expect
      .element(screen.getByRole('link', { name: 'Old media' }))
      .toBeVisible()
    await expect
      .element(screen.getByRole('link', { name: 'Newest' }))
      .toBeVisible()
    expect(screen.getByRole('radio').all()).toHaveLength(0)
  })

  describe('permanent deletion', () => {
    it('asks first, names what is left behind, and removes the row only after the server agrees', async () => {
      const service = installShelfService({
        profiles: [
          live(NEWEST, 'Newest'),
          archived(OLD_ARCHIVED, 'Old media'),
          archived(SECOND_ARCHIVED, 'Older media'),
        ],
      })
      const screen = await renderShelf()
      await screen.getByText('Archive 2', { exact: true }).click()

      await menuOf(screen, 'Old media').click()
      await screen
        .getByRole('menuitem', { name: 'Delete permanently…' })
        .click()
      await expect
        .element(
          screen.getByRole('dialog', {
            name: 'Delete profile “Old media” permanently?',
          }),
        )
        .toBeVisible()
      await expect
        .element(
          screen.getByText(/subscription links of this profile will stop/),
        )
        .toBeVisible()
      await expect
        .element(
          screen.getByText(/Published files and the delivery log are kept/),
        )
        .toBeVisible()
      // Asking is not deleting.
      expect(service.posts()).toEqual([])

      await screen.getByRole('button', { name: 'Cancel' }).click()
      await expect.element(screen.getByRole('dialog')).not.toBeInTheDocument()
      expect(service.posts()).toEqual([])
      await expect
        .element(screen.getByRole('link', { name: 'Old media' }))
        .toBeVisible()

      await menuOf(screen, 'Old media').click()
      await screen
        .getByRole('menuitem', { name: 'Delete permanently…' })
        .click()
      await expect
        .element(
          screen.getByRole('button', {
            name: 'Delete permanently',
            exact: true,
          }),
        )
        .toHaveClass('rv-button--danger')
      await screen
        .getByRole('button', { name: 'Delete permanently', exact: true })
        .click()

      await expect
        .element(screen.getByRole('link', { name: 'Old media' }))
        .not.toBeInTheDocument()
      await expect
        .element(screen.getByText('Profile “Old media” deleted'))
        .toBeVisible()
      // The count follows the deletion, and the other archived row stays.
      await expect
        .element(screen.getByRole('radio', { name: 'Archive 1' }))
        .toBeInTheDocument()
      await expect
        .element(screen.getByRole('link', { name: 'Older media' }))
        .toBeVisible()
      expect(service.posts()).toEqual([`/v1/profiles/${OLD_ARCHIVED}/delete`])
      expect(service.deleted()).toBe(1)
    })

    it('returns to the shelf and still says the profile was deleted when it was the last archived one', async () => {
      installShelfService({
        profiles: [live(NEWEST, 'Newest'), archived(OLD_ARCHIVED, 'Old media')],
      })
      const screen = await renderShelf()
      await screen.getByText('Archive 1', { exact: true }).click()
      await menuOf(screen, 'Old media').click()
      await screen
        .getByRole('menuitem', { name: 'Delete permanently…' })
        .click()
      await screen
        .getByRole('button', { name: 'Delete permanently', exact: true })
        .click()

      await expect
        .element(screen.getByText('Profile “Old media” deleted'))
        .toBeVisible()
      await expect
        .element(screen.getByRole('link', { name: 'Newest' }))
        .toBeVisible()
      expect(screen.getByRole('radio').all()).toHaveLength(0)
    })

    it('lists the archived row actions with the permanent one last', async () => {
      installShelfService({
        profiles: [live(NEWEST, 'Newest'), archived(OLD_ARCHIVED, 'Old media')],
      })
      const screen = await renderShelf()
      await screen.getByText('Archive 1', { exact: true }).click()
      await menuOf(screen, 'Old media').click()

      const items = screen
        .getByRole('menuitem')
        .all()
        .map((item) => item.element().textContent.trim())
      expect(items).toEqual([
        'Open profile',
        'Export to file',
        'Restore',
        'Delete permanently…',
      ])
    })

    // Another tab may have deleted it first. The server answers not found, and
    // the row still has to leave and say why.
    it('withdraws the row and says it was deleted when the server no longer knows the profile', async () => {
      const service = installShelfService({
        deleteRefusal: { body: { error: 'not found' }, status: 404 },
        profiles: [live(NEWEST, 'Newest'), archived(OLD_ARCHIVED, 'Old media')],
      })
      const screen = await renderShelf()
      await screen.getByText('Archive 1', { exact: true }).click()
      await menuOf(screen, 'Old media').click()
      await screen
        .getByRole('menuitem', { name: 'Delete permanently…' })
        .click()
      await screen
        .getByRole('button', { name: 'Delete permanently', exact: true })
        .click()

      await expect
        .element(screen.getByText('Profile “Old media” deleted'))
        .toBeVisible()
      await expect.element(screen.getByRole('dialog')).not.toBeInTheDocument()
      await expect
        .element(screen.getByRole('link', { name: 'Newest' }))
        .toBeVisible()
      expect(service.deleted()).toBe(0)
    })

    // A deletion that also cleans routers can stop at one of them. The
    // question then says the profile stayed, names the router's refusal in
    // the words a send uses, and reads again what is left there.
    it('says the profile stayed and why when cleaning a router stops the deletion', async () => {
      const surface = {
        output_id: `output-${OLD_ARCHIVED}`,
        target_id: 'keenetic-dns',
        endpoint: 'http://192.0.2.1',
        connection: { id: 'device-1', name: 'Home router' },
        cleanable: true,
        fqdn_groups: ['routevane-a'],
      }
      const service = installShelfService({
        deleteRefusal: {
          body: {
            error: 'operation failed',
            code: 'rollback_failed',
            retired: false,
            device_cleanup: [],
          },
          status: 502,
        },
        deviceObjects: { device_objects: [surface], cleanable: true },
        profiles: [live(NEWEST, 'Newest'), archived(OLD_ARCHIVED, 'Old media')],
      })
      const screen = await renderShelf()
      await screen.getByText('Archive 1', { exact: true }).click()
      await menuOf(screen, 'Old media').click()
      await screen
        .getByRole('menuitem', { name: 'Delete permanently…' })
        .click()
      await expect
        .element(
          screen.getByText(
            /groups and routes already sent to routers stay in place/,
          ),
        )
        .toBeVisible()
      await screen
        .getByRole('switch', { name: 'Also remove from the router' })
        .click()
      // The question's body follows the switch.
      await expect
        .element(
          screen.getByText(
            /groups and routes sent to routers are removed from them/,
          ),
        )
        .toBeVisible()
      await screen
        .getByRole('button', { name: 'Delete permanently', exact: true })
        .click()

      await expect
        .element(
          screen.getByText('The rollback failed. Check the device by hand.'),
        )
        .toBeVisible()
      await expect
        .element(
          screen.getByText(
            'The profile was not deleted. Below is what is still on the routers.',
          ),
        )
        .toBeVisible()
      const deletion = service.calls.find(
        (call) => call.method === 'POST' && call.path.endsWith('/delete'),
      )
      expect(JSON.parse(deletion?.body ?? '{}')).toEqual({
        remove_from_devices: true,
      })
      const reads = service.calls.filter((call) =>
        call.path.endsWith('/device-objects'),
      )
      expect(reads).toHaveLength(2)
      expect(service.deleted()).toBe(0)
    })

    it('keeps the question open and says why when the server refuses', async () => {
      const service = installShelfService({
        deleteRefusal: {
          body: {
            error: 'profile is not archived',
            code: 'profile_not_archived',
          },
          status: 409,
        },
        profiles: [live(NEWEST, 'Newest'), archived(OLD_ARCHIVED, 'Old media')],
      })
      const screen = await renderShelf()
      await screen.getByText('Archive 1', { exact: true }).click()
      await menuOf(screen, 'Old media').click()
      await screen
        .getByRole('menuitem', { name: 'Delete permanently…' })
        .click()
      await screen
        .getByRole('button', { name: 'Delete permanently', exact: true })
        .click()

      await expect
        .element(
          screen.getByText(
            /The profile is not archived, so it cannot be deleted/,
          ),
        )
        .toBeVisible()
      await expect.element(screen.getByRole('dialog')).toBeVisible()
      // Asking again cannot succeed, so the act is no longer offered.
      await expect
        .element(
          screen.getByRole('button', {
            name: 'Delete permanently',
            exact: true,
          }),
        )
        .not.toBeInTheDocument()
      // The server's English is never shown.
      await expect
        .element(screen.getByText('profile is not archived'))
        .not.toBeInTheDocument()
      expect(service.deleted()).toBe(0)
    })
  })
})
