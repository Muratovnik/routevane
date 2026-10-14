<script setup lang="ts">
import { computed, nextTick, onMounted, watch } from 'vue'

import {
  useDeployment,
  type SendField,
} from '@/features/send-artifact/model/useDeployment'
import { MANUAL_CONNECTION } from '@/features/send-artifact/model/useDeploymentConnections'
import { installationSteps } from '@/features/send-artifact/model/installationSteps'
import { useLocale } from '@/shared/i18n/useLocale'
import {
  localizedTargetHint,
  localizedTargetTitle,
  type TargetOption,
} from '@/shared/api/catalog'
import type { ChoiceOption, Fact, StatusTone } from '@/shared/ui/types'
import RvCodeBlock from '@/shared/ui/RvCodeBlock.vue'
import RvButton from '@/shared/ui/RvButton.vue'
import RvDisclosure from '@/shared/ui/RvDisclosure.vue'
import RvFacts from '@/shared/ui/RvFacts.vue'
import RvField from '@/shared/ui/RvField.vue'
import RvIcon from '@/shared/ui/RvIcon.vue'
import RvInfoTip from '@/shared/ui/RvInfoTip.vue'
import RvPageHeader from '@/shared/ui/RvPageHeader.vue'
import RvSelect from '@/shared/ui/RvSelect.vue'
import RvStateNotice from '@/shared/ui/RvStateNotice.vue'
import RvStatus from '@/shared/ui/RvStatus.vue'
import RvSwitch from '@/shared/ui/RvSwitch.vue'
import RvTextInput from '@/shared/ui/RvTextInput.vue'

/**
 * Sending one published file of one format: to a saved connection, or with
 * details entered for this send only, after a preview of what will change. The
 * by-hand path — the file and how to install it — is always below.
 */
const props = defineProps<{
  artifactId: string
  breadcrumbs: { label: string; to: string }[]
  /** The name the format's groups carry on a router; empty when it has none. */
  groupPrefix: string
  heading: string
  target: TargetOption | null
  targetId: string
  preferredDeviceId?: string
}>()

const { formatBytes, locale, t, tor } = useLocale()
const deployment = useDeployment(
  () => props.artifactId,
  () => props.targetId,
  () => props.preferredDeviceId ?? '',
)
const connections = deployment.connections

// A saved connection names itself; what it reaches is the note under its name,
// which is the one place the screen states it.
const connectionOptions = computed<ChoiceOption[]>(() => [
  ...connections.compatible.value.map((connection) => ({
    value: connection.id,
    label: connection.name,
    note: [connection.address, connection.account, connection.interfaceName]
      .filter((value) => value !== '')
      .join(' · '),
  })),
  { value: MANUAL_CONNECTION, label: t('send.connection.manual') },
])
const noSavedConnections = computed(
  () =>
    connections.state.value === 'ready' &&
    connections.compatible.value.length === 0,
)

const targetTitle = computed(() =>
  localizedTargetTitle(props.target, locale.value, props.targetId),
)
const installationHint = computed(() =>
  localizedTargetHint(props.target, locale.value),
)
const steps = computed(() => installationSteps(installationHint.value))

// The fields carry the words the Connections screen uses for the same thing,
// per deployer: a router's address for Keenetic, a configuration path for
// sing-box. A plugin target the dictionary does not know keeps its own caption.
const addressLabel = computed(() =>
  tor(
    `deploy.field.address.${props.targetId}`,
    deployment.requirements.value?.addressLabel ?? t('send.field.address'),
  ),
)
const accountLabel = computed(() =>
  tor(`deploy.field.account.${props.targetId}`, t('send.field.account')),
)
const passwordLabel = computed(() =>
  tor(`deploy.field.password.${props.targetId}`, t('send.field.password')),
)
const interfaceLabel = computed(() =>
  tor(
    `deploy.field.interface.${props.targetId}`,
    deployment.requirements.value?.interfaceLabel ?? t('send.field.interface'),
  ),
)

