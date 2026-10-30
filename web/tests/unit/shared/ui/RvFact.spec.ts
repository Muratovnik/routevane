import { describe, expect, it } from 'vitest'
import { render } from 'vitest-browser-vue'

import RvFact from '@/shared/ui/RvFact.vue'

describe('RvFact', () => {
  it('states a fact in words', async () => {
    const screen = await render(RvFact, { props: { label: 'Password saved' } })

    await expect.element(screen.getByText('Password saved')).toBeVisible()
  })

  // A fact is not a status: a green dot beside «Password saved» read as a
  // check that never ran. The mark is the check, in the brand accent rather
  // than the status palette's colour.
  it('takes the check mark in the brand accent, not a status dot', async () => {
    const screen = await render(RvFact, { props: { label: 'Password saved' } })
    const mark = screen.getByTestId('rv-fact-mark').element()
    // The colour the accent role resolves to, read the way the mark reads it.
    const probe = document.createElement('span')
    probe.style.color = 'var(--rv-color-accent-ink)'
    document.body.append(probe)
    const accent = getComputedStyle(probe).color
    probe.remove()

    expect(mark.querySelector('svg')).not.toBeNull()
    expect(getComputedStyle(mark).color).toBe(accent)
    await expect.element(screen.getByRole('status')).not.toBeInTheDocument()
  })
})
