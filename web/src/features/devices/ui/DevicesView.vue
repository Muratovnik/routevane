<script setup lang="ts">
import { targetIcon } from '@/shared/lib/targetIcon'
import { computed, nextTick, onMounted, ref, watch } from 'vue'

import { useDevices } from '@/features/devices/model/useDevices'
import { localizedTargetTitle } from '@/shared/api/catalog'
import { useLocale } from '@/shared/i18n/useLocale'
import type { ChoiceOption } from '@/shared/ui/types'
import RvButton from '@/shared/ui/RvButton.vue'
import RvIcon from '@/shared/ui/RvIcon.vue'
import RvPageHeader from '@/shared/ui/RvPageHeader.vue'
import RvField from '@/shared/ui/RvField.vue'
import RvSelect from '@/shared/ui/RvSelect.vue'
import RvStateNotice from '@/shared/ui/RvStateNotice.vue'
import RvTextInput from '@/shared/ui/RvTextInput.vue'

const { locale, t, tor } = useLocale()
const devices = useDevices()

// A registered device carries the title it was registered under; the catalog
// is what can say that title in the reader's language today.
const targetTitle = (targetID: string, fallback: string): string =>
  localizedTargetTitle(
    devices.targets.value.find((target) => target.id === targetID),
    locale.value,
    fallback,
  )

const targetID = ref('')
const targetChoices = computed<ChoiceOption[]>(() =>
  devices.targets.value.map((target) => ({
    label: targetTitle(target.id, target.title),
    value: target.id,
    icon: targetIcon(target.id),
  })),
)
const name = ref('')
const address = ref('')
const account = ref('')
const interfaceName = ref('')
const confirmation = ref('')
const selectedID = ref('')
const editorHeading = ref<HTMLElement | null>(null)

const openEditor = async (id = ''): Promise<void> => {
  if (devices.busy.value) return
  credential.value = ''
  selectedID.value = id
  await nextTick()
  editorHeading.value?.focus()
}

const selectedDevice = computed(
  () =>
    devices.devices.value.find((device) => device.id === selectedID.value) ??
    null,
)
const creating = computed(() => selectedDevice.value === null)
// Reconcile only an authoritative registry. A failed read keeps the selection
// and draft in place until the GET-only recovery has answered.
watch([devices.state, devices.busy, devices.devices], () => {
  if (devices.state.value !== 'ready' || devices.busy.value) return
  if (selectedID.value !== '' && selectedDevice.value === null)
    void openEditor(devices.devices.value[0]?.id ?? '')
})
const nameTouched = ref(false)
const addressTouched = ref(false)
const accountTouched = ref(false)
const interfaceTouched = ref(false)

const selectedRequirements = computed(
  () =>
    devices.deployableTargets.value.find(
      (target) => target.targetID === targetID.value,
    )?.requirements ?? null,
)
// The address field means a different thing per deployer — a router URL for
// Keenetic, a configuration path for sing-box — so the dictionary names it per
// target and follows the chosen language. A plugin target the dictionary does
// not know keeps its own caption rather than getting a wrong generic one.
const addressLabel = computed(() =>
  tor(
    `deploy.field.address.${targetID.value}`,
    selectedRequirements.value?.addressLabel ?? t('devices.field.address'),
  ),
)
const addressPlaceholder = computed(
  () => selectedRequirements.value?.addressExample ?? '',
)
const showAccount = computed(
  () =>
    targetID.value !== '' &&
    selectedRequirements.value?.needsCredential === true,
)
const showInterface = computed(
  () => selectedRequirements.value?.needsInterface === true,
)
const interfaceLabel = computed(() =>
  tor(
    `deploy.field.interface.${targetID.value}`,
    selectedRequirements.value?.interfaceLabel ?? t('devices.field.interface'),
  ),
)
const accountLabel = computed(() =>
  tor(`deploy.field.account.${targetID.value}`, t('devices.field.account')),
)
const interfaceHint = computed(() => {
  const key = `deploy.field.interface.${targetID.value}.hint`
  const value = tor(key, '')
  return value === key ? '' : value
})

const needsCredential = (device: { targetID: string }): boolean =>
  devices.deployableTargets.value.find(
    (target) => target.targetID === device.targetID,
  )?.requirements.needsCredential === true

