<script setup lang="ts">
import { computed, onMounted } from 'vue'

import ServicePicker from '@/entities/list-composition/ui/ServicePicker.vue'
import { useCreateList } from '@/features/create-list/model/useCreateList'
import { useLocale } from '@/shared/i18n/useLocale'
import type { ChoiceGroup } from '@/shared/ui/kinds'
import RvButton from '@/shared/ui/RvButton.vue'
import RvCombobox from '@/shared/ui/RvCombobox.vue'
import RvStateNotice from '@/shared/ui/RvStateNotice.vue'

const emit = defineEmits<{
  created: [listID: string, targetID: string]
}>()

const { formatNumber, t, tc } = useLocale()
const setup = useCreateList()

// The one quiet status line this screen already has. A forecast that first has
// to read a service's sources takes a few seconds, and saying so is better than
// numbers that appear from nowhere — it explains a wait, it gates nothing.
const stageMessage = computed(() => {
  if (setup.flowState.value === 'creating') return t('create.stage.creating')
  return setup.observing.value ? t('serviceCard.observing') : ''
})

// What the draft would weigh in this format. A format that states no bound
// says only the size; one that states a bound says the size against it.
function forecastLabel(targetID: string): string {
  const forecast = setup.forecastFor(targetID)
  if (forecast === null) return ''
  return forecast.maximumRules === 0
    ? tc('create.forecast.rules', forecast.projectedRules)
    : t('create.forecast.of', {
        m: formatNumber(forecast.maximumRules),
        n: formatNumber(forecast.projectedRules),
      })
}

function overflowing(targetID: string): boolean {
  return setup.forecastFor(targetID)?.fits === false
}

// One line under a format's name: how it is delivered, and what this draft
// would weigh in it. A format states both before it is chosen, so the choice is
// made on the numbers rather than after them.
function targetNote(targetID: string): string {
  const method = setup.deployable(targetID)
    ? t('create.target.automatic')
    : t('create.target.manual')
  const forecast = forecastLabel(targetID)
  return forecast === '' ? method : `${method} · ${forecast}`
}

// Routers and applications stay named runs inside the one list, because
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

// The chosen format's forecast stays on the screen after the list closes over
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

async function submit(): Promise<void> {
  const created = await setup.create()
  if (created !== null) emit('created', created.listID, created.targetID)
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

    <form v-else class="create__form" @submit.prevent="submit">
      <section
        aria-labelledby="create-services-title"
        class="create__services"
        role="group"
      >
        <h2 id="create-services-title" class="create__legend">
          {{ t('create.services') }}
        </h2>
        <div class="create__services-body">
          <ServicePicker
            v-model="setup.composition.value"
            fill
            :initial-priority="setup.defaultPriority.value"
            :categories="setup.categories.value"
            :disabled="setup.busy.value"
            :forecast="setup.selectedForecast.value"
            :overlap-unavailable="setup.selectedTargetID.value === ''"
            :forecast-pending="setup.forecastPending.value"
            :list-name="setup.name.value"
            pending
            :services="setup.services.value"
            :retryable="setup.selectedTargetID.value !== ''"
            @reorder="setup.setPriority"
            @retry="setup.retryForecast"
          />
        </div>
      </section>

      <aside class="create__settings" :aria-label="t('create.settings')">
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

        <div class="create__submit">
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
        </div>
      </aside>
    </form>

    <RvStateNotice
      v-if="setup.flowState.value === 'failed'"
      :body="t('create.failed.body')"
      live
      :title="t('create.failed')"
      tone="failed"
    />
  </section>
</template>

<style scoped src="./CreateList.css"></style>
