<script setup lang="ts">
import { computed, ref } from 'vue'

import { useLocale } from '@/shared/i18n/useLocale'
import type { TargetOption } from '@/shared/api/catalog'
import type { DeviceCard } from '@/shared/api/devices'
import type { OutputCard } from '@/shared/api/outputs'
import type { RefreshInterval, Schedule } from '@/shared/api/profiles'
import type { ChoiceGroup, ChoiceOption } from '@/shared/ui/types'
import RvButton from '@/shared/ui/RvButton.vue'
import RvIcon from '@/shared/ui/RvIcon.vue'
import RvInfoTip from '@/shared/ui/RvInfoTip.vue'
import RvSelect from '@/shared/ui/RvSelect.vue'
import RvStateNotice from '@/shared/ui/RvStateNotice.vue'
import RvStatus from '@/shared/ui/RvStatus.vue'

const props = defineProps<{
  // An archived profile keeps every output it has and every file they published;
  // what it no longer accepts is a new one. The form is withdrawn rather than
  // disabled, because a disabled control still promises the action exists here.
  archived: boolean
  busy: boolean
  deployable: (output: OutputCard) => boolean
  devices: DeviceCard[]
  profileId: string
  outputs: OutputCard[]
  schedule: Schedule | null
  selectedId: string
  targetGroups: { kind: string; targets: TargetOption[] }[]
  // What to call a format here. The catalog owns the words and the page owns
  // the reader's language, so the panel is told rather than deciding.
  targetTitle: (targetID: string, fallback?: string) => string
}>()

const emit = defineEmits<{
  bind: [targetID: string]
  bindDevice: [outputID: string, deviceID: string]
  select: [outputID: string]
  setSchedule: [interval: RefreshInterval]
}>()

const { dateTime, t } = useLocale()
const chosenTarget = ref('')

const formatTime = (value: string): string => {
  const parsed = new Date(value)
  return Number.isNaN(parsed.valueOf()) ? '—' : dateTime.value.format(parsed)
}

const add = (): void => {
  if (chosenTarget.value === '') return
  emit('bind', chosenTarget.value)
  chosenTarget.value = ''
}

const outputTitle = (output: OutputCard): string =>
  props.targetTitle(output.targetID, output.targetTitle)

const deviceChoices = (output: OutputCard): ChoiceOption[] =>
  props.devices
    .filter(
      (device) =>
        device.targetID === output.targetID &&
        (device.deployable || device.id === output.deviceID),
    )
    .map((device) => ({
      disabled: !device.deployable,
      label: `${device.name} · ${t(device.autoDeliver ? 'outputs.device.ready' : 'outputs.device.disabled')}`,
      value: device.id,
    }))

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

const readiness = (
  output: OutputCard,
): 'choose' | 'auto' | 'refresh' | 'ready' => {
  if (output.deviceID === '') return 'choose'
  const device = props.devices.find(
    (candidate) => candidate.id === output.deviceID,
  )
  if (device?.autoDeliver !== true) return 'auto'
  if ((props.schedule?.effective ?? 'off') === 'off') return 'refresh'
  return 'ready'
}

const readinessLabel = (output: OutputCard): string => {
  const state = readiness(output)
  if (state === 'ready' && output.targetKind === 'router') {
    return t('outputs.readiness.ready.router')
  }
  return t(`outputs.readiness.${state}`)
}

// Devices and applications stay named runs: a format is looked for by the kind
// of thing it feeds before it is looked for by name.
const targetChoices = computed<ChoiceGroup[]>(() =>
  props.targetGroups.map((group) => ({
    key: group.kind,
    label: t('kind.' + group.kind),
    options: group.targets.map((target) => ({
      label: props.targetTitle(target.id, target.title),
      mono: '.' + target.fileExtension,
      value: target.id,
    })),
  })),
)
</script>

