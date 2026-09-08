<script setup lang="ts">
import { computed, onMounted, watch } from 'vue'

import { useDeployment } from '@/features/send-artifact/model/useDeployment'
import { useLocale } from '@/shared/i18n/useLocale'
import {
  localizedTargetHint,
  localizedTargetTitle,
  type TargetOption,
} from '@/shared/api/catalog'
import type { Fact, StatusTone } from '@/shared/ui/types'
import RvButton from '@/shared/ui/RvButton.vue'
import RvFacts from '@/shared/ui/RvFacts.vue'
import RvField from '@/shared/ui/RvField.vue'
import RvIcon from '@/shared/ui/RvIcon.vue'
import RvStateNotice from '@/shared/ui/RvStateNotice.vue'
import RvStatus from '@/shared/ui/RvStatus.vue'
import RvTextInput from '@/shared/ui/RvTextInput.vue'

const props = defineProps<{
  artifactId: string
  profileName: string
  profileId: string
  target: TargetOption | null
  targetId: string
}>()

const { formatBytes, locale, t, tor } = useLocale()
const deployment = useDeployment(
  () => props.artifactId,
  () => props.targetId,
)

// The catalog names the destination and says where its file belongs. Both are
// catalog content, so both follow the reader's language only where the catalog
// itself offers a second rendition.
const targetTitle = computed(() =>
  localizedTargetTitle(props.target, locale.value, props.targetId),
)
const installationHint = computed(() =>
  localizedTargetHint(props.target, locale.value),
)

// The address field means a different thing per deployer — a router URL for
// Keenetic, a configuration path for sing-box — so the dictionary names it per
// target. A plugin target the dictionary does not know keeps its own caption.
const addressLabel = computed(() =>
  tor(
    `deploy.field.address.${props.targetId}`,
    deployment.requirements.value?.addressLabel ?? t('send.field.address'),
  ),
)
const interfaceLabel = computed(() =>
  tor(
    `deploy.field.interface.${props.targetId}`,
    deployment.requirements.value?.interfaceLabel ?? t('send.field.interface'),
  ),
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
  const format = props.target?.fileExtension.toUpperCase() ?? ''
  return format === ''
    ? t('profile.download.plain')
    : t('profile.download', { format })
})

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
    <header class="send__header">
      <div class="send__identity">
        <h1 id="send-title" class="send__title">{{ t('send.title') }}</h1>
        <p class="send__for">{{ t('send.for', { name: profileName }) }}</p>
      </div>
      <RvStatus
        :label="t(`send.status.${deployment.state.value}`)"
        :tone="statusTone"
      />
    </header>

    <RvStateNotice
      v-if="deployment.targetsState.value === 'loading'"
      live
      :title="t('send.loading')"
      tone="busy"
    />
    <RvStateNotice
      v-else-if="deployment.targetsState.value === 'failed'"
      :body="t('send.targets.failed.body')"
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
      <form class="send__form" @submit.prevent="deployment.review">
        <RvField
          :error="
            deployment.addressError.value === null
              ? undefined
              : t(deployment.addressError.value, {
                  example: deployment.requirements.value?.addressExample ?? '',
                })
          "
          input-id="send-device"
          :label="addressLabel"
        >
          <template #default="{ describedBy, invalid }">
            <RvTextInput
              v-model="deployment.device.value"
              :described-by="describedBy"
              :disabled="deployment.busy.value"
              input-id="send-device"
              :invalid="invalid"
              :placeholder="deployment.requirements.value?.addressExample"
              @blur="deployment.addressTouched.value = true"
            />
          </template>
        </RvField>

        <template v-if="deployment.requirements.value?.needsCredential">
          <RvField input-id="send-user" :label="t('send.field.account')">
            <template #default="{ describedBy }">
              <RvTextInput
                v-model="deployment.username.value"
                autocomplete="username"
                :described-by="describedBy"
                :disabled="deployment.busy.value"
                input-id="send-user"
              />
            </template>
          </RvField>
          <RvField input-id="send-password" :label="t('send.field.password')">
            <template #default="{ describedBy }">
              <RvTextInput
                v-model="deployment.password.value"
                autocomplete="current-password"
                :described-by="describedBy"
                :disabled="deployment.busy.value"
                input-id="send-password"
                type="password"
              />
            </template>
          </RvField>
        </template>

        <RvField
          v-if="deployment.requirements.value?.needsInterface"
          input-id="send-interface"
          :label="interfaceLabel"
        >
          <template #default="{ describedBy }">
            <RvTextInput
              v-model="deployment.interfaceName.value"
              :described-by="describedBy"
              :disabled="deployment.busy.value"
              input-id="send-interface"
            />
          </template>
        </RvField>

        <div class="send__submit">
          <RvButton
            :disabled="!deployment.canSubmit.value"
            type="submit"
            variant="secondary"
          >
            {{
              deployment.state.value === 'planning'
                ? t('send.review.busy')
                : t('send.review')
            }}
          </RvButton>
        </div>
      </form>

      <RvStateNotice
        v-if="deployment.errorKey.value !== null"
        :body="
          deployment.outcome.value?.rolledBack === true
            ? t('send.rolledBack')
            : undefined
        "
        live
        :title="t(deployment.errorKey.value)"
        tone="failed"
      />

      <section
        v-if="
          deployment.plan.value !== null && deployment.state.value !== 'applied'
        "
        aria-labelledby="send-plan-title"
        class="send__plan"
      >
        <h2 id="send-plan-title" class="send__section-title">
          {{ t('send.plan') }}
        </h2>
        <RvFacts :items="planFacts" />
        <p class="send__note">{{ t('send.plan.backup') }}</p>
        <RvButton
          :disabled="deployment.busy.value"
          variant="primary"
          @click="deployment.apply"
        >
          {{
            deployment.state.value === 'applying'
              ? t('send.apply.busy')
              : t('send.apply')
          }}
        </RvButton>
      </section>

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
        <h2 id="send-steps-title" class="send__section-title">
          {{ t('send.steps') }}
        </h2>
        <ol class="send__steps">
          <li
            v-for="event in deployment.outcome.value.events"
            :key="event.step"
            class="send__step"
          >
            <span>{{ t(`send.step.${event.step}`) }}</span>
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
      <p v-if="installationHint !== ''" class="send__note">
        {{ installationHint }}
      </p>
    </section>

    <div class="send__footer">
      <RvButton size="compact" :to="`/profiles/${profileId}`" variant="quiet">
        {{ t('send.back') }}
      </RvButton>
    </div>
  </section>
</template>

<style scoped>
.send {
  display: grid;
  gap: var(--rv-space-8);
  width: 100%;
}

.send__header {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-3) var(--rv-space-5);
  align-items: baseline;
  justify-content: space-between;
}

.send__identity {
  display: grid;
  gap: var(--rv-space-1);
}

.send__title {
  font-size: var(--rv-text-page);
  line-height: var(--rv-leading-tight);
  letter-spacing: var(--rv-tracking-title);
}

.send__for {
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
  overflow-wrap: anywhere;
}

.send__section-title {
  font-size: var(--rv-text-section);
}

.send__form {
  display: grid;
  gap: var(--rv-space-5);
  max-width: 32rem;
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

.send__footer {
  display: flex;
}
</style>
