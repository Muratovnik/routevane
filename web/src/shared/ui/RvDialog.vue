<script setup lang="ts">
import {
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogOverlay,
  DialogPortal,
  DialogRoot,
  DialogTitle,
} from 'reka-ui'
import { computed, onBeforeUnmount, ref, watch } from 'vue'

import { openDialogs } from '@/shared/ui/dialogStack'
import RvIcon from '@/shared/ui/RvIcon.vue'

/**
 * The product's modal surface.
 *
 * A native `<dialog>` sits in the top layer only when `showModal` succeeds, and
 * a card opened from a scrolled page was landing outside the viewport whenever
 * it did not. This owns the geometry instead: the panel is portalled to the
 * document body and positioned against the viewport, so what is behind it can
 * be any length and any scroll offset.
 *
 * The primitives underneath supply the parts a modal has to get right — the
 * scroll lock on the page behind, the focus trap, Escape, the click on the
 * scrim, and the aria wiring between the panel, its title and its description.
 * Everything visible is ours.
 */
const props = defineProps<{
  /** What to call the control that dismisses the panel. */
  closeLabel: string
  /** Standing background for the panel, read out with its title. */
  description?: string
  /**
   * Whether the body's content claims the panel's whole height rather than
   * sitting at its top. The slot is then one element, and that element owns
   * what inside it scrolls — a table that fills the sheet instead of a band of
   * nothing between the last row and the footer.
   */
  fill?: boolean
  open: boolean
  title: string
  /**
   * `sheet` is the full-height working surface on the trailing edge; `panel` is
   * a centred box for one bounded secondary decision.
   */
  variant?: 'sheet' | 'panel'
}>()

const emit = defineEmits<{
  'update:open': [value: boolean]
}>()

// Where the keyboard was before this panel took it. The primitive returns focus
// to a trigger element, and this dialog is opened by state rather than by one,
// so the opener is remembered here and handed the focus back itself.
const opener = ref<HTMLElement | null>(null)

// Whether something was already dimming the page when this panel opened.
const nested = ref(false)
// Whether this instance is currently one of the panels the counter is counting.
// Without it a repeated `false` — a close, then an unmount — would decrement
// the stack twice and leave the next panel believing it is nested.
let counted = false

function enter(): void {
  if (counted) return
  nested.value = openDialogs.value > 0
  openDialogs.value += 1
  counted = true
}

function leave(): void {
  if (!counted) return
  openDialogs.value -= 1
  counted = false
  nested.value = false
}

watch(
  () => props.open,
  (open) => {
    if (!open) {
      leave()
      return
    }
    enter()
    const active = document.activeElement
    opener.value = active instanceof HTMLElement ? active : null
  },
  { immediate: true },
)

onBeforeUnmount(leave)

// A panel with no description states that it has none, which is the escape the
// primitive documents. Passing the attribute as `undefined` would instead strip
// the wiring the primitive puts there when a description does exist.
const describedBy = computed<{ 'aria-describedby'?: string }>(() =>
  props.description === undefined ? { 'aria-describedby': 'undefined' } : {},
)

function onCloseAutoFocus(event: Event): void {
  const element = opener.value
  if (element === null || !element.isConnected) return
  event.preventDefault()
  element.focus()
}

// The primitive would hand the keyboard to the first tabbable element, which
// is the close control in the header. A panel opened to be filled in starts in
// its first field instead: that is where the next keystroke belongs, and a
// keystroke that lands before this delayed focus is not undone by it.
function onOpenAutoFocus(event: Event): void {
  const panel = event.target
  if (!(panel instanceof HTMLElement)) return
  const field = panel.querySelector<HTMLElement>(
    '.rv-dialog__body :is(input:not([type="hidden"]), textarea, select, [role="combobox"]):not([disabled])',
  )
  if (field === null) return
  event.preventDefault()
  field.focus()
}
</script>

<template>
  <DialogRoot :open="open" @update:open="emit('update:open', $event)">
    <DialogPortal>
      <DialogOverlay
        class="rv-dialog__scrim"
        :class="{ 'rv-dialog__scrim--nested': nested }"
      />
      <DialogContent
        class="rv-dialog"
        :class="`rv-dialog--${variant ?? 'sheet'}`"
        v-bind="describedBy"
        @close-auto-focus="onCloseAutoFocus"
        @open-auto-focus="onOpenAutoFocus"
      >
        <header class="rv-dialog__header">
          <div class="rv-dialog__heading">
            <DialogTitle class="rv-dialog__title">{{ title }}</DialogTitle>
            <DialogDescription
              v-if="description !== undefined"
              class="rv-dialog__description"
            >
              {{ description }}
            </DialogDescription>
          </div>
          <DialogClose :aria-label="closeLabel" class="rv-dialog__close">
            <RvIcon name="close" />
          </DialogClose>
        </header>

        <div class="rv-dialog__body" :class="{ 'rv-dialog__body--fill': fill }">
          <slot />
        </div>

        <footer v-if="$slots.footer" class="rv-dialog__footer">
          <slot name="footer" />
        </footer>
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>

