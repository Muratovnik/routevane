<script setup lang="ts">
import { computed, nextTick, ref, useTemplateRef } from 'vue'

import FormatSettingsDialog from '@/features/view-profile/ui/FormatSettingsDialog.vue'
import type { AutomaticDeliveryCapability } from '@/features/view-profile/model/useProfileView'
import { useLocale } from '@/shared/i18n/useLocale'
import { connectionPageHash } from '@/shared/lib/connectionHash'
import { targetIcon } from '@/shared/lib/targetIcon'
import type { TargetOption } from '@/shared/api/catalog'
import type { DeviceCard } from '@/shared/api/devices'
import type { OutputCard } from '@/shared/api/outputs'
import type { RefreshInterval, Schedule } from '@/shared/api/profiles'
import type { ChoiceGroup, ChoiceOption, MenuItem } from '@/shared/ui/types'
import RvButton from '@/shared/ui/RvButton.vue'
import RvField from '@/shared/ui/RvField.vue'
import RvIcon from '@/shared/ui/RvIcon.vue'
import RvMenu from '@/shared/ui/RvMenu.vue'
import RvSearchSelect from '@/shared/ui/RvSearchSelect.vue'
import RvSelect from '@/shared/ui/RvSelect.vue'
import RvStateNotice from '@/shared/ui/RvStateNotice.vue'
import RvStatus from '@/shared/ui/RvStatus.vue'
import RvTable from '@/shared/ui/RvTable.vue'
import RvTooltip from '@/shared/ui/RvTooltip.vue'

/**
 * Everything a profile publishes in, around one table: the profile's refresh
 * rule and «Add format» above it, one row per format, and every act on a format
 * in that row's menu. An archived profile keeps its formats and their files;
 * what it no longer accepts is change, so the controls that would make one are
 * withdrawn and the bound connection is stated rather than offered.
 */
const props = defineProps<{
  archived: boolean
  automaticDelivery: (device: DeviceCard) => AutomaticDeliveryCapability
  busy: boolean
  deployable: (output: OutputCard) => boolean
  devices: DeviceCard[]
  /** Formats left out of the picker by the operator's choice in Settings. */
  hiddenTargetCount?: number
  profileId: string
  outputs: OutputCard[]
  schedule: Schedule | null
  scheduleSaving?: boolean
  targetGroups: { kind: string; targets: TargetOption[] }[]
  // What to call a format here. The catalog owns the words and the page owns
  // the reader's language, so the panel is told rather than deciding.
  targetTitle: (targetID: string, fallback?: string) => string
}>()

const emit = defineEmits<{
  bind: [targetID: string]
  bindDevice: [outputID: string, deviceID: string]
  rebuild: [outputID: string]
  reissue: [outputID: string]
  remove: [outputID: string]
  setSchedule: [interval: RefreshInterval]
}>()

const { locale, t } = useLocale()

// A table cell holds the whole moment on one line: the short date and the
// minute, not the seconds a cell would have to cut.
const compactTime = computed(
  () =>
    new Intl.DateTimeFormat(locale.value, {
      dateStyle: 'short',
      timeStyle: 'short',
    }),
)

const formatTime = (value: string): string => {
  const parsed = new Date(value)
  return Number.isNaN(parsed.valueOf()) ? '—' : compactTime.value.format(parsed)
}

const outputTitle = (output: OutputCard): string =>
  props.targetTitle(output.targetID, output.targetTitle)

const deviceDeliveryState = (
  device: DeviceCard,
): 'ready' | 'disabled' | 'unavailable' | 'unknown' => {
  const capability = props.automaticDelivery(device)
  if (capability !== 'available') return capability
  return device.autoDeliver ? 'ready' : 'disabled'
}

const boundDevice = (output: OutputCard): DeviceCard | null =>
  props.devices.find(
    (candidate) =>
      candidate.id === output.deviceID &&
      candidate.targetID === output.targetID,
  ) ?? null

const compatibleDevices = (output: OutputCard): DeviceCard[] =>
  props.devices.filter(
    (device) =>
      device.targetID === output.targetID &&
      (device.deployable || device.id === output.deviceID),
  )

// «None» is a choice like any other, so leaving a format without a connection
// is done in the same place as giving it one. The connection's state is a
// note under its name, never part of the name.
const NO_DEVICE = 'none'
const deviceChoices = (output: OutputCard): ChoiceOption[] => [
  { label: t('outputs.device.none'), value: NO_DEVICE },
  ...compatibleDevices(output).map((device) => ({
    disabled: !device.deployable,
    label: device.name,
    note: t(`outputs.device.${deviceDeliveryState(device)}`),
    value: device.id,
  })),
]

