<script setup lang="ts">
import RvTable from '@/shared/ui/RvTable.vue'
import RvPageHeader from '@/shared/ui/RvPageHeader.vue'
import { computed, onMounted, ref } from 'vue'

import { profileHref } from '@/features/profiles/lib/profileHref'
import { useProfileMenu } from '@/features/profiles/model/useProfileMenu'
import { useProfiles } from '@/features/profiles/model/useProfiles'
import ProfileDeleteDialog from '@/features/profiles/ui/ProfileDeleteDialog.vue'
import {
  cleanupRefusalKey,
  useDeviceCleanup,
} from '@/entities/device-cleanup/model/useDeviceCleanup'
import DeviceCleanupChoice from '@/entities/device-cleanup/ui/DeviceCleanupChoice.vue'
import { useLocale } from '@/shared/i18n/useLocale'
import type { ProfileCard } from '@/shared/api/profiles'
import { legacyImportedOrdinal } from '@/shared/lib/legacyProfile'
import { parseLegacyProfileHash } from '@/shared/lib/profileHash'
import { targetIcon } from '@/shared/lib/targetIcon'
import type { StatusTone } from '@/shared/ui/types'
import RvButton from '@/shared/ui/RvButton.vue'
import RvIcon from '@/shared/ui/RvIcon.vue'
import RvInfoTip from '@/shared/ui/RvInfoTip.vue'
import RvMenu from '@/shared/ui/RvMenu.vue'
import RvSegmented from '@/shared/ui/RvSegmented.vue'
import RvStateNotice from '@/shared/ui/RvStateNotice.vue'
import RvStatus from '@/shared/ui/RvStatus.vue'

const { dateTime, t, tc } = useLocale()
const library = useProfiles()
const { menuItems } = useProfileMenu(library)
const route = useRoute()
const router = useRouter()

const openRoute = (event: MouseEvent, card: ProfileCard): void => {
  if (
    event.defaultPrevented ||
    event.button !== 0 ||
    event.ctrlKey ||
    event.metaKey ||
    event.shiftKey ||
    event.altKey
  )
    return
  const target = event.target
  if (
    target instanceof Element &&
    target.closest('a, button, input, select, [role="menu"]')
  )
    return
  if (window.getSelection()?.toString()) return
  void router.push(profileHref(card))
}

const archivedShelf = computed(() => library.shelf.value === 'archived')

const displayName = (card: ProfileCard): string => {
  const ordinal = legacyImportedOrdinal(card.name)
  return ordinal === null
    ? card.name
    : t('profiles.imported.name', { number: ordinal })
}

const nameOf = (id: string): string => {
  const card = library.rows.value.find((candidate) => candidate.id === id)
  return card === undefined ? '' : displayName(card)
}

// The outcome of the last action taken on the shelf. Each action withdraws the
// previous outcome when it starts, so at most one of these is ever set. A
// failure carries a mark as well as words.
const outcome = computed((): { text: string; tone?: StatusTone } | null => {
  if (library.rebuilding.value !== '')
    return {
      text: t('profiles.rebuilding', {
        name: nameOf(library.rebuilding.value),
      }),
      tone: 'busy',
    }
  if (library.copiedID.value !== '') return { text: t('profiles.copied') }
  if (library.rebuiltID.value !== '')
    return {
      text: t('profiles.rebuilt', { name: nameOf(library.rebuiltID.value) }),
    }
  if (library.justRestored.value !== null)
    return {
      text: t('profiles.restore.done', {
        name: displayName(library.justRestored.value),
      }),
    }
  if (library.justDeleted.value !== null)
    return {
      text: t('profiles.delete.done', {
        name: displayName(library.justDeleted.value),
      }),
    }
  if (library.copyFailedID.value !== '')
    return { text: t('profiles.copyFailed'), tone: 'failed' }
  if (library.exportFailedID.value !== '')
    return { text: t('profiles.export.failed'), tone: 'failed' }
  if (library.rebuildFailedID.value !== '')
    return {
      text: t('profiles.rebuild.failed', {
        name: nameOf(library.rebuildFailedID.value),
      }),
      tone: 'failed',
    }
  const refused = library.shelveFailed.value
  if (refused !== null) {
    const name = nameOf(refused.id)
    return {
      text: refused.archive
        ? t('profiles.archive.failed', { name })
        : t('profiles.restore.failed', { name }),
      tone: 'failed',
    }
  }
  return null
})

