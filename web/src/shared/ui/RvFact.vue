<script setup lang="ts">
import RvIcon from '@/shared/ui/RvIcon.vue'

/**
 * A fact the product holds, stated: «Пароль сохранён», «Входит в профиль».
 *
 * A fact is not a status. A status (`RvStatus`) reports how something is
 * going and can turn bad — a send failed, a source is not answering — so it
 * takes the status dot and its colours. A fact is either there or not, and a
 * green dot beside it would read as a check that never ran. It takes the check
 * mark in the brand accent, and words.
 */
defineProps<{
  label: string
}>()
</script>

<template>
  <span class="rv-fact">
    <!-- Decoration beside the words, so it is hidden from the accessibility
         tree and carries a test hook instead of a role. -->
    <span aria-hidden="true" class="rv-fact__mark" data-testid="rv-fact-mark"
      ><RvIcon name="check"
    /></span>
    <span class="rv-fact__label">{{ label }}</span>
  </span>
</template>

<style scoped>
.rv-fact {
  display: inline-flex;
  gap: var(--rv-space-2);
  align-items: baseline;
  color: var(--rv-color-ink);
  font-size: var(--rv-text-interface);
}

.rv-fact__mark {
  display: inline-flex;
  align-self: center;
  color: var(--rv-color-accent-ink);
}

.rv-fact__label {
  overflow-wrap: anywhere;
}
</style>
