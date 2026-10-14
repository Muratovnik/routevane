<script setup lang="ts">
import { computed } from 'vue'

import { useLocale } from '@/shared/i18n/useLocale'
import RvButton from '@/shared/ui/RvButton.vue'
import RvDialog from '@/shared/ui/RvDialog.vue'
import RvStateNotice from '@/shared/ui/RvStateNotice.vue'

/**
 * The confirmation an irreversible act on one format asks for: reissuing its
 * link or removing it from the profile (ADR 0051). The dialog states what will
 * happen, beside the act, and a refusal stays inside it with the act still
 * offered. Both acts end something other devices rely on, the old link or the
 * format itself, so both confirm with the destructive button.
 */
const props = defineProps<{
  busy: boolean
  /** A message key for the last refusal, or null. */
  error: string | null
  kind: 'reissue' | 'remove'
  open: boolean
  target: string
}>()

const emit = defineEmits<{
  confirm: []
  'update:open': [open: boolean]
}>()

const { t } = useLocale()
const copy = computed(() => `outputs.${props.kind}`)
</script>

<template>
  <RvDialog
    :close-label="t('action.close')"
    :dismissible="!busy"
    :open="open"
    :title="t(`${copy}.confirm`, { target })"
    variant="panel"
    @update:open="emit('update:open', $event)"
  >
    <div class="output-confirm">
      <p class="output-confirm__consequence">{{ t(`${copy}.confirm.body`) }}</p>
      <RvStateNotice
        v-if="error !== null"
        live
        :title="t(error)"
        tone="failed"
      />
    </div>
    <template #footer>
      <RvButton
        :disabled="busy"
        variant="quiet"
        @click="emit('update:open', false)"
      >
        {{ t('action.cancel') }}
      </RvButton>
      <RvButton
        :loading="busy"
        :loading-label="t(`${copy}.submit`)"
        variant="danger"
        @click="emit('confirm')"
      >
        {{ t(`${copy}.submit`) }}
      </RvButton>
    </template>
  </RvDialog>
</template>

<style scoped>
/* The panel's body is the caller's to pad; it keeps the header's inset. */
.output-confirm {
  display: grid;
  gap: var(--rv-space-4);
}

.output-confirm__consequence {
  max-width: var(--rv-measure-prose);
  font-size: var(--rv-text-interface);
}
</style>
