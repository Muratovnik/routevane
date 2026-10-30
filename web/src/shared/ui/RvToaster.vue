<script setup lang="ts">
import {
  ToastAction,
  ToastClose,
  ToastDescription,
  ToastProvider,
  ToastRoot,
  ToastTitle,
  ToastViewport,
} from 'reka-ui'

import {
  NOTICE_ACTION_DURATION,
  NOTICE_DURATION,
  type ShownNotice,
  useNotify,
} from '@/shared/model/useNotify'
import RvButton from '@/shared/ui/RvButton.vue'
import RvIcon from '@/shared/ui/RvIcon.vue'

/**
 * Where a finished act reports itself: one stack in the corner of the window,
 * mounted once by the shell, for every page.
 *
 * Reka's toast supplies the mechanics a notice has to get right — the polite
 * live region, the pause while the pointer or the keyboard is on it, the
 * hotkey that moves focus to the stack, swipe and Escape to dismiss. What is
 * drawn is ours, in the shared overlay look.
 */
defineProps<{
  /** What to call the control that closes one notice. */
  closeLabel: string
  /** The name of the region; `{hotkey}` is replaced with the key that reaches it. */
  label: string
}>()

const { dismiss, notices } = useNotify()

const duration = (notice: ShownNotice): number =>
  notice.action === undefined ? NOTICE_DURATION : NOTICE_ACTION_DURATION

const onOpen = (notice: ShownNotice, open: boolean): void => {
  if (!open) dismiss(notice.id)
}

const run = (notice: ShownNotice): void => {
  notice.action?.run()
  dismiss(notice.id)
}
</script>

<template>
  <ToastProvider swipe-direction="right">
    <ToastRoot
      v-for="notice in notices"
      :key="notice.id"
      class="rv-toaster__toast"
      :duration="duration(notice)"
      :open="true"
      @update:open="onOpen(notice, $event)"
    >
      <span
        class="rv-toaster__icon"
        :class="`rv-toaster__icon--${notice.tone ?? 'ready'}`"
      >
        <RvIcon :name="notice.tone === 'waiting' ? 'info' : 'check'" />
      </span>
      <div class="rv-toaster__content">
        <ToastTitle class="rv-toaster__title">{{ notice.title }}</ToastTitle>
        <ToastDescription
          v-if="notice.body !== undefined && notice.body !== ''"
          class="rv-toaster__body"
          >{{ notice.body }}</ToastDescription
        >
        <ToastAction
          v-if="notice.action !== undefined"
          as-child
          :alt-text="notice.action.label"
        >
          <RvButton size="compact" @click="run(notice)">{{
            notice.action.label
          }}</RvButton>
        </ToastAction>
      </div>
      <ToastClose :aria-label="closeLabel" class="rv-toaster__close">
        <RvIcon name="close" />
      </ToastClose>
    </ToastRoot>
    <ToastViewport class="rv-toaster" :label="label" />
  </ToastProvider>
</template>

<!-- Reka renders each notice into the viewport through a teleport of its own,
     from a root with several nodes, so neither a scope attribute nor a class
     reaches the elements below from a scoped block. The `rv-toaster` names are
     this component's alone, so the unscoped block claims nothing else. -->
<style>
.rv-toaster {
  position: fixed;
  inset-block-end: var(--rv-space-6);
  inset-inline-end: var(--rv-space-6);
  z-index: 9;
  display: flex;
  flex-direction: column;
  gap: var(--rv-space-3);
  width: min(24rem, calc(100vw - 2 * var(--rv-space-6)));
  margin: 0;
  padding: 0;
  list-style: none;
  outline: none;
}

.rv-toaster__toast {
  display: grid;
  grid-template-columns: auto 1fr auto;
  gap: var(--rv-space-3);
  align-items: start;
  padding: var(--rv-space-4);
  color: var(--rv-color-ink);
  font-size: var(--rv-text-interface);
  line-height: var(--rv-leading-normal);
  background: var(--rv-color-surface-raised);
  border: var(--rv-border-hair) solid var(--rv-color-rule-raised);
  border-radius: var(--rv-radius-md);
  box-shadow: var(--rv-shadow-raised);
}

.rv-toaster__toast:focus-visible {
  outline: var(--rv-border-mark) solid var(--rv-color-focus);
  outline-offset: var(--rv-focus-offset);
}

.rv-toaster__toast[data-state='open'] {
  animation: rv-toaster-in var(--rv-motion-normal) var(--rv-motion-ease-out);
}

/* stylelint-disable custom-property-pattern -- Reka names the distance it measured. */
.rv-toaster__toast[data-swipe='move'] {
  translate: var(--reka-toast-swipe-move-x) 0;
}
/* stylelint-enable custom-property-pattern */

.rv-toaster__icon {
  display: inline-flex;
  padding-block-start: var(--rv-space-1);
}

.rv-toaster__icon--ready {
  color: var(--rv-color-status-ready);
}

.rv-toaster__icon--waiting {
  color: var(--rv-color-ink-muted);
}

.rv-toaster__content {
  display: grid;
  gap: var(--rv-space-2);
  justify-items: start;
  min-width: 0;
}

.rv-toaster__title {
  font-weight: 600;
  overflow-wrap: anywhere;
}

.rv-toaster__body {
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
  overflow-wrap: anywhere;
}

.rv-toaster__close {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: var(--rv-control-inline);
  height: var(--rv-control-inline);
  padding: 0;
  color: var(--rv-color-ink-tertiary);
  background: transparent;
  border: 0;
  border-radius: var(--rv-radius-sm);
  cursor: pointer;
}

.rv-toaster__close:hover {
  color: var(--rv-color-ink);
  background: var(--rv-color-surface-raised-hover);
}

.rv-toaster__close:focus-visible {
  outline: var(--rv-border-mark) solid var(--rv-color-focus);
  outline-offset: var(--rv-focus-offset);
}

@keyframes rv-toaster-in {
  from {
    opacity: 0;
    translate: 0 var(--rv-space-3);
  }
}

@media (prefers-reduced-motion: reduce) {
  .rv-toaster__toast[data-state='open'] {
    animation: none;
  }
}
</style>
