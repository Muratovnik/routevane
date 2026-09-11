import { describe, expect, it } from 'vitest'
import { render } from 'vitest-browser-vue'

import RvIcon from '@/shared/ui/RvIcon.vue'

describe('RvIcon', () => {
  it('turns in place only while the command it names is working', async () => {
    // The glyph is decoration beside an accessible name, so it is deliberately
    // aria-hidden and its test hook is the only way to reach the drawing.
    const screen = await render(RvIcon, { props: { name: 'refresh' } })
    const glyph = screen.getByTestId('rv-icon')
    expect(getComputedStyle(glyph.element()).animationName).toBe('none')

    await screen.rerender({ spin: true })
    expect(getComputedStyle(glyph.element()).animationName).not.toBe('none')

    await screen.rerender({ spin: false })
    expect(getComputedStyle(glyph.element()).animationName).toBe('none')
  })
})
