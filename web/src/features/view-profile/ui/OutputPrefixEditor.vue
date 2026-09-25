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
    <div class="output-prefix__row">
      <RvField
        class="output-prefix__field"
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
            mono
            :disabled="disabled || editor.state.value === 'saving'"
            placeholder="routevane"
          />
        </template>
      </RvField>
      <RvButton
        type="submit"
        :loading="editor.state.value === 'saving'"
        :disabled="
          disabled ||
          !editor.valid.value ||
          editor.draft.value === editor.saved.value
        "
        >{{ t('outputs.prefix.save') }}</RvButton
      >
    </div>
    <p class="output-prefix__note">{{ t('outputs.prefix.hint') }}</p>
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
  max-width: var(--rv-measure-prose);
  white-space: normal;
  overflow-wrap: anywhere;
  margin-top: var(--rv-space-3);
}

.output-prefix__note {
  margin: 0;
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
}

.output-prefix__row {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  gap: var(--rv-space-3);
}

.output-prefix__field {
  flex: 1 1 var(--rv-composer-field-width);
  min-width: 0;
  max-width: var(--rv-measure-field);
}
</style>
