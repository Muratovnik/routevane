<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'

import type { ListDetail } from '@/entities/profile-composition/model/types'
import {
  useListDetail,
  type ListDetailProps,
} from '@/entities/profile-composition/model/useListDetail'
import { useLocale } from '@/shared/i18n/useLocale'
import RvButton from '@/shared/ui/RvButton.vue'
import RvDialog from '@/shared/ui/RvDialog.vue'
import RvField from '@/shared/ui/RvField.vue'
import RvFilePicker from '@/shared/ui/RvFilePicker.vue'
import RvIcon from '@/shared/ui/RvIcon.vue'
import RvTooltip from '@/shared/ui/RvTooltip.vue'
import RvInfoTip from '@/shared/ui/RvInfoTip.vue'
import RvSelect from '@/shared/ui/RvSelect.vue'
import RvStatus from '@/shared/ui/RvStatus.vue'
import RvTextInput from '@/shared/ui/RvTextInput.vue'
import RvTextarea from '@/shared/ui/RvTextarea.vue'

/**
 * One list, read in either of the two flows the product has (ADR 0029): the
 * library that curates it, and the route being composed, which only reads it
 * and decides whether it belongs in that route.
 *
 * The card is the surface. Which requests each flow makes, and the state they
 * read and write, is `useListDetail`; what is here is the markup, the words on
 * it, and where the keyboard goes.
 */
const props = defineProps<ListDetailProps>()

const emit = defineEmits<{
  close: []
  /** compose: the list's membership in the route this card was opened from. */
  include: [add: boolean]
  created: [detail: ListDetail]
  updated: [detail: ListDetail]
  /**
   * library: the operator asked for this list to stop existing. The flow that
   * owns the library confirms it and reports the refusal, so one confirmation
   * and one refusal serve both ways in.
   */
  remove: [detail: ListDetail]
}>()

const { t, tc } = useLocale()

const {
  active,
  activeRequest,
  addError,
  addValuesDraft,
  addValuesOpen,
  composing,
  contents,
  contentsState,
  curating,
  dismissalBlocked,
  domainsDraft,
  domainsError,
  effectiveSources,
  enabledCount,
  entryMutations,
  feedError,
  feedFormat,
  feedFormats,
  feedOpen,
  feedURL,
  filter,
  importStatus,
  interactionBusy,
  libraryHref,
  membershipLabel,
  observing,
  onAddFeed,
  onAddValues,
  onAddValuesOpen,
  onCreate,
  onImportFile,
  onOpenChange,
  onRefreshSources,
  onRemove,
  onRemoveSource,
  onRenameCustom,
  onRetryRefresh,
  onSourcesOpenChange,
  onToggleRow,
  onToggleSource,
  openContents,
  originLabel,
  refreshError,
  refreshing,
  refreshSkipped,
  requestClose,
  rows,
  saving,
  sourceCount,
  sourceError,
  sourceLabel,
  sourceMutations,
  sources,
  sourcesOpen,
  titleDraft,
  titleError,
  toggleBlocked,
  visibleRows,
} = useListDetail(props, {
  close: () => emit('close'),
  created: (detail) => emit('created', detail),
  remove: (detail) => emit('remove', detail),
  updated: (detail) => emit('updated', detail),
})

const filterInput = ref<HTMLInputElement | null>(null)

// USlideover moves focus as soon as the sheet mounts. During the first source
// read the filter is disabled, so move focus once it is ready only when
// the keyboard is still on the sheet's own fallback control.
watch(contentsState, async (state) => {
  if (state !== 'ready' || props.list === null) return
  const request = activeRequest()
  const focused = document.activeElement
  await nextTick()
  await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()))
  const input = filterInput.value
  if (input === null || request !== activeRequest()) return
  const panel = input.closest('[role=dialog]')
  if (document.activeElement !== focused || !panel?.contains(focused)) return
  input.focus()
})
</script>

