<script setup lang="ts">
import {
  computed,
  nextTick,
  onMounted,
  ref,
  useId,
  useTemplateRef,
  watch,
} from 'vue'

import {
  addressRefused,
  failureBodyKey,
} from '@/features/devices/lib/deviceFailure'
import { useConnectionFields } from '@/features/devices/model/useConnectionFields'
import { useDevices } from '@/features/devices/model/useDevices'
import ConnectionDetail from '@/features/devices/ui/ConnectionDetail.vue'
import ConnectionFields from '@/features/devices/ui/ConnectionFields.vue'
import ConnectionList from '@/features/devices/ui/ConnectionList.vue'
import FormatReference from '@/features/devices/ui/FormatReference.vue'
import { localizedTargetTitle } from '@/shared/api/catalog'
import type { DeviceCard } from '@/shared/api/devices'
import { useLocale } from '@/shared/i18n/useLocale'
import { targetIcon } from '@/shared/lib/targetIcon'
import RvButton from '@/shared/ui/RvButton.vue'
import RvField from '@/shared/ui/RvField.vue'
import RvIcon from '@/shared/ui/RvIcon.vue'
import RvPageHeader from '@/shared/ui/RvPageHeader.vue'
import RvSelect from '@/shared/ui/RvSelect.vue'
import RvStateNotice from '@/shared/ui/RvStateNotice.vue'
import type { ChoiceOption } from '@/shared/ui/types'

// A link from another screen can name the connection to open; the Connections
// page reads it from the address and hands it in.
const props = defineProps<{ initialConnection?: string }>()
// The connection the operator opened, so the page can keep it in the address.
const emit = defineEmits<{ select: [id: string] }>()

const { locale, t } = useLocale()
const devices = useDevices()

// A registered device carries the title it was registered under; the catalog
// is what can say that title in the reader's language today.
const targetTitle = (targetID: string, fallback: string): string =>
  localizedTargetTitle(
    devices.targets.value.find((target) => target.id === targetID),
    locale.value,
    fallback,
  )
const titleOf = (connection: DeviceCard): string =>
  targetTitle(connection.targetID, connection.targetTitle)

const targetID = ref('')
const targetChoices = computed<ChoiceOption[]>(() =>
  devices.targets.value.map((target) => ({
    label: targetTitle(target.id, target.title),
    value: target.id,
    icon: targetIcon(target.id),
  })),
)

const draft = ref({ account: '', address: '', interfaceName: '', name: '' })
const reveal = ref(false)
// The service refused this draft's address and named the field; the words stay
// at the field until the address or the format changes.
const refusedAddress = ref(false)
const confirmation = ref('')
const selectedID = ref('')
// The connection the form replaced, so that leaving the form by its own action
// returns there rather than to an arbitrary row.
const returnTo = ref('')
const createHeadingID = useId()
const emptyHeadingID = useId()
const createHeading = ref<HTMLElement | null>(null)
const emptyHeading = ref<HTMLElement | null>(null)
const detail = ref<InstanceType<typeof ConnectionDetail> | null>(null)
const fields = useTemplateRef<InstanceType<typeof ConnectionFields>>('fields')
// Whether the operator asked to register one. An empty registry is not the
// same state as registering: it states that it holds nothing and offers the
// act, rather than putting a form in front of someone who has not asked.
const composing = ref(false)

const selectedDevice = computed(
  () =>
    devices.devices.value.find((device) => device.id === selectedID.value) ??
    null,
)
// The form is open, as opposed to nothing being selected: an empty registry
// selects nothing either, and its own action must stay available.
const creating = computed(
  () => composing.value && selectedDevice.value === null,
)
const empty = computed(
  () => devices.devices.value.length === 0 && !composing.value,
)

const focusSelection = async (): Promise<void> => {
  await nextTick()
  if (selectedID.value === '') emptyHeading.value?.focus()
  else detail.value?.focusHeading()
}

const openConnection = async (id: string): Promise<void> => {
  if (devices.busy.value) return
  composing.value = false
  selectedID.value = id
  emit('select', id)
  await focusSelection()
}

const startComposing = async (): Promise<void> => {
  if (devices.busy.value) return
  if (selectedID.value !== '') returnTo.value = selectedID.value
  composing.value = true
  selectedID.value = ''
  await nextTick()
  createHeading.value?.focus()
}

// The model changes this array only after a confirmed write or registry read.
// Selection follows a confirmed removal even while the follow-up GET runs;
// a failed read on its own changes neither the selection nor an unrelated draft.
watch(devices.devices, () => {
  if (selectedID.value === '' || selectedDevice.value !== null) return
  selectedID.value = devices.devices.value[0]?.id ?? ''
  void focusSelection()
})