const importedCount = computed(
  () =>
    library.rows.value.filter(
      (card) => legacyImportedOrdinal(card.name) !== null,
    ).length,
)

// The filter is offered while there is an archive to look at, and stays while
// the archive is what is being looked at, even if it just emptied.
const showFilter = computed(
  () => library.archivedCount.value > 0 || archivedShelf.value,
)
const shelfOptions = computed(() => [
  { label: t('profiles.filter.active'), value: 'active' },
  {
    label: t('profiles.filter.archived', {
      count: library.archivedCount.value,
    }),
    value: 'archived',
  },
])
const onShelf = (value: string): void => {
  if (value === 'active' || value === 'archived')
    void library.selectShelf(value)
}

onMounted(() => {
  // The retired drawer address `/#list={id}` still resolves: an old bookmark
  // lands on the profile's page instead of an empty shelf.
  const legacyID = parseLegacyProfileHash(route.hash)
  if (legacyID !== '') {
    void router.replace(`/profiles/${legacyID}`)
    return
  }
  void library.initialize()
})

const updatedAt = (card: ProfileCard): string => {
  const output = library
    .publishedOutputs(card)
    .sort((left, right) =>
      (right.latest?.contentCreatedAt ?? '').localeCompare(
        left.latest?.contentCreatedAt ?? '',
      ),
    )[0]
  if (output?.latest === null || output === undefined)
    return t('profiles.noArtifact')
  const parsed = new Date(output.latest.contentCreatedAt)
  return Number.isNaN(parsed.valueOf()) ? '—' : dateTime.value.format(parsed)
}

const archivedSince = (card: ProfileCard): string => {
  const parsed = new Date(card.archivedAt)
  return Number.isNaN(parsed.valueOf()) ? '—' : dateTime.value.format(parsed)
}

// Permanent deletion asks first, and says what it leaves behind. The row it is
// asked about is held here until the answer, because the model's rows change
// the moment the server agrees.
const pendingDelete = ref<ProfileCard | null>(null)

// Deleting can also remove what the profile's formats delivered to routers;
// the question reads what that would be when it opens.
const cleanup = useDeviceCleanup()
const {
  chosen: removeFromDevices,
  objects: cleanupObjects,
  requested: cleanupRequested,
  state: cleanupState,
} = cleanup
// The deletion stopped at a router after asking to clean them: what is left
// there is read again and the question says the profile stayed.
const cleanupStopped = ref(false)

const askDelete = (card: ProfileCard): void => {
  library.deleteFailure.value = ''
  cleanupStopped.value = false
  cleanup.open({ kind: 'profile', id: card.id })
  pendingDelete.value = card
}

const confirmDelete = async (): Promise<void> => {
  const card = pendingDelete.value
  if (card === null) return
  cleanupStopped.value = false
  const cleaning = cleanupRequested.value
  const done = await library.removeForever(card, cleaning)
  if (done || library.deleteFailure.value === 'gone') {
    pendingDelete.value = null
    return
  }
  if (cleaning) {
    cleanupStopped.value = true
    void cleanup.reread()
  }
}

// A deletion that stopped at a router names why, in the words a send uses.
const cleanupRefusal = computed(() => {
  const key = cleanupRefusalKey(library.deleteRefusal.value)
  return key === '' ? '' : t(key)
})

const deleteFailureText = computed(() => {
  const card = pendingDelete.value
  if (card === null) return ''
  switch (library.deleteFailure.value) {
    case 'notArchived':
      return t('profiles.delete.notArchived')
    case 'gone':
      return t('profiles.delete.gone')
    case 'failed':
      return cleanupRefusal.value === ''
        ? t('profiles.delete.failed', { name: displayName(card) })
        : cleanupRefusal.value
    default:
      return ''
  }
})

