<script setup lang="ts">
import { computed } from 'vue'

import { BRAND_GLYPHS } from '@/shared/lib/brandGlyphs'
import type { IconName } from '@/shared/ui/types'

/**
 * The product's icon set: hand-set 16-unit strokes, one per meaning. Icons are
 * always decorative here — the accessible name belongs to the control that
 * hosts the icon, so every rendering is aria-hidden.
 *
 * A vendor's mark is not one of those meanings and is not drawn here: a name
 * that stands for a publisher is served from `brandGlyphs`, in that
 * publisher's own artwork and its own coordinate space, which is why the
 * viewBox below is read from the glyph rather than fixed at 16.
 *
 * `spin` turns the glyph in place while the act it names is running. It is for
 * a command whose own glyph is the progress — a refresh — so the control keeps
 * its width and its identity instead of growing a second mark.
 */
const props = defineProps<{
  name: IconName
  spin?: boolean
}>()

const brand = computed(() => BRAND_GLYPHS[props.name] ?? null)

const strokes: Record<string, string[]> = {
  archive: ['M2 3.5h12v3H2v-3Z', 'M3 6.5v7h10v-7', 'M6.5 9.5h3'],
  check: ['M3 8.5l3.5 3.5L13 5'],
  chevron: ['M4 6.5 8 10.5l4-4'],
  close: ['M3 3l10 10', 'M13 3 3 13'],
  copy: [
    'M6 6h6.5A1.5 1.5 0 0 1 14 7.5v5a1.5 1.5 0 0 1-1.5 1.5h-5A1.5 1.5 0 0 1 6 12.5V6Z',
    'M10 6V3.5A1.5 1.5 0 0 0 8.5 2h-5A1.5 1.5 0 0 0 2 3.5v5A1.5 1.5 0 0 0 3.5 10H6',
  ],
  download: ['M8 2.5v8', 'M4.5 7.5 8 11l3.5-3.5', 'M3 13.5h10'],
  drag: ['M5 4h6', 'M5 8h6', 'M5 12h6'],
  edit: ['M11 2.5 13.5 5 6 12.5 3 13l.5-3L11 2.5Z'],
  external: [
    'M6.5 3.5H4A1.5 1.5 0 0 0 2.5 5v7A1.5 1.5 0 0 0 4 13.5h7a1.5 1.5 0 0 0 1.5-1.5V9.5',
    'M9.5 2.5h4v4',
    'M13.5 2.5 8 8',
  ],
  file: ['M4 1.5h5l3 3v10H4v-13Z', 'M9 1.5v3h3', 'M6 8h4', 'M6 11h4'],
  info: ['M8 14.5A6.5 6.5 0 1 0 8 1.5a6.5 6.5 0 0 0 0 13Z', 'M8 7.5V11'],
  library: ['M6 4.5h7.5', 'M6 8h7.5', 'M6 11.5h7.5'],
  plus: ['M8 3v10', 'M3 8h10'],
  refresh: ['M13.5 8A5.5 5.5 0 1 1 11.9 4.1', 'M13.5 2v3h-3'],
  search: ['M7 11.5a4.5 4.5 0 1 0 0-9 4.5 4.5 0 0 0 0 9Z', 'M10.5 10.5 14 14'],
  send: ['M14 2 7.5 8.5', 'M14 2 9.7 14l-2.2-5.5L2 6.3 14 2Z'],
  settings: [
    'M2.5 5.5h5.75',
    'M11.75 5.5h1.75',
    'M10 7.25a1.75 1.75 0 1 0 0-3.5 1.75 1.75 0 0 0 0 3.5Z',
    'M2.5 10.5h1.75',
    'M7.75 10.5h5.75',
    'M6 12.25a1.75 1.75 0 1 0 0-3.5 1.75 1.75 0 0 0 0 3.5Z',
  ],
  targets: [
    'M2 9h12v3.5A1.5 1.5 0 0 1 12.5 14h-9A1.5 1.5 0 0 1 2 12.5V9Z',
    'M5 9V5.5',
    'M11 9V4',
    'M4.75 11.5h.5',
  ],
  trash: [
    'M3 4.5h10',
    'M6 4.5V3h4v1.5',
    'M4.5 4.5l.6 8.5h5.8l.6-8.5',
    'M6.8 7.5v3.5',
    'M9.2 7.5v3.5',
  ],
  warning: ['M8 2.5 14.5 13.5H1.5L8 2.5Z', 'M8 7v3'],
}

const dots: Record<string, [number, number, number][]> = {
  dots: [
    [8, 3.5, 1.15],
    [8, 8, 1.15],
    [8, 12.5, 1.15],
  ],
  info: [[8, 5, 0.85]],
  library: [
    [3.25, 4.5, 0.9],
    [3.25, 8, 0.9],
    [3.25, 11.5, 0.9],
  ],
  warning: [[8, 12, 0.85]],
}
</script>

<template>
  <!-- Decoration beside an accessible name, so it is hidden from the
       accessibility tree and carries a test hook rather than a role. -->
  <svg
    class="rv-icon"
    :class="{ 'rv-icon--spin': spin }"
    aria-hidden="true"
    data-testid="rv-icon"
    fill="none"
    focusable="false"
    :viewBox="brand?.viewBox ?? '0 0 16 16'"
  >
    <template v-if="brand">
      <path
        v-for="(d, index) in brand.fills ?? []"
        :key="`f${index}`"
        :d="d"
        fill="currentColor"
      />
      <path
        v-for="(d, index) in brand.strokes ?? []"
        :key="`b${index}`"
        :d="d"
        stroke="currentColor"
        stroke-linecap="round"
        stroke-linejoin="round"
        :stroke-width="brand.strokeWidth"
      />
    </template>
    <template v-else>
      <path
        v-for="(d, index) in strokes[name] ?? []"
        :key="`s${index}`"
        :d="d"
        stroke="currentColor"
        stroke-linecap="round"
        stroke-linejoin="round"
        stroke-width="1.5"
      />
      <circle
        v-for="([cx, cy, r], index) in dots[name] ?? []"
        :key="`d${index}`"
        :cx="cx"
        :cy="cy"
        fill="currentColor"
        :r="r"
      />
    </template>
  </svg>
</template>

<style scoped>
.rv-icon {
  flex: none;
  width: 1em;
  height: 1em;
  font-size: 1rem;
}

/* Reduced motion is neutralised once, globally, for every animation on this
   surface; nothing is repeated here. */
.rv-icon--spin {
  animation: rv-icon-spin var(--rv-motion-working) linear infinite;
}

@keyframes rv-icon-spin {
  to {
    transform: rotate(1turn);
  }
}
</style>
