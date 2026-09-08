<script setup lang="ts">
import { useOutputPrefix } from '@/features/view-profile/model/useOutputPrefix'
import { useLocale } from '@/shared/i18n/useLocale'
import RvButton from '@/shared/ui/RvButton.vue'
import RvField from '@/shared/ui/RvField.vue'
import RvTextInput from '@/shared/ui/RvTextInput.vue'
import RvStateNotice from '@/shared/ui/RvStateNotice.vue'

const props = defineProps<{
  outputId: string
  prefix: string
  disabled: boolean
}>()
const { t } = useLocale()
const editor = useOutputPrefix(
  () => props.outputId,
  () => props.prefix,
)
</script>

<template>
  <form class="output-prefix" @submit.prevent="editor.save">
    <RvField
      :input-id="`output-prefix-${outputId}`"
      :label="t('outputs.prefix')"
      :error="editor.valid.value ? undefined : t('outputs.prefix.invalid')"
    >
      <template #default="{ describedBy, invalid }">
        <RvTextInput
          v-model="editor.draft.value"
          :input-id="`output-prefix-${outputId}`"
          :described-by="describedBy"
          :invalid="invalid"
          :disabled="disabled || editor.state.value === 'saving'"
          placeholder="routevane"
        />
      </template>
    </RvField>
    <p class="output-prefix__note">{{ t('outputs.prefix.hint') }}</p>
    <RvButton
      type="submit"
      size="compact"
      :loading="editor.state.value === 'saving'"
      :disabled="
        disabled ||
        !editor.valid.value ||
        editor.draft.value === editor.saved.value
      "
      >{{ t('outputs.prefix.save') }}</RvButton
    >
    <RvStateNotice
      v-if="editor.state.value === 'saved'"
      :title="t('outputs.prefix.saved')"
      tone="ready"
      live
    />
    <RvStateNotice
      v-if="editor.state.value === 'failed'"
      :title="t('outputs.prefix.failed')"
      tone="failed"
      live
    >
      <template #action
        ><RvButton
          :disabled="disabled || !editor.valid.value"
          @click="editor.save"
          >{{ t('action.retry') }}</RvButton
        ></template
      >
    </RvStateNotice>
  </form>
</template>

<style scoped>
.output-prefix {
  display: grid;
  gap: var(--rv-space-2);
  margin-top: var(--rv-space-3);
}

.output-prefix__note {
  margin: 0;
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
}
</style>