<template>
  <div class="outputs">
    <RvStateNotice
      v-if="props.outputs.length === 0"
      :body="t('outputs.empty.body')"
      :title="t('outputs.empty')"
      tone="waiting"
    />
    <div v-else class="outputs__scroll">
      <table class="outputs__table">
        <thead>
          <tr>
            <th scope="col">{{ t('outputs.column.target') }}</th>
            <th scope="col">{{ t('outputs.column.format') }}</th>
            <th scope="col">{{ t('outputs.column.device') }}</th>
            <th scope="col">{{ t('outputs.column.updated') }}</th>
            <th scope="col">
              <span class="outputs__visually-hidden">
                {{ t('outputs.column.actions') }}
              </span>
            </th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="output in props.outputs"
            :key="output.id"
            :class="{
              'outputs__row--selected': output.id === props.selectedId,
            }"
          >
            <td class="outputs__cell-target">
              <span class="outputs__name">
                <button
                  class="outputs__select"
                  type="button"
                  @click="emit('select', output.id)"
                >
                  {{ outputTitle(output) }}
                </button>
                <span v-if="output.targetKind !== ''" class="outputs__kind">
                  {{ t(`kind.${output.targetKind}.one`) }}
                </span>
              </span>
            </td>
            <td class="outputs__cell-format">
              {{
                output.fileExtension === '' ? '—' : `.${output.fileExtension}`
              }}
            </td>
            <td class="outputs__cell-device">
              <label
                :for="`output-device-${output.id}`"
                class="outputs__visually-hidden"
              >
                {{ t('outputs.device.label', { target: outputTitle(output) }) }}
              </label>
              <template v-if="props.deployable(output)">
                <div
                  v-if="deviceChoices(output).length > 0"
                  class="outputs__device"
                >
                  <RvSelect
                    :disabled="props.busy || props.archived"
                    searchable
                    :input-id="`output-device-${output.id}`"
                    :loading="props.busy"
                    :model-value="output.deviceID"
                    :options="deviceChoices(output)"
                    :placeholder="t('outputs.device.none')"
                    @update:model-value="emit('bindDevice', output.id, $event)"
                  />
                  <RvButton
                    v-if="output.deviceID !== ''"
                    :disabled="props.busy || props.archived"
                    size="compact"
                    variant="quiet"
                    @click="emit('bindDevice', output.id, '')"
                  >
                    {{ t('outputs.device.detach') }}
                  </RvButton>
                </div>
                <RvButton
                  v-else
                  size="compact"
                  to="/connections"
                  variant="quiet"
                >
                  <RvIcon name="plus" />
                  {{ t('outputs.device.add') }}
                </RvButton>
                <RvStatus
                  v-if="!props.archived && deviceChoices(output).length > 0"
                  class="outputs__readiness"
                  :label="readinessLabel(output)"
                  :tone="
                    readiness(output) === 'ready'
                      ? 'ready'
                      : readiness(output) === 'choose'
                        ? 'waiting'
                        : 'warning'
                  "
                />
              </template>
              <span v-else class="outputs__manual">
                {{ t('outputs.device.manual') }}
              </span>
            </td>
            <td class="outputs__cell-updated">
              <span class="outputs__updated">
                <RvStatus
                  v-if="output.lastAttempt?.status === 'failed'"
                  :label="t('outputs.failed')"
                  tone="failed"
                />
                <span>
                  {{
                    output.latest === null
                      ? t('profiles.noArtifact')
                      : formatTime(output.latest.contentCreatedAt)
                  }}
                </span>
              </span>
            </td>
            <td class="outputs__cell-actions">
              <div class="outputs__actions">
                <RvButton
                  v-if="output.latest !== null"
                  :aria-label="
                    t('outputs.download.aria', { target: outputTitle(output) })
                  "
                  :href="`/v1/artifacts/${output.latest.id}`"
                  size="compact"
                  variant="quiet"
                >
                  <RvIcon name="download" />
                  {{ t('outputs.download') }}
                </RvButton>
                <RvButton
                  v-if="props.deployable(output)"
                  size="compact"
                  :to="`/profiles/${props.profileId}/send/${output.id}`"
                  variant="quiet"
                >
                  <RvIcon name="send" />
                  {{ t('outputs.send') }}
                </RvButton>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <section
      v-if="props.schedule !== null && !props.archived"
      aria-labelledby="profile-schedule"
      class="outputs__schedule"
    >
      <h2 id="profile-schedule" class="outputs__section-title">
        {{ t('profile.schedule') }}
      </h2>
      <div class="outputs__schedule-field">
        <RvSelect
          v-model="scheduleValue"
          :disabled="props.busy"
          input-id="profile-schedule-select"
          labelled-by="profile-schedule"
          :loading="props.busy"
          :options="scheduleOptions"
          :placeholder="t('profile.schedule.default')"
        />
      </div>
      <p
        v-if="props.schedule.lastRefreshFailed"
        class="outputs__schedule-note"
        role="status"
      >
        {{ t('profile.schedule.failedNote') }}
      </p>
    </section>

    <form v-if="!props.archived" class="outputs__add" @submit.prevent="add">
      <label class="outputs__add-label" for="outputs-target">
        {{ t('outputs.add') }}
        <RvInfoTip
          :label="t('outputs.add.info')"
          :text="t('outputs.add.info.text')"
        />
      </label>
      <div class="outputs__add-row">
        <div class="outputs__target-field">
          <RvSelect
            v-model="chosenTarget"
            :disabled="props.busy || props.targetGroups.length === 0"
            :groups="targetChoices"
            searchable
            input-id="outputs-target"
            :loading="props.busy"
            :placeholder="t('outputs.add.placeholder')"
          />
        </div>
        <RvButton
          :disabled="props.busy || chosenTarget === ''"
          :loading="props.busy"
          type="submit"
          variant="secondary"
        >
          <RvIcon name="plus" />
          {{ t('outputs.add.submit') }}
        </RvButton>
      </div>
      <p v-if="props.targetGroups.length === 0" class="outputs__note">
        {{ t('outputs.add.none') }}
      </p>
    </form>
  </div>
