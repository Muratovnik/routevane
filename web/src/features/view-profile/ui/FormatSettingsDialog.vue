<script setup lang="ts">
import { nextTick, useId, watch } from 'vue'

import { useOutputPrefix } from '@/features/view-profile/model/useOutputPrefix'
import { useLocale } from '@/shared/i18n/useLocale'
import RvButton from '@/shared/ui/RvButton.vue'
import RvDialog from '@/shared/ui/RvDialog.vue'
import RvField from '@/shared/ui/RvField.vue'
import RvStateNotice from '@/shared/ui/RvStateNotice.vue'
import RvTextInput from '@/shared/ui/RvTextInput.vue'

/**
 * The settings one format has beyond its row. Today that is the FQDN group
 * prefix of the domain-based Keenetic format (ADR 0049). A saved prefix takes
 * effect at the next build, which the footer says beside Save; «Rebuild now»
 * saves a changed draft and asks for that build at once.
 */
const props = defineProps<{
  busy: boolean
  open: boolean
  outputId: string
  prefix: string
  title: string
}>()

const emit = defineEmits<{
  'update:open': [open: boolean]
  rebuild: [outputID: string]
}>()

const { t } = useLocale()
const editor = useOutputPrefix(
  () => props.outputId,
  () => props.prefix,
)
const inputID = `format-prefix-${useId()}`

// Every opening starts from what is stored: a cancelled draft is gone.
watch(
  () => props.open,
  (open) => {
    if (open) editor.reset()
  },
)

const focusField = async (): Promise<void> => {
  await nextTick()
  document.getElementById(inputID)?.focus()
}

const save = async (): Promise<void> => {
  const outcome = await editor.save()
  if (outcome === 'invalid') await focusField()
  else if (outcome === 'saved' || outcome === 'unchanged')
    emit('update:open', false)
}

const rebuildNow = async (): Promise<void> => {
  const outcome = await editor.save()
  if (outcome === 'invalid') {
    await focusField()
    return
  }
  if (outcome === 'failed') return
  emit('update:open', false)
  emit('rebuild', props.outputId)
}
</script>

<template>
  <RvDialog
    :close-label="t('action.close')"
    :dismissible="editor.state.value !== 'saving'"
    :open="open"
    :title="t('outputs.settings.title', { target: title })"
    variant="panel"
    @update:open="emit('update:open', $event)"
  >
    <form
      id="format-settings-form"
      class="format-settings"
      @submit.prevent="save"
    >
      <RvField
        :error="editor.invalid.value ? t('outputs.prefix.invalid') : undefined"
        :input-id="inputID"
        :label="t('outputs.prefix')"
      >
        <template #default="{ describedBy, invalid }">
          <RvTextInput
            v-model="editor.draft.value"
            :described-by="describedBy"
            :disabled="editor.state.value === 'saving'"
            :input-id="inputID"
            :invalid="invalid"
            mono
            placeholder="routevane"
          />
        </template>
      </RvField>
      <RvStateNotice
        v-if="editor.state.value === 'failed'"
        live
        :title="t('outputs.prefix.failed')"
        tone="failed"
      />
    </form>
    <template #footer>
      <p class="format-settings__consequence">
        {{ t('outputs.settings.nextBuild') }}
      </p>
      <div class="format-settings__actions">
        <RvButton
          :disabled="busy || editor.state.value === 'saving'"
          variant="secondary"
          @click="rebuildNow"
        >
          {{ t('outputs.settings.rebuild') }}
        </RvButton>
        <RvButton
          form="format-settings-form"
          :loading="editor.state.value === 'saving'"
          :loading-label="t('outputs.prefix.save')"
          type="submit"
          variant="primary"
        >
          {{ t('outputs.prefix.save') }}
        </RvButton>
      </div>
    </template>
  </RvDialog>
</template>

<style scoped>
/* The panel's body is the caller's to pad; it keeps the header's inset. */
.format-settings {
  display: grid;
  gap: var(--rv-space-4);
  min-width: 0;
}

/* The two acts stay together; the consequence wraps above them first. */
.format-settings__actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-3);
  margin-inline-start: auto;
}

.format-settings__consequence {
  margin-inline-end: auto;
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
}
</style>
