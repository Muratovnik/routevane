<script setup lang="ts">
import { computed } from 'vue'

import {
  localizedTargetHint,
  localizedTargetTitle,
  type TargetOption,
} from '@/shared/api/catalog'
import { useLocale } from '@/shared/i18n/useLocale'
import { targetIcon } from '@/shared/lib/targetIcon'
import RvButton from '@/shared/ui/RvButton.vue'
import RvDisclosure from '@/shared/ui/RvDisclosure.vue'
import RvIcon from '@/shared/ui/RvIcon.vue'
import RvInfoTip from '@/shared/ui/RvInfoTip.vue'
import RvStateNotice from '@/shared/ui/RvStateNotice.vue'
import RvTable from '@/shared/ui/RvTable.vue'

/**
 * Every format this build carries, as reference material beside the
 * connections the operator made. It states what the catalog says and whether
 * each format can be delivered to automatically; which formats the profile
 * composer offers is a preference and lives in Settings.
 *
 * It reads nothing itself. The catalog and the deployment requirements are the
 * screen's, and a second read of either could disagree with the first.
 */
const props = defineProps<{
  /** The formats that have a deployer, or null while that is not known. */
  deliverableIds: ReadonlySet<string> | null
  targets: TargetOption[]
}>()

const emit = defineEmits<{ retry: [] }>()

const { locale, t } = useLocale()

const title = (target: TargetOption): string =>
  localizedTargetTitle(target, locale.value, target.id)

const hint = (target: TargetOption): string =>
  localizedTargetHint(target, locale.value)

// What the collapsed section already answers: which formats this build
// carries. The catalog states them, so a build that gains or loses one says
// so here without anything being rewritten.
const summary = computed(() =>
  props.targets.map((target) => title(target)).join(' · '),
)

const delivery = (target: TargetOption): string => {
  if (props.deliverableIds === null) return t('targets.auto.unknown')
  return t(
    props.deliverableIds.has(target.id)
      ? 'targets.auto.yes'
      : 'targets.auto.no',
  )
}
</script>

<template>
  <!-- Secondary reference, not a destination: the operator's own connections
       are the page, and this is the catalog they are instances of. The
       disclosure carries the heading and the names it holds, so the region
       takes its name from the same words rather than repeating them on screen,
       and the summary is the catalog's own answer rather than a written list. -->
  <RvDisclosure borderless :hint="summary" :summary="t('targets.title')">
    <div class="formats">
      <RvStateNotice
        v-if="deliverableIds === null"
        :body="t('targets.degraded.body')"
        :title="t('targets.degraded')"
        tone="warning"
      >
        <template #action>
          <RvButton @click="emit('retry')">{{ t('action.retry') }}</RvButton>
        </template>
      </RvStateNotice>
      <RvTable class="formats__scroll">
        <thead>
          <tr>
            <th scope="col">{{ t('targets.column.target') }}</th>
            <th scope="col">{{ t('targets.column.kind') }}</th>
            <th scope="col">{{ t('targets.column.format') }}</th>
            <th scope="col">{{ t('targets.column.auto') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="target in targets" :key="target.id">
            <td class="formats__cell-name">
              <span class="formats__name">
                <RvIcon :name="targetIcon(target.id)" />
                {{ title(target) }}
                <RvInfoTip
                  :label="t('targets.hint', { target: title(target) })"
                  :text="hint(target)"
                />
              </span>
            </td>
            <td>{{ t(`kind.${target.kind}.one`) }}</td>
            <td class="formats__cell-format">.{{ target.fileExtension }}</td>
            <td>{{ delivery(target) }}</td>
          </tr>
        </tbody>
      </RvTable>
    </div>
  </RvDisclosure>
</template>

<style scoped>
.formats {
  display: grid;
  gap: var(--rv-space-6);
  width: 100%;
}

.formats__scroll {
  overflow-x: auto;

  --rv-table-min-width: var(--rv-targets-table-width);
}

.formats__cell-name {
  font-weight: 600;
  white-space: nowrap;
}

/* The cell stays a table cell; the flex row lives inside it, or the name
   column would fall out of the table's row grid. */
.formats__name {
  display: inline-flex;
  gap: var(--rv-space-1);
  align-items: center;
}

.formats__cell-format {
  font-size: var(--rv-text-dense);
  font-family: var(--rv-font-mono);
}
</style>