const selectedRequirements = computed(
  () =>
    devices.deployableTargets.value.find(
      (target) => target.targetID === targetID.value,
    )?.requirements ?? null,
)
const { problems: problemsOf } = useConnectionFields(
  selectedRequirements,
  targetID,
)

// The name is the operator's own words and survives a change of target; what
// the previous target's deployer asked for does not.
watch(targetID, () => {
  draft.value = { ...draft.value, account: '', address: '', interfaceName: '' }
  reveal.value = false
  refusedAddress.value = false
})
// A refusal was about the address as it was typed; a new address is a new
// question.
watch(
  () => draft.value.address,
  () => {
    refusedAddress.value = false
  },
)
// The words under the failure notice, or null when the refusal is stated at its
// own field instead.
const workBodyKey = computed(() => failureBodyKey(devices.failure.value))

watch(selectedID, () => {
  confirmation.value = ''
  if (!devices.busy.value) devices.work.value = 'idle'
})
watch(creating, (open) => {
  if (!open) return
  confirmation.value = ''
  if (!devices.busy.value) devices.work.value = 'idle'
})

const trimmed = computed(() => ({
  account: draft.value.account.trim(),
  address: draft.value.address.trim(),
  interfaceName: draft.value.interfaceName.trim(),
  name: draft.value.name.trim(),
}))

const problems = computed(() => problemsOf(trimmed.value))

// Submitting stays possible while the draft is incomplete: what is wrong is
// stated beside the fields on submit, where a disabled button would only be
// silent. What can never be submitted is a form with no target to describe or
// no answer yet to what that target's deployer needs.
const canSubmit = computed(
  () =>
    !devices.busy.value &&
    devices.state.value !== 'stale' &&
    devices.requirementsState.value === 'ready' &&
    targetID.value !== '',
)

const addDisabled = computed(
  () =>
    creating.value ||
    devices.state.value === 'loading' ||
    devices.state.value === 'failed' ||
    devices.busy.value ||
    !devices.catalogAvailable.value,
)

// A named connection that is not registered — removed since the link was made —
// falls back to the default, and the keyboard goes only where the link pointed.
// The first successful read of the registry applies this rule, whether it is
// the one made on arrival or the one a retry makes after that failed.
const selectInitial = async (): Promise<void> => {
  const requested = props.initialConnection ?? ''
  const named = devices.devices.value.some((device) => device.id === requested)
  selectedID.value = named ? requested : (devices.devices.value[0]?.id ?? '')
  if (named) await focusSelection()
}

onMounted(async () => {
  await devices.initialize()
  await selectInitial()
})

const retryRead = async (): Promise<void> => {
  await devices.initialize()
  if (devices.state.value !== 'failed' && selectedID.value === '')
    await selectInitial()
}

const clearDraft = (): void => {
  targetID.value = ''
  draft.value = { account: '', address: '', interfaceName: '', name: '' }
  reveal.value = false
  refusedAddress.value = false
}

// Cancel is the form's own exit: it discards the draft and puts back whatever
// the form took the work area from — the connection it replaced, the first
// saved one, or the empty registry's own statement. Choosing a connection from
// the list is a different act and keeps the draft for the next visit.
const cancelCreation = async (): Promise<void> => {
  if (devices.busy.value) return
  clearDraft()
  const known = devices.devices.value.some(
    (device) => device.id === returnTo.value,
  )
  await openConnection(
    known ? returnTo.value : (devices.devices.value[0]?.id ?? ''),
  )
}

const submit = async (): Promise<void> => {
  if (!canSubmit.value) return
  if (Object.keys(problems.value).length > 0) {
    reveal.value = true
    await nextTick()
    fields.value?.focusFirstInvalid()
    return
  }
  const done = await devices.register(
    targetID.value,
    trimmed.value.name,
    trimmed.value.address,
    trimmed.value.account,
    trimmed.value.interfaceName,
  )
  if (!done) {
    if (addressRefused(devices.failure.value)) {
      refusedAddress.value = true
      await nextTick()
      fields.value?.focus('address')
    }
    return
  }
  const requirements = selectedRequirements.value
  // A connection whose deployer is unknown, or that needs a password this
  // system has nowhere to keep, is delivered to by hand. Otherwise what remains
  // before unattended delivery depends on whether a password is part of it.
  let messageKey = 'devices.registered.free'
  if (
    requirements === null ||
    (requirements.needsCredential && !devices.secretStoreAvailable.value)
  )
    messageKey = 'devices.registered.manual'
  else if (requirements.needsCredential)
    messageKey = 'devices.registered.credential'
  await openConnection(devices.registeredID.value)
  confirmation.value = t(messageKey)
  clearDraft()
}
</script>

