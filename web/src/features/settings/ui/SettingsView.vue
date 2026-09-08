<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'

import { useSettings } from '@/features/settings/model/useSettings'
import ConfigTransferPanel from '@/features/settings/ui/ConfigTransferPanel.vue'
import type { RefreshInterval } from '@/shared/api/profiles'
import { useLocale } from '@/shared/i18n/useLocale'
import { localeNames, type Locale } from '@/shared/i18n/messages'
import {
  useSurfacePreferences,
  type Appearance,
  type DetailMode,
} from '@/shared/model/useSurfacePreferences'
import RvButton from '@/shared/ui/RvButton.vue'
import RvSettingsSection from '@/shared/ui/RvSettingsSection.vue'
import RvPageHeader from '@/shared/ui/RvPageHeader.vue'
import RvSegmented from '@/shared/ui/RvSegmented.vue'
import RvStateNotice from '@/shared/ui/RvStateNotice.vue'

const { locale, setLocale, t } = useLocale()
const preferences = useSurfacePreferences()
const settings = useSettings()
const router = useRouter()

const localeOptions = computed(() =>
  (Object.keys(localeNames) as Locale[]).map((value) => ({
    label: localeNames[value],
    value,
  })),
)

const appearanceOptions = computed(() => [
  { label: t('settings.theme.system'), value: 'system' },
  { label: t('settings.theme.dark'), value: 'dark' },
  { label: t('settings.theme.light'), value: 'light' },
])

const modeOptions = computed(() => [
  { label: t('settings.mode.simple'), value: 'simple' },
  { label: t('settings.mode.expert'), value: 'expert' },
])

const origin = computed(() =>
  typeof window === 'undefined' ? '127.0.0.1' : window.location.host,
)

const refreshOptions = computed(() => [
  { label: t('settings.refresh.off'), value: 'off' },
  { label: t('settings.refresh.daily'), value: 'daily' },
  { label: t('settings.refresh.weekly'), value: 'weekly' },
])

onMounted(() => {
  void settings.initialize()
})

const onRefreshInterval = async (value: string): Promise<void> => {
  await settings.setRefreshInterval(value as RefreshInterval)
}

const onLocale = (value: string): void => {
  setLocale(value as Locale)
}

const onAppearance = (value: string): void => {
  preferences.setAppearance(value as Appearance)
}

const onMode = (value: string): void => {
  preferences.setMode(value as DetailMode)
}

const onConfigTransferApplied = (): void => {
  // Routes are server-owned. Landing on their shelf creates a fresh read from
  // the imported state instead of keeping any pre-import rows in this view.
  void router.push('/')
}
</script>

<template>
  <section aria-labelledby="settings-title" class="settings">
    <RvPageHeader title-id="settings-title" :title="t('settings.title')" />
    <div class="settings__content">
      <RvSettingsSection :title="t('settings.interface')">
        <div class="settings__fields">
          <RvSegmented
            :label="t('settings.locale')"
            :model-value="locale"
            name="rv-locale"
            :options="localeOptions"
            @update:model-value="onLocale"
          />
          <RvSegmented
            :label="t('settings.theme')"
            :model-value="preferences.appearance.value"
            name="rv-appearance"
            :options="appearanceOptions"
            @update:model-value="onAppearance"
          />
          <div class="settings__copy">
            <RvSegmented
              :label="t('settings.mode')"
              :model-value="preferences.mode.value"
              name="rv-detail-mode"
              :options="modeOptions"
              @update:model-value="onMode"
            />
            <p class="settings__note">{{ t('settings.mode.note') }}</p>
          </div>
        </div>
      </RvSettingsSection>

      <RvSettingsSection
        :title="t('settings.refresh')"
        :description="t('settings.refresh.note')"
      >
        <RvStateNotice
          v-if="settings.readState.value === 'failed'"
          :body="
            t(
              settings.refreshInterval.value === null
                ? 'settings.refresh.read.failed.body'
                : 'settings.refresh.read.failed.stale',
            )
          "
          live
          :title="t('settings.refresh.read.failed')"
          tone="warning"
        >
          <template #action>
            <RvButton @click="settings.retry">{{ t('action.retry') }}</RvButton>
          </template>
        </RvStateNotice>
        <div class="settings__copy">
          <RvSegmented
            label-hidden
            :disabled="!settings.canChange.value"
            :label="t('settings.refresh')"
            :model-value="settings.refreshInterval.value ?? ''"
            name="rv-refresh-interval"
            :options="refreshOptions"
            @update:model-value="onRefreshInterval"
          />
          <p
            class="settings__note settings__feedback"
            :class="{
              'rv-loading-feedback':
                settings.readState.value === 'loading' &&
                settings.writeState.value !== 'failed',
            }"
            role="status"
          >
            {{
              settings.writeState.value === 'failed'
                ? t('settings.refresh.failed')
                : settings.writeState.value === 'saving'
                  ? t('settings.refresh.saving')
                  : settings.readState.value === 'loading'
                    ? t(
                        settings.refreshInterval.value === null
                          ? 'settings.refresh.reading'
                          : 'settings.refresh.refreshing',
                      )
                    : ''
            }}
          </p>
        </div>
      </RvSettingsSection>

      <ConfigTransferPanel @applied="onConfigTransferApplied" />
      <RvSettingsSection :title="t('settings.runtime')">
        <p class="settings__runtime">
          {{ t('settings.runtime.address') }} ·
          <span class="settings__mono">{{ origin }}</span>
        </p>
      </RvSettingsSection>
    </div>
  </section>
</template>

<style scoped>
.settings {
  display: grid;
  gap: var(--rv-space-6);
  width: 100%;
}

.settings__content {
  display: grid;
  gap: var(--rv-space-6);
  width: min(100%, var(--rv-settings-width));
  container: settings-content / inline-size;
}

.settings__fields {
  display: grid;
  gap: var(--rv-space-6);
}

.settings__copy {
  display: grid;
  gap: var(--rv-space-2);
  min-width: 0;
}

.settings__note {
  max-width: var(--rv-measure-prose);
  color: var(--rv-color-ink-tertiary);
  font-size: var(--rv-text-dense);
}

.settings__runtime {
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-interface);
}

.settings__feedback {
  min-height: 1lh;
}

.settings__mono {
  font-family: var(--rv-font-mono);
}
</style>
