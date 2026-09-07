<script setup lang="ts">
import { computed, ref } from 'vue'

import { useConfigTransfer } from '@/features/settings/model/useConfigTransfer'
import { useLocale } from '@/shared/i18n/useLocale'
import RvButton from '@/shared/ui/RvButton.vue'
import RvDialog from '@/shared/ui/RvDialog.vue'
import RvFilePicker from '@/shared/ui/RvFilePicker.vue'
import RvStateNotice from '@/shared/ui/RvStateNotice.vue'

const emit = defineEmits<{ applied: [] }>()

const { t, tc } = useLocale()
const transfer = useConfigTransfer()
const confirming = ref(false)
const fileInputID = 'config-transfer-file'

const countRows = computed(() => {
  const counts = transfer.preview.value?.counts
  if (counts === undefined) return []
  return [
    {
      key: 'customLists',
      text: tc('configTransfer.count.customLists', counts.customLists),
    },
    {
      key: 'customCategories',
      text: tc(
        'configTransfer.count.customCategories',
        counts.customCategories,
      ),
    },
    {
      key: 'customSources',
      text: tc('configTransfer.count.customSources', counts.customSources),
    },
    { key: 'routes', text: tc('configTransfer.count.profiles', counts.routes) },
    {
      key: 'devices',
      text: tc('configTransfer.count.devices', counts.devices),
    },
    {
      key: 'outputs',
      text: tc('configTransfer.count.outputs', counts.outputs),
    },
  ]
})

const failureMessage = computed(() => {
  if (transfer.failure.value === '') return ''
  return t(`configTransfer.failure.${transfer.failure.value}`)
})

function onFileSelected(file: File): void {
  confirming.value = false
  void transfer.choose(file)
}

function requestConfirmation(): void {
  if (transfer.canApply.value) confirming.value = true
}

async function apply(): Promise<void> {
  if (!(await transfer.apply())) return
  confirming.value = false
  emit('applied')
}
</script>