<template>
  <RvDialog
    adaptive
    :close-label="t('action.close')"
    :dismissible="!dismissalBlocked"
    :fill="list !== null && creating !== true"
    :open="active"
    :title="list === null ? t('listCard.new') : list.title"
    @update:open="onOpenChange"
  >
    <template v-if="composing && list !== null" #actions>
      <RvTooltip :text="t('listCard.openLibrary')">
        <a
          class="list-card__library-link"
          :aria-label="t('listCard.openLibrary')"
          :href="libraryHref"
          rel="noopener"
          target="_blank"
          ><RvIcon name="external"
        /></a>
      </RvTooltip>
    </template>
    <!-- Creation: a name and the first domains. -->
    <form v-if="creating" class="list-card__section" @submit.prevent="onCreate">
      <RvField
        :error="titleError"
        input-id="list-title"
        :label="t('listCard.title.field')"
      >
        <template #default="{ describedBy, invalid }">
          <RvTextInput
            v-model="titleDraft"
            :described-by="describedBy"
            :disabled="saving"
            input-id="list-title"
            :invalid="invalid"
            maxlength="120"
          />
        </template>
      </RvField>
      <RvField
        :error="domainsError"
        :hint="t('listCard.new.domains.hint')"
        input-id="list-domains"
        :label="t('listCard.new.domains.field')"
      >
        <template #default="{ describedBy, invalid }">
          <RvTextarea
            v-model="domainsDraft"
            :described-by="describedBy"
            :disabled="saving"
            input-id="list-domains"
            :invalid="invalid"
            :rows="6"
          />
        </template>
      </RvField>
      <div class="list-card__actions">
        <RvButton :disabled="saving" variant="quiet" @click="requestClose">
          {{ t('action.cancel') }}
        </RvButton>
        <RvButton
          :disabled="saving"
          :loading="saving"
          type="submit"
          variant="primary"
        >
          {{ saving ? t('listCard.saving') : t('listCard.create') }}
        </RvButton>
      </div>
    </form>

    <!-- One element owns the sheet's height, so the table below fills it and
         the card ends where the panel ends. -->
    <div v-else-if="list !== null" class="list-card__body">
      <!-- A custom list's name is the operator's and stays editable where the
           list itself is the subject. -->
      <section
        v-if="curating && list.custom === true"
        aria-labelledby="list-card-name"
        class="list-card__section"
      >
        <h3 id="list-card-name" class="list-card__visually-hidden">
          {{ t('listCard.title.field') }}
        </h3>
        <div class="list-card__rename">
          <RvField
            :error="titleError"
            input-id="list-title"
            :label="t('listCard.title.field')"
          >
            <template #default="{ describedBy, invalid }">
              <RvTextInput
                v-model="titleDraft"
                :described-by="describedBy"
                :disabled="saving"
                input-id="list-title"
                :invalid="invalid"
                maxlength="120"
              />
            </template>
          </RvField>
          <RvButton
            :disabled="saving || titleDraft.trim() === list.title"
            :loading="saving"
            variant="secondary"
            @click="onRenameCustom"
          >
            {{ t('listCard.title.save') }}
          </RvButton>
        </div>
      </section>

      <!-- The one contents table: domains, addresses and networks. The card
           opens on it, because it is what the card is for. -->
      <section
        aria-labelledby="list-card-contents"
        class="list-card__section list-card__section--base list-card__section--fill"
        :aria-busy="contentsState === 'loading'"
      >
        <div class="list-card__heading">
          <h3 id="list-card-contents">{{ t('listCard.domains') }}</h3>
          <RvInfoTip
            :label="t('listCard.domains.info')"
            :text="t('listCard.domains.intro')"
          />
          <strong class="list-card__count">
            {{
              contents === null
                ? '—'
                : tc('listCard.domains.count', enabledCount)
            }}
          </strong>
        </div>
        <div class="list-card__commands">
          <!-- Reading the sources is the frequent act; editing which sources
               there are is the rare one, so the frequent one is the control
               and the rare one opens a panel. -->
          <RvTooltip :text="t('listCard.refresh')">
            <RvButton
              class="list-card__refresh-button"
              :aria-label="`${t('listCard.refresh')}: ${t('listCard.sources.open', { count: sourceCount })}`"
              :disabled="interactionBusy || contentsState !== 'ready'"
              size="compact"
              type="button"
              variant="secondary"
              @click="onRefreshSources"
            >
              <RvIcon
                name="refresh"
                :class="{ 'list-card__refresh-icon--busy': refreshing }"
              />
              <span>{{
                t('listCard.sources.open', { count: sourceCount })
              }}</span>
            </RvButton>
          </RvTooltip>
          <RvTooltip v-if="curating" :text="t('listCard.sources.configure')">
            <RvButton
              class="list-card__sources-button"
              :aria-label="t('listCard.sources.configure')"
              :disabled="interactionBusy"
              size="compact"
              type="button"
              variant="secondary"
              @click="sourcesOpen = true"
              ><RvIcon name="settings"
            /></RvButton>
          </RvTooltip>

          <!-- Source reads keep one compact, reserved status row. The retry
               control is present but invisible until an error so the filter
               and the known rows do not move when the answer changes. -->
          <!-- The row changes its own role with the answer it carries, so no
               single accessible query names the row itself; a test that reads
               the height it reserves needs a hook of its own. -->
          <div
            class="list-card__refresh-status"
            data-testid="rv-list-card-status"
            :role="
              refreshError !== '' || contentsState === 'failed'
                ? 'alert'
                : 'status'
            "
          >
            <RvStatus
              v-if="contentsState === 'loading'"
              class="list-card__refresh-indicator"
              :label="t('listCard.loading')"
              tone="busy"
            />
            <RvStatus
              v-else-if="contentsState === 'failed'"
              class="list-card__refresh-indicator"
              :label="t('listCard.failed.body')"
              tone="failed"
            />
            <RvStatus
              v-else-if="refreshing"
              class="list-card__refresh-indicator"
              :label="
                t(observing ? 'listCard.observing' : 'listCard.refresh.busy')
              "
              tone="busy"
            />
            <RvStatus
              v-else-if="refreshError !== ''"
              class="list-card__refresh-indicator"
              :label="refreshError"
              tone="failed"
            />
            <RvStatus
              v-else-if="sources.length === 0"
              class="list-card__refresh-indicator"
              :label="t('listCard.refresh.none')"
              tone="waiting"
            />
            <RvTooltip
              v-else-if="contents?.observed === true"
              :text="t('listCard.refresh.ready')"
            >
              <span
                class="list-card__ready"
                role="img"
                :aria-label="t('listCard.refresh.ready')"
                tabindex="0"
                ><RvIcon name="check"
              /></span>
            </RvTooltip>
            <RvStatus
              v-else
              class="list-card__refresh-indicator"
              :label="t('listCard.refresh.waiting')"
              tone="waiting"
            />
            <RvButton
              class="list-card__refresh-retry"
              :aria-hidden="
                refreshError === '' && contentsState !== 'failed'
                  ? 'true'
                  : undefined
              "
              :disabled="
                (refreshError === '' && contentsState !== 'failed') ||
                interactionBusy
              "
              size="compact"
              type="button"
              @click="
                contentsState === 'failed' && list !== null
                  ? openContents(list.id)
                  : onRetryRefresh()
              "
            >
              {{ t('action.retry') }}
            </RvButton>
          </div>
        </div>

        <div class="list-card__toolbar">
          <label class="list-card__filter">
            <RvIcon name="search" />
            <span class="list-card__visually-hidden">
              {{ t('listCard.filter') }}
            </span>
            <input
              ref="filterInput"
              v-model="filter"
              :disabled="contentsState !== 'ready'"
              class="list-card__filter-input"
              :placeholder="t('listCard.filter')"
              type="search"
            />
          </label>
          <RvButton
            v-if="curating"
            :disabled="interactionBusy"
            type="button"
            variant="secondary"
            @click="addValuesOpen = true"
          >
            <RvIcon name="plus" />
            {{ t('listCard.domains.add') }}
          </RvButton>
        </div>

        <!-- Composing reads the list here; the one act this route owns is
               the footer. The per-list override a composition can carry
               replaces the catalog's domain seeds and nothing else, so a switch
               on this row could not take an observed rule out of this route —
               and it must not pretend to. Excluding one value from one route is
               a feature of its own, with its own model. -->
        <ul
          class="list-card__rows"
          :aria-label="composing ? t('listCard.domains') : undefined"
          :tabindex="composing ? 0 : undefined"
        >
          <template v-if="contentsState === 'loading'">
            <li
              v-for="index in 8"
              :key="index"
              class="list-card__loading-row"
              aria-hidden="true"
            />
          </template>
          <li
            v-for="row in visibleRows"
            :key="row.value"
            :class="{ 'list-card__row--disabled': !row.enabled }"
          >
            <label v-if="curating" class="list-card__switch">
              <input
                :checked="row.enabled"
                :disabled="toggleBlocked"
                type="checkbox"
                @change="
                  onToggleRow(row, ($event.target as HTMLInputElement).checked)
                "
              />
              <span class="list-card__row-copy">
                <span class="list-card__value">{{ row.value }}</span>
                <small>{{ originLabel(row) }}</small>
              </span>
            </label>
            <span v-else class="list-card__row-copy list-card__entry">
              <span class="list-card__value">{{ row.value }}</span>
              <small>{{ originLabel(row) }}</small>
              <small v-if="!row.enabled" class="list-card__row-state">
                {{ t('listCard.domains.disabledInLibrary') }}
              </small>
            </span>
            <template v-if="curating">
              <small
                v-if="entryMutations.isPending(row.value)"
                class="list-card__row-state"
                role="status"
              >
                {{ t('listCard.mutation.pending') }}
              </small>
              <span
                v-else-if="entryMutations.getState(row.value).error !== null"
                class="list-card__row-recovery"
                role="alert"
              >
                <small class="list-card__row-state">
                  {{ t('listCard.mutation.failed') }}
                </small>
                <button
                  :aria-label="
                    t('listCard.mutation.retry.aria', {
                      entry: row.value,
                    })
                  "
                  class="list-card__row-action"
                  type="button"
                  @click="entryMutations.retry(row.value)"
                >
                  {{ t('listCard.mutation.retry') }}
                </button>
              </span>
            </template>
          </li>
        </ul>
        <p
          v-if="contentsState === 'ready' && rows.length === 0"
          class="list-card__muted"
          role="status"
        >
          {{ t('listCard.domains.empty') }}
        </p>
        <p
          v-else-if="contentsState === 'ready' && visibleRows.length === 0"
          class="list-card__muted"
          role="status"
        >
          {{ t('listCard.filter.empty') }}
        </p>

        <p v-if="importStatus !== ''" class="list-card__muted" role="status">
          {{ importStatus }}
        </p>
        <p v-if="refreshSkipped > 0" class="list-card__muted" role="status">
          {{ tc('listCard.refresh.skipped', refreshSkipped) }}
        </p>
        <!-- Composing leaves nothing under the table: the sheet's height is the
             table's, and the footer sits directly under its last row. -->
        <p v-if="curating" class="list-card__aside">
          <button
            class="list-card__link list-card__link--grave"
            :disabled="interactionBusy"
            type="button"
            @click="onRemove"
          >
            <RvIcon name="trash" />
            {{ t('listCard.remove') }}
          </button>
        </p>
      </section>
    </div>

    <template v-if="composing && !creating" #footer>
      <p class="list-card__membership">
        <RvStatus
          :label="membershipLabel"
          :tone="included === true ? 'ready' : 'waiting'"
        />
        <small v-if="pending">{{ t('listCard.pending') }}</small>
      </p>
      <RvButton
        :disabled="interactionBusy"
        variant="primary"
        @click="emit('include', included !== true)"
      >
        {{ included === true ? t('listDetail.remove') : t('listDetail.add') }}
      </RvButton>
    </template>
  </RvDialog>

  <!-- Adding is one bounded act taken over the table, not a form growing out of
       the bottom of it: the panel arrives where the pointer already is, and the
       table it adds to stays visible behind it. -->
  <RvDialog
    :close-label="t('action.close')"
    :dismissible="!interactionBusy"
    :open="curating && addValuesOpen && list !== null"
    :title="t('listCard.domains.add')"
    variant="panel"
    @update:open="onAddValuesOpen"
  >
    <form
      id="list-add-entries-form"
      class="list-card__entries"
      @submit.prevent="onAddValues"
    >
      <RvField
        :error="addError"
        :hint="t('listCard.domains.hint')"
        input-id="list-add-entries"
        :label="t('listCard.domains.field')"
      >
        <template #default="{ describedBy, invalid }">
          <RvTextarea
            v-model="addValuesDraft"
            :described-by="describedBy"
            :disabled="interactionBusy"
            input-id="list-add-entries"
            :invalid="invalid"
            :rows="6"
          />
        </template>
      </RvField>
      <div class="list-card__entries-import">
        <RvFilePicker
          accept=".txt,.bat,.lst,.json,.csv,text/plain"
          :action-label="t('listCard.import')"
          :disabled="interactionBusy"
          :empty-label="t('listCard.import.none')"
          :hint="t('listCard.import.hint')"
          input-id="list-card-import"
          :label="t('listCard.import')"
          @select="onImportFile"
        />
      </div>
    </form>
    <template #footer>
      <RvButton
        :disabled="interactionBusy"
        variant="quiet"
        @click="onAddValuesOpen(false)"
      >
        {{ t('action.cancel') }}
      </RvButton>
      <RvButton
        :disabled="interactionBusy"
        :loading="saving"
        form="list-add-entries-form"
        type="submit"
        variant="primary"
      >
        {{ t('listCard.domains.submit') }}
      </RvButton>
    </template>
  </RvDialog>

  <!-- Which feeds this list reads is a set of managed objects, not a preamble
       to the table: it is opened when it is the subject and closed the rest of
       the time. -->
  <RvDialog
    :close-label="t('action.close')"
    :dismissible="!interactionBusy"
    :open="curating && sourcesOpen && list !== null"
    :title="t('listCard.sources')"
    variant="panel"
    @update:open="onSourcesOpenChange"
  >
    <section class="list-card__section">
      <p v-if="sourceError !== ''" class="list-card__error" role="alert">
        {{ sourceError }}
      </p>

      <ul v-if="sources.length > 0" class="list-card__rows">
        <li v-for="source in effectiveSources" :key="source.id">
          <label class="list-card__switch">
            <input
              :checked="source.enabled"
              :disabled="toggleBlocked"
              type="checkbox"
              @change="
                onToggleSource(
                  source.id,
                  ($event.target as HTMLInputElement).checked,
                )
              "
            />
            <span class="list-card__row-copy">
              <strong>
                {{
                  source.custom && source.url !== '' ? source.url : source.id
                }}
              </strong>
              <small>{{ sourceLabel(source.id, source.type) }}</small>
            </span>
          </label>
          <template v-if="sourceMutations.isPending(source.id)">
            <small class="list-card__row-state" role="status">
              {{ t('listCard.mutation.pending') }}
            </small>
          </template>
          <span
            v-else-if="sourceMutations.getState(source.id).error !== null"
            class="list-card__row-recovery"
            role="alert"
          >
            <small class="list-card__row-state">
              {{ t('listCard.mutation.failed') }}
            </small>
            <button
              :aria-label="
                t('listCard.mutation.retry.aria', { entry: source.id })
              "
              class="list-card__row-action"
              type="button"
              @click="sourceMutations.retry(source.id)"
            >
              {{ t('listCard.mutation.retry') }}
            </button>
          </span>
          <button
            v-if="source.custom"
            :aria-label="t('listCard.feed.remove.aria', { source: source.url })"
            class="list-card__row-action"
            :disabled="interactionBusy"
            type="button"
            @click="onRemoveSource(source.id)"
          >
            {{ t('listCard.feed.remove') }}
          </button>
        </li>
      </ul>
      <p v-else class="list-card__muted">
        {{ t('listCard.sources.none') }}
      </p>

      <button
        v-if="!feedOpen"
        class="list-card__add"
        :disabled="interactionBusy"
        type="button"
        @click="feedOpen = true"
      >
        <RvIcon name="plus" />
        {{ t('listCard.feed.add') }}
      </button>
      <form v-else class="list-card__inline-form" @submit.prevent="onAddFeed">
        <RvField
          :error="feedError"
          :hint="t('listCard.feed.hint')"
          input-id="list-feed-url"
          :label="t('listCard.feed.url')"
        >
          <template #default="{ describedBy, invalid }">
            <RvTextInput
              v-model="feedURL"
              :described-by="describedBy"
              :disabled="interactionBusy"
              input-id="list-feed-url"
              :invalid="invalid"
              placeholder="https://example.com/list.txt"
            />
          </template>
        </RvField>
        <RvField input-id="list-feed-format" :label="t('listCard.feed.format')">
          <template #default="{ describedBy }">
            <RvSelect
              v-model="feedFormat"
              :described-by="describedBy"
              :disabled="interactionBusy"
              input-id="list-feed-format"
              :options="feedFormats"
              :placeholder="t('listCard.feed.format')"
            />
          </template>
        </RvField>
        <div class="list-card__actions">
          <RvButton
            :disabled="interactionBusy"
            size="compact"
            variant="quiet"
            @click="feedOpen = false"
          >
            {{ t('action.cancel') }}
          </RvButton>
          <RvButton
            :disabled="interactionBusy"
            :loading="saving"
            size="compact"
            type="submit"
            variant="secondary"
          >
            {{ t('listCard.feed.submit') }}
          </RvButton>
        </div>
      </form>
    </section>
  </RvDialog>
