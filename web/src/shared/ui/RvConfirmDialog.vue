<script setup lang="ts">
import RvButton from '@/shared/ui/RvButton.vue'
import RvDialog from '@/shared/ui/RvDialog.vue'
import RvStateNotice from '@/shared/ui/RvStateNotice.vue'

/**
 * The one confirmation an act asks for before it runs.
 *
 * The title names the act and the object it acts on («Удалить список
 * «ChatGPT»?»), the body says what will happen — the consequence a reader
 * decides on, not a restatement of the title — and an act that cannot be taken
 * back says so in its own line. A refusal stays inside the dialog with the act
 * still offered, so the reader corrects course where the decision was made.
 *
 * The confirming button is `danger` for an act that ends something and the
 * primary look for one that can be undone or only adds (UI.md, Components).
 */
const props = withDefaults(
  defineProps<{
    busy?: boolean
    cancelLabel: string
    /**
     * Whether pressing the act again can succeed. A refusal that only a change
     * elsewhere can lift — profiles still naming the list — leaves the reader
     * one way out, so the act is no longer offered.
     */
    confirmable?: boolean
    closeLabel: string
    confirmLabel: string
    /** The refusal of the last attempt, already in words, or null. */
    error?: string | null
    /** The line that says the act cannot be undone, when it cannot. */
    irreversible?: string
    open: boolean
    title: string
    tone?: 'danger' | 'primary'
  }>(),
  {
    busy: false,
    confirmable: true,
    error: null,
    irreversible: undefined,
    tone: 'danger',
  },
)

const emit = defineEmits<{
  confirm: []
  'update:open': [open: boolean]
}>()

const close = (): void => {
  if (!props.busy) emit('update:open', false)
}
</script>

<template>
  <RvDialog
    :close-label="closeLabel"
    :dismissible="!busy"
    :open="open"
    :title="title"
    variant="panel"
    @update:open="emit('update:open', $event)"
  >
    <div class="rv-confirm">
      <div v-if="$slots.default" class="rv-confirm__consequence">
        <slot />
      </div>
      <p v-if="irreversible !== undefined" class="rv-confirm__irreversible">
        {{ irreversible }}
      </p>
      <slot name="choices" />
      <!-- A refusal that is more than a sentence — one that names what holds
           the object — is drawn by the caller; a plain one is a notice here. -->
      <slot name="refusal" />
      <RvStateNotice v-if="error !== null" live :title="error" tone="failed">
        <slot name="error" />
      </RvStateNotice>
    </div>
    <template #footer>
      <RvButton :disabled="busy" variant="quiet" @click="close">
        {{ cancelLabel }}
      </RvButton>
      <RvButton
        v-if="confirmable"
        :loading="busy"
        :loading-label="confirmLabel"
        :variant="tone"
        @click="emit('confirm')"
      >
        {{ confirmLabel }}
      </RvButton>
    </template>
  </RvDialog>
</template>

<style scoped>
.rv-confirm {
  display: grid;
  gap: var(--rv-space-4);
}

.rv-confirm__consequence {
  display: grid;
  gap: var(--rv-space-3);
  max-width: var(--rv-measure-prose);
  font-size: var(--rv-text-interface);
}

.rv-confirm__irreversible {
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
}
</style>