// What each field takes sits behind the informer beside its label, in the words
// the Connections screen uses for the same field; the shape of a value is its
// placeholder. A plugin target the dictionary does not know has neither.
const addressInfo = computed(() =>
  tor(`deploy.field.address.${props.targetId}.hint`, ''),
)
const interfaceInfo = computed(() =>
  tor(`deploy.field.interface.${props.targetId}.hint`, ''),
)
const interfacePlaceholder = computed(() =>
  tor(`deploy.field.interface.${props.targetId}.placeholder`, ''),
)

const statusTone = computed<StatusTone>(() => {
  const state = deployment.state.value
  if (state === 'applied') return 'ready'
  if (state === 'failed') return 'failed'
  return state === 'planning' || state === 'applying' ? 'busy' : 'waiting'
})

const planFacts = computed<Fact[]>(() => {
  const plan = deployment.plan.value
  if (plan === null) return []
  return [
    { key: 'device', label: t('send.plan.device'), value: plan.title },
    {
      key: 'file',
      label: t('send.plan.file'),
      value: formatBytes(plan.sizeBytes),
    },
  ]
})

const attemptVisible = computed(
  () =>
    deployment.outcome.value !== null ||
    ['applying', 'outcome_unknown'].includes(deployment.state.value),
)

// Where the current form would send, in the shape an attempt remembers.
const currentRecipient = computed(() => {
  if (connections.manual.value)
    return {
      device: deployment.device.value.trim(),
      username: deployment.username.value.trim(),
      interfaceName: deployment.interfaceName.value.trim(),
    }
  const selected = connections.selected.value
  return selected === null
    ? null
    : {
        device: selected.address,
        username: selected.account,
        interfaceName: selected.interfaceName,
      }
})
// The attempt's destination is stated only when it is not the one chosen now;
// otherwise it repeats what the choice above already says.
const recipientDiffers = computed(() => {
  const recipient = deployment.attemptRecipient.value
  const current = currentRecipient.value
  if (recipient === null) return false
  return (
    current === null ||
    recipient.device !== current.device ||
    recipient.username !== current.username ||
    recipient.interfaceName !== current.interfaceName
  )
})
const recipientFacts = computed<Fact[]>(() => {
  const recipient = deployment.attemptRecipient.value
  if (recipient === null) return []
  const facts: Fact[] = [
    {
      key: 'address',
      label: addressLabel.value,
      value: recipient.device,
      mono: true,
    },
  ]
  if (recipient.username !== '')
    facts.push({
      key: 'account',
      label: accountLabel.value,
      value: recipient.username,
      mono: true,
    })
  if (recipient.interfaceName !== '')
    facts.push({
      key: 'interface',
      label: interfaceLabel.value,
      value: recipient.interfaceName,
      mono: true,
    })
  return facts
})

const outcomeFacts = computed<Fact[]>(() => {
  const outcome = deployment.outcome.value
  if (outcome === null) return []
  const items: Fact[] = []
  if (outcome.vendor !== '') {
    items.push({
      key: 'device',
      label: t('send.facts.device'),
      value: `${outcome.vendor} ${outcome.firmwareVersion}`.trim(),
    })
  }
  if (outcome.interfaceName !== '') {
    items.push({
      key: 'interface',
      label: t('send.facts.interface'),
      mono: true,
      value: outcome.interfaceName,
    })
  }
  if (outcome.backupHash !== '') {
    items.push({
      key: 'backup',
      label: t('send.facts.backup'),
      mono: true,
      value: `${outcome.backupHash.slice(0, 16)}…`,
    })
  }
  return items
})

const downloadLabel = computed(() => {
  const extension = props.target?.fileExtension ?? ''
  return extension === ''
    ? t('profile.download.plain')
    : t('send.manual.download', { extension })
})