</template>

<style scoped>
/* The sheet's body, as one column: the sections above keep their height and the
   contents section takes everything that is left. */
.list-card__body {
  display: flex;
  flex-direction: column;
  min-height: 0;
  overflow-y: auto;
}

.list-card__section {
  display: grid;
  gap: var(--rv-space-4);
  padding: var(--rv-space-5) var(--rv-space-6);
}

.list-card__section + .list-card__section {
  border-top: var(--rv-border-hair) solid var(--rv-color-rule-strong);
}

.list-card__section--base {
  background: var(--rv-color-surface);
}

/* A column rather than a grid, because exactly one of its children — the
   table — is allowed to take the height the others do not need. */
.list-card__section--fill {
  display: flex;
  flex: 1;
  flex-direction: column;
  gap: var(--rv-space-4);
  min-height: min-content;
}

.list-card__heading {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-2) var(--rv-space-3);
  align-items: center;
}

.list-card__heading h3 {
  font-size: var(--rv-text-interface);
}

.list-card__count {
  margin-left: auto;
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
}

/* A fact the composing card states but does not offer to change: the same
   words as the control beside the table in the other flow, without the box
   that would promise something to press. */
.list-card__fact {
  color: var(--rv-color-ink-tertiary);
  font-size: var(--rv-text-dense);
}

