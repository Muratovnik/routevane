const channel = (value: number): number => {
  const scaled = value / 255
  return scaled <= 0.03928 ? scaled / 12.92 : ((scaled + 0.055) / 1.055) ** 2.4
}

const luminance = (color: string): number => {
  const [red, green, blue] = (color.match(/[\d.]+/g) ?? []).map(Number)
  return (
    0.2126 * channel(red ?? 0) +
    0.7152 * channel(green ?? 0) +
    0.0722 * channel(blue ?? 0)
  )
}

/** WCAG relative-luminance contrast of two opaque computed `rgb()` colours. */
export const contrast = (first: string, second: string): number => {
  const [lighter, darker] = [luminance(first), luminance(second)].sort(
    (a, b) => b - a,
  )
  return (lighter! + 0.05) / (darker! + 0.05)
}

/**
 * Runs `body` with the document in one explicit theme, then puts it back. The
 * unit browser prefers a light scheme, so a spec that reads a themed colour
 * names the theme instead of inheriting whichever one the browser reports.
 */
export const inTheme = async (
  theme: 'dark' | 'light',
  body: () => Promise<void>,
): Promise<void> => {
  document.documentElement.setAttribute('data-rv-theme', theme)
  try {
    await body()
  } finally {
    document.documentElement.removeAttribute('data-rv-theme')
  }
}

/**
 * A token as the browser paints it, read off a probe rather than from the
 * token's text: a role may be written with an alpha channel or as a reference
 * to another, so only a painted box compares with a painted component.
 */
export const paintedToken = (name: string): string => {
  const probe = document.createElement('span')
  probe.style.background = `var(${name})`
  document.body.append(probe)
  const painted = getComputedStyle(probe).backgroundColor
  probe.remove()
  return painted
}
