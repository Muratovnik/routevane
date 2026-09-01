<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'

import { useDevices } from '@/features/devices/model/useDevices'
import { localizedTargetTitle } from '@/shared/api/catalog'
import { useLocale } from '@/shared/i18n/useLocale'
import type { ChoiceOption } from '@/shared/ui/kinds'
import RvButton from '@/shared/ui/RvButton.vue'
import RvSelect from '@/shared/ui/RvSelect.vue'
import RvStateNotice from '@/shared/ui/RvStateNotice.vue'

const { locale, t, tor } = useLocale()
const devices = useDevices()

// A registered device carries the title it was registered under; the catalog
// is what can say that title in the reader's language today.
function targetTitle(targetID: string, fallback: string): string {
  return localizedTargetTitle(
    devices.targets.value.find((target) => target.id === targetID),
    locale.value,
    fallback,
  )
}

const targetID = ref('')
const targetChoices = computed<ChoiceOption[]>(() =>
  devices.targets.value.map((target) => ({
    label: targetTitle(target.id, target.title),
    value: target.id,
  })),
)
const name = ref('')
const address = ref('')
const account = ref('')
const interfaceName = ref('')

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

function needsCredential(device: { targetID: string }): boolean {
  return (
    devices.deployableTargets.value.find(
      (target) => target.targetID === device.targetID,
    )?.requirements.needsCredential === true
  )
}

watch(targetID, () => {
  address.value = ''
  account.value = ''
  interfaceName.value = ''
})

// The credential is held in this field for exactly as long as it takes to send
// it, and cleared the moment it leaves. Nothing in this tab keeps it.
const credentials = ref<Record<string, string>>({})

const canRegister = computed(
  () =>
    !devices.busy.value &&
    targetID.value !== '' &&
    name.value.trim() !== '' &&
    address.value.trim() !== '' &&
    (!showInterface.value || interfaceName.value.trim() !== ''),
)

onMounted(() => {
  void devices.initialize()
})

async function submit(): Promise<void> {
  if (!canRegister.value) return
  const done = await devices.register(
    targetID.value,
    name.value.trim(),
    address.value.trim(),
    account.value.trim(),
    interfaceName.value.trim(),
  )
  if (done) {
    name.value = ''
    address.value = ''
    account.value = ''
    interfaceName.value = ''
  }
}

async function onEnable(id: string): Promise<void> {
  const device = devices.devices.value.find((candidate) => candidate.id === id)
  if (device === undefined) return
  const credential = credentials.value[id] ?? ''
  if (needsCredential(device) && credential === '') return
  const done = await devices.enable(id, credential)
  credentials.value = { ...credentials.value, [id]: '' }
  if (!done) return
}
</script>