</template>

<style scoped>
.outputs {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: var(--rv-space-8);
}

/* The table scrolls inside its own box on narrow screens; the page never
   scrolls sideways. The box is also the containing block, so the hidden column
   header cannot escape it and widen the document. */
.outputs__scroll {
  position: relative;
  overflow-x: auto;
}

.outputs__table {
  width: 100%;
  min-width: 34rem;
  border-collapse: collapse;
}

.outputs__table th {
  padding: var(--rv-space-3) var(--rv-space-4);
  color: var(--rv-color-ink-muted);
  font-weight: 600;
  font-size: var(--rv-text-dense);
  text-align: start;
  border-bottom: var(--rv-border-hair) solid var(--rv-color-rule-strong);
}

.outputs__table td {
  padding: var(--rv-space-3) var(--rv-space-4);
  font-size: var(--rv-text-interface);
  vertical-align: middle;
  border-bottom: var(--rv-border-hair) solid var(--rv-color-rule);
}

.outputs__row--selected td {
  background: var(--rv-color-surface-selected);
}

.outputs__cell-target {
  white-space: nowrap;
}

/* The cell stays a table cell; the flex column lives inside it, or the name
   column would fall out of the table's row grid. */
.outputs__name {
  display: inline-flex;
  flex-direction: column;
  gap: 0.125rem;
  align-items: flex-start;
}

.outputs__select {
  display: inline-flex;
  align-items: center;
  min-height: var(--rv-control-compact);
  padding: 0;
  color: var(--rv-color-ink);
  font: inherit;
  font-weight: 600;
  background: none;
  border: 0;
  cursor: pointer;
}

.outputs__select:focus-visible {
  outline: var(--rv-border-mark) solid var(--rv-color-focus);
  outline-offset: 0.125rem;
}

.outputs__select:hover {
  color: var(--rv-color-accent-ink);
}

.outputs__kind {
  color: var(--rv-color-ink-tertiary);
  font-size: var(--rv-text-meta);
}

.outputs__cell-format {
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
  font-family: var(--rv-font-mono);
}

.outputs__cell-updated {
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
  white-space: nowrap;
}

.outputs__cell-device {
  min-width: var(--rv-measure-field);
}

.outputs__device {
  display: flex;
  gap: var(--rv-space-2);
  align-items: center;
}

.outputs__readiness {
  margin-top: var(--rv-space-2);
}

.outputs__manual {
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
}

.outputs__updated {
  display: grid;
  gap: 0.125rem;
}

.outputs__cell-actions {
  white-space: nowrap;
}

.outputs__actions {
  display: flex;
  gap: var(--rv-space-2);
  align-items: center;
  justify-content: flex-end;
}

.outputs__add {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: var(--rv-space-3);
  max-width: 36rem;
}

.outputs__schedule {
  display: grid;
  gap: var(--rv-space-3);
  max-width: 28rem;
  padding-top: var(--rv-space-6);
  border-top: var(--rv-border-hair) solid var(--rv-color-rule);
}

.outputs__section-title {
  font-size: var(--rv-text-section);
}

.outputs__schedule-field {
  width: 100%;
}

.outputs__schedule-note {
  color: var(--rv-color-status-warning);
  font-size: var(--rv-text-dense);
}

.outputs__add-label {
  display: flex;
  gap: var(--rv-space-2);
  align-items: center;
  font-weight: 600;
  font-size: var(--rv-text-section);
}

.outputs__add-row {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-2);
  align-items: center;
}

.outputs__target-field {
  flex: 1 1 14rem;
  min-width: 0;
}

.outputs__note {
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
}

.outputs__visually-hidden {
  position: absolute;
  width: 0.0625rem;
  height: 0.0625rem;
  overflow: hidden;
  white-space: nowrap;
  clip-path: inset(50%);
}
</style>
