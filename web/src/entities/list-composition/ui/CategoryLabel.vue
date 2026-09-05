<script setup lang="ts">
defineProps<{ id: string; label: string }>()
const categoryTones: Record<string, string> = {
  ai: 'violet',
  'ai-tools': 'cyan',
  development: 'amber',
  education: 'green',
  games: 'pink',
  google: 'blue',
  infrastructure: 'slate',
  music: 'pink',
  video: 'red',
  messengers: 'cyan',
  socials: 'violet',
  work: 'blue',
  torrents: 'amber',
}
function categoryTone(id: string): string {
  return (
    categoryTones[id] ??
    ['violet', 'cyan', 'amber', 'green', 'pink', 'blue'][
      [...id].reduce((value, char) => value + char.codePointAt(0)!, 0) % 6
    ]!
  )
}
</script>
<template>
  <span class="category-label"
    ><span
      class="category-label__dot"
      :data-tone="categoryTone(id)"
      aria-hidden="true"
    />{{ label }}</span
  >
</template>
<style scoped>
.category-label {
  display: inline-flex;
  align-items: center;
  gap: var(--rv-space-2);
  max-width: 100%;
  margin-inline-end: var(--rv-space-3);
  overflow-wrap: anywhere;
}

.category-label__dot {
  flex: none;
  width: var(--rv-space-2);
  height: var(--rv-space-2);
  border-radius: var(--rv-radius-lg);
  background: var(--rv-color-category-slate);
}

.category-label__dot[data-tone='violet'] {
  background: var(--rv-color-category-violet);
}

.category-label__dot[data-tone='cyan'] {
  background: var(--rv-color-category-cyan);
}

.category-label__dot[data-tone='amber'] {
  background: var(--rv-color-category-amber);
}

.category-label__dot[data-tone='green'] {
  background: var(--rv-color-category-green);
}

.category-label__dot[data-tone='pink'] {
  background: var(--rv-color-category-pink);
}

.category-label__dot[data-tone='blue'] {
  background: var(--rv-color-category-blue);
}

.category-label__dot[data-tone='slate'] {
  background: var(--rv-color-category-slate);
}

.category-label__dot[data-tone='red'] {
  background: var(--rv-color-category-red);
}
</style>
