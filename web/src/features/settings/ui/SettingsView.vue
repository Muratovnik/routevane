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
import RvDisclosure from '@/shared/ui/RvDisclosure.vue'
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
      <section aria-labelledby="settings-interface" class="settings__section">
        <h2 id="settings-interface" class="settings__section-title">
          {{ t('settings.interface') }}
        </h2>
        <div class="settings__rows">
          <div class="settings__row">
            <p class="settings__label" aria-hidden="true">
              {{ t('settings.locale') }}
            </p>
            <RvSegmented
              label-hidden
              :label="t('settings.locale')"
              :model-value="locale"
              name="rv-locale"
              :options="localeOptions"
              @update:model-value="onLocale"
            />
          </div>
          <div class="settings__row">
            <p class="settings__label" aria-hidden="true">
              {{ t('settings.theme') }}
            </p>
            <RvSegmented
              label-hidden
              :label="t('settings.theme')"
              :model-value="preferences.appearance.value"
              name="rv-appearance"
              :options="appearanceOptions"
              @update:model-value="onAppearance"
            />
          </div>
          <div class="settings__row">
            <div class="settings__copy">
              <p class="settings__label" aria-hidden="true">
                {{ t('settings.mode') }}
              </p>
              <p class="settings__note">{{ t('settings.mode.note') }}</p>
            </div>
            <RvSegmented
              label-hidden
              :label="t('settings.mode')"
              :model-value="preferences.mode.value"
              name="rv-detail-mode"
              :options="modeOptions"
              @update:model-value="onMode"
            />
          </div>
        </div>
      </section>

      <section aria-labelledby="settings-refresh" class="settings__section">
        <h2 id="settings-refresh" class="settings__section-title">
          {{ t('settings.refresh') }}
        </h2>
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
        <div class="settings__rows">
          <div class="settings__row">
            <div class="settings__copy">
              <p class="settings__label" aria-hidden="true">
                {{ t('settings.refresh.label') }}
              </p>
              <p class="settings__note">{{ t('settings.refresh.note') }}</p>
            </div>
            <div class="settings__copy">
              <RvSegmented
                label-hidden
                :disabled="!settings.canChange.value"
                :label="t('settings.refresh.label')"
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
          </div>
        </div>
      </section>

      <ConfigTransferPanel @applied="onConfigTransferApplied" />
      <RvDisclosure :summary="t('settings.runtime')">
        <p class="settings__runtime">
          {{ t('settings.runtime.address') }} ·
          <span class="settings__mono">{{ origin }}</span>
        </p>
      </RvDisclosure>
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
  gap: var(--rv-space-8);
  width: min(100%, var(--rv-settings-width));
  container: settings-content / inline-size;
}

.settings__section {
  display: grid;
  gap: var(--rv-space-5);
}

.settings__section-title {
  font-size: var(--rv-text-section);
}

.settings__rows {
  display: grid;
  border-top: var(--rv-border-hair) solid var(--rv-color-rule);
}

.settings__row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  align-items: start;
  gap: var(--rv-space-6);
  padding-block: var(--rv-space-5);
  border-bottom: var(--rv-border-hair) solid var(--rv-color-rule);
}

.settings__copy {
  display: grid;
  gap: var(--rv-space-2);
  min-width: 0;
}

.settings__label {
  font-size: var(--rv-text-interface);
  font-weight: 600;
  padding-block: var(--rv-space-2);
}

@container settings-content (width < 42rem) {
  .settings__row {
    grid-template-columns: minmax(0, 1fr);
    gap: var(--rv-space-3);
  }
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