/* A named action that is not the point of the screen: it reads as a control
   rather than a link, and it says what it opens and how much is in there. */
.list-card__quiet {
  display: inline-flex;
  gap: var(--rv-space-2);
  align-items: center;
  min-height: var(--rv-control-compact);
  padding: 0 var(--rv-space-2);
  color: var(--rv-color-ink-muted);
  font: inherit;
  font-size: var(--rv-text-dense);
  background: transparent;
  border: var(--rv-border-hair) solid var(--rv-color-rule);
  border-radius: var(--rv-radius-sm);
  cursor: pointer;
}

.list-card__quiet:hover {
  color: var(--rv-color-ink);
  background: var(--rv-color-surface-hover);
}

.list-card__quiet:disabled {
  color: var(--rv-color-ink-tertiary);
  background: transparent;
  opacity: var(--rv-disabled-opacity);
  cursor: not-allowed;
}

.list-card__muted {
  max-width: var(--rv-measure-prose);
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
}

.list-card__error {
  color: var(--rv-color-status-failed);
  font-size: var(--rv-text-dense);
}

/* Source reads are a compact fact beside the table. Keeping the retry slot in
   the row even while it is hidden means loading, failure and recovery share
   the same geometry; the row itself can still grow for translated or zoomed
   copy instead of clipping it. */
