<script setup lang="ts">
import { onMounted } from 'vue'

import { useFormatVisibility } from '@/features/settings/model/useFormatVisibility'
import { useLocale } from '@/shared/i18n/useLocale'
import RvButton from '@/shared/ui/RvButton.vue'
import RvSettingsSection from '@/shared/ui/RvSettingsSection.vue'
import RvStateNotice from '@/shared/ui/RvStateNotice.vue'
import RvSwitch from '@/shared/ui/RvSwitch.vue'

/**
 * The formats this build carries, each with the one choice that is the
 * operator's to make: whether the composer offers it. What a format is and
 * whether it can be delivered to automatically is reference material and lives
 * in Connections.
 */
const { t } = useLocale()
const formats = useFormatVisibility()

onMounted(() => {
  void formats.load()
})
</script>

<template>
  <RvSettingsSection :title="t('settings.formats.title')">
    <RvStateNotice
      v-if="formats.state.value === 'loading'"
      live
      :title="t('settings.formats.loading')"
      tone="busy"
    />
    <RvStateNotice
      v-else-if="formats.state.value === 'failed'"
      :body="t('settings.formats.failed.body')"
      live
      :title="t('settings.formats.failed')"
      tone="failed"
    >
      <template #action>
        <RvButton @click="formats.load">{{ t('action.retry') }}</RvButton>
      </template>
    </RvStateNotice>
    <RvStateNotice
      v-else-if="formats.state.value === 'empty'"
      :title="t('settings.formats.empty')"
      tone="waiting"
    />
    <!-- The group is named by what each switch decides, and each switch by the
         format it decides it for. -->
    <fieldset v-else class="formats">
      <legend class="formats__legend">{{ t('settings.formats.show') }}</legend>
      <RvSwitch
        v-for="format in formats.choices.value"
        :key="format.id"
        :label="format.title"
        :model-value="format.shown"
        @update:model-value="formats.setShown(format.id, $event)"
      />
    </fieldset>
  </RvSettingsSection>
</template>

<style scoped>
.formats {
  display: grid;
  gap: var(--rv-space-4);
  min-width: 0;
  margin: 0;
  padding: 0;
  border: 0;
}

.formats__legend {
  padding: 0;
  margin-block-end: var(--rv-space-3);
  color: var(--rv-color-ink);
  font-weight: 600;
  font-size: var(--rv-text-dense);
}
</style>