<template>
  <section aria-labelledby="devices-title" class="devices">
    <RvPageHeader title-id="devices-title" :title="t('connections.title')" />

    <RvStateNotice
      v-if="devices.state.value === 'loading'"
      live
      :title="t('devices.loading')"
      tone="busy"
    />
    <RvStateNotice
      v-else-if="devices.state.value === 'failed'"
      :body="t(`failure.${devices.readFailure.value}.body`)"
      live
      :title="t('devices.failed')"
      tone="failed"
    >
      <template #action>
        <RvButton @click="retryRead">{{ t('action.retry') }}</RvButton>
      </template>
    </RvStateNotice>

    <template v-else>
      <RvStateNotice
        v-if="confirmation !== '' && selectedDevice === null"
        :title="confirmation"
        tone="ready"
      />
      <RvStateNotice
        v-if="!devices.catalogAvailable.value"
        :body="t('devices.catalog.failed.body')"
        :title="t('devices.catalog.failed')"
        tone="warning"
      >
        <template #action>
          <RvButton @click="devices.initialize">{{
            t('action.retry')
          }}</RvButton>
        </template>
      </RvStateNotice>

      <RvStateNotice
        v-if="devices.state.value === 'stale'"
        :body="t('devices.stale.body')"
        live
        :title="t('devices.stale')"
        tone="warning"
      >
        <template #action>
          <RvButton @click="devices.retryDevices">{{
            t('action.retry')
          }}</RvButton>
        </template>
      </RvStateNotice>

      <section
        v-if="empty"
        :aria-labelledby="emptyHeadingID"
        class="devices__nothing"
      >
        <h2
          :id="emptyHeadingID"
          ref="emptyHeading"
          class="devices__title"
          tabindex="-1"
        >
          {{ t('devices.empty') }}
        </h2>
        <p class="devices__lead">{{ t('devices.empty.body') }}</p>
        <div>
          <RvButton
            :disabled="addDisabled"
            variant="primary"
            @click="startComposing"
          >
            <RvIcon name="plus" />{{ t('devices.add') }}
          </RvButton>
        </div>
      </section>

      <div
        v-else
        class="devices__workspace"
        :class="{
          'devices__workspace--editing': devices.devices.value.length > 0,
        }"
      >
        <ConnectionList
          v-if="devices.devices.value.length > 0"
          :add-disabled="addDisabled"
          :busy="devices.busy.value"
          :connections="devices.devices.value"
          :selected-id="selectedID"
          :title-of="titleOf"
          @add="startComposing"
          @select="openConnection"
        />

        <ConnectionDetail
          v-if="selectedDevice"
          :key="selectedDevice.id"
          ref="detail"
          v-model:confirmation="confirmation"
          :connection="selectedDevice"
          :devices="devices"
          :target-title="titleOf(selectedDevice)"
        />

        <section
          v-else
          :aria-labelledby="createHeadingID"
          class="devices__editor"
          :class="{
            'devices__editor--standalone': devices.devices.value.length === 0,
          }"
        >
          <h2
            :id="createHeadingID"
            ref="createHeading"
            class="devices__title"
            tabindex="-1"
          >
            {{ t('devices.add') }}
          </h2>

          <RvStateNotice
            v-if="devices.requirementsState.value !== 'ready'"
            :body="
              devices.requirementsState.value === 'loading'
                ? undefined
                : t('devices.requirements.failed.body')
            "
            :title="
              t(
                devices.requirementsState.value === 'loading'
                  ? 'devices.requirements.reading'
                  : 'devices.requirements.failed',
              )
            "
            :tone="
              devices.requirementsState.value === 'loading' ? 'busy' : 'warning'
            "
          >
            <template #action>
              <RvButton
                :disabled="devices.requirementsState.value === 'loading'"
                @click="devices.retryRequirements"
                >{{ t('action.retry') }}</RvButton
              >
            </template>
          </RvStateNotice>

          <form
            v-if="devices.catalogAvailable.value"
            id="device-create"
            class="devices__form"
            novalidate
            @submit.prevent="submit"
          >
            <RvField
              class="devices__target-field"
              input-id="device-target"
              :label="t('devices.field.target')"
            >
              <template #default="{ describedBy, invalid }">
                <RvSelect
                  v-model="targetID"
                  :described-by="describedBy"
                  :disabled="
                    devices.busy.value || devices.state.value === 'stale'
                  "
                  input-id="device-target"
                  :invalid="invalid"
                  :options="targetChoices"
                  :placeholder="t('devices.field.target.pick')"
                  searchable
                />
              </template>
            </RvField>

            <ConnectionFields
              v-if="targetID !== ''"
              ref="fields"
              v-model:account="draft.account"
              v-model:address="draft.address"
              v-model:interface-name="draft.interfaceName"
              v-model:name="draft.name"
              :disabled="devices.busy.value || devices.state.value === 'stale'"
              id-prefix="device-create"
              :problems="problems"
              :refused-address="refusedAddress"
              :requirements="selectedRequirements"
              :reveal="reveal"
              :target-id="targetID"
            />
          </form>

          <RvStateNotice
            v-if="devices.work.value === 'failed' && workBodyKey !== null"
            :body="t(workBodyKey)"
            live
            :title="t('devices.work.failed')"
            tone="failed"
          />
          <div class="devices__actions">
            <RvButton :disabled="devices.busy.value" @click="cancelCreation">{{
              t('action.cancel')
            }}</RvButton>
            <!-- Pressing the button must not take focus from the field being
                 left: leaving it states its problem, the line that appears
                 moves this button, and the click would then land beside it. -->
            <RvButton
              :disabled="!canSubmit"
              form="device-create"
              type="submit"
              variant="primary"
              @mousedown.prevent
              >{{ t('action.save') }}</RvButton
            >
          </div>
        </section>
      </div>

      <!-- What this build can talk to at all is reference material behind one
           click, read from the same catalog and requirements this screen
           already holds: a second read of either could disagree with the
           first. -->
      <FormatReference
        v-if="
          devices.catalogAvailable.value && devices.targets.value.length > 0
        "
        :deliverable-ids="
          devices.requirementsState.value === 'ready'
            ? new Set(
                devices.deployableTargets.value.map((entry) => entry.targetID),
              )
            : null
        "
        :failure="devices.requirementsFailure.value"
        :targets="devices.targets.value"
        @retry="devices.retryRequirements"
      />
    </template>
  </section>
