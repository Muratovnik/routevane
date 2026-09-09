<script setup lang="ts">
import RvTable from '@/shared/ui/RvTable.vue'
import { targetIcon } from '@/shared/lib/targetIcon'
import { onMounted, ref } from 'vue'

import { useLocale } from '@/shared/i18n/useLocale'
import { useSurfacePreferences } from '@/shared/model/useSurfacePreferences'
import {
  loadCatalog,
  localizedTargetHint,
  localizedTargetTitle,
  type TargetOption,
} from '@/shared/api/catalog'
import { loadDeployableTargets } from '@/shared/api/deploy'
import RvIcon from '@/shared/ui/RvIcon.vue'
import RvButton from '@/shared/ui/RvButton.vue'
import RvInfoTip from '@/shared/ui/RvInfoTip.vue'
import RvStateNotice from '@/shared/ui/RvStateNotice.vue'

type State = 'loading' | 'ready' | 'empty' | 'failed'

const { locale, t } = useLocale()
const preferences = useSurfacePreferences()

const state = ref<State>('loading')
const targets = ref<TargetOption[]>([])
const deployableIDs = ref<Set<string>>(new Set())
const deployabilityKnown = ref(false)

onMounted(() => {
  void initialize()
})

const initialize = async (): Promise<void> => {
  state.value = 'loading'
  deployabilityKnown.value = false
  const [catalog, deployables] = await Promise.allSettled([
    loadCatalog(),
    loadDeployableTargets(),
  ])
  if (deployables.status === 'fulfilled') {
    deployableIDs.value = new Set(
      deployables.value.map((entry) => entry.targetID),
    )
    deployabilityKnown.value = true
  }
  if (catalog.status === 'rejected') {
    state.value = 'failed'
    return
  }
  targets.value = catalog.value.targets
  state.value = targets.value.length === 0 ? 'empty' : 'ready'
}

const visible = (target: TargetOption): boolean =>
  !preferences.hiddenTargets.value.includes(target.id)

const title = (target: TargetOption): string =>
  localizedTargetTitle(target, locale.value, target.id)

const hint = (target: TargetOption): string =>
  localizedTargetHint(target, locale.value)

const onToggle = (target: TargetOption, event: Event): void => {
  const input = event.target as HTMLInputElement
  preferences.setTargetHidden(target.id, !input.checked)
}
</script>

<template>
  <!-- Secondary reference, not a destination: the operator's own connections
       are the page, and this is the catalog they are instances of. The
       disclosure that holds it carries the heading, so the region takes its
       name from the same words rather than repeating them on screen. -->
  <section :aria-label="t('targets.title')" class="targets">
    <RvStateNotice
      v-if="state === 'loading'"
      live
      :title="t('targets.loading')"
      tone="busy"
    />
    <RvStateNotice
      v-else-if="state === 'failed'"
      :body="t('targets.failed.body')"
      live
      :title="t('targets.failed')"
      tone="failed"
    >
      <template #action>
        <RvButton @click="initialize">{{ t('action.retry') }}</RvButton>
      </template>
    </RvStateNotice>
    <RvStateNotice
      v-else-if="state === 'empty'"
      :title="t('targets.empty')"
      tone="waiting"
    />

    <template v-else>
      <RvStateNotice
        v-if="!deployabilityKnown"
        :body="t('targets.degraded.body')"
        :title="t('targets.degraded')"
        tone="warning"
      >
        <template #action>
          <RvButton @click="initialize">{{ t('action.retry') }}</RvButton>
        </template>
      </RvStateNotice>
      <RvTable class="targets__scroll">
        <thead>
          <tr>
            <th scope="col">{{ t('targets.column.target') }}</th>
            <th scope="col">{{ t('targets.column.kind') }}</th>
            <th scope="col">{{ t('targets.column.format') }}</th>
            <th scope="col">{{ t('targets.column.auto') }}</th>
            <th scope="col">{{ t('targets.column.visible') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="target in targets" :key="target.id">
            <td class="targets__cell-name">
              <span class="targets__name">
                <RvIcon :name="targetIcon(target.id)" />
                {{ title(target) }}
                <RvInfoTip
                  :label="t('targets.hint', { target: title(target) })"
                  :text="hint(target)"
                />
              </span>
            </td>
            <td>{{ t(`kind.${target.kind}.one`) }}</td>
            <td class="targets__cell-format">.{{ target.fileExtension }}</td>
            <td>
              {{
                !deployabilityKnown
                  ? t('targets.auto.unknown')
                  : deployableIDs.has(target.id)
                    ? t('targets.auto.yes')
                    : t('targets.auto.no')
              }}
            </td>
            <td>
              <label class="targets__toggle">
                <input
                  :aria-label="t('targets.toggle', { target: title(target) })"
                  :checked="visible(target)"
                  class="targets__checkbox"
                  type="checkbox"
                  @change="onToggle(target, $event)"
                />
              </label>
            </td>
          </tr>
        </tbody>
      </RvTable>
    </template>
  </section>
</template>

<style scoped>
.targets {
  display: grid;
  gap: var(--rv-space-6);
  width: 100%;
}

.targets__scroll {
  overflow-x: auto;

  --rv-table-min-width: var(--rv-targets-table-width);
}

.targets__cell-name {
  font-weight: 600;
  white-space: nowrap;
}

/* The cell stays a table cell; the flex row lives inside it, or the name
   column would fall out of the table's row grid. */
.targets__name {
  display: inline-flex;
  gap: var(--rv-space-1);
  align-items: center;
}

.targets__cell-format {
  font-size: var(--rv-text-dense);
  font-family: var(--rv-font-mono);
}

.targets__toggle {
  display: inline-flex;
  align-items: center;
  min-width: var(--rv-control-compact);
  min-height: var(--rv-control-compact);
  justify-content: center;
  cursor: pointer;
}

.targets__checkbox {
  width: var(--rv-control-choice);
  height: var(--rv-control-choice);
  accent-color: var(--rv-color-accent);
}

.targets__checkbox:focus-visible {
  outline: var(--rv-border-mark) solid var(--rv-color-focus);
  outline-offset: 0.125rem;
}
</style>
