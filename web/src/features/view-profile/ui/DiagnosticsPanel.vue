<script setup lang="ts">
import { computed, ref } from 'vue'

import type { DiagnosticRule } from '@/shared/api/artifacts'
import { useLocale } from '@/shared/i18n/useLocale'
import type { ChoiceOption } from '@/shared/ui/types'
import RvSelect from '@/shared/ui/RvSelect.vue'
import RvStatus from '@/shared/ui/RvStatus.vue'
import RvTable from '@/shared/ui/RvTable.vue'
import RvTextInput from '@/shared/ui/RvTextInput.vue'

/**
 * The rules one build put into its file, and the ones it left out, as one
 * table: list, value and reason. The value is compared character by character
 * and takes the mono role; the reason is words. Whether a rule was included is
 * a column only when some rule was not, because a column that says «Included»
 * on every row tells the reader nothing. The table fills the window and scrolls
 * its own rows; the per-list totals follow it as an addition to the page.
 */
const props = defineProps<{
  listTitle: (id: string) => string
  reasonLabel: (code: string) => string
  rules: DiagnosticRule[]
}>()

const { t, tc } = useLocale()

const ALL_LISTS = 'all'
const listFilter = ref(ALL_LISTS)
const query = ref('')

const hasExclusions = computed(() => props.rules.some((rule) => rule.excluded))

const listOptions = computed<ChoiceOption[]>(() => {
  const ids = [...new Set(props.rules.map((rule) => rule.listID))]
  return [
    { label: t('profile.diagnostics.allLists'), value: ALL_LISTS },
    ...ids
      .map((id) => ({ label: props.listTitle(id), value: id }))
      .sort((left, right) => left.label.localeCompare(right.label)),
  ]
})

const reasonText = (rule: DiagnosticRule): string =>
  rule.reasons.map(props.reasonLabel).join(', ')

const shown = computed(() => {
  const needle = query.value.trim().toLocaleLowerCase()
  return props.rules.filter(
    (rule) =>
      (listFilter.value === ALL_LISTS || rule.listID === listFilter.value) &&
      (needle === '' ||
        [rule.value, props.listTitle(rule.listID), reasonText(rule)].some(
          (text) => text.toLocaleLowerCase().includes(needle),
        )),
  )
})

const rulesByList = computed<{ id: string; count: number }[]>(() => {
  const counts = new Map<string, number>()
  for (const rule of props.rules) {
    if (rule.excluded) continue
    counts.set(rule.listID, (counts.get(rule.listID) ?? 0) + 1)
  }
  return [...counts.entries()]
    .map(([id, count]) => ({ count, id }))
    .sort((left, right) =>
      props.listTitle(left.id).localeCompare(props.listTitle(right.id)),
    )
})

const columns = computed(() => (hasExclusions.value ? 4 : 3))
</script>