</template>

<style scoped>
.devices {
  display: grid;
  gap: var(--rv-space-6);
  width: 100%;
  min-width: 0;
  container: connections / inline-size;
}

/* Each column stands at its own height. Stretching them to a common one made
   the registered list borrow its height from whatever was open beside it, so
   asking to register a connection — a form shorter than a connection's
   parameters — pulled the list of connections down with it, and the operator
   watched the thing they were reading shrink for pressing "add" next to it. */
.devices__workspace {
  display: grid;
  gap: var(--rv-space-6);
  align-items: start;
  min-width: 0;
  width: 100%;
}

.devices__workspace--editing {
  grid-template-columns: minmax(0, 1fr) minmax(0, 2fr);
}

.devices__editor {
  display: grid;
  gap: var(--rv-space-6);
  align-content: start;
  min-width: 0;
  width: 100%;
  min-height: var(--rv-connections-compose-height);
  padding: var(--rv-space-6);
  background: var(--rv-color-surface);
  border: var(--rv-border-hair) solid var(--rv-color-rule);
  border-radius: var(--rv-radius-lg);
}

.devices__nothing {
  display: grid;
  gap: var(--rv-space-4);
  justify-items: start;
  width: 100%;
  min-width: 0;
  padding: var(--rv-space-8) var(--rv-space-6);
  background: var(--rv-color-surface);
  border: var(--rv-border-hair) solid var(--rv-color-rule);
  border-radius: var(--rv-radius-lg);
}

/* An empty registry and the form that fills it are one surface that changes
   what it says, not two panels of different sizes taking each other's place:
   the operator pressed "add", and a box that jumps from the page's full width
   to a third of it reads as having been replaced rather than answered. The
   floor is a floor — enlarged text or a longer language grows past it. */
.devices__nothing,
.devices__editor--standalone {
  width: min(100%, var(--rv-measure-form));
  min-height: var(--rv-connections-compose-height);
  align-content: start;
}

.devices__lead {
  max-width: var(--rv-measure-prose);
  color: var(--rv-color-ink-muted);
}

.devices__title {
  font-size: var(--rv-text-module);
  overflow-wrap: anywhere;
}

.devices__form {
  display: grid;
  gap: var(--rv-space-5);
  min-width: 0;
}

.devices__target-field {
  width: min(100%, var(--rv-measure-field));
}

.devices__actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-3);
  justify-content: flex-end;
}

@container connections (width < 48rem) {
  .devices__workspace--editing {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
