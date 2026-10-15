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

  // The set's own strokes stay inside 2..14 of the 16 units, the way the other
  // glyphs are set, and a new glyph draws something.
  it.each(['link', 'unlink'] as const)(
    'draws the %s glyph inside the product icons’ box',
    async (name) => {
      const screen = await render(RvIcon, { props: { name } })
      const svg = screen.getByTestId('rv-icon').element()
      const frame = svg.getBoundingClientRect()
      const ink = [...svg.querySelectorAll('path')].map((path) =>
        path.getBoundingClientRect(),
      )
      expect(ink.length).toBeGreaterThan(1)
      const left = Math.min(...ink.map((box) => box.left)) - frame.left
      const right = Math.max(...ink.map((box) => box.right)) - frame.left
      const top = Math.min(...ink.map((box) => box.top)) - frame.top
      const bottom = Math.max(...ink.map((box) => box.bottom)) - frame.top
      for (const edge of [left, top]) expect(edge).toBeGreaterThanOrEqual(1.25)
      for (const edge of [right, bottom])
        expect(edge).toBeLessThanOrEqual(14.75)
    },
  )

  // A publisher's artwork runs to the edge of its own box, so drawn like the
  // product's strokes — which stay inside 2..14 of 16 units — it looks larger
  // than the icon beside it. Its ink takes the same share, on the same centre.
  it.each(['amnezia', 'keenetic', 'mikrotik', 'openwrt', 'sing-box'] as const)(
    'draws the %s mark inside the optical box of the product icons',
    async (name) => {
      const screen = await render(RvIcon, { props: { name } })
      const svg = screen.getByTestId('rv-icon').element()
      const frame = svg.getBoundingClientRect()
      expect(frame.width).toBe(16)

      const ink = [...svg.querySelectorAll('path')].map((path) =>
        path.getBoundingClientRect(),
      )
      const left = Math.min(...ink.map((box) => box.left))
      const right = Math.max(...ink.map((box) => box.right))
      const top = Math.min(...ink.map((box) => box.top))
      const bottom = Math.max(...ink.map((box) => box.bottom))

      // 12 of 16 units is the product's own box; the larger side may reach it.
      expect(Math.max(right - left, bottom - top)).toBeLessThanOrEqual(12.5)
      expect(Math.max(right - left, bottom - top)).toBeGreaterThan(9)
      expect(Math.abs((left + right) / 2 - (frame.left + 8))).toBeLessThan(0.75)
      expect(Math.abs((top + bottom) / 2 - (frame.top + 8))).toBeLessThan(0.75)
    },
  )
})