const hasKnownRequirements = (targetID: string): boolean =>
  devices.requirementsState.value === 'ready' &&
  devices.deployableTargets.value.some((target) => target.targetID === targetID)

watch(targetID, () => {
  address.value = ''
  account.value = ''
  interfaceName.value = ''
  nameTouched.value = false
  addressTouched.value = false
  accountTouched.value = false
  interfaceTouched.value = false
})

// The credential is held in this field for exactly as long as it takes to send
// it, and cleared the moment it leaves. Nothing in this tab keeps it.
const credential = ref('')
watch(selectedID, () => {
  credential.value = ''
  confirmation.value = ''
  if (!devices.busy.value) devices.work.value = 'idle'
})
watch(creating, (open) => {
  if (!open) return
  confirmation.value = ''
  if (!devices.busy.value) devices.work.value = 'idle'
})

const canRegister = computed(
  () =>
    !devices.busy.value &&
    devices.state.value !== 'stale' &&
    devices.requirementsState.value === 'ready' &&
    targetID.value !== '' &&
    name.value.trim() !== '' &&
    address.value.trim() !== '' &&
    (!showAccount.value || account.value.trim() !== '') &&
    (!showInterface.value || interfaceName.value.trim() !== ''),
)

const fieldError = (
  value: string,
  required: boolean,
  touched: boolean,
): string | undefined =>
  required && touched && value.trim() === ''
    ? t('devices.validation.required')
    : undefined

onMounted(async () => {
  await devices.initialize()
  selectedID.value = devices.devices.value[0]?.id ?? ''
})

const clearDraft = (): void => {
  targetID.value = ''
  name.value = ''
  address.value = ''
  account.value = ''
  interfaceName.value = ''
  nameTouched.value = false
  addressTouched.value = false
  accountTouched.value = false
  interfaceTouched.value = false
}

const submit = async (): Promise<void> => {
  if (!canRegister.value) {
    nameTouched.value = true
    addressTouched.value = true
    accountTouched.value = true
    interfaceTouched.value = true
    return
  }
  const done = await devices.register(
    targetID.value,
    name.value.trim(),
    address.value.trim(),
    account.value.trim(),
    interfaceName.value.trim(),
  )
  if (done) {
    const target = devices.targets.value.find(
      (candidate) => candidate.id === targetID.value,
    )
    const manualOnly =
      selectedRequirements.value === null ||
      (selectedRequirements.value.needsCredential &&
        !devices.secretStoreAvailable.value)
    let messageKey =
      target?.kind === 'router'
        ? 'devices.registered.router'
        : 'devices.registered.other'
    if (manualOnly) messageKey = 'devices.registered.manual'
    await openEditor(devices.registeredID.value)
    confirmation.value = t(messageKey)
    clearDraft()
  }
}

const onEnable = async (id: string): Promise<void> => {
  const device = devices.devices.value.find((candidate) => candidate.id === id)
  if (device === undefined) return
  const submittedCredential = credential.value
  if (needsCredential(device) && submittedCredential === '') return
  credential.value = ''
  const done = await devices.enable(id, submittedCredential)
  if (!done) return
  const target = devices.targets.value.find(
    (candidate) => candidate.id === device.targetID,
  )
  confirmation.value = t(
    target?.kind === 'router'
      ? 'devices.auto.enabled.next.router'
      : 'devices.auto.enabled.next.other',
  )
}

const onDisable = async (id: string): Promise<void> => {
  confirmation.value = ''
  await devices.disable(id)
}

const onForget = async (id: string): Promise<void> => {
  confirmation.value = ''
  await devices.forget(id)
}
</script>

