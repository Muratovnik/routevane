<script setup lang="ts">
import { useLocale } from '@/shared/i18n/useLocale'
import RvButton from '@/shared/ui/RvButton.vue'
import RvDialog from '@/shared/ui/RvDialog.vue'
import RvIcon from '@/shared/ui/RvIcon.vue'
import RvStatus from '@/shared/ui/RvStatus.vue'

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
}>()

defineEmits<{ cancel: []; confirm: [] }>()

const { t } = useLocale()
</script>

<template>
  <RvDialog
    :close-label="t('action.close')"
    :dismissible="!busy"
    :open="name !== null"
    :title="t('profiles.delete.title', { name: name ?? '' })"
    variant="panel"
    @update:open="$event === false && $emit('cancel')"
  >
    <div class="profile-delete">
      <p class="profile-delete__body">{{ t('profiles.delete.body') }}</p>
      <p v-if="failure !== ''" role="alert">
        <RvStatus :label="failure" tone="failed" />
      </p>
    </div>
    <template #footer>
      <RvButton :disabled="busy" variant="quiet" @click="$emit('cancel')">
        {{ t('action.cancel') }}
      </RvButton>
      <RvButton
        :disabled="blocked"
        :loading="busy"
        :loading-label="t('profiles.delete.confirm')"
        variant="danger"
        @click="$emit('confirm')"
      >
        <RvIcon name="trash" />
        {{ t('profiles.delete.confirm') }}
      </RvButton>
    </template>
  </RvDialog>
</template>

<style scoped>
/* The panel's body carries no padding of its own; the content sets the inset
   its header and footer share. */
.profile-delete {
  display: grid;
  gap: var(--rv-space-3);
  padding: var(--rv-space-5) var(--rv-space-6);
}

.profile-delete__body {
  max-width: var(--rv-measure-prose);
  color: var(--rv-color-ink-muted);
}
</style>