<template>
  <section aria-labelledby="devices-title" class="devices">
    <h1 id="devices-title" class="devices__title">
      {{ t('connections.title') }}
    </h1>

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
        v-else-if="!devices.deploymentCatalogAvailable.value"
        :body="t('devices.requirements.failed.body')"
        :title="t('devices.requirements.failed')"
        tone="warning"
      >
        <template #action>
          <RvButton @click="devices.initialize">{{
            t('action.retry')
          }}</RvButton>
        </template>
      </RvStateNotice>
      <RvStateNotice
        v-if="devices.devices.value.length === 0"
        :body="t('devices.empty.body')"
        :title="t('devices.empty')"
        tone="waiting"
      />
      <ul v-else class="devices__list">
        <li v-for="device in devices.devices.value" :key="device.id">
          <article class="devices__card">
            <h2 class="devices__name">{{ device.name }}</h2>
            <p class="devices__meta">
              {{ targetTitle(device.targetID, device.targetTitle) }} ·
              <span class="devices__mono">{{ device.address }}</span>
              <template v-if="device.account !== ''">
                · {{ device.account }}
              </template>
              <template v-if="device.interfaceName !== ''">
                · <span class="devices__mono">{{ device.interfaceName }}</span>
              </template>
            </p>
            <p v-if="!device.deployable" class="devices__meta">
              {{ t('devices.manualOnly') }}
            </p>

            <div v-if="device.autoDeliver" class="devices__row">
              <p class="devices__meta">
                {{
                  t(
                    needsCredential(device)
                      ? 'devices.auto.on'
                      : 'devices.auto.on.noCredential',
                  )
                }}
              </p>
              <RvButton
                :disabled="devices.busy.value"
                variant="quiet"
                @click="devices.disable(device.id)"
              >
                {{ t('devices.auto.disable') }}
              </RvButton>
            </div>
            <div
              v-else-if="
                device.deployable &&
                (devices.secretStoreAvailable.value || !needsCredential(device))
              "
              class="devices__row"
            >
              <label
                v-if="needsCredential(device)"
                class="devices__field-label"
                :for="`device-credential-${device.id}`"
              >
                {{ t('devices.auto.label') }}
              </label>
              <input
                v-if="needsCredential(device)"
                :id="`device-credential-${device.id}`"
                autocomplete="off"
                class="devices__input"
                :disabled="devices.busy.value"
                type="password"
                :value="credentials[device.id] ?? ''"
                @input="
                  credentials = {
                    ...credentials,
                    [device.id]: ($event.target as HTMLInputElement).value,
                  }
                "
              />
              <RvButton
                :disabled="
                  devices.busy.value ||
                  (needsCredential(device) &&
                    (credentials[device.id] ?? '') === '')
                "
                @click="onEnable(device.id)"
              >
                {{ t('devices.auto.enable') }}
              </RvButton>
              <p class="devices__meta">
                {{
                  t(
                    needsCredential(device)
                      ? 'devices.auto.consent'
                      : 'devices.auto.consent.noCredential',
                  )
                }}
              </p>
            </div>
            <p v-else-if="device.deployable" class="devices__meta">
              {{ t('devices.auto.unavailable') }}
            </p>

            <RvButton
              :disabled="devices.busy.value"
              variant="quiet"
              @click="devices.forget(device.id)"
            >
              {{ t('devices.forget') }}
            </RvButton>
          </article>
        </li>
      </ul>

      <form
        v-if="devices.catalogAvailable.value"
        class="devices__form"
        @submit.prevent="submit"
      >
        <h2 class="devices__section-title">{{ t('devices.add') }}</h2>
        <label class="devices__field-label" for="device-target">
          {{ t('devices.field.target') }}
        </label>
        <div class="devices__field">
          <RvSelect
            v-model="targetID"
            :disabled="devices.busy.value"
            input-id="device-target"
            :options="targetChoices"
            :placeholder="t('devices.field.target.pick')"
          />
        </div>

        <label
          v-if="targetID !== ''"
          class="devices__field-label"
          for="device-name"
        >
          {{ t('devices.field.name') }}
        </label>
        <input
          v-if="targetID !== ''"
          id="device-name"
          v-model="name"
          class="devices__input"
          :disabled="devices.busy.value"
          maxlength="120"
          type="text"
        />

        <label
          v-if="targetID !== ''"
          class="devices__field-label"
          for="device-address"
        >
          {{ addressLabel }}
        </label>
        <input
          v-if="targetID !== ''"
          id="device-address"
          v-model="address"
          class="devices__input"
          :disabled="devices.busy.value"
          maxlength="512"
          :placeholder="addressPlaceholder"
          type="text"
        />

        <label
          v-if="showAccount"
          class="devices__field-label"
          for="device-account"
        >
          {{ t('devices.field.account') }}
        </label>
        <input
          v-if="showAccount"
          id="device-account"
          v-model="account"
          class="devices__input"
          :disabled="devices.busy.value"
          maxlength="120"
          type="text"
        />

        <label
          v-if="showInterface"
          class="devices__field-label"
          for="device-interface"
        >
          {{ interfaceLabel }}
        </label>
        <input
          v-if="showInterface"
          id="device-interface"
          v-model="interfaceName"
          class="devices__input"
          :disabled="devices.busy.value"
          maxlength="120"
          type="text"
        />

        <RvButton :disabled="!canRegister" type="submit" variant="primary">
          {{ t('devices.add.submit') }}
        </RvButton>
      </form>

      <RvStateNotice
        v-if="devices.work.value === 'failed'"
        :body="t('devices.work.failed.body')"
        live
        :title="t('devices.work.failed')"
        tone="failed"
      />
    </template>
  </section>
</template>

<style scoped src="./DevicesView.css"></style>
