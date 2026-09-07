<script setup lang="ts">
import { computed, onMounted } from 'vue'

import RvComposerForm from '@/shared/ui/RvComposerForm.vue'
import RvComposer from '@/shared/ui/RvComposer.vue'
import ListPicker from '@/entities/profile-composition/ui/ListPicker.vue'
import { useCreateProfile } from '@/features/create-profile/model/useCreateProfile'
import { useLocale } from '@/shared/i18n/useLocale'
import type { ChoiceGroup } from '@/shared/ui/kinds'
import RvButton from '@/shared/ui/RvButton.vue'
import RvCombobox from '@/shared/ui/RvCombobox.vue'
import RvStateNotice from '@/shared/ui/RvStateNotice.vue'

const emit = defineEmits<{
  created: [profileID: string, targetID: string]
}>()

const { formatNumber, t, tc } = useLocale()
const setup = useCreateProfile()

// The one quiet status line this screen already has. A forecast that first has
// to read a list's sources takes a few seconds, and saying so is better than
// numbers that appear from nowhere — it explains a wait, it gates nothing.
const stageMessage = computed(() => {
  if (setup.flowState.value === 'creating') return t('create.stage.creating')
  return setup.observing.value ? t('listCard.observing') : ''
})

// What the draft would weigh in this format. A format that states no bound
// says only the size; one that states a bound says the size against it.
const forecastLabel = (targetID: string): string => {
  const forecast = setup.forecastFor(targetID)
  if (forecast === null) return ''
  if (forecast.incompleteLists?.length) return t('forecast.partial.target')
  return forecast.maximumRules === 0
    ? tc('create.forecast.rules', forecast.projectedRules)
    : t('create.forecast.of', {
        m: formatNumber(forecast.maximumRules),
        n: formatNumber(forecast.projectedRules),
      })
}

const overflowing = (targetID: string): boolean => {
  const forecast = setup.forecastFor(targetID)
  return !forecast?.incompleteLists?.length && forecast?.fits === false
}

// One line under a format's name: how it is delivered, and what this draft
// would weigh in it. A format states both before it is chosen, so the choice is
// made on the numbers rather than after them.
const targetNote = (targetID: string): string => {
  const method = setup.deployable(targetID)
    ? t('create.target.automatic')
    : t('create.target.manual')
  const forecast = forecastLabel(targetID)
  return forecast === '' ? method : `${method} · ${forecast}`
}

// Routers and applications stay named runs inside the one profile, because
// "which kind of thing is this" is still how an operator looks for it.
const targetChoices = computed<ChoiceGroup[]>(() =>
  setup.targetGroups.value.map((group) => ({
    key: group.kind,
    label: t(`kind.${group.kind}`),
    options: group.targets.map((target) => ({
      label: setup.targetTitle(target),
      mono: `.${target.fileExtension}`,
      note: targetNote(target.id),
      value: target.id,
      warning: overflowing(target.id)
        ? t('create.forecast.overflow')
        : undefined,
    })),
  })),
)

// The chosen format's forecast stays on the screen after the profile closes over
// it, because it is the number the create button is judged against.
const chosenForecast = computed(() =>
  forecastLabel(setup.selectedTargetID.value),
)

const chosenTarget = computed<string>({
  get: () => setup.selectedTargetID.value,
  set: (value) => setup.setTarget(value),
})

// The refusal names a way forward when there is one, and says plainly that
// there is none when there is not.
const blockedBody = computed(() => {
  if (!setup.blocked.value) return ''
  return setup.suggestedTarget.value === null
    ? t('create.forecast.none')
    : t('create.forecast.suggest', {
        target: setup.suggestedTargetTitle.value,
      })
})

onMounted(() => {
  void setup.initialize()
})

const submit = async (): Promise<void> => {
  const created = await setup.create()
  if (created !== null) emit('created', created.profileID, created.targetID)
}
</script>

