/**
 * Colour arithmetic for the suites that judge what an operator can see.
 *
 * The library draws with `color-mix`, so a computed colour can come back in any
 * CSS colour space. The browser itself converts it: a one-pixel canvas takes the
 * value and answers sRGB, which is then composited over an opaque ground.
 */
import type { Page } from '@playwright/test'

export type Rgb = [number, number, number]

export const toRgb = (page: Page, value: string, ground: Rgb): Promise<Rgb> =>
  page.evaluate(
    ([css, under]) => {
      const canvas = document.createElement('canvas')
      canvas.width = 1
      canvas.height = 1
      const context = canvas.getContext('2d', { willReadFrequently: true })!
      context.clearRect(0, 0, 1, 1)
      context.fillStyle = css
      context.fillRect(0, 0, 1, 1)
      const [r, g, b, a] = context.getImageData(0, 0, 1, 1).data
      const alpha = a! / 255
      return [
        Math.round(r! * alpha + under![0]! * (1 - alpha)),
        Math.round(g! * alpha + under![1]! * (1 - alpha)),
        Math.round(b! * alpha + under![2]! * (1 - alpha)),
      ] as Rgb
    },
    [value, ground] as const,
  )

const channel = (value: number): number => {
  const unit = value / 255
  return unit <= 0.04045 ? unit / 12.92 : ((unit + 0.055) / 1.055) ** 2.4
}

const luminance = ([r, g, b]: Rgb): number =>
  0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b)

/**
 * The WCAG contrast ratio of two CSS colours. The first may be translucent and
 * is composited over the second, which must be opaque.
 */
export const contrastRatio = async (
  page: Page,
  foreground: string,
  background: string,
): Promise<number> => {
  const ground = await toRgb(page, background, [255, 255, 255])
  const top = await toRgb(page, foreground, ground)
  const [lighter, darker] = [luminance(top), luminance(ground)].sort(
    (a, b) => b - a,
  )
  return (lighter! + 0.05) / (darker! + 0.05)
}
