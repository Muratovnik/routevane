<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'

import type { RefreshInterval } from '@/shared/api/lists'
import { loadSettings, saveDefaultRefreshInterval } from '@/shared/api/settings'
import { useLocale } from '@/shared/i18n/useLocale'
import { localeNames, type Locale } from '@/shared/i18n/messages'
import {
  useSurfacePreferences,
  type Appearance,
  type DetailMode,
} from '@/shared/model/useSurfacePreferences'
import RvSegmented from '@/shared/ui/RvSegmented.vue'

const { locale, setLocale, t } = useLocale()
const preferences = useSurfacePreferences()

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

// The refresh rule is the server's, not this browser's: the process acts on it
// whether or not anyone has a tab open. It is read and written over the API for
// that reason, and a failed write says so rather than leaving the control
// showing a value nothing stored.
const refreshInterval = ref<RefreshInterval>('off')
const refreshState = ref<'loading' | 'ready' | 'saving' | 'failed'>('loading')
let refreshRequest = 0

const refreshOptions = computed(() => [
  { label: t('settings.refresh.off'), value: 'off' },
  { label: t('settings.refresh.daily'), value: 'daily' },
  { label: t('settings.refresh.weekly'), value: 'weekly' },
])

onMounted(async () => {
  try {
    refreshInterval.value = (await loadSettings()).refreshInterval || 'off'
    refreshState.value = 'ready'
  } catch {
    refreshState.value = 'failed'
  }
})

async function onRefreshInterval(value: string): Promise<void> {
  if (refreshState.value === 'loading' || refreshState.value === 'saving')
    return
  const request = ++refreshRequest
  const previous = refreshInterval.value
  refreshInterval.value = value as RefreshInterval
  refreshState.value = 'saving'
  try {
    await saveDefaultRefreshInterval(value as RefreshInterval)
    if (request !== refreshRequest) return
    refreshState.value = 'ready'
  } catch {
    if (request !== refreshRequest) return
    refreshInterval.value = previous
    refreshState.value = 'failed'
  }
}

function onLocale(value: string): void {
  setLocale(value as Locale)
}

function onAppearance(value: string): void {
  preferences.setAppearance(value as Appearance)
}

function onMode(value: string): void {
  preferences.setMode(value as DetailMode)
}
</script>

<template>
  <section aria-labelledby="settings-title" class="settings">
    <header>
      <h1 id="settings-title" class="settings__title">
        {{ t('settings.title') }}
      </h1>
    </header>

    <section aria-labelledby="settings-interface" class="settings__section">
      <h2 id="settings-interface" class="settings__section-title">
        {{ t('settings.interface') }}
      </h2>
      <div class="settings__rows">
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
        <div class="settings__row">
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
    </section>

    <section aria-labelledby="settings-refresh" class="settings__section">
      <h2 id="settings-refresh" class="settings__section-title">
        {{ t('settings.refresh') }}
      </h2>
      <div class="settings__rows">
        <div class="settings__row">
          <RvSegmented
            :disabled="refreshState === 'loading' || refreshState === 'saving'"
            :label="t('settings.refresh.label')"
            :model-value="refreshInterval"
            name="rv-refresh-interval"
            :options="refreshOptions"
            @update:model-value="onRefreshInterval"
          />
          <p class="settings__note" role="status">
            {{
              refreshState === 'failed'
                ? t('settings.refresh.failed')
                : refreshState === 'saving'
                  ? t('settings.refresh.saving')
                  : t('settings.refresh.note')
            }}
          </p>
        </div>
      </div>
    </section>

    <section aria-labelledby="settings-runtime" class="settings__section">
      <h2 id="settings-runtime" class="settings__section-title">
        {{ t('settings.runtime') }}
      </h2>
      <p class="settings__runtime">
        {{ t('settings.runtime.address') }} ·
        <span class="settings__mono">{{ origin }}</span>
      </p>
    </section>
  </section>
</template>

<style scoped>
.settings {
  display: grid;
  gap: var(--rv-space-10);
  width: 100%;
}

.settings__title {
  font-size: var(--rv-text-page);
  line-height: var(--rv-leading-tight);
  letter-spacing: var(--rv-tracking-title);
}

.settings__section {
  display: grid;
  gap: var(--rv-space-5);
  padding-top: var(--rv-space-6);
  border-top: var(--rv-border-hair) solid var(--rv-color-rule);
}

.settings__section-title {
  font-size: var(--rv-text-section);
}

.settings__rows {
  display: grid;
  gap: var(--rv-space-8);
  justify-items: start;
}

.settings__row {
  display: grid;
  gap: var(--rv-space-3);
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

.settings__mono {
  font-family: var(--rv-font-mono);
}
</style>