// A saved password is used unless the operator asks for another one; the
// field appears for that send only.
const savedPasswordInUse = computed(
  () =>
    !connections.manual.value &&
    connections.storedCredential.value &&
    !deployment.passwordNeeded.value,
)
// A typed password can be kept in the connection; one typed beside a saved
// password replaces it.
const offerToSave = computed(
  () =>
    !connections.manual.value &&
    connections.secretStoreAvailable.value &&
    (!connections.storedCredential.value || deployment.overridePassword.value),
)
const saveLabel = computed(() =>
  connections.storedCredential.value
    ? t('send.password.replace')
    : t('send.password.save'),
)
// The message a failed send states. A refusal of the connection that relied on
// its saved password says so, because the password is what to replace.
const failureTitle = computed(() =>
  deployment.savedPasswordRefused.value
    ? 'send.password.rejected'
    : (deployment.errorKey.value ?? ''),
)
const offerAnotherPassword = computed(
  () =>
    deployment.savedPasswordRefused.value && !deployment.overridePassword.value,
)

// Each field's element is named by the field it holds, so a refused preview
// can move the keyboard to the first field it names.
const fieldID = (field: SendField): string => `send-${field}`
const fieldError = (field: SendField): string | undefined => {
  const key = deployment.fieldErrors.value[field]
  if (key === null) return undefined
  // The service refused the address, and says so in the words of this format's
  // own kind of address, the same words the Connections screen uses.
  if (key === 'send.address.rejected')
    return tor(
      `deploy.field.address.${props.targetId}.rejected`,
      t('send.address.rejected'),
    )
  return t(key, {
    example: deployment.requirements.value?.addressExample ?? '',
  })
}

const review = async (): Promise<void> => {
  const invalid = await deployment.review()
  if (invalid === null) return
  await nextTick()
  document.getElementById(fieldID(invalid))?.focus()
}

const enterAnotherPassword = async (): Promise<void> => {
  deployment.overridePassword.value = true
  await nextTick()
  document.getElementById(fieldID('password'))?.focus()
}

// A new artifact invalidates a plan made for the previous one.
watch(
  () => [props.artifactId, props.targetId],
  () => deployment.reset(),
)

onMounted(() => {
  void deployment.initialize()
})
</script>