const undoArchive = (): void => {
  const card = library.justArchived.value
  if (card !== null) void library.setArchived(card, false)
}

const onMenu = (card: ProfileCard, key: string): void => {
  if (key.startsWith('export:'))
    void library.exportFile(card, key.slice('export:'.length))
  if (key.startsWith('copy:')) {
    const output = card.outputs.find(
      (candidate) => candidate.id === key.slice('copy:'.length),
    )
    if (output !== undefined) void library.copyContents(card, output)
  }
  if (key === 'rebuild') void library.rebuild(card)
  if (key === 'archive') void library.setArchived(card, true)
  if (key === 'restore') void library.setArchived(card, false)
  if (key === 'delete') askDelete(card)
}
</script>

<template>
  <section aria-labelledby="profiles-title" class="profiles">
    <RvPageHeader title-id="profiles-title" :title="t('profiles.title')">
      <p class="profiles__outcome" role="status">
        <RvStatus
          v-if="outcome !== null && outcome.tone !== undefined"
          :label="outcome.text"
          :tone="outcome.tone"
        />
        <template v-else>{{ outcome?.text }}</template>
      </p>
      <RvButton size="touch" to="/profiles/new" variant="primary">
        <RvIcon name="plus" />
        {{ t('profiles.new') }}
      </RvButton>
    </RvPageHeader>

    <RvStateNotice
      v-if="library.justArchived.value !== null"
      live
      :title="
        t('profiles.archive.done', {
          name: displayName(library.justArchived.value),
        })
      "
      tone="ready"
    >
      <template #action>
        <RvButton
          :disabled="library.shelving.value !== ''"
          @click="undoArchive"
        >
          {{ t('profiles.archive.undo') }}
        </RvButton>
      </template>
    </RvStateNotice>

    <RvStateNotice
      v-if="library.state.value === 'ready' && importedCount > 0"
      :body="t('profiles.imported.body')"
      :title="tc('profiles.imported.title', importedCount)"
      tone="waiting"
    />

    <div v-if="showFilter" class="profiles__filter">
      <RvSegmented
        label-hidden
        :label="t('profiles.filter.label')"
        :model-value="library.shelf.value"
        name="profiles-shelf"
        :options="shelfOptions"
        @update:model-value="onShelf"
      />
      <RvInfoTip
        :label="t('profiles.archived.info')"
        :text="t('profiles.archived.body')"
      />
    </div>

    <RvStateNotice
      v-if="library.state.value === 'loading'"
      live
      :title="t('profiles.loading')"
      tone="busy"
    />
    <RvStateNotice
      v-else-if="library.state.value === 'failed'"
      :body="t(`failure.${library.failure.value}.body`)"
      live
      :title="t('profiles.failed')"
      tone="failed"
    >
      <template #action>
        <RvButton @click="library.initialize">
          {{ t('action.retry') }}
        </RvButton>
      </template>
    </RvStateNotice>
    <!-- The page header carries the one primary action, so the empty shelf does
         not repeat it. -->
    <div v-else-if="library.state.value === 'empty'" class="profiles__empty">
      <p class="profiles__empty-title">{{ t('profiles.empty') }}</p>
      <p class="profiles__empty-body">{{ t('profiles.empty.body') }}</p>
    </div>

    <!-- The scroll box the table lives in. It is a presentational containing
         block with no role and no name, and a test reads its own scroll
         extent, so it carries a test hook. -->
    <RvTable
      v-else-if="library.rows.value.length > 0"
      class="profiles__scroll"
      data-testid="rv-profiles-scroll"
    >
      <thead>
        <tr>
          <th scope="col">{{ t('profiles.column.name') }}</th>
          <th scope="col">{{ t('profiles.column.outputs') }}</th>
          <th scope="col">{{ t('profiles.column.updated') }}</th>
          <th v-if="archivedShelf" scope="col">
            {{ t('profiles.column.archived') }}
          </th>
          <th scope="col">
            <span class="rv-visually-hidden">
              {{ t('profiles.column.actions') }}
            </span>
          </th>
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="card in library.rows.value"
          :key="card.id"
          class="profiles__row"
          @click="openRoute($event, card)"
        >
          <td class="profiles__cell-name">
            <NuxtLink class="profiles__link" :to="profileHref(card)">
              {{ displayName(card) }}
            </NuxtLink>
            <span
              v-if="library.composition(card) !== displayName(card)"
              class="profiles__lists"
            >
              {{ library.composition(card) }}
            </span>
          </td>
          <td
            class="profiles__cell-outputs"
            :data-label="t('profiles.column.outputs')"
            :class="{
              'profiles__cell-outputs--none': card.outputs.length === 0,
            }"
          >
            <span v-if="card.outputs.length === 0">
              {{ t('profiles.noOutputs') }}
            </span>
            <ul v-else class="profiles__formats">
              <li
                v-for="output in card.outputs"
                :key="output.id"
                class="profiles__format"
              >
                <RvIcon :name="targetIcon(output.targetID)" />
                {{ library.outputTitle(output) }}
              </li>
            </ul>
          </td>
          <td
            class="profiles__cell-updated"
            :data-label="t('profiles.column.updated')"
            :class="{
              'profiles__cell-updated--none':
                library.downloadable(card) === null &&
                !library.buildFailed(card),
            }"
          >
            <div class="profiles__updated">
              <RvStatus
                v-if="library.buildFailed(card)"
                :label="t('profiles.buildFailed')"
                tone="failed"
              />
              <span
                v-if="
                  !library.buildFailed(card) ||
                  library.downloadable(card) !== null
                "
              >
                {{ updatedAt(card) }}
              </span>
            </div>
          </td>
          <td
            v-if="archivedShelf"
            class="profiles__cell-archived"
            :data-label="t('profiles.column.archived')"
          >
            {{ archivedSince(card) }}
          </td>
          <td
            class="profiles__cell-actions"
            :data-label="t('profiles.column.actions')"
          >
            <div class="profiles__actions">
              <RvMenu
                :items="menuItems(card)"
                :label="t('profiles.menu', { name: displayName(card) })"
                @select="(key) => onMenu(card, key)"
              />
            </div>
          </td>
        </tr>
      </tbody>
    </RvTable>
    <RvStateNotice
      v-else
      :body="t('profiles.allArchived.body')"
      :title="t('profiles.allArchived')"
      tone="waiting"
    />

    <div
      v-if="library.state.value === 'ready' && library.nextCursor.value !== ''"
      class="profiles__pagination"
    >
      <p v-if="library.moreFailed.value" role="status">
        {{ t('profiles.loadMore.failed') }}
      </p>
      <RvButton
        :loading="library.loadingMore.value"
        :loading-label="t('profiles.loading')"
        @click="library.loadMore"
      >
        {{
          library.moreFailed.value ? t('action.retry') : t('profiles.loadMore')
        }}
      </RvButton>
    </div>

    <ProfileDeleteDialog
      :blocked="library.deleteFailure.value === 'notArchived'"
      :busy="library.deleting.value !== ''"
      :failure="deleteFailureText"
      :name="pendingDelete === null ? null : displayName(pendingDelete)"
      :removes-from-devices="cleanupRequested"
      @cancel="pendingDelete = null"
      @confirm="confirmDelete"
    >
      <template v-if="cleanupStopped" #error>
        <p>{{ t('cleanup.stopped.profile') }}</p>
      </template>
      <DeviceCleanupChoice
        v-model="removeFromDevices"
        :disabled="library.deleting.value !== ''"
        :objects="cleanupObjects"
        :state="cleanupState"
      />
    </ProfileDeleteDialog>
  </section>
