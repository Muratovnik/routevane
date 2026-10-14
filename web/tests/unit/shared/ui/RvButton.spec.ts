import { describe, expect, it } from 'vitest'
import { render, type RenderResult } from 'vitest-browser-vue'

import RvButton from '@/shared/ui/RvButton.vue'

import { contrast, inTheme, paintedToken } from './support/contrast'

// Renders share one document, so each control is read by the words on it.
const painted = (
  screen: RenderResult<unknown>,
  name: string,
): CSSStyleDeclaration =>
  getComputedStyle(screen.getByRole('button', { name }).element())

const TRANSPARENT = 'rgba(0, 0, 0, 0)'

describe('RvButton', () => {
  it('forwards pending state while keeping one accessible progress mark', async () => {
    const screen = await render(RvButton, {
      props: { loading: true, loadingLabel: 'Saving profile' },
      slots: { default: 'Save' },
    })

    const button = screen.getByRole('button', { name: 'Saving profile' })
    await expect.element(button).toHaveAttribute('aria-busy', 'true')
    await expect.element(button).toBeDisabled()
    await expect.element(button).toHaveTextContent('Save')
    // The spinner is decoration next to that accessible name, so it is
    // deliberately aria-hidden and its test hook is the only way to see it.
    await expect.element(screen.getByTestId('rv-button-spinner')).toBeVisible()
  })

  // An unavailable primary used to be indistinguishable from the secondary
  // beside it, so the screen stopped saying which act was the principal one.
  it('keeps the primary act recognizable while it is unavailable', async () => {
    const screen = await render(RvButton, {
      props: { disabled: true, variant: 'primary' },
      slots: { default: 'Save and rebuild' },
    })
    await render(RvButton, {
      props: { disabled: true, variant: 'secondary' },
      slots: { default: 'Cancel' },
    })
    await render(RvButton, {
      props: { variant: 'primary' },
      slots: { default: 'Rebuild now' },
    })

    const unavailable = painted(screen, 'Save and rebuild')
    expect(unavailable.backgroundColor).not.toBe(
      painted(screen, 'Cancel').backgroundColor,
    )
    // Quieted, not merely repainted: the ground is the accent at low
    // emphasis, not the full accent and not a third neutral.
    expect(unavailable.backgroundColor).not.toBe(
      painted(screen, 'Rebuild now').backgroundColor,
    )
    expect(unavailable.backgroundColor).toBe(
      paintedToken('--rv-color-accent-quiet'),
    )
    // The secondary keeps the edge that is its own; the primary has none.
    expect(unavailable.borderTopColor).toBe(TRANSPARENT)
    expect(painted(screen, 'Cancel').borderTopColor).not.toBe(TRANSPARENT)
  })

  // The act that cannot be taken back is told from the principal act: it asks
  // the library for its error colour, stands on the danger ground and keeps
  // its words readable on it, resting and hovered, in both themes. Unavailable
  // is a ground of its own, so a working confirmation does not turn neutral.
  it.each(['dark', 'light'] as const)(
    'paints the destructive act on the danger ground in the %s theme',
    async (theme) => {
      await inTheme(theme, async () => {
        const screen = await render(RvButton, {
          props: { variant: 'danger' },
          slots: { default: 'Delete permanently' },
        })
        await render(RvButton, {
          props: { variant: 'primary' },
          slots: { default: 'Save' },
        })
        await render(RvButton, {
          props: { disabled: true, variant: 'danger' },
          slots: { default: 'Forget' },
        })

        const button = screen.getByRole('button', {
          name: 'Delete permanently',
        })
        await expect.element(button).toHaveAttribute('data-color', 'error')
        await expect.element(button).toHaveAttribute('data-variant', 'link')
        const danger = painted(screen, 'Delete permanently')
        expect(danger.backgroundColor).toBe(paintedToken('--rv-color-danger'))
        expect(danger.backgroundColor).not.toBe(
          painted(screen, 'Save').backgroundColor,
        )
        expect(
          contrast(danger.color, danger.backgroundColor),
        ).toBeGreaterThanOrEqual(4.5)
        expect(
          contrast(
            paintedToken('--rv-color-ink-inverted'),
            paintedToken('--rv-color-danger-hover'),
          ),
        ).toBeGreaterThanOrEqual(4.5)

        // Unavailable, it is the same act at low emphasis, not a neutral one.
        const unavailable = painted(screen, 'Forget')
        expect(unavailable.backgroundColor).toBe(
          paintedToken('--rv-color-danger-quiet'),
        )
        expect(unavailable.backgroundColor).not.toBe(
          painted(screen, 'Save').backgroundColor,
        )
      })
    },
  )

  it('keeps the principal act off the danger look', async () => {
    const screen = await render(RvButton, {
      props: { variant: 'primary' },
      slots: { default: 'Archive' },
    })

    const archive = screen.getByRole('button', { name: 'Archive' })
    await expect.element(archive).toHaveAttribute('data-color', 'neutral')
    await expect.element(archive).not.toHaveClass('rv-button--danger')
  })

  // An unavailable button has to read as unavailable beside the same button
  // when it is available: it gives up its ground and its words dim, yet they stay legible (disabled controls are exempt from 4.5:1, not
  // from being read).
  it.each(['dark', 'light'] as const)(
    'draws an unavailable button apart from an available one in the %s theme',
    async (theme) => {
      await inTheme(theme, async () => {
        const screen = await render(RvButton, {
          props: { variant: 'secondary' },
          slots: { default: 'Add format' },
        })
        await render(RvButton, {
          props: { disabled: true, variant: 'secondary' },
          slots: { default: 'Add formats' },
        })
        await render(RvButton, {
          props: { disabled: true, variant: 'quiet' },
          slots: { default: 'Retry' },
        })

        const enabled = painted(screen, 'Add format')
        const disabled = painted(screen, 'Add formats')
        expect(disabled.backgroundColor).not.toBe(enabled.backgroundColor)
        expect(disabled.color).not.toBe(enabled.color)
        expect(disabled.color).toBe(paintedToken('--rv-color-ink-disabled'))
        // Legible against the surface it stands on, at 3:1.
        expect(
          contrast(disabled.color, paintedToken('--rv-color-canvas')),
        ).toBeGreaterThanOrEqual(3)
        // A quiet button has no edge to give up, and stays without one.
        expect(painted(screen, 'Retry').borderTopColor).toBe(TRANSPARENT)
      })
    },
  )

  // A button shares the standard field height with the field beside it; only
  // a page header's principal action asks for the larger target, and a row or
  // a filter for the compact one (docs/UI.md, Tokens and State and honesty).
  it('stands at the field height unless it asks for another size', async () => {
    const screen = await render(RvButton, { slots: { default: 'Add' } })
    await render(RvButton, {
      props: { size: 'touch' },
      slots: { default: 'New list' },
    })
    await render(RvButton, {
      props: { size: 'compact' },
      slots: { default: 'Retry' },
    })

    const height = (name: string): number =>
      screen.getByRole('button', { name }).element().getBoundingClientRect()
        .height
    expect(height('Add')).toBe(40)
    expect(height('New list')).toBe(44)
    expect(height('Retry')).toBe(36)
  })

  it('forwards an href as an external native-link request', async () => {
    const screen = await render(RvButton, {
      props: { disabled: false, href: '/artifact' },
      slots: { default: 'Download' },
    })

    const link = screen.getByRole('link', { name: 'Download' })
    await expect.element(link).toHaveAttribute('href', '/artifact')
    await expect.element(link).toHaveAttribute('data-external', 'true')

    // A destination that is not available is refused rather than navigable: no
    // href to follow, and the refusal stated where a reader hears it.
    await screen.rerender({ disabled: true })
    const refused = screen.getByText('Download')
    await expect.element(refused).toHaveAttribute('aria-disabled', 'true')
    await expect.element(refused).not.toHaveAttribute('href')
  })

  it('forwards internal destinations to the router with the visual variant', async () => {
    const screen = await render(RvButton, {
      props: { to: '/profiles/new', variant: 'primary' },
      slots: { default: 'Create' },
    })

    const link = screen.getByRole('link', { name: 'Create' })
    await expect.element(link).toHaveAttribute('href', '/profiles/new')
    await expect.element(link).toHaveAttribute('data-external', 'false')
    // The library button is always asked for its own `link` variant while the
    // Routevane variant stays a facade class, so the two visual systems cannot
    // collide on one element.
    await expect.element(link).toHaveAttribute('data-variant', 'link')
    await expect.element(link).toHaveClass('rv-button--primary')
  })
})