<style scoped>
.rv-dialog__scrim {
  position: fixed;
  z-index: 5;
  inset: 0;
  background: var(--rv-color-scrim);
}

/* One dimming per stack: a panel opened over a sheet darkens nothing twice. The
   layer stays, so a pointer landing outside it still closes the panel. */
.rv-dialog__scrim--nested {
  background: transparent;
}

.rv-dialog {
  position: fixed;
  z-index: 6;
  display: grid;
  grid-template-rows: auto minmax(0, 1fr) auto;
  overflow: hidden;
  color: var(--rv-color-ink);
  background: var(--rv-color-canvas);
  box-shadow: var(--rv-shadow-raised);
}

/* The working surface: full height on the trailing edge, wide enough to read an
   address and its origin on one line, and the whole viewport when there is no
   room beside it. */
.rv-dialog--sheet {
  inset: 0 0 0 auto;
  width: min(var(--rv-dialog-width), 100%);
  height: 100dvh;
  border-left: var(--rv-border-hair) solid var(--rv-color-rule);
}

/* One bounded decision: centred, no taller than it needs to be, and never
   taller than the viewport it is centred in. */
.rv-dialog--panel {
  top: 50%;
  left: 50%;
  width: min(var(--rv-dialog-panel-width), calc(100% - var(--rv-space-8)));
  max-height: min(
    var(--rv-dialog-panel-height),
    calc(100dvh - var(--rv-space-8))
  );
  border: var(--rv-border-hair) solid var(--rv-color-rule-strong);
  border-radius: var(--rv-radius-lg);
  transform: translate(-50%, -50%);
}

.rv-dialog__header {
  display: flex;
  gap: var(--rv-space-4);
  align-items: flex-start;
  padding: var(--rv-space-5) var(--rv-space-6);
  border-bottom: var(--rv-border-hair) solid var(--rv-color-rule);
}

.rv-dialog__heading {
  display: grid;
  gap: var(--rv-space-1);
  flex: 1;
  min-width: 0;
}

.rv-dialog__title {
  font-weight: 600;
  font-size: var(--rv-text-module);
  overflow-wrap: anywhere;
}

/* The reach of the panel, stated once and always: quiet type, because it is a
   standing fact rather than something that just happened. */
.rv-dialog__description {
  max-width: var(--rv-measure-prose);
  color: var(--rv-color-ink-tertiary);
  font-size: var(--rv-text-meta);
}

.rv-dialog__close {
  display: inline-flex;
  flex: none;
  align-items: center;
  justify-content: center;
  width: var(--rv-control-touch);
  height: var(--rv-control-touch);
  padding: 0;
  color: var(--rv-color-ink-muted);
  background: transparent;
  border: 0;
  border-radius: var(--rv-radius-md);
  cursor: pointer;
}

.rv-dialog__close:hover {
  color: var(--rv-color-ink);
  background: var(--rv-color-surface-hover);
}

.rv-dialog__body {
  display: grid;
  align-content: start;
  min-height: 0;
  overflow-y: auto;
}

/* The panel's own height, handed to the content instead of kept as empty space
   under it. The body scrolls nothing itself in this mode: whatever the slot
   puts in that height owns its own scrolling. */
.rv-dialog__body--fill {
  align-content: stretch;
  overflow: hidden;
}

.rv-dialog__footer {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-4);
  align-items: center;
  justify-content: space-between;
  padding: var(--rv-space-5) var(--rv-space-6);
  border-top: var(--rv-border-hair) solid var(--rv-color-rule);
}

@media (width <= 36rem) {
  .rv-dialog__header,
  .rv-dialog__footer {
    padding-right: var(--rv-space-4);
    padding-left: var(--rv-space-4);
  }

  /* At this width a centred box is the screen; it stops pretending otherwise. */
  .rv-dialog--panel {
    width: 100%;
    max-height: 100dvh;
    height: 100dvh;
    border: 0;
    border-radius: 0;
  }
}
</style>