<template>
  <section aria-labelledby="send-title" class="send">
    <RvPageHeader
      :breadcrumb-label="t('profile.breadcrumb')"
      :breadcrumbs="breadcrumbs"
      :title="heading"
      title-id="send-title"
    >
      <RvStatus
        v-if="deployment.state.value !== 'idle'"
        :label="t(`send.status.${deployment.state.value}`)"
        :tone="statusTone"
      />
    </RvPageHeader>

    <RvStateNotice
      v-if="deployment.targetsState.value === 'loading'"
      live
      :title="t('send.loading')"
      tone="busy"
    />
    <RvStateNotice
      v-else-if="deployment.targetsState.value === 'failed'"
      :body="t(`failure.${deployment.targetsFailure.value}.body`)"
      live
      :title="t('send.targets.failed')"
      tone="failed"
    >
      <template #action>
        <RvButton @click="deployment.initialize">
          {{ t('action.retry') }}
        </RvButton>
      </template>
    </RvStateNotice>
    <RvStateNotice
      v-else-if="deployment.targetsState.value === 'unsupported'"
      :title="t('send.unsupported', { device: targetTitle })"
      tone="waiting"
    />

    <template v-if="deployment.targetsState.value === 'ready'">
      <form class="send__form" novalidate @submit.prevent="review">
        <RvStateNotice
          v-if="connections.state.value === 'failed'"
          :body="t('send.connection.failed.body')"
          live
          :title="t('send.connection.failed')"
          tone="failed"
        >
          <template #action>
            <RvButton @click="connections.initialize">
              {{ t('action.retry') }}
            </RvButton>
          </template>
        </RvStateNotice>
        <p v-if="noSavedConnections" class="send__none">
          <span>{{ t('send.connection.none') }}</span>
          <NuxtLink class="send__link" to="/connections">
            {{ t('send.connection.add') }}
          </NuxtLink>
        </p>
        <RvField
          v-else
          input-id="send-connection"
          :label="t('send.connection.label')"
        >
          <template #default="{ describedBy }">
            <RvSelect
              v-model="connections.selectedID.value"
              :described-by="describedBy"
              :disabled="deployment.busy.value"
              input-id="send-connection"
              :loading="connections.state.value === 'loading'"
              :loading-label="t('send.connection.loading')"
              :options="connectionOptions"
              :placeholder="t('send.connection.choose')"
            />
          </template>
        </RvField>
        <p
          v-if="
            connections.preferredUnavailable.value ||
            (!connections.manual.value && connections.selected.value === null)
          "
          class="send__note"
        >
          {{ t('send.connection.unavailable') }}
        </p>
        <RvField
          v-if="connections.manual.value"
          :error="fieldError('address')"
          :input-id="fieldID('address')"
          :label="addressLabel"
        >
          <template #default="{ describedBy, invalid }">
            <div class="send__control">
              <RvTextInput
                v-model="deployment.device.value"
                :described-by="describedBy"
                :disabled="deployment.busy.value"
                :input-id="fieldID('address')"
                mono
                :invalid="invalid"
                :placeholder="deployment.requirements.value?.addressExample"
                @blur="deployment.addressTouched.value = true"
              />
              <span v-if="addressInfo !== ''" class="send__info">
                <RvInfoTip
                  :label="t('devices.field.address.info')"
                  :text="addressInfo"
                />
              </span>
            </div>
          </template>
        </RvField>

        <template v-if="deployment.requirements.value?.needsCredential">
          <RvField
            v-if="connections.manual.value"
            :error="fieldError('username')"
            :input-id="fieldID('username')"
            :label="accountLabel"
          >
            <template #default="{ describedBy, invalid }">
              <RvTextInput
                v-model="deployment.username.value"
                autocomplete="username"
                :described-by="describedBy"
                :disabled="deployment.busy.value"
                :input-id="fieldID('username')"
                :invalid="invalid"
                mono
              />
            </template>
          </RvField>
          <div
            v-if="
              !connections.manual.value && connections.storedCredential.value
            "
            class="send__saved"
          >
            <span class="send__saved-fact">
              <RvIcon name="check" />
              {{ t('devices.credential.saved') }}
            </span>
            <RvButton
              v-if="savedPasswordInUse && !offerAnotherPassword"
              :disabled="deployment.busy.value"
              size="compact"
              variant="quiet"
              @click="enterAnotherPassword"
            >
              {{ t('send.password.override') }}
            </RvButton>
          </div>
          <RvField
            v-if="deployment.passwordNeeded.value"
            :error="fieldError('password')"
            :input-id="fieldID('password')"
            :label="passwordLabel"
          >
            <template #default="{ describedBy, invalid }">
              <RvTextInput
                v-model="deployment.password.value"
                autocomplete="current-password"
                :described-by="describedBy"
                :disabled="deployment.busy.value"
                :input-id="fieldID('password')"
                :invalid="invalid"
                type="password"
              />
            </template>
          </RvField>
          <RvSwitch
            v-if="offerToSave"
            v-model="deployment.savePassword.value"
            :description="t('send.password.save.consequence')"
            :disabled="deployment.busy.value"
            :label="saveLabel"
          />
          <RvStatus
            v-if="deployment.passwordSaveFailed.value"
            :label="t(deployment.passwordSaveFailure.value)"
            tone="failed"
          />
        </template>

        <RvField
          v-if="
            connections.manual.value &&
            deployment.requirements.value?.needsInterface
          "
          :error="fieldError('interface')"
          :input-id="fieldID('interface')"
          :label="interfaceLabel"
        >
          <template #default="{ describedBy, invalid }">
            <div class="send__control">
              <RvTextInput
                v-model="deployment.interfaceName.value"
                :described-by="describedBy"
                :disabled="deployment.busy.value"
                :input-id="fieldID('interface')"
                :invalid="invalid"
                mono
                :placeholder="interfacePlaceholder"
              />
              <span v-if="interfaceInfo !== ''" class="send__info">
                <RvInfoTip
                  :label="t('devices.field.interface.info')"
                  :text="interfaceInfo"
                />
              </span>
            </div>
          </template>
        </RvField>

        <div class="send__submit">
          <RvButton
            :disabled="!deployment.available.value"
            :loading="deployment.state.value === 'planning'"
            :loading-label="t('send.review.busy')"
            type="submit"
            :variant="deployment.plan.value === null ? 'primary' : 'secondary'"
          >
            {{ t('send.review') }}
          </RvButton>
        </div>
      </form>

      <RvStateNotice
        v-if="deployment.errorKey.value !== null && !attemptVisible"
        live
        :title="t(failureTitle)"
        tone="failed"
      >
        <template v-if="offerAnotherPassword" #action>
          <RvButton @click="enterAnotherPassword">
            {{ t('send.password.override') }}
          </RvButton>
        </template>
      </RvStateNotice>

      <section
        v-if="
          deployment.plan.value !== null &&
          ['planned', 'applying'].includes(deployment.state.value)
        "
        aria-labelledby="send-plan-title"
        class="send__plan"
      >
        <h2 id="send-plan-title" class="send__section-title">
          {{ t('send.plan') }}
        </h2>
        <RvFacts :items="planFacts" />
        <template v-if="deployment.plan.value.fqdnChanges !== undefined">
          <p class="send__note">{{ t('send.plan.fqdn') }}</p>
          <RvCodeBlock
            v-if="deployment.plan.value.fqdnChanges.length > 0"
            :caption="t('send.plan.fqdn.commands')"
            :text="deployment.plan.value.fqdnChanges.join('\n')"
          />
          <p v-else class="send__note">{{ t('send.plan.fqdn.unchanged') }}</p>
        </template>
        <p class="send__note">{{ t('send.plan.backup') }}</p>
        <RvButton
          :disabled="deployment.busy.value"
          :loading="deployment.state.value === 'applying'"
          :loading-label="t('send.apply.busy')"
          variant="primary"
          @click="deployment.apply"
        >
          {{ t('send.apply') }}
        </RvButton>
      </section>

      <section
        v-if="attemptVisible"
        aria-labelledby="send-attempt-title"
        class="send__attempt"
      >
        <h2 id="send-attempt-title" class="send__section-title">
          {{ t('send.attempt.title') }}
        </h2>
        <template v-if="recipientDiffers">
          <p class="send__note">{{ t('send.attempt.recipient') }}</p>
          <RvFacts :items="recipientFacts" />
        </template>
        <p
          v-else-if="deployment.attemptRecipient.value === null"
          class="send__note"
        >
          {{ t('send.attempt.recipientUnknown') }}
        </p>

        <RvStateNotice
          v-if="deployment.state.value === 'outcome_unknown'"
          :body="t('send.outcomeUnknown.body')"
          live
          :title="t('send.outcomeUnknown.title')"
          tone="waiting"
        >
          <template #action>
            <RvButton
              :disabled="deployment.checkingOutcome.value"
              @click="deployment.checkOutcome"
            >
              {{
                deployment.checkingOutcome.value
                  ? t('send.outcomeUnknown.checking')
                  : t('send.outcomeUnknown.check')
              }}
            </RvButton>
          </template>
        </RvStateNotice>
        <RvStateNotice
          v-if="deployment.errorKey.value !== null"
          :body="
            deployment.outcome.value?.rolledBack === true
              ? t('send.rolledBack')
              : undefined
          "
          live
          :title="t(failureTitle)"
          tone="failed"
        >
          <template v-if="offerAnotherPassword" #action>
            <RvButton @click="enterAnotherPassword">
              {{ t('send.password.override') }}
            </RvButton>
          </template>
        </RvStateNotice>
        <RvStateNotice
          v-if="deployment.state.value === 'applied'"
          :body="t('send.applied.body')"
          live
          :title="t('send.applied')"
          tone="ready"
        />

        <RvFacts v-if="outcomeFacts.length > 0" :items="outcomeFacts" />

        <section
          v-if="deployment.outcome.value !== null"
          aria-labelledby="send-steps-title"
          class="send__audit"
        >
          <h3 id="send-steps-title" class="send__section-title">
            {{ t('send.steps') }}
          </h3>
          <ol class="send__steps">
            <li
              v-for="event in deployment.outcome.value.events"
              :key="event.step"
              class="send__step"
            >
              <span>{{
                tor(`send.step.${event.step}`, t('send.step.other'))
              }}</span>
              <strong
                :class="{
                  'send__step-outcome--failed': event.outcome !== 'success',
                }"
              >
                {{
                  event.outcome === 'success'
                    ? t('send.step.success')
                    : t('send.step.failed')
                }}
              </strong>
            </li>
          </ol>
        </section>
      </section>
    </template>

    <section
      v-if="deployment.targetsState.value !== 'loading'"
      aria-labelledby="send-manual-title"
      class="send__manual"
    >
      <h2 id="send-manual-title" class="send__section-title">
        {{ t('send.manual') }}
      </h2>
      <div class="send__manual-actions">
        <RvButton :href="`/v1/artifacts/${artifactId}`" variant="secondary">
          <RvIcon name="download" />
          {{ downloadLabel }}
        </RvButton>
      </div>
      <RvDisclosure
        v-if="steps.length > 0"
        borderless
        :summary="t('send.manual.howto')"
      >
        <div class="send__howto">
          <ol v-if="steps.length > 1" class="send__install-steps">
            <li v-for="(step, index) in steps" :key="index">{{ step }}</li>
          </ol>
          <p v-else>{{ steps[0] }}</p>
          <p v-if="groupPrefix !== ''" class="send__prefix">
            {{ t('send.manual.prefix', { prefix: groupPrefix }) }}
          </p>
        </div>
      </RvDisclosure>
    </section>
  </section>