</template>

<style scoped>
.profiles {
  container: routes / inline-size;
  display: grid;
  gap: var(--rv-space-4);
  width: 100%;
}

/* The outcome of the last action is announced and shown without moving the
   table below it. */
.profiles__outcome {
  min-height: 1.25rem;
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
}

/* The filter and what explains it share one line above the table. */
.profiles__filter {
  display: flex;
  gap: var(--rv-space-2);
  align-items: center;
}

.profiles__empty {
  display: grid;
  gap: var(--rv-space-2);
  justify-items: start;
  padding: var(--rv-space-6) 0;
}

.profiles__empty-title {
  font-weight: 600;
  font-size: var(--rv-text-module);
}

.profiles__empty-body {
  color: var(--rv-color-ink-muted);
}

/* The table scrolls inside its own box on narrow screens; the page never
   scrolls sideways. The box is also the containing block, so the hidden
   column header cannot escape it and widen the document. */
.profiles__scroll {
  --rv-table-min-width: var(--rv-profiles-table-width);
}

.profiles__cell-name {
  min-width: 13rem;
  font-weight: 600;
}

.profiles__link {
  display: inline-flex;
  align-items: center;
  min-height: 1.75rem;
  color: var(--rv-color-ink);
  text-decoration: none;
}

.profiles__link:hover {
  color: var(--rv-color-accent-ink);
  text-decoration: underline;
}

