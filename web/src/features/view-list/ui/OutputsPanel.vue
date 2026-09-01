<script setup lang="ts">
import { computed, ref } from 'vue'

import { useLocale } from '@/shared/i18n/useLocale'
import type { TargetOption } from '@/shared/api/catalog'
import type { DeviceCard } from '@/shared/api/devices'
import type { OutputCard } from '@/shared/api/outputs'
import type { ChoiceGroup, ChoiceOption } from '@/shared/ui/kinds'
import RvButton from '@/shared/ui/RvButton.vue'
import RvIcon from '@/shared/ui/RvIcon.vue'
import RvInfoTip from '@/shared/ui/RvInfoTip.vue'
import RvSelect from '@/shared/ui/RvSelect.vue'
import RvStateNotice from '@/shared/ui/RvStateNotice.vue'
import RvStatus from '@/shared/ui/RvStatus.vue'

const props = defineProps<{
  // An archived list keeps every output it has and every file they published;
  // what it no longer accepts is a new one. The form is withdrawn rather than
  // disabled, because a disabled control still promises the action exists here.
  archived: boolean
  busy: boolean
  deployable: (output: OutputCard) => boolean
  devices: DeviceCard[]
  listId: string
  outputs: OutputCard[]
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
}>()

const { dateTime, t } = useLocale()
const chosenTarget = ref('')

function formatTime(value: string): string {
  const parsed = new Date(value)
  return Number.isNaN(parsed.valueOf()) ? '—' : dateTime.value.format(parsed)
}

function add(): void {
  if (chosenTarget.value === '') return
  emit('bind', chosenTarget.value)
  chosenTarget.value = ''
}

function outputTitle(output: OutputCard): string {
  return props.targetTitle(output.targetID, output.targetTitle)
}

function deviceChoices(output: OutputCard): ChoiceOption[] {
  return props.devices
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
              <div
                v-if="deviceChoices(output).length > 0"
                class="outputs__device"
              >
                <RvSelect
                  :disabled="props.busy || props.archived"
                  :input-id="`output-device-${output.id}`"
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
              <span v-else>—</span>
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
                      ? t('library.noArtifact')
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
                  :to="`/lists/${props.listId}/send/${output.id}`"
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
            input-id="outputs-target"
            :placeholder="t('outputs.add.placeholder')"
          />
        </div>
        <RvButton
          :disabled="props.busy || chosenTarget === ''"
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

<style scoped src="./OutputsPanel.css"></style>
