<script setup lang="ts">
import { useLocale } from '@/shared/i18n/useLocale'
import RvConfirmDialog from '@/shared/ui/RvConfirmDialog.vue'

/**
 * The question asked before a profile is deleted for good. It names the profile
 * and says what the deletion leaves behind, because the answer cannot be taken
 * back; the refusal it may meet is stated inside it, where the operator is
 * looking.
 */
defineProps<{
  /** The profile being asked about, or null while no question is open. */
  name: string | null
  busy: boolean
  /** Words for why the last attempt did not delete, empty when none failed. */
  failure: string
  /** The profile is not archived, so asking again cannot succeed. */
  blocked: boolean
  /** The deletion also takes from routers what its formats sent there. */
  removesFromDevices?: boolean
}>()

defineEmits<{ cancel: []; confirm: [] }>()

const { t } = useLocale()
</script>

<template>
  <RvConfirmDialog
    :busy="busy"
    :cancel-label="t('action.cancel')"
    :close-label="t('action.close')"
    :confirm-label="t('profiles.delete.confirm')"
    :confirmable="!blocked"
    :error="failure === '' ? null : failure"
    :open="name !== null"
    :title="t('profiles.delete.title', { name: name ?? '' })"
    @confirm="$emit('confirm')"
    @update:open="$event === false && $emit('cancel')"
  >
    <p>
      {{
        removesFromDevices === true
          ? t('profiles.delete.body.clean')
          : t('profiles.delete.body')
      }}
    </p>
    <!-- What else the deletion can do, chosen by the caller. -->
    <template #choices><slot /></template>
    <template #error><slot name="error" /></template>
  </RvConfirmDialog>
</template>