.list-card__refresh-status {
  flex: 1 1 var(--rv-composer-field-width);
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: var(--rv-space-3);
  align-items: center;
  min-width: 0;

  /* A compact status and its Retry control share the same minimum height.
     Longer failure explanations may wrap when recovery needs more context. */
  min-height: var(--rv-control-compact);
}

.list-card__refresh-indicator {
  flex: 1 1 auto;
  min-width: 0;
}

.list-card__refresh-retry {
  flex: none;
}

.list-card__refresh-retry[aria-hidden='true'] {
  visibility: hidden;
}

.list-card__aside {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-3);
  align-items: center;
}

.list-card__link {
  display: inline-flex;
  gap: var(--rv-space-2);
  align-items: center;
  min-height: var(--rv-control-compact);
  padding: 0 var(--rv-space-2);
  color: var(--rv-color-accent-ink);
  font: inherit;
  font-size: var(--rv-text-dense);
  text-decoration: none;
  background: transparent;
  border: 0;
  border-radius: var(--rv-radius-sm);
  cursor: pointer;
}

.list-card__link:hover {
  background: var(--rv-color-accent-quiet);
}

.list-card__link--grave {
  color: var(--rv-color-status-failed);
}

.list-card__link--grave:hover {
  background: var(--rv-color-surface-hover);
}