<template>
  <section aria-labelledby="settings-config-transfer" class="config-transfer">
    <h2 id="settings-config-transfer" class="config-transfer__title">
      {{ t('configTransfer.title') }}
    </h2>

    <div class="config-transfer__actions">
      <RvButton
        :disabled="transfer.downloadState.value === 'downloading'"
        :loading="transfer.downloadState.value === 'downloading'"
        :loading-label="t('configTransfer.download.busy')"
        @click="transfer.download"
      >
        {{
          transfer.downloadState.value === 'downloading'
            ? t('configTransfer.download.busy')
            : t('configTransfer.download')
        }}
      </RvButton>
      <p aria-live="polite" class="config-transfer__download-status">
        {{
          transfer.downloadState.value === 'failed'
            ? t('configTransfer.download.failed')
            : ''
        }}
      </p>
    </div>

    <div class="config-transfer__file">
      <p class="config-transfer__file-label">
        {{ t('configTransfer.file.label') }}
      </p>
      <RvFilePicker
        accept="application/json,.json"
        :action-label="
          transfer.fileName.value === ''
            ? t('configTransfer.file.choose')
            : t('configTransfer.file.replace')
        "
        :disabled="transfer.applyState.value === 'applying'"
        :empty-label="t('configTransfer.file.empty')"
        :file-name="transfer.fileName.value"
        :hint="t('configTransfer.file.limit')"
        :input-id="fileInputID"
        :label="t('configTransfer.file.label')"
        @select="onFileSelected"
      />
    </div>

    <p class="config-transfer__boundary">
      {{ t('configTransfer.boundary') }}
    </p>
    <p class="config-transfer__boundary">
      {{ t('configTransfer.afterImport') }}
    </p>

    <RvStateNotice
      v-if="transfer.state.value === 'reading'"
      live
      :title="t('configTransfer.reading')"
      tone="busy"
    />
    <RvStateNotice
      v-else-if="failureMessage !== ''"
      live
      :title="failureMessage"
      tone="failed"
    >
      <template v-if="transfer.failure.value === 'preview'" #action>
        <RvButton @click="transfer.previewSelected">
          {{ t('action.retry') }}
        </RvButton>
      </template>
    </RvStateNotice>

    <div
      v-if="transfer.fileName.value !== ''"
      class="config-transfer__file-row"
    >
      <RvButton
        :disabled="
          !transfer.canPreview.value || transfer.state.value === 'previewing'
        "
        :loading="transfer.state.value === 'previewing'"
        :loading-label="t('configTransfer.preview.busy')"
        @click="transfer.previewSelected"
      >
        {{
          transfer.state.value === 'previewing'
            ? t('configTransfer.preview.busy')
            : t('configTransfer.preview')
        }}
      </RvButton>
    </div>

    <RvStateNotice
      v-if="transfer.state.value === 'previewing'"
      live
      :title="t('configTransfer.preview.busy')"
      tone="busy"
    />

    <div
      v-if="transfer.preview.value !== null"
      aria-labelledby="config-transfer-preview"
      class="config-transfer__preview"
      role="region"
    >
      <h3 id="config-transfer-preview" class="config-transfer__preview-title">
        {{ t('configTransfer.preview.title') }}
      </h3>
      <ul class="config-transfer__counts">
        <li v-for="row in countRows" :key="row.key">{{ row.text }}</li>
      </ul>
      <ul
        v-if="transfer.preview.value.warnings.length > 0"
        class="config-transfer__warnings"
      >
        <li
          v-for="(warning, index) in transfer.preview.value.warnings"
          :key="`${warning}-${index}`"
        >
          {{ t(`configTransfer.warning.${warning}`) }}
        </li>
      </ul>
      <RvButton
        :disabled="!transfer.canApply.value"
        variant="primary"
        @click="requestConfirmation"
      >
        {{ t('configTransfer.apply.review') }}
      </RvButton>
    </div>

    <RvDialog
      v-model:open="confirming"
      :close-label="t('action.close')"
      :description="t('configTransfer.confirm.description')"
      :dismissible="transfer.applyState.value !== 'applying'"
      :title="t('configTransfer.confirm.title')"
      variant="panel"
    >
      <RvStateNotice
        v-if="transfer.applyState.value === 'failed'"
        live
        :title="t('configTransfer.apply.failed')"
        tone="failed"
      >
        <template #action>
          <RvButton :disabled="!transfer.canApply.value" @click="apply">
            {{ t('configTransfer.apply') }}
          </RvButton>
        </template>
      </RvStateNotice>
      <p class="config-transfer__confirm-copy">
        {{ t('configTransfer.confirm.body') }}
      </p>
      <template #footer>
        <RvButton
          :disabled="transfer.applyState.value === 'applying'"
          @click="confirming = false"
        >
          {{ t('action.cancel') }}
        </RvButton>
        <RvButton
          :disabled="!transfer.canApply.value"
          :loading="transfer.applyState.value === 'applying'"
          :loading-label="t('configTransfer.apply.busy')"
          variant="primary"
          @click="apply"
        >
          {{
            transfer.applyState.value === 'applying'
              ? t('configTransfer.apply.busy')
              : t('configTransfer.apply')
          }}
        </RvButton>
      </template>
    </RvDialog>
  </section>
</template>

<style scoped>
.config-transfer {
  display: grid;
  gap: var(--rv-space-5);
  padding-top: var(--rv-space-6);
  border-top: var(--rv-border-hair) solid var(--rv-color-rule);
}

.config-transfer__actions,
.config-transfer__file,
.config-transfer__preview {
  display: grid;
  gap: var(--rv-space-3);
  justify-items: start;
}

.config-transfer__title {
  font-size: var(--rv-text-section);
}

.config-transfer__download-status,
.config-transfer__file-note,
.config-transfer__boundary,
.config-transfer__confirm-copy {
  max-width: var(--rv-measure-prose);
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
}

.config-transfer__download-status {
  min-height: var(--rv-leading-normal);
  color: var(--rv-color-status-failed);
}

.config-transfer__file-label {
  color: var(--rv-color-ink);
  font-weight: 600;
  font-size: var(--rv-text-dense);
  letter-spacing: var(--rv-tracking-label);
}

.config-transfer__file-row {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-3);
  align-items: center;
}

.config-transfer__preview-title {
  font-size: var(--rv-text-module);
}

.config-transfer__counts,
.config-transfer__warnings {
  display: grid;
  gap: var(--rv-space-1);
  padding-left: var(--rv-space-5);
  font-size: var(--rv-text-interface);
}

.config-transfer__warnings {
  color: var(--rv-color-status-warning);
}

.config-transfer__confirm-copy {
  padding: var(--rv-space-5) var(--rv-space-6);
}
</style>
