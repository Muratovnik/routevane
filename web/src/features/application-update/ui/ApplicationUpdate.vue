<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import type { DesktopUpdateState } from '@/shared/api/desktop'
import { useLocale } from '@/shared/i18n/useLocale'
import RvIcon from '@/shared/ui/RvIcon.vue'
import RvButton from '@/shared/ui/RvButton.vue'
import RvTooltip from '@/shared/ui/RvTooltip.vue'

defineProps<{ collapsed: boolean }>()
const { t } = useLocale()
const state = ref<DesktopUpdateState>({ status: 'disabled' })
const visible = computed(
  () => !['disabled', 'idle'].includes(state.value.status),
)
const busy = computed(() =>
  ['downloading', 'installing'].includes(state.value.status),
)
const label = computed(() => {
  if (state.value.status === 'downloading')
    return t('update.downloading').replace(
      '{percent}',
      String(state.value.percent ?? 0),
    )
  if (state.value.status === 'installing') return t('update.installing')
  if (state.value.status === 'error') return t('update.retry')
  return t('update.available')
})
const hint = computed(() =>
  state.value.status === 'error'
    ? t('update.failed')
    : t('update.hint').replace('{version}', state.value.version ?? ''),
)
let unsubscribe: (() => void) | undefined
onMounted(async () => {
  const bridge = window.routevaneDesktop?.updates
  if (!bridge) return
  let received = false
  unsubscribe = bridge.subscribe((next) => {
    received = true
    state.value = next
  })
  try {
    const initial = await bridge.state()
    if (!received) state.value = initial
  } catch {
    /* The browser/older shell has no updater. */
  }
})
onUnmounted(() => unsubscribe?.())
const apply = async () => {
  try {
    await window.routevaneDesktop?.updates?.apply()
  } catch {
    state.value = { ...state.value, status: 'error' }
  }
}
</script>

<template>
  <RvTooltip v-if="visible" :text="hint">
    <RvButton
      class="application-update"
      :class="{ 'application-update--collapsed': collapsed }"
      type="button"
      :disabled="busy"
      :aria-label="label"
      :loading="busy"
      :loading-label="label"
      @click="apply"
    >
      <RvIcon
        v-if="!busy"
        :name="state.status === 'error' ? 'warning' : 'download'"
      />
      <span class="application-update__label">{{ label }}</span>
    </RvButton>
  </RvTooltip>
</template>

<!-- The RvButton facade forwards the native class through Nuxt UI. Keep this
     feature's unique layout class global so it reaches that native root. -->
<style>
.application-update {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  flex: none;
  align-items: center;
  justify-content: start;
  gap: var(--rv-space-3);
  width: 100%;
  min-height: var(--rv-control-touch);
  overflow: hidden;
  padding: var(--rv-space-3);
  transition:
    grid-template-columns var(--rv-motion-normal) var(--rv-motion-ease-out),
    gap var(--rv-motion-normal) var(--rv-motion-ease-out),
    padding-inline var(--rv-motion-normal) var(--rv-motion-ease-out);
  color: var(--rv-color-accent);
  background: var(--rv-color-chrome-raised);
  border: var(--rv-border-hair) solid var(--rv-color-rule-strong);
  border-radius: var(--rv-radius-md);
  text-align: start;
  font-size: var(--rv-text-interface);
  cursor: pointer;
}

.application-update:disabled {
  cursor: wait;
}

.application-update__label {
  white-space: nowrap;
  overflow: hidden;
  transition: opacity var(--rv-motion-normal) var(--rv-motion-ease-out);
}

/* This button sits on the same rail as the navigation and closes the same way:
   the label's track runs to nothing, the gap closes behind it and the inset
   grows to the one that puts the glyph on the rail's centre line. Every step is
   a length, so the glyph travels there instead of being re-aligned in a frame. */
.application-update--collapsed {
  grid-template-columns: auto minmax(0, 0fr);
  gap: 0;
  padding-inline: var(--rv-rail-inset);
}

.application-update--collapsed .application-update__label {
  opacity: 0;
}

@container (width <= 40rem) {
  .application-update--collapsed {
    grid-template-columns: auto minmax(0, 1fr);
    gap: var(--rv-space-3);
    padding-inline: var(--rv-space-3);
  }

  .application-update--collapsed .application-update__label {
    opacity: 1;
  }
}
</style>
