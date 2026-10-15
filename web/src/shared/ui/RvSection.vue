<script setup lang="ts">
import { useId, useTemplateRef } from 'vue'

/**
 * One section of a page: a framed surface named by its own heading, which is
 * also its accessible name. Every section draws the same ground, edge, inset
 * and heading, so pages differ in what a section holds, not in how it looks.
 *
 * `layout="beside"` puts the heading in a column of its own to the left of the
 * content and stacks the two where the section is too narrow for both.
 * `flush` lets the content run to the frame's edges — rows that carry their
 * own inset — below a header divided from it by a hairline; when the host
 * caps the section's height, the content scrolls inside the frame.
 *
 * Slots: the default slot is the content and always the section's last
 * element; `title` replaces the heading's words (a glyph beside a name);
 * `meta` stands in the heading's line without becoming part of its name (a
 * count); `actions` ends the header. The host sets `--rv-section-gap` on the
 * section for the space between the content's own children.
 */
withDefaults(
  defineProps<{
    title: string
    description?: string
    layout?: 'stacked' | 'beside'
    flush?: boolean
  }>(),
  { description: undefined, layout: 'stacked', flush: false },
)

const headingID = useId()
const heading = useTemplateRef<HTMLElement>('heading')
// A host that replaces what the section shows moves the keyboard to it.
defineExpose({ focusHeading: (): void => heading.value?.focus() })
</script>

<template>
  <section
    class="rv-section"
    :class="{
      'rv-section--beside': layout === 'beside',
      'rv-section--flush': flush,
    }"
    :aria-labelledby="headingID"
  >
    <header
      class="rv-section__header"
      :class="{ 'rv-section__header--described': description }"
    >
      <div class="rv-section__heading">
        <div class="rv-section__title-line">
          <h2
            :id="headingID"
            ref="heading"
            class="rv-section__title"
            tabindex="-1"
          >
            <slot name="title">{{ title }}</slot>
          </h2>
          <slot name="meta" />
        </div>
        <p v-if="description" class="rv-section__description">
          {{ description }}
        </p>
      </div>
      <div v-if="$slots.actions" class="rv-section__actions">
        <slot name="actions" />
      </div>
    </header>
    <div class="rv-section__content"><slot /></div>
  </section>
</template>

<style scoped>
.rv-section {
  display: grid;
  gap: var(--rv-space-6);
  align-content: start;
  min-width: 0;
  padding: var(--rv-space-6);
  background: var(--rv-color-surface);
  border: var(--rv-border-hair) solid var(--rv-color-rule);
  border-radius: var(--rv-radius-lg);
}

.rv-section__header {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-3);
  align-items: center;
  min-width: 0;
}

/* Under a description the heading is two lines, and an action centred on both
   would stand between them; it keeps the heading's first line instead. */
.rv-section__header--described {
  align-items: flex-start;
}

.rv-section__heading {
  display: grid;
  gap: var(--rv-space-2);
  flex: 1 1 auto;
  min-width: 0;
}

.rv-section__title-line {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-3);
  align-items: baseline;
  min-width: 0;
}

.rv-section__title {
  display: flex;
  gap: var(--rv-space-3);
  align-items: center;
  min-width: 0;
  font-size: var(--rv-text-module);
  overflow-wrap: anywhere;
}

.rv-section__description {
  max-width: var(--rv-measure-prose);
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
}

.rv-section__actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-3);
  align-items: center;
}

.rv-section__content {
  display: grid;
  gap: var(--rv-section-gap);
  align-content: start;
  min-width: 0;
}

/* The heading keeps its column while the content has its least width beside
   it; below that each takes the whole line, the heading first. The content's
   overwhelming grow factor leaves the heading at its own width while the two
   share a line. */
.rv-section--beside {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-6) var(--rv-space-8);
  align-items: flex-start;
}

.rv-section--beside > .rv-section__header {
  flex: 1 1 var(--rv-section-heading-width);
}

.rv-section--beside > .rv-section__content {
  flex: 999 1 var(--rv-section-content-min);
}

.rv-section--flush {
  grid-template-rows: auto minmax(0, 1fr);
  gap: 0;
  padding: 0;
}

.rv-section--flush > .rv-section__header {
  padding: var(--rv-space-6);
  border-bottom: var(--rv-border-hair) solid var(--rv-color-rule);
}

.rv-section--flush > .rv-section__content {
  min-height: 0;
  overflow-y: auto;
}
</style>