.list-card__link:disabled {
  opacity: var(--rv-disabled-opacity);
  cursor: not-allowed;
}

.list-card__rename {
  display: flex;
  gap: var(--rv-space-3);
  align-items: flex-end;
}

.list-card__rename > :first-child {
  flex: 1;
}

.list-card__visually-hidden {
  position: absolute;
  width: 0.0625rem;
  height: 0.0625rem;
  overflow: hidden;
  white-space: nowrap;
  clip-path: inset(50%);
}

/* The filter and the way in share one line above the table: the control that
   adds a row belongs where the rows are, not below everything they say. */
.list-card__toolbar {
  display: flex;
  gap: var(--rv-space-3);
  align-items: center;
}

.list-card__filter {
  display: flex;
  flex: 1;
  gap: var(--rv-space-2);
  align-items: center;
  min-width: 0;
  min-height: var(--rv-control-touch);
  padding: 0 var(--rv-space-3);
  color: var(--rv-color-ink-tertiary);
  background: var(--rv-color-canvas);
  border: var(--rv-border-hair) solid var(--rv-color-rule-strong);
  border-radius: var(--rv-radius-sm);
}

.list-card__filter:focus-within {
  outline: var(--rv-border-mark) solid var(--rv-color-focus);
  outline-offset: var(--rv-border-hair);
}