<template>
  <section aria-labelledby="create-title" class="create">
    <header class="create__header">
      <h1 id="create-title" class="create__title">{{ t('create.title') }}</h1>
    </header>

    <RvStateNotice
      v-if="setup.catalogState.value === 'loading'"
      live
      :title="t('create.catalog.loading')"
      tone="busy"
    />
    <RvStateNotice
      v-else-if="setup.catalogState.value === 'failed'"
      :body="t('create.catalog.failed.body')"
      live
      :title="t('create.catalog.failed')"
      tone="failed"
    >
      <template #action>
        <RvButton @click="setup.initialize">{{ t('action.retry') }}</RvButton>
      </template>
    </RvStateNotice>
    <RvStateNotice
      v-else-if="setup.catalogState.value === 'empty'"
      :body="t('create.catalog.empty.body')"
      live
      :title="t('create.catalog.empty')"
      tone="warning"
    />

    <RvComposer v-else :settings-label="t('create.settings')">
      <section
        aria-labelledby="create-lists-title"
        class="create__lists"
        role="group"
      >
        <h2 id="create-lists-title" class="create__legend">
          {{ t('create.lists') }}
        </h2>
        <div class="create__lists-body">
          <ListPicker
            v-model="setup.composition.value"
            fill
            :initial-priority="setup.defaultPriority.value"
            :categories="setup.categories.value"
            :disabled="setup.busy.value"
            :forecast="setup.selectedForecast.value"
            :forecast-failure="
              setup.forecastFailure.value
                ? t(`forecast.failure.${setup.forecastFailure.value}`)
                : undefined
            "
            :overlap-unavailable="setup.selectedTargetID.value === ''"
            :forecast-pending="setup.forecastPending.value"
            :refreshing="setup.observing.value"
            :profile-name="setup.name.value"
            pending
            :lists="setup.lists.value"
            :retryable="setup.selectedTargetID.value !== ''"
            @reorder="setup.setPriority"
            @refresh="setup.refreshSources"
            @retry="setup.retryForecast"
          />
        </div>
      </section>

      <template #settings="{ compact }">
        <RvComposerForm
          class="create__form"
          :compact="compact"
          @submit.prevent="submit"
        >
          <div class="create__name">
            <label class="create__name-label" for="create-name">
              {{ t('create.name') }}
            </label>
            <input
              id="create-name"
              class="create__name-input"
              :disabled="setup.busy.value"
              maxlength="120"
              :placeholder="t('create.name.placeholder')"
              type="text"
              :value="setup.name.value"
              @input="setup.setName(($event.target as HTMLInputElement).value)"
            />
          </div>

          <div class="create__target">
            <label class="create__target-label" for="create-target">
              {{ t('create.target') }}
            </label>
            <template v-if="setup.targets.value.length > 0">
              <RvCombobox
                v-model="chosenTarget"
                :disabled="setup.busy.value"
                :empty-label="t('create.target.noMatches')"
                :groups="targetChoices"
                input-id="create-target"
                :placeholder="t('create.target.placeholder')"
                :toggle-label="t('create.target.toggle')"
              />
              <p v-if="chosenForecast !== ''" class="create__forecast">
                {{ chosenForecast }}
              </p>
            </template>
            <RvStateNotice
              v-else
              :body="t('create.target.empty.body')"
              :title="t('create.target.empty')"
              tone="warning"
            />
          </div>

          <template #actions>
            <RvStateNotice
              v-if="setup.blocked.value"
              :body="blockedBody"
              class="create__blocked"
              live
              :title="
                t('create.forecast.blocked', {
                  target: setup.selectedTargetTitle.value,
                })
              "
              tone="warning"
            >
              <template v-if="setup.suggestedTarget.value !== null" #action>
                <RvButton
                  :disabled="setup.busy.value"
                  size="compact"
                  @click="setup.setTarget(setup.suggestedTarget.value.id)"
                >
                  {{
                    t('create.forecast.switch', {
                      target: setup.suggestedTargetTitle.value,
                    })
                  }}
                </RvButton>
              </template>
            </RvStateNotice>
            <RvButton
              :disabled="!setup.canCreate.value"
              :loading="setup.busy.value"
              type="submit"
              variant="primary"
            >
              {{ setup.busy.value ? stageMessage : t('create.submit') }}
            </RvButton>
          </template>
        </RvComposerForm>
      </template>
    </RvComposer>

    <RvStateNotice
      v-if="setup.flowState.value === 'failed'"
      :body="t('create.failed.body')"
      live
      :title="t('create.failed')"
      tone="failed"
    />
  </section>
</template>

<style scoped>
.create {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: var(--rv-space-5) var(--rv-space-6);
  align-items: start;
  grid-template-rows: auto minmax(0, 1fr);
  height: 100%;
  min-height: 0;
  width: 100%;
}

.create__title {
  font-size: var(--rv-text-page);
  line-height: var(--rv-leading-tight);
  letter-spacing: var(--rv-tracking-title);
}

.create__header,
.create__lists {
  position: relative;
  min-height: 0;
  height: 100%;
  grid-column: 1;
}

.create__header {
  display: grid;
  gap: var(--rv-space-3);
}

.create__lists {
  min-width: 0;
  margin: 0;
  padding: 0;
  border: 0;
}

.create__legend {
  position: absolute;
  width: var(--rv-border-hair);
  height: var(--rv-border-hair);
  overflow: hidden;
  clip-path: inset(50%);
}

.create__lists-body {
  height: 100%;
  min-height: 0;
  min-width: 0;
}

.create__target {
  display: grid;
  gap: var(--rv-space-2);
  min-width: 0;
  max-width: var(--rv-measure-field);
}

.create__target-label {
  font-weight: 600;
  font-size: var(--rv-text-dense);
}

/* The projected size sits under the field in the same register as the rest of
   the small print: it is a fact about the chosen format, not an alert. The
   refusal beside the create button is what carries the warning colour. */
.create__forecast {
  color: var(--rv-color-ink-tertiary);
  font-size: var(--rv-text-meta);
  font-variant-numeric: tabular-nums;
}

.create__name {
  display: grid;
  gap: var(--rv-space-2);
  min-width: 0;
  max-width: var(--rv-measure-field);
}

.create__name-label {
  font-weight: 600;
  font-size: var(--rv-text-dense);
}

.create__name-input {
  min-height: var(--rv-control-touch);
  padding: 0 var(--rv-space-4);
  color: var(--rv-color-ink);
  font: inherit;
  background: var(--rv-color-field);
  border: var(--rv-border-hair) solid var(--rv-color-rule);
  border-radius: var(--rv-radius-md);
}

.create__name-input:focus-visible {
  outline: var(--rv-border-mark) solid var(--rv-color-focus);
  outline-offset: var(--rv-focus-offset);
}

.create__submit {
  display: grid;
  gap: var(--rv-space-3);
  justify-items: stretch;
}

/* The refusal explains the disabled control beneath it, so it takes the
   reading width rather than the button's. */
.create__blocked {
  justify-self: stretch;
  max-width: var(--rv-measure-prose);
}

@container (width <= 54rem) {
  .create {
    --rv-picker-fill-height: auto;
    --rv-picker-fill-min: var(--rv-picker-mobile-height);
    --rv-picker-fill-max: var(--rv-picker-mobile-height);

    height: auto;
    grid-template-rows: auto;
    grid-template-columns: minmax(0, 1fr);
  }

  .create__lists,
  .create__lists-body {
    height: auto;
  }
}

@media (width <= 64rem), (height <= 36rem) {
  .create {
    height: auto;
  }
}
</style>