.profiles__lists {
  display: block;
  margin-top: var(--rv-space-1);
  color: var(--rv-color-ink-muted);
  font-weight: 400;
  font-size: var(--rv-text-meta);
}

.profiles__cell-outputs {
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
}

.profiles__cell-outputs--none {
  color: var(--rv-color-ink-tertiary);
}

.profiles__formats {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-1) var(--rv-space-4);
  margin: 0;
  padding: 0;
  list-style: none;
}

.profiles__format {
  display: inline-flex;
  gap: var(--rv-space-2);
  align-items: center;
}

.profiles__cell-updated,
.profiles__cell-archived {
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
  white-space: nowrap;
}

.profiles__cell-updated--none {
  color: var(--rv-color-ink-tertiary);
}

/* A failed build states itself above the time of the file that is still
   published, when there is one. */
.profiles__updated {
  display: grid;
  gap: var(--rv-space-1);
  justify-items: start;
}

.profiles__cell-actions {
  white-space: nowrap;
}

/* The cell stays a table cell; the flex row lives inside it, or the actions
   column would fall out of the table's row grid. */
.profiles__actions {
  display: flex;
  gap: var(--rv-space-2);
  align-items: center;
  justify-content: flex-end;
}

.profiles__pagination {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-3);
  align-items: center;
}

.profiles__pagination p {
  margin: 0;
  color: var(--rv-color-status-failed);
}

@container routes (width <= 40rem) {
  .profiles__scroll {
    overflow-x: visible;

    --rv-table-display: block;
    --rv-table-min-width: 0;
  }

  .profiles__scroll thead {
    position: absolute;
    width: 0.0625rem;
    height: 0.0625rem;
    overflow: hidden;
    clip-path: inset(50%);
  }

  .profiles__scroll tbody {
    display: grid;
    gap: var(--rv-space-3);
  }

  .profiles__scroll tr {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    gap: var(--rv-space-2) var(--rv-space-4);
    padding: var(--rv-space-4);
    background: var(--rv-color-surface);
    border: var(--rv-border-hair) solid var(--rv-color-rule);
    border-radius: var(--rv-radius-md);
  }

  .profiles__scroll td {
    min-height: 0;
    padding: 0;
    border-bottom: 0;
  }

  .profiles__cell-name {
    grid-column: 1 / -1;
    min-width: 0;
  }

  .profiles__cell-outputs,
  .profiles__cell-updated,
  .profiles__cell-archived {
    display: grid;
    grid-template-columns: minmax(6rem, 0.4fr) minmax(0, 1fr);
    gap: var(--rv-space-3);
    white-space: normal;
  }

  .profiles__cell-outputs::before,
  .profiles__cell-updated::before,
  .profiles__cell-archived::before {
    color: var(--rv-color-ink-tertiary);
    font-size: var(--rv-text-meta);
    content: attr(data-label);
  }

  .profiles__cell-actions {
    grid-row: 2 / span 3;
    grid-column: 2;
    align-self: center;
  }

  .profiles__cell-actions::before {
    content: none;
  }
}

.profiles__row {
  cursor: pointer;
}

.profiles__row:hover,
.profiles__row:focus-within {
  background: var(--rv-color-surface-hover);
}
</style>
