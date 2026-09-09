<script setup lang="ts">
import type { IconName } from '@/shared/ui/types'

/**
 * The product's icon set: hand-set 16-unit strokes, one per meaning. Icons are
 * always decorative here — the accessible name belongs to the control that
 * hosts the icon, so every rendering is aria-hidden.
 */
defineProps<{
  name: IconName
}>()

const strokes: Record<string, string[]> = {
  amnezia: ['M2.5 13 8 2.5 13.5 13', 'M5 9.5h6', 'M8 6v3.5'],
  keenetic: ['M4 2.5v11', 'M12 2.5 6.5 8 12 13.5'],
  mikrotik: ['M2.5 3.5a10 10 0 0 0 10 10', 'M5 2.5a8.5 8.5 0 0 0 8.5 8.5'],
  openwrt: [
    'M3 6a7 7 0 0 1 10 0',
    'M5 8a4 4 0 0 1 6 0',
    'M8 10v3.5',
    'M6 13.5h4',
  ],
  'sing-box': [
    'M8 1.5 14 5v6L8 14.5 2 11V5L8 1.5Z',
    'M2 5l6 3.5L14 5',
    'M8 8.5v6',
    'M5 3.25l6 3.5',
  ],
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
    aria-hidden="true"
    data-testid="rv-icon"
    fill="none"
    focusable="false"
    viewBox="0 0 16 16"
  >
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
  </svg>
</template>

<style scoped>
.rv-icon {
  flex: none;
  width: 1em;
  height: 1em;
  font-size: 1rem;
}
</style>
