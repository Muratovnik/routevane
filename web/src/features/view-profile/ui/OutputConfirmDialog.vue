<script setup lang="ts">
import { computed } from 'vue'

import { useLocale } from '@/shared/i18n/useLocale'
import RvConfirmDialog from '@/shared/ui/RvConfirmDialog.vue'

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
  /** The removal also takes from routers what the format sent there. */
  removesFromDevices?: boolean
  target: string
}>()

const emit = defineEmits<{
  confirm: []
  'update:open': [open: boolean]
}>()

const { t } = useLocale()
const copy = computed(() => `outputs.${props.kind}`)
const body = computed(() =>
  props.kind === 'remove' && props.removesFromDevices === true
    ? t('outputs.remove.confirm.body.clean')
    : t(`${copy.value}.confirm.body`),
)
</script>

<template>
  <RvConfirmDialog
    :busy="busy"
    :cancel-label="t('action.cancel')"
    :close-label="t('action.close')"
    :confirm-label="t(`${copy}.submit`)"
    :error="error === null ? null : t(error)"
    :open="open"
    :title="t(`${copy}.confirm`, { target })"
    @confirm="emit('confirm')"
    @update:open="emit('update:open', $event)"
  >
    <p>{{ body }}</p>
    <!-- What else the act can do, chosen by the caller. -->
    <template #choices><slot /></template>
    <template #error><slot name="error" /></template>
  </RvConfirmDialog>
</template>