const chooseDevice = (output: OutputCard, value: string): void => {
  const deviceID = value === NO_DEVICE ? '' : value
  if (deviceID !== output.deviceID) emit('bindDevice', output.id, deviceID)
}

const FOLLOW_SETTINGS = 'default'
const scheduleOptions = computed<ChoiceOption[]>(() => [
  { label: t('profile.schedule.default'), value: FOLLOW_SETTINGS },
  { label: t('settings.refresh.off'), value: 'off' },
  { label: t('settings.refresh.daily'), value: 'daily' },
  { label: t('settings.refresh.weekly'), value: 'weekly' },
])

const scheduleValue = computed<string>({
  get: () => {
    const interval = props.schedule?.interval ?? ''
    return interval === '' ? FOLLOW_SETTINGS : interval
  },
  set: (value) => {
    emit(
      'setSchedule',
      (value === FOLLOW_SETTINGS ? '' : value) as RefreshInterval,
    )
  },
})

// The connection page owns the state of a connection; a row only leads there,
// to the connection itself, selected.
const connectionLink = (output: OutputCard): string =>
  `/connections${connectionPageHash(output.deviceID)}`

// The date the file last reached the connection the row is bound to. The server
// already reports only that connection's send; the comparison guards against a
// card read just before the row was re-bound.
const lastSent = (output: OutputCard): string => {
  const delivery = output.lastDelivery
  if (
    delivery === null ||
    output.deviceID === '' ||
    delivery.deviceID !== output.deviceID
  ) {
    return '—'
  }
  return formatTime(delivery.completedAt)
}

// Formats stay grouped by the kind of thing they feed: a reader looks for a
// router or an application before looking for a name.
const targetChoices = computed<ChoiceGroup[]>(() =>
  props.targetGroups.map((group) => ({
    key: group.kind,
    label: t('kind.' + group.kind),
    options: group.targets.map((target) => ({
      label: props.targetTitle(target.id, target.title),
      mono: '.' + target.fileExtension,
      value: target.id,
      icon: targetIcon(target.id),
    })),
  })),
)
// With every format added, or the rest hidden in Settings, «Add format» has
// nothing to open. The control stays where it is, disabled, and the note beside
// it is its accessible description: the reason is read with the control.
const nothingToAdd = computed(() => props.targetGroups.length === 0)
const addTrigger = useTemplateRef<HTMLElement>('addTrigger')

/** Brings the operator to «Add format», as the profile menu promises. */
const focusAdd = async (): Promise<void> => {
  await nextTick()
  addTrigger.value?.querySelector('button')?.focus()
}
defineExpose({ focusAdd })

const settingsFor = ref<OutputCard | null>(null)
const settingsOpen = ref(false)
const hasSettings = (output: OutputCard): boolean =>
  output.targetID === 'keenetic-dns'

const rowMenu = (output: OutputCard): MenuItem[] => {
  const items: MenuItem[] = []
  const opens = output.deviceID !== ''
  if (opens) {
    items.push({
      icon: 'targets',
      key: 'connection',
      label: t('outputs.menu.open'),
      to: connectionLink(output),
    })
  }
  if (props.archived) return items
  if (hasSettings(output)) {
    items.push({
      disabled: props.busy,
      icon: 'settings',
      key: 'settings',
      label: t('outputs.menu.settings'),
      separatorBefore: opens,
    })
  }
  items.push({
    disabled: props.busy,
    icon: 'refresh',
    key: 'rebuild',
    label: t('outputs.menu.rebuild'),
    separatorBefore: opens && !hasSettings(output),
  })
  if (output.latest !== null) {
    items.push({
      disabled: props.busy,
      icon: 'link',
      key: 'reissue',
      label: t('outputs.menu.reissue'),
    })
  }
  items.push({
    disabled: props.busy,
    icon: 'trash',
    key: 'remove',
    label: t('outputs.menu.remove'),
    separatorBefore: true,
  })
  return items
}

const onRowMenu = (output: OutputCard, key: string): void => {
  if (key === 'settings') {
    settingsFor.value = output
    settingsOpen.value = true
  }
  if (key === 'rebuild') emit('rebuild', output.id)
  if (key === 'reissue') emit('reissue', output.id)
  if (key === 'remove') emit('remove', output.id)
}
</script>