<template>
  <div class="diagnostics">
    <section
      aria-labelledby="profile-diagnostics-rules"
      class="diagnostics__rules"
    >
      <slot name="scope" />
      <div class="diagnostics__heading">
        <h2 id="profile-diagnostics-rules" class="diagnostics__title">
          {{ t('profile.diagnostics.rules') }}
        </h2>
        <p v-if="!hasExclusions" class="diagnostics__note">
          {{ t('profile.diagnostics.empty') }}
        </p>
      </div>
      <div class="diagnostics__toolbar">
        <span id="profile-diagnostics-list-label" class="rv-visually-hidden">
          {{ t('profile.diagnostics.list') }}
        </span>
        <div class="diagnostics__list">
          <RvSelect
            v-model="listFilter"
            input-id="profile-diagnostics-list"
            :label="t('profile.diagnostics.list')"
            labelled-by="profile-diagnostics-list-label"
            :options="listOptions"
            :placeholder="t('profile.diagnostics.allLists')"
          />
        </div>
        <RvTextInput
          v-model="query"
          :aria-label="t('profile.diagnostics.search')"
          class="diagnostics__search"
          :placeholder="t('profile.diagnostics.search')"
          type="search"
        />
      </div>
      <RvTable class="diagnostics__table" dense sticky-header>
        <thead>
          <tr>
            <th v-if="hasExclusions" scope="col">
              {{ t('profile.diagnostics.column.state') }}
            </th>
            <th scope="col">{{ t('profile.diagnostics.column.list') }}</th>
            <th scope="col">{{ t('profile.diagnostics.column.value') }}</th>
            <th scope="col">{{ t('profile.diagnostics.column.reason') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="rule in shown"
            :key="`${rule.excluded}-${rule.listID}-${rule.value}`"
          >
            <!-- A state is a dot and words: entered the file, or held out of
                 it for the reason beside it. -->
            <td v-if="hasExclusions">
              <RvStatus
                :label="
                  rule.excluded
                    ? t('profile.diagnostics.excluded')
                    : t('profile.diagnostics.included')
                "
                :tone="rule.excluded ? 'waiting' : 'ready'"
              />
            </td>
            <td>{{ listTitle(rule.listID) }}</td>
            <td class="diagnostics__value">{{ rule.value }}</td>
            <td class="diagnostics__reason">{{ reasonText(rule) }}</td>
          </tr>
          <tr v-if="shown.length === 0">
            <td class="diagnostics__nothing" :colspan="columns">
              {{ t('choice.empty') }}
            </td>
          </tr>
        </tbody>
      </RvTable>
    </section>

    <section
      aria-labelledby="profile-diagnostics-per-list"
      class="diagnostics__counts"
    >
      <h2 id="profile-diagnostics-per-list" class="diagnostics__title">
        {{ t('profile.diagnostics.perList') }}
      </h2>
      <ul class="diagnostics__count-list">
        <li
          v-for="entry in rulesByList"
          :key="entry.id"
          class="diagnostics__count"
        >
          <span>{{ listTitle(entry.id) }}</span>
          <strong>{{ tc('profile.rules', entry.count) }}</strong>
        </li>
      </ul>
    </section>
  </div>
</template>

<style scoped>
.diagnostics {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: var(--rv-space-8);
  min-width: 0;
}

/* The rules region has a standing height taken from the window, the same one
   the composition table stands at on the tab beside it: its rows scroll inside
   the table, and the totals below it lengthen the page instead. */
.diagnostics__rules {
  display: flex;
  flex-direction: column;
  gap: var(--rv-space-3);
  height: var(--rv-composition-floor);
  min-height: 0;
}

.diagnostics__rules > * {
  flex-shrink: 0;
}

.diagnostics__table {
  flex: 1 1 0;
  flex-shrink: 1;
  min-height: 0;
}

.diagnostics__heading {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-1) var(--rv-space-4);
  align-items: baseline;
}

.diagnostics__title {
  font-size: var(--rv-text-section);
}

.diagnostics__note {
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
}

.diagnostics__toolbar {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-3);
  align-items: center;
}

.diagnostics__list {
  width: min(100%, var(--rv-measure-field));
}

.diagnostics__search {
  flex: 1 1 var(--rv-composer-field-width);
  min-width: 0;
  max-width: var(--rv-measure-field);
}

/* A rule value is read whole: it keeps one line and the table scrolls inside
   its own frame if a value is wider than the window; the reason wraps. */
.diagnostics__value {
  font-family: var(--rv-font-mono);
  white-space: nowrap;
}

.diagnostics__reason {
  color: var(--rv-color-ink-muted);
}

.diagnostics__nothing {
  color: var(--rv-color-ink-muted);
}

.diagnostics__counts {
  display: grid;
  gap: var(--rv-space-3);
}

.diagnostics__count-list {
  display: grid;
  gap: var(--rv-space-1);
  max-width: var(--rv-measure-prose);
}

.diagnostics__count {
  display: flex;
  gap: var(--rv-space-3);
  justify-content: space-between;
  padding: var(--rv-space-2) 0;
  font-size: var(--rv-text-dense);
  border-bottom: var(--rv-border-hair) solid var(--rv-color-rule);
}

/* Outside the fill layout the region takes its natural height and the table
   is capped on its own — the boundary the composition panel uses. */
@media (width <= 64rem), (height <= 36rem) {
  .diagnostics__rules {
    height: auto;
  }

  .diagnostics__table {
    flex: none;
    max-height: var(--rv-picker-mobile-height);
  }
}
</style>