<template>
  <section aria-labelledby="devices-title" class="devices">
    <RvPageHeader title-id="devices-title" :title="t('connections.title')">
      <RvButton
        v-show="!creating"
        :disabled="
          devices.state.value === 'loading' ||
          devices.state.value === 'failed' ||
          devices.busy.value ||
          !devices.catalogAvailable.value
        "
        variant="primary"
        @click="openEditor()"
      >
        <RvIcon name="plus" />{{ t('devices.add') }}
      </RvButton>
    </RvPageHeader>

    <RvStateNotice
      v-if="devices.state.value === 'loading'"
      live
      :title="t('devices.loading')"
      tone="busy"
    />
    <RvStateNotice
      v-else-if="devices.state.value === 'failed'"
      :body="t('devices.failed.body')"
      live
      :title="t('devices.failed')"
      tone="failed"
    >
      <template #action>
        <RvButton @click="devices.initialize">{{ t('action.retry') }}</RvButton>
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
      <div
        class="devices__workspace"
        :class="{
          'devices__workspace--editing': devices.devices.value.length > 0,
        }"
      >
        <ul v-if="devices.devices.value.length > 0" class="devices__list">
          <li
            v-for="device in devices.devices.value"
            :key="device.id"
            class="devices__connection"
          >
            <button
              type="button"
              class="devices__choose"
              :class="{ 'devices__choose--selected': device.id === selectedID }"
              :aria-label="t('devices.configure.aria', { name: device.name })"
              :aria-pressed="device.id === selectedID"
              :disabled="devices.busy.value"
              @click="openEditor(device.id)"
            >
              <RvIcon :name="targetIcon(device.targetID)" />
              <span class="devices__identity">
                <strong class="devices__name">{{ device.name }}</strong>
                <span class="devices__meta">
                  {{ targetTitle(device.targetID, device.targetTitle) }}
                </span>
                <span class="devices__meta devices__mono">{{
                  device.address
                }}</span>
                <span class="devices__delivery">
                  {{
                    t(
                      !device.deployable
                        ? 'devices.manualOnly'
                        : device.autoDeliver
                          ? 'devices.auto.on.noCredential'
                          : 'devices.auto.off',
                    )
                  }}
                </span>
              </span>
            </button>
          </li>
        </ul>

        <section class="devices__editor" aria-labelledby="device-editor-title">
          <div class="devices__editor-header">
            <h2
              id="device-editor-title"
              ref="editorHeading"
              tabindex="-1"
              class="devices__section-title"
            >
              {{ creating ? t('devices.add') : selectedDevice?.name }}
            </h2>
          </div>
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
          <div v-if="selectedDevice" class="devices__details">
            <RvStateNotice
              v-if="confirmation !== ''"
              :title="confirmation"
              live
              tone="ready"
            />
            <dl class="devices__facts">
              <div>
                <dt>{{ t('devices.field.target') }}</dt>
                <dd>
                  {{
                    targetTitle(
                      selectedDevice.targetID,
                      selectedDevice.targetTitle,
                    )
                  }}
                </dd>
              </div>
              <div>
                <dt>{{ t('devices.field.address') }}</dt>
                <dd class="devices__mono">{{ selectedDevice.address }}</dd>
              </div>
              <div v-if="selectedDevice.account">
                <dt>{{ t('devices.field.account') }}</dt>
                <dd>{{ selectedDevice.account }}</dd>
              </div>
              <div v-if="selectedDevice.interfaceName">
                <dt>{{ t('devices.field.interface') }}</dt>
                <dd class="devices__mono">
                  {{ selectedDevice.interfaceName }}
                </dd>
              </div>
            </dl>
            <section
              v-if="
                selectedDevice.autoDeliver ||
                devices.requirementsState.value === 'ready'
              "
              class="devices__automation"
              :aria-label="t('devices.auto.title')"
            >
              <h3 class="devices__section-title">
                {{ t('devices.auto.title') }}
              </h3>
              <template v-if="selectedDevice.autoDeliver">
                <p class="devices__meta">
                  {{
                    t(
                      hasKnownRequirements(selectedDevice.targetID) &&
                        needsCredential(selectedDevice)
                        ? 'devices.auto.on'
                        : 'devices.auto.on.noCredential',
                    )
                  }}
                </p>
                <RvButton
                  :disabled="
                    devices.busy.value || devices.state.value === 'stale'
                  "
                  @click="onDisable(selectedDevice.id)"
                  >{{ t('devices.auto.disable') }}</RvButton
                >
              </template>
              <form
                v-else-if="
                  selectedDevice.deployable &&
                  hasKnownRequirements(selectedDevice.targetID) &&
                  (devices.secretStoreAvailable.value ||
                    !needsCredential(selectedDevice))
                "
                class="devices__automation"
                @submit.prevent="onEnable(selectedDevice.id)"
              >
                <p class="devices__meta">
                  {{
                    t(
                      needsCredential(selectedDevice)
                        ? 'devices.auto.consent'
                        : 'devices.auto.consent.noCredential',
                    )
                  }}
                </p>
                <RvField
                  v-if="needsCredential(selectedDevice)"
                  :label="t('devices.auto.label')"
                  input-id="device-credential"
                >
                  <RvTextInput
                    v-model="credential"
                    input-id="device-credential"
                    type="password"
                    autocomplete="off"
                    :disabled="
                      devices.busy.value || devices.state.value === 'stale'
                    "
                  />
                </RvField>
                <RvButton
                  type="submit"
                  :disabled="
                    devices.busy.value ||
                    devices.state.value === 'stale' ||
                    (needsCredential(selectedDevice) && credential === '')
                  "
                  >{{ t('devices.auto.enable') }}</RvButton
                >
              </form>
              <p v-else class="devices__meta">
                {{
                  t(
                    !selectedDevice.deployable
                      ? 'devices.manualOnly'
                      : 'devices.auto.unavailable',
                  )
                }}
              </p>
            </section>
            <RvStateNotice
              v-if="devices.work.value === 'failed'"
              :body="t('devices.work.failed.body')"
              live
              :title="t('devices.work.failed')"
              tone="failed"
            />
            <RvStateNotice
              v-if="devices.state.value === 'stale'"
              :body="t('devices.stale.body')"
              live
              :title="t('devices.stale')"
              tone="warning"
            >
              <template #action
                ><RvButton @click="devices.retryDevices">{{
                  t('action.retry')
                }}</RvButton></template
              >
            </RvStateNotice>
            <RvButton
              :disabled="devices.busy.value || devices.state.value === 'stale'"
              variant="quiet"
              @click="onForget(selectedDevice.id)"
              >{{ t('devices.forget') }}</RvButton
            >
          </div>
          <template v-if="creating">
            <form
              v-if="devices.catalogAvailable.value"
              id="device-create"
              class="devices__form"
              @submit.prevent="submit"
            >
              <RvField
                class="devices__target-field"
                input-id="device-target"
                :label="t('devices.field.target')"
              >
                <template #default="{ describedBy, invalid }">
                  <div class="devices__field">
                    <RvSelect
                      v-model="targetID"
                      :described-by="describedBy"
                      :disabled="
                        devices.busy.value || devices.state.value === 'stale'
                      "
                      searchable
                      input-id="device-target"
                      :invalid="invalid"
                      :options="targetChoices"
                      :placeholder="t('devices.field.target.pick')"
                    />
                  </div>
                </template>
              </RvField>

              <RvField
                v-if="targetID !== ''"
                :error="fieldError(name, true, nameTouched)"
                input-id="device-name"
                :label="t('devices.field.name')"
              >
                <template #default="{ describedBy, invalid }">
                  <RvTextInput
                    v-model="name"
                    :described-by="describedBy"
                    :disabled="
                      devices.busy.value || devices.state.value === 'stale'
                    "
                    input-id="device-name"
                    :invalid="invalid"
                    maxlength="120"
                    @blur="nameTouched = true"
                  />
                </template>
              </RvField>

              <RvField
                v-if="targetID !== ''"
                :error="fieldError(address, true, addressTouched)"
                input-id="device-address"
                :label="addressLabel"
              >
                <template #default="{ describedBy, invalid }">
                  <RvTextInput
                    v-model="address"
                    :described-by="describedBy"
                    :disabled="
                      devices.busy.value || devices.state.value === 'stale'
                    "
                    input-id="device-address"
                    :invalid="invalid"
                    maxlength="512"
                    :placeholder="addressPlaceholder"
                    @blur="addressTouched = true"
                  />
                </template>
              </RvField>

              <RvField
                v-if="showAccount"
                :error="fieldError(account, true, accountTouched)"
                input-id="device-account"
                :label="accountLabel"
              >
                <template #default="{ describedBy, invalid }">
                  <RvTextInput
                    v-model="account"
                    :described-by="describedBy"
                    :disabled="
                      devices.busy.value || devices.state.value === 'stale'
                    "
                    input-id="device-account"
                    :invalid="invalid"
                    maxlength="120"
                    @blur="accountTouched = true"
                  />
                </template>
              </RvField>

              <RvField
                v-if="showInterface"
                :error="fieldError(interfaceName, true, interfaceTouched)"
                :hint="interfaceHint"
                input-id="device-interface"
                :label="interfaceLabel"
              >
                <template #default="{ describedBy, invalid }">
                  <RvTextInput
                    v-model="interfaceName"
                    :described-by="describedBy"
                    :disabled="
                      devices.busy.value || devices.state.value === 'stale'
                    "
                    input-id="device-interface"
                    :invalid="invalid"
                    maxlength="120"
                    @blur="interfaceTouched = true"
                  />
                </template>
              </RvField>
            </form>

            <RvStateNotice
              v-if="devices.work.value === 'failed'"
              :body="t('devices.work.failed.body')"
              live
              :title="t('devices.work.failed')"
              tone="failed"
            />
            <RvStateNotice
              v-if="devices.state.value === 'stale'"
              :body="t('devices.stale.body')"
              live
              :title="t('devices.stale')"
              tone="warning"
            >
              <template #action
                ><RvButton @click="devices.retryDevices">{{
                  t('action.retry')
                }}</RvButton></template
              >
            </RvStateNotice>
            <div class="devices__actions">
              <RvButton
                :disabled="
                  devices.busy.value || devices.state.value === 'stale'
                "
                @click="clearDraft"
                >{{ t('devices.clear') }}</RvButton
              >
              <RvButton
                :disabled="!canRegister"
                form="device-create"
                type="submit"
                variant="primary"
                >{{ t('devices.add.submit') }}</RvButton
              >
            </div>
          </template>
        </section>
      </div>
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