</template>

<style scoped>
.send {
  display: grid;
  gap: var(--rv-space-8);
  width: 100%;
}

.send__section-title {
  font-size: var(--rv-text-section);
}

.send__form {
  display: grid;
  gap: var(--rv-space-5);
  max-width: 32rem;
}

/* The informer of a field stands at the end of its label row, where the
   library draws a field's own hint. The control's own box is the positioning
   context, so the informer rises from the top of the control to the row above
   it and takes no room from the label or the control. */
.send__control {
  position: relative;
}

.send__info {
  position: absolute;
  inset-block-end: 100%;
  inset-inline-end: 0;
}

.send__none {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-1) var(--rv-space-3);
  align-items: baseline;
}

.send__link {
  color: var(--rv-color-accent-ink);
  font-weight: 600;
}

.send__link:focus-visible {
  outline: var(--rv-border-mark) solid var(--rv-color-focus);
  outline-offset: var(--rv-focus-offset);
}

.send__saved {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-2) var(--rv-space-4);
  align-items: center;
}

.send__saved-fact {
  display: inline-flex;
  gap: var(--rv-space-2);
  align-items: center;
  font-weight: 600;
}

.send__submit {
  display: flex;
  gap: var(--rv-space-3);
  align-items: center;
}

.send__plan {
  display: grid;
  gap: var(--rv-space-4);
  justify-items: start;
  max-width: 32rem;
  padding: var(--rv-space-5);
  background: var(--rv-color-surface);
  border: var(--rv-border-hair) solid var(--rv-color-rule);
  border-radius: var(--rv-radius-md);
}

.send__note {
  max-width: var(--rv-measure-prose);
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
}

.send__attempt,
.send__audit {
  display: grid;
  gap: var(--rv-space-3);
  max-width: 32rem;
}

.send__steps {
  display: grid;
  gap: var(--rv-space-1);
}

.send__step {
  display: flex;
  gap: var(--rv-space-3);
  justify-content: space-between;
  padding: var(--rv-space-2) 0;
  font-size: var(--rv-text-dense);
  border-bottom: var(--rv-border-hair) solid var(--rv-color-rule);
}

.send__step-outcome--failed {
  color: var(--rv-color-status-failed);
}

.send__manual {
  display: grid;
  gap: var(--rv-space-3);
  padding-top: var(--rv-space-4);
  border-top: var(--rv-border-hair) solid var(--rv-color-rule);
}

.send__manual-actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-3);
}

.send__howto {
  display: grid;
  gap: var(--rv-space-3);
  max-width: var(--rv-measure-prose);
}

/* The catalog numbers its steps in one line; they are read as a list. */
.send__install-steps {
  display: grid;
  gap: var(--rv-space-2);
  margin: 0;
  padding-inline-start: var(--rv-space-5);
  list-style: decimal;
}

.send__prefix {
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
}
</style>