<template>
  <div class="outputs">
    <div v-if="!props.archived" class="outputs__toolbar">
      <div v-if="props.schedule !== null" class="outputs__schedule">
        <RvField
          input-id="profile-schedule-select"
          :label="t('profile.schedule')"
        >
          <template #default="{ describedBy }">
            <RvSelect
              v-model="scheduleValue"
              :described-by="describedBy"
              :disabled="props.scheduleSaving"
              input-id="profile-schedule-select"
              :loading="props.scheduleSaving"
              :options="scheduleOptions"
              :placeholder="t('profile.schedule.default')"
            />
          </template>
        </RvField>
        <RvStatus
          v-if="props.schedule.lastRefreshFailed"
          :label="t('profile.schedule.failedNote')"
          tone="warning"
        />
      </div>
      <div ref="addTrigger" class="outputs__add">
        <span id="outputs-add-label" class="rv-visually-hidden">
          {{ t('outputs.add') }}
        </span>
        <span v-if="nothingToAdd" id="outputs-add-reason" class="outputs__note">
          {{
            (props.hiddenTargetCount ?? 0) > 0
              ? t('outputs.add.hidden')
              : t('outputs.add.none')
          }}
        </span>
        <RvSearchSelect
          :described-by="nothingToAdd ? 'outputs-add-reason' : undefined"
          :disabled="props.busy || nothingToAdd"
          :groups="targetChoices"
          labelled-by="outputs-add-label"
          :loading="props.busy"
          model-value=""
          :placeholder="t('outputs.add')"
          size="compact"
          :trigger-label="t('outputs.add')"
          @update:model-value="emit('bind', $event)"
        />
      </div>
    </div>

    <RvStateNotice
      v-if="props.outputs.length === 0"
      :body="t('outputs.empty.body')"
      :title="t('outputs.empty')"
      tone="waiting"
    />
    <RvTable v-else class="outputs__scroll">
      <thead>
        <tr>
          <th scope="col">{{ t('outputs.column.target') }}</th>
          <th scope="col">{{ t('outputs.column.device') }}</th>
          <th scope="col">{{ t('outputs.column.updated') }}</th>
          <th scope="col">{{ t('outputs.column.sent') }}</th>
          <th scope="col">
            <span class="rv-visually-hidden">
              {{ t('outputs.column.actions') }}
            </span>
          </th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="output in props.outputs" :key="output.id">
          <td class="outputs__cell-target">
            <!-- The format names its own row and nothing else: reading this
                 table is not a choice, so no row is marked as the chosen one. -->
            <span class="outputs__format">
              <RvIcon :name="targetIcon(output.targetID)" />
              <span class="outputs__format-name">{{
                outputTitle(output)
              }}</span>
              <span
                v-if="output.fileExtension !== ''"
                class="outputs__extension"
              >
                .{{ output.fileExtension }}
              </span>
            </span>
            <span v-if="output.targetKind !== ''" class="outputs__kind">
              {{ t(`kind.${output.targetKind}.one`) }}
            </span>
          </td>
          <td class="outputs__cell-device">
            <span v-if="!props.deployable(output)" class="outputs__muted">
              {{ t('outputs.device.manual') }}
            </span>
            <span v-else-if="props.archived" class="outputs__bound">
              {{ boundDevice(output)?.name ?? t('outputs.device.none') }}
            </span>
            <template v-else-if="compatibleDevices(output).length > 0">
              <label
                :id="`output-device-${output.id}-label`"
                :for="`output-device-${output.id}`"
                class="rv-visually-hidden"
              >
                {{ t('outputs.device.label', { target: outputTitle(output) }) }}
              </label>
              <RvSelect
                :disabled="props.busy"
                searchable
                :input-id="`output-device-${output.id}`"
                :label="
                  t('outputs.device.label', { target: outputTitle(output) })
                "
                :labelled-by="`output-device-${output.id}-label`"
                :loading="props.busy"
                :model-value="
                  output.deviceID === '' ? NO_DEVICE : output.deviceID
                "
                :options="deviceChoices(output)"
                :placeholder="t('outputs.device.none')"
                @update:model-value="chooseDevice(output, $event)"
              />
            </template>
            <RvButton v-else size="compact" to="/connections" variant="quiet">
              {{ t('outputs.device.add') }}
            </RvButton>
          </td>
          <td class="outputs__cell-updated">
            <RvStatus
              v-if="output.lastAttempt?.status === 'failed'"
              :label="t('outputs.failed')"
              tone="failed"
            />
            <span
              class="outputs__date"
              :class="{ 'outputs__date--none': output.latest === null }"
            >
              {{
                output.latest === null
                  ? t('profiles.noArtifact')
                  : formatTime(output.latest.contentCreatedAt)
              }}
            </span>
          </td>
          <td class="outputs__cell-sent">
            <span class="outputs__date">{{ lastSent(output) }}</span>
          </td>
          <td class="outputs__cell-actions">
            <div class="outputs__actions">
              <RvTooltip
                v-if="output.latest !== null"
                :text="
                  t('outputs.download.aria', { target: outputTitle(output) })
                "
                side="top"
              >
                <RvButton
                  icon-only
                  :href="`/v1/artifacts/${output.latest.id}`"
                  :label="
                    t('outputs.download.aria', { target: outputTitle(output) })
                  "
                  variant="quiet"
                >
                  <RvIcon name="download" />
                </RvButton>
              </RvTooltip>
              <RvTooltip
                v-if="!props.archived && props.deployable(output)"
                :text="t('outputs.send.aria', { target: outputTitle(output) })"
                side="top"
              >
                <RvButton
                  icon-only
                  :label="
                    t('outputs.send.aria', { target: outputTitle(output) })
                  "
                  :to="`/profiles/${props.profileId}/send/${output.id}`"
                  variant="quiet"
                >
                  <RvIcon name="send" />
                </RvButton>
              </RvTooltip>
              <RvMenu
                v-if="rowMenu(output).length > 0"
                :items="rowMenu(output)"
                :label="t('outputs.menu', { target: outputTitle(output) })"
                @select="onRowMenu(output, $event)"
              />
            </div>
          </td>
        </tr>
      </tbody>
    </RvTable>

    <FormatSettingsDialog
      v-if="settingsFor !== null"
      v-model:open="settingsOpen"
      :busy="props.busy"
      :output-id="settingsFor.id"
      :prefix="settingsFor.fqdnGroupPrefix ?? ''"
      :title="outputTitle(settingsFor)"
      @rebuild="emit('rebuild', $event)"
    />
  </div>