.list-card__filter-input {
  flex: 1;
  min-width: 0;

  /* The wrapper owns the height; a minimum here would add the wrapper's
     border on top of it and leave the field two pixels taller than the
     controls beside it. */
  align-self: stretch;
  padding: 0;
  color: var(--rv-color-ink);
  font: inherit;
  background: transparent;
  border: 0;
  outline: 0;
}

.list-card__filter-input::placeholder {
  color: var(--rv-color-ink-tertiary);
}

/* The table keeps its own scroll so the heading above it and the actions below
   it stay in place. In a panel it is capped; in the sheet it claims the height
   the sheet actually has, which is what keeps the footer off an empty band. */
.list-card__rows {
  display: grid;
  grid-auto-rows: max-content;
  align-content: start;
  max-height: var(--rv-picker-height);
  overflow-y: auto;
  overscroll-behavior: contain;
  margin: 0;
  padding: 0;
  list-style: none;
  border-top: var(--rv-border-hair) solid var(--rv-color-rule);
}

.list-card__section--fill .list-card__rows {
  flex: 1;
  max-height: none;

  /* Keep a usable table when enlarged controls consume the sheet. Size
     containment excludes the entire list from the section's intrinsic height;
     the body can then scroll its controls without replacing the table scroll. */
  min-height: calc(var(--rv-row-default) * 2);
  contain: size;
}

.list-card__loading-row::before {
  content: '';
  display: block;
  width: 60%;
  height: var(--rv-space-4);
  margin-block: var(--rv-space-3);
  background: var(--rv-color-surface-muted);
  border-radius: var(--rv-radius-sm);
}

.list-card__rows li {
  display: flex;
  gap: var(--rv-space-4);
  align-items: center;
  justify-content: space-between;
  min-height: var(--rv-row-default);
  border-bottom: var(--rv-border-hair) solid var(--rv-color-rule);
}

.list-card__switch {
  display: flex;
  flex: 1;
  gap: var(--rv-space-3);
  align-items: center;
  min-width: 0;
  min-height: var(--rv-control-touch);
  padding: var(--rv-space-2) 0;
  cursor: pointer;
}

.list-card__switch input {
  flex: none;
  width: var(--rv-control-choice);
  height: var(--rv-control-choice);
  margin: 0;
  accent-color: var(--rv-color-accent);
}

.list-card__switch input:focus-visible {
  outline: var(--rv-border-mark) solid var(--rv-color-focus);
  outline-offset: var(--rv-border-hair);
}

.list-card__row-copy {
  display: grid;
  gap: var(--rv-space-1);
  min-width: 0;
}

/* A composing row is read, not pressed, so it takes the switch's box without
   the switch and the two flows still read as one table. */
.list-card__entry {
  flex: 1;
  align-content: center;
  min-height: var(--rv-control-touch);
  padding: var(--rv-space-2) 0;
}

.list-card__row-copy strong {
  overflow-wrap: anywhere;
}

.list-card__row-copy small {
  color: var(--rv-color-ink-tertiary);
  font-size: var(--rv-text-meta);
}

.list-card__row-recovery {
  display: inline-flex;
  flex: none;
  flex-wrap: wrap;
  gap: var(--rv-space-1) var(--rv-space-2);
  align-items: center;
}

.list-card__row-recovery .list-card__row-state {
  color: var(--rv-color-status-failed);
}

.list-card__value {
  font-family: var(--rv-font-mono);
  font-size: var(--rv-text-dense);
  overflow-wrap: anywhere;
}

.list-card__row-action {
  flex: none;
  min-height: var(--rv-control-compact);
  padding: 0 var(--rv-space-2);
  color: var(--rv-color-ink-muted);
  font: inherit;
  font-size: var(--rv-text-dense);
  background: transparent;
  border: 0;
  border-radius: var(--rv-radius-sm);
  cursor: pointer;
}

