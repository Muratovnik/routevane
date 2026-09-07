import { describe, expect, it } from 'vitest'
import { render } from 'vitest-browser-vue'

import RvButton from '@/shared/ui/RvButton.vue'

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