</template>

<style scoped>
.outputs {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: var(--rv-space-4);
  min-width: 0;
}

/* The refresh rule is one field; «Add format» belongs to the table it adds a
   row to, so it stands at the table's trailing edge, on the field's control. */
.outputs__toolbar {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-3) var(--rv-space-5);
  align-items: flex-end;
  justify-content: space-between;
}

.outputs__schedule {
  display: grid;
  gap: var(--rv-space-2);
  width: min(100%, var(--rv-measure-field));
}

.outputs__add {
  display: flex;
  gap: var(--rv-space-3);
  align-items: center;
  margin-inline-start: auto;
}

.outputs__note,
.outputs__muted {
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
  white-space: nowrap;
}

/* The table scrolls inside its own box on narrow screens; the page never
   scrolls sideways. Each cell keeps to one line and shortens a long name with
   an ellipsis rather than growing the row. */
.outputs__scroll {
  --rv-table-min-width: var(--rv-outputs-table-width);
  --rv-table-layout: fixed;
  --rv-table-cell-padding: var(--rv-space-3);
}

/* The format and the two dates keep the width their one line needs and the
   actions the width of their three square buttons, so the «⋮» trigger is never
   squeezed below its target size; the connection takes what is left. */
.outputs__scroll th:nth-child(1) {
  width: 24%;
}

.outputs__scroll th:nth-child(3),
.outputs__scroll th:nth-child(4) {
  width: 10rem;
}

.outputs__scroll th:nth-child(5) {
  width: calc(
    3 * var(--rv-control-compact) + 2 * var(--rv-space-1) + 2 *
      var(--rv-space-3)
  );
}

.outputs__cell-target,
.outputs__cell-updated,
.outputs__cell-sent {
  overflow: hidden;
}

.outputs__format {
  display: flex;
  align-items: center;
  gap: var(--rv-space-2);
  min-width: 0;
  min-height: var(--rv-control-compact);
  font-weight: 600;
  white-space: nowrap;
}

.outputs__format-name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}

.outputs__extension {
  flex-shrink: 0;
  color: var(--rv-color-ink-muted);
  font-weight: 400;
  font-size: var(--rv-text-dense);
  font-family: var(--rv-font-mono);
}

.outputs__kind {
  display: block;
  color: var(--rv-color-ink-tertiary);
  font-size: var(--rv-text-meta);
  white-space: nowrap;
}

.outputs__cell-device {
  min-width: 0;
}

.outputs__bound {
  display: block;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}

.outputs__cell-updated,
.outputs__cell-sent {
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
  white-space: nowrap;
}

.outputs__date {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* A date fits the column; the words saying there is no file do not in every
   language, and cut short they no longer say it. They wrap instead. */
.outputs__date--none {
  white-space: normal;
}

.outputs__actions {
  display: flex;
  flex-wrap: nowrap;
  gap: var(--rv-space-1);
  align-items: center;
  justify-content: flex-end;
}

.outputs__actions > * {
  flex-shrink: 0;
}
</style>