.devices__list {
  display: grid;
  min-width: 0;
  list-style: none;
  align-content: start;
  gap: var(--rv-space-2);
}

.devices__workspace {
  display: grid;
  gap: var(--rv-space-8);
  align-items: start;
  min-width: 0;
  width: 100%;
}

.devices__workspace--editing {
  grid-template-columns: minmax(0, 1fr) minmax(0, 2fr);
}

.devices__choose {
  display: flex;
  align-items: flex-start;
  gap: var(--rv-space-4);
  width: 100%;
  padding: var(--rv-space-4);
  font: inherit;
  text-align: start;
  color: var(--rv-color-ink);
  background: var(--rv-color-surface);
  border: var(--rv-border-hair) solid transparent;
  border-radius: var(--rv-radius-lg);
  cursor: pointer;
}

.devices__choose:hover {
  background: var(--rv-color-surface-hover);
}

.devices__choose--selected {
  border-color: var(--rv-color-accent);
  background: var(--rv-color-surface-selected);
}

.devices__choose:disabled {
  cursor: not-allowed;
}

.devices__editor {
  display: grid;
  gap: var(--rv-space-6);
  min-width: 0;
  width: 100%;
  border: var(--rv-border-hair) solid var(--rv-color-rule);
  padding: var(--rv-space-6);
  background: var(--rv-color-surface);
  border-radius: var(--rv-radius-lg);
}

