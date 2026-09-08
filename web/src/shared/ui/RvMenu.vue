<script setup lang="ts">
import {
  DropdownMenuContent,
  DropdownMenuPortal,
  DropdownMenuRoot,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from 'reka-ui'

import type { IconName, MenuItem } from '@/shared/ui/types'
import RvIcon from '@/shared/ui/RvIcon.vue'
import RvMenuItem from '@/shared/ui/RvMenuItem.vue'

/**
 * A compact action or choice menu.
 *
 * The panel is a viewport overlay portalled to the document body, so a table or
 * a sheet never has to own its geometry and never grows its own scroll box
 * around it. Grouped choices are a submenu rather than a longer flat list:
 * ArrowRight or a click opens one, ArrowLeft or Escape returns to the item that
 * opened it, and neither closes the menu.
 */
const props = defineProps<{
  disabled?: boolean
  items: MenuItem[]
  /** Accessible name of the trigger, e.g. "Actions for list X". */
  label: string
  /** Overflow menus stay icon-only; primary choice menus can name themselves. */
  triggerIcon?: IconName
  triggerText?: string
}>()

const emit = defineEmits<{
  select: [key: string]
}>()

const grouped = (item: MenuItem): boolean =>
  item.children !== undefined && item.children.length > 0

const choose = (item: MenuItem): void => {
  if (item.disabled !== true) emit('select', item.key)
}

// Closing normally hands the keyboard back to the trigger. When the chosen
// action has already moved it somewhere else — a dialog that opened and took
// its first field — the trigger does not take it back a frame later, because
// that late focus would blur the field and undo what was typed into it.
const onCloseAutoFocus = (event: Event): void => {
  const active = document.activeElement
  if (
    !(active instanceof HTMLElement) ||
    active === document.body ||
    active.closest('.rv-menu__panel') !== null ||
    active.closest('.rv-menu__trigger') !== null
  ) {
    return
  }
  event.preventDefault()
}
</script>

<template>
  <DropdownMenuRoot>
    <DropdownMenuTrigger
      :aria-label="label"
      class="rv-menu__trigger"
      :class="{ 'rv-menu__trigger--text': triggerText !== undefined }"
      :disabled="disabled === true"
    >
      <RvIcon :name="triggerIcon ?? 'dots'" />
      <span v-if="triggerText !== undefined">{{ triggerText }}</span>
      <RvIcon v-if="triggerText !== undefined" name="chevron" />
    </DropdownMenuTrigger>
    <DropdownMenuPortal>
      <DropdownMenuContent
        align="end"
        class="rv-menu__panel"
        :collision-padding="8"
        :side-offset="4"
        @close-auto-focus="onCloseAutoFocus"
      >
        <template v-for="item in props.items" :key="item.key">
          <DropdownMenuSub v-if="grouped(item)">
            <DropdownMenuSubTrigger
              class="rv-menu__item"
              :class="{ 'rv-menu__item--separated': item.separatorBefore }"
              :disabled="item.disabled === true"
            >
              <RvIcon v-if="item.icon !== undefined" :name="item.icon" />
              <span class="rv-menu__item-label">{{ item.label }}</span>
              <RvIcon class="rv-menu__item-next" name="chevron" />
            </DropdownMenuSubTrigger>
            <DropdownMenuPortal>
              <DropdownMenuSubContent
                class="rv-menu__panel"
                :collision-padding="8"
                :side-offset="4"
              >
                <RvMenuItem
                  v-for="child in item.children"
                  :key="child.key"
                  :item="child"
                  @select="choose(child)"
                />
              </DropdownMenuSubContent>
            </DropdownMenuPortal>
          </DropdownMenuSub>

          <RvMenuItem v-else :item="item" @select="choose(item)" />
        </template>
      </DropdownMenuContent>
    </DropdownMenuPortal>
  </DropdownMenuRoot>
</template>

<style scoped>
.rv-menu__trigger {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: var(--rv-control-compact);
  height: var(--rv-control-compact);
  padding: 0;
  color: var(--rv-color-ink-muted);
  background: transparent;
  border: var(--rv-border-hair) solid transparent;
  border-radius: var(--rv-radius-md);
  cursor: pointer;
}

.rv-menu__trigger:hover {
  color: var(--rv-color-ink);
  background: var(--rv-color-surface-hover);
}

.rv-menu__trigger[data-state='open'] {
  color: var(--rv-color-ink);
  background: var(--rv-color-surface-hover);
}

.rv-menu__trigger:disabled {
  opacity: var(--rv-disabled-opacity);
  cursor: not-allowed;
}

.rv-menu__trigger--text {
  gap: var(--rv-space-2);
  width: auto;
  padding: 0 var(--rv-space-3);
  color: var(--rv-color-ink);
  font-weight: 600;
  font-size: var(--rv-text-dense);
  font-family: inherit;
}
</style>

<!-- The panel and its items are styled outside the scoped block above, and they
     have to be. `PopperContent` renders its own positioning wrapper as this
     component's root element, so the scope attribute lands on that wrapper while
     these classes land on the element inside it: a scoped rule would compile to
     `.rv-menu__panel[data-v-…]` and match nothing, leaving an open menu as
     unpainted text over the page. The `rv-menu__` names are this component's
     alone, so an unscoped block claims nothing else. -->
<style>
.rv-menu__panel {
  z-index: 7;
  display: grid;
  min-width: var(--rv-overlay-min-width);
  max-width: calc(100dvw - var(--rv-space-4));

  /* The positioner states how much room is left on the side it chose;
     a panel taller than that would bleed past the viewport edge. */
  /* stylelint-disable custom-property-pattern -- Reka names the room it measured. */
  max-height: min(
    var(--rv-overlay-height),
    var(--reka-dropdown-menu-content-available-height, var(--rv-overlay-height))
  );
  /* stylelint-enable custom-property-pattern */
  overflow-y: auto;
  overscroll-behavior: contain;
  padding: var(--rv-space-1);
  background: var(--rv-color-surface);
  border: var(--rv-border-hair) solid var(--rv-color-rule-strong);
  border-radius: var(--rv-radius-md);
  box-shadow: var(--rv-shadow-raised);
  animation: rv-menu-in var(--rv-motion-fast) var(--rv-motion-ease-out);
}

@keyframes rv-menu-in {
  from {
    box-shadow: var(--rv-shadow-panel);
  }
}

.rv-menu__item {
  display: flex;
  gap: var(--rv-space-2);
  align-items: center;
  min-height: var(--rv-control-default);
  padding: 0 var(--rv-space-3);
  color: var(--rv-color-ink);
  font-size: var(--rv-text-dense);
  font-family: inherit;
  text-align: start;
  text-decoration: none;
  background: transparent;
  border: 0;
  border-radius: var(--rv-radius-sm);
  cursor: pointer;
  overflow-wrap: anywhere;
}

.rv-menu__item--separated {
  margin-top: var(--rv-space-1);
  padding-top: var(--rv-space-1);
  border-top: var(--rv-border-hair) solid var(--rv-color-rule);
}

.rv-menu__item[data-highlighted] {
  background: var(--rv-color-surface-hover);
  outline: none;
}

.rv-menu__item[data-disabled] {
  color: var(--rv-color-ink-tertiary);
  cursor: not-allowed;
}

.rv-menu__item .rv-icon {
  color: var(--rv-color-ink-tertiary);
}

.rv-menu__item-label {
  min-width: 0;
}

.rv-menu__item-next {
  margin-left: auto;
  transform: rotate(-90deg);
}
</style>
