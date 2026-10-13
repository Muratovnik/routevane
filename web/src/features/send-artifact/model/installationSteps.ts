/**
 * The steps of a catalog installation instruction.
 *
 * The catalog keeps an instruction on one line, so a target that has steps
 * numbers them in place: «1. … 2. … 3. …». A step begins only where the next
 * number in sequence stands at the start or after a space and is followed by
 * a dot and a space, so a version such as «5.0» or a number out of sequence is
 * never read as a new step. An instruction without that numbering is one step.
 */
const MARKER = /(^|\s)(\d+)\.\s+/g

export const installationSteps = (hint: string): string[] => {
  const text = hint.trim()
  const starts: { at: number; body: number }[] = []
  for (const found of text.matchAll(MARKER)) {
    if (Number(found[2]) !== starts.length + 1) continue
    const at = found.index + (found[1] ?? '').length
    if (starts.length === 0 && at !== 0) break
    starts.push({ at, body: found.index + found[0].length })
  }
  if (starts.length < 2) return text === '' ? [] : [text]
  return starts.map((start, index) =>
    text.slice(start.body, starts[index + 1]?.at ?? text.length).trim(),
  )
}