.devices__editor-header,
.devices__actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: var(--rv-space-3);
}

.devices__actions {
  justify-content: flex-end;
}

.devices__identity {
  flex: 1;
}

.devices__identity,
.devices__automation {
  display: grid;
  gap: var(--rv-space-2);
  min-width: 0;
  justify-items: start;
}

.devices__name,
.devices__section-title {
  font-size: var(--rv-text-section);
  overflow-wrap: anywhere;
}

.devices__meta {
  min-width: 0;
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
  overflow-wrap: anywhere;
}

.devices__delivery {
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
}

.devices__mono {
  font-family: var(--rv-font-mono);
}

.devices__details {
  display: grid;
  gap: var(--rv-space-6);
}

.devices__facts {
  display: grid;
  gap: var(--rv-space-3);
}

.devices__facts > div {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 2fr);
  gap: var(--rv-space-4);
}

.devices__facts dt {
  color: var(--rv-color-ink-muted);
}

.devices__facts dd {
  min-width: 0;
  margin: 0;
  overflow-wrap: anywhere;
}

.devices__automation {
  gap: var(--rv-space-4);
}

.devices__form {
  display: grid;
  grid-template-columns: repeat(
    auto-fit,
    minmax(min(100%, var(--rv-measure-field)), 1fr)
  );
  gap: var(--rv-space-5);
  min-width: 0;
}

.devices__target-field {
  grid-column: 1 / -1;
  width: min(100%, var(--rv-measure-field));
}

.devices__field {
  width: 100%;
  min-width: 0;
}

@container connections (width < 48rem) {
  .devices__workspace--editing {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
