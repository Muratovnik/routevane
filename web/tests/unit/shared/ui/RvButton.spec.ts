import { describe, expect, it } from 'vitest'
import { render, type RenderResult } from 'vitest-browser-vue'

import RvButton from '@/shared/ui/RvButton.vue'

// Renders share one document, so each control is read by the words on it.
const painted = (
  screen: RenderResult<unknown>,
  name: string,
): CSSStyleDeclaration =>
  getComputedStyle(screen.getByRole('button', { name }).element())

const TRANSPARENT = 'rgba(0, 0, 0, 0)'

// A token as the browser paints it, read off a probe rather than from the
// token's text: the quiet accent is written with an alpha channel, so only a
// painted box compares with a painted button.
const paintedToken = (name: string): string => {
  const probe = document.createElement('span')
  probe.style.background = `var(${name})`
  document.body.append(probe)
  const painted = getComputedStyle(probe).backgroundColor
  probe.remove()
  return painted
}

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