.list-card__row-action:hover {
  color: var(--rv-color-ink);
  background: var(--rv-color-surface-hover);
}

.list-card__add {
  display: inline-flex;
  gap: var(--rv-space-2);
  align-items: center;
  justify-self: start;
  min-height: var(--rv-control-compact);
  padding: 0 var(--rv-space-2);
  color: var(--rv-color-accent-ink);
  font: inherit;
  font-weight: 600;
  font-size: var(--rv-text-dense);
  background: transparent;
  border: 0;
  border-radius: var(--rv-radius-sm);
  cursor: pointer;
}

.list-card__add:hover {
  background: var(--rv-color-accent-quiet);
}

.list-card__add:disabled {
  background: transparent;
  opacity: var(--rv-disabled-opacity);
  cursor: not-allowed;
}

/* The file control is driven by the button beside it. It stays in the document
   so the browser can open the picker, and out of the tab order so the operator
   meets one control rather than two. */
.list-card__file {
  position: absolute;
  width: 0.0625rem;
  height: 0.0625rem;
  overflow: hidden;
  clip-path: inset(50%);
}

.list-card__add:focus-visible,
.list-card__link:focus-visible,
.list-card__row-action:focus-visible,
.list-card__quiet:focus-visible {
  outline: var(--rv-border-mark) solid var(--rv-color-focus);
  outline-offset: var(--rv-focus-offset);
}

.list-card__entries {
  display: grid;
  gap: var(--rv-space-4);
  padding: var(--rv-space-5) var(--rv-space-6);
}

/* Reading a file the operator already has is the same act as typing into the
   field above, so it stands beside it rather than under a heading of its own. */
.list-card__entries-import {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-2) var(--rv-space-3);
  align-items: center;
}

.list-card__inline-form {
  display: grid;
  gap: var(--rv-space-3);
  padding: var(--rv-space-4);
  background: var(--rv-color-surface-muted);
  border-radius: var(--rv-radius-md);
}

.list-card__actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-2);
  justify-content: flex-end;
}

/* One line: where the list stands in this route, and — while the route is an
   unsaved draft — that the answer is waiting on a save. */
.list-card__membership {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-2);
  align-items: baseline;
  min-width: 0;
}

.list-card__membership small {
  color: var(--rv-color-ink-tertiary);
  font-size: var(--rv-text-meta);
}

/* Disabled library values remain fully opaque so their value, origin and
   state each keep AA contrast. Text, rather than faded containment, carries
   the distinction for every theme. */
.list-card__row--disabled {
  color: var(--rv-color-ink-muted);
}

.list-card__row--disabled .list-card__value,
.list-card__row--disabled .list-card__row-copy small {
  color: var(--rv-color-ink-muted);
}

.list-card__row--disabled .list-card__row-state {
  color: var(--rv-color-ink);
  font-weight: 600;
}

@container dialog (width <= 36rem) {
  .list-card__section,
  .list-card__entries {
    padding-right: var(--rv-space-4);
    padding-left: var(--rv-space-4);
  }

  /* Two controls that no longer fit one line take two rather than shrinking
     the filter to nothing. */
  .list-card__toolbar {
    flex-wrap: wrap;
  }

  .list-card__rename {
    align-items: stretch;
    flex-direction: column;
  }

  .list-card__actions > * {
    flex: 1 1 auto;
  }
}

.list-card__commands {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--rv-space-2) var(--rv-space-3);
}

.list-card__refresh-button {
  flex: none;
}

.list-card__sources-button {
  flex: none;
  width: var(--rv-control-compact);
  padding: 0;
}

.list-card__ready {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  justify-self: start;
  width: var(--rv-control-compact);
  height: var(--rv-control-compact);
  color: var(--rv-color-status-ready);
}

.list-card__library-link {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: none;
  width: var(--rv-control-touch);
  height: var(--rv-control-touch);
  color: var(--rv-color-ink-muted);
  border-radius: var(--rv-radius-sm);
}

.list-card__library-link:hover {
  background: var(--rv-color-surface-hover);
}

.list-card__refresh-icon--busy {
  animation: list-card-refresh var(--rv-motion-working) linear infinite;
}

@keyframes list-card-refresh {
  to {
    transform: rotate(360deg);
  }
}

@media (prefers-reduced-motion: reduce) {
  .list-card__refresh-icon--busy {
    animation: none;
  }
}
</style>
