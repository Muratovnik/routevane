<script setup lang="ts">
import RvTable from '@/shared/ui/RvTable.vue'
import RvPageHeader from '@/shared/ui/RvPageHeader.vue'
import { computed, onMounted } from 'vue'

import { useProfiles } from '@/features/profiles/model/useProfiles'
import { useLocale } from '@/shared/i18n/useLocale'
import type { ExportFormat } from '@/shared/api/exports'
import type { ProfileCard } from '@/shared/api/profiles'
import type { OutputCard } from '@/shared/api/outputs'
import { legacyImportedOrdinal } from '@/shared/lib/legacyProfile'
import {
  profilePageHash,
  parseLegacyProfileHash,
} from '@/shared/lib/profileHash'
import type { MenuItem } from '@/shared/ui/types'
import RvButton from '@/shared/ui/RvButton.vue'
import RvIcon from '@/shared/ui/RvIcon.vue'
import RvDisclosure from '@/shared/ui/RvDisclosure.vue'
import RvMenu from '@/shared/ui/RvMenu.vue'
import RvStateNotice from '@/shared/ui/RvStateNotice.vue'

const { dateTime, t, tc, tor } = useLocale()
const library = useProfiles()
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

const copyMessage = computed(() => {
  if (library.copiedID.value !== '') return t('profiles.copied')
  if (library.copyFailedID.value !== '') return t('profiles.copyFailed')
  if (library.exportFailedID.value !== '') return t('profiles.export.failed')
  return ''
})
const importedCount = computed(
  () =>
    [...library.rows.value, ...library.archived.value].filter(
      (card) => legacyImportedOrdinal(card.name) !== null,
    ).length,
)
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
  const output = publishedOutputs(card).sort((left, right) =>
    (right.latest?.contentCreatedAt ?? '').localeCompare(
      left.latest?.contentCreatedAt ?? '',
    ),
  )[0]
  if (output?.latest === null || output === undefined)
    return t('profiles.noArtifact')
  const parsed = new Date(output.latest.contentCreatedAt)
  return Number.isNaN(parsed.valueOf()) ? '—' : dateTime.value.format(parsed)
}

const publishedOutputs = (card: ProfileCard): OutputCard[] =>
  card.outputs.filter((output) => output.latest !== null)

const displayName = (card: ProfileCard): string => {
  const ordinal = legacyImportedOrdinal(card.name)
  return ordinal === null
    ? card.name
    : t('profiles.imported.name', { number: ordinal })
}

const outputSummary = (card: ProfileCard): string => {
  if (card.outputs.length === 0) return t('profiles.noOutputs')
  return card.outputs.map(library.outputTitle).join(', ')
}

const exportLabel = (format: ExportFormat): string =>
  tor(`export.format.${format.rendererID}`, format.fileExtension.toUpperCase())

const profileHref = (card: ProfileCard, tab = ''): string =>
  `/profiles/${card.id}${profilePageHash({ tab })}`

const archivedSince = (card: ProfileCard): string => {
  const parsed = new Date(card.archivedAt)
  return Number.isNaN(parsed.valueOf())
    ? '—'
    : t('profiles.archived.since', { time: dateTime.value.format(parsed) })
}

const menuItems = (card: ProfileCard): MenuItem[] => {
  const items: MenuItem[] = [
    {
      icon: 'edit',
      key: 'open',
      label: t('profiles.configure'),
      to: profileHref(card),
    },
  ]
  items.push({
    icon: 'download',
    key: 'downloads',
    label: t('profiles.download.menu'),
    children: library.exportFormats.value.map((format) => ({
      disabled: library.exporting.value !== '',
      icon: 'download',
      key: `export:${format.id}`,
      label: exportLabel(format),
    })),
  })
  const deployItems = library.deployableOutputs(card).map((output) => ({
    icon: 'send' as const,
    key: `send:${output.id}`,
    label: t('profiles.sendTarget', { target: library.outputTitle(output) }),
    to: `/profiles/${card.id}/send/${output.id}`,
  }))
  if (deployItems.length === 1) items.push(deployItems[0] as MenuItem)
  if (deployItems.length > 1) {
    items.push({
      children: deployItems,
      icon: 'send',
      key: 'send',
      label: t('profiles.send'),
    })
  }
  items.push({
    icon: 'plus',
    key: 'connections',
    label: t('profiles.connections'),
    to: profileHref(card, 'outputs'),
  })
  if (library.downloadable(card) !== null) {
    items.push({ icon: 'copy', key: 'copy', label: t('profiles.copy') })
  }
  items.push({
    icon: 'archive',
    key: card.archivedAt === '' ? 'archive' : 'restore',
    label:
      card.archivedAt === '' ? t('profiles.archive') : t('profiles.restore'),
    separatorBefore: true,
  })
  return items
}

const onMenu = (card: ProfileCard, key: string): void => {
  if (key.startsWith('export:'))
    void library.exportFile(card, key.slice('export:'.length))
  if (key === 'copy') void library.copyContents(card)
  if (key === 'archive') void library.setArchived(card, true)
  if (key === 'restore') void library.setArchived(card, false)
}
</script>

<template>
  <section aria-labelledby="profiles-title" class="profiles">
    <RvPageHeader title-id="profiles-title" :title="t('profiles.title')">
      <p class="profiles__copy-message" role="status">{{ copyMessage }}</p>
      <RvButton to="/profiles/new" variant="primary">
        <RvIcon name="plus" />
        {{ t('profiles.new') }}
      </RvButton>
    </RvPageHeader>

    <RvStateNotice
      v-if="library.state.value === 'ready' && importedCount > 0"
      :body="t('profiles.imported.body')"
      :title="tc('profiles.imported.title', importedCount)"
      tone="waiting"
    />

    <RvStateNotice
      v-if="library.state.value === 'loading'"
      live
      :title="t('profiles.loading')"
      tone="busy"
    />
    <RvStateNotice
      v-else-if="library.state.value === 'failed'"
      :body="t('profiles.failed.body')"
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
    <div v-else-if="library.state.value === 'empty'" class="profiles__empty">
      <p class="profiles__empty-title">{{ t('profiles.empty') }}</p>
      <p class="profiles__empty-body">{{ t('profiles.empty.body') }}</p>
      <RvButton to="/profiles/new" variant="secondary">
        <RvIcon name="plus" />
        {{ t('profiles.new') }}
      </RvButton>
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
          <th scope="col">
            <span class="profiles__visually-hidden">
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
            {{ outputSummary(card) }}
          </td>
          <td
            class="profiles__cell-updated"
            :data-label="t('profiles.column.updated')"
            :class="{
              'profiles__cell-updated--none':
                library.downloadable(card) === null,
            }"
          >
            {{ updatedAt(card) }}
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

    <!-- An archived profile leaves the shelf, not the library: its file and its
         subscription are still working, so the row stays findable. -->
    <RvDisclosure
      v-if="
        library.state.value === 'ready' && library.archived.value.length > 0
      "
      class="profiles__archive"
      :hint="tc('profiles.archived.count', library.archived.value.length)"
      :summary="t('profiles.archived')"
    >
      <p class="profiles__archive-body">{{ t('profiles.archived.body') }}</p>
      <ul class="profiles__archive-list">
        <li
          v-for="card in library.archived.value"
          :key="card.id"
          class="profiles__archive-row"
        >
          <NuxtLink class="profiles__link" :to="profileHref(card)">
            {{ displayName(card) }}
          </NuxtLink>
          <span class="profiles__archive-since">{{ archivedSince(card) }}</span>
          <RvMenu
            :items="menuItems(card)"
            :label="t('profiles.menu', { name: displayName(card) })"
            @select="(key) => onMenu(card, key)"
          />
        </li>
      </ul>
    </RvDisclosure>

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
  </section>
</template>

<style scoped>
.profiles {
  container: routes / inline-size;
  display: grid;
  gap: var(--rv-space-4);
  width: 100%;
}

/* The copy result is announced and shown without moving the table below it. */
.profiles__copy-message {
  min-height: 1.25rem;
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
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

.profiles__empty .rv-button {
  margin-top: var(--rv-space-2);
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

.profiles__cell-updated {
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
  white-space: nowrap;
}

.profiles__cell-updated--none {
  color: var(--rv-color-ink-tertiary);
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

.profiles__visually-hidden {
  position: absolute;
  width: 0.0625rem;
  height: 0.0625rem;
  overflow: hidden;
  white-space: nowrap;
  clip-path: inset(50%);
}

/* The archive is one disclosure below the shelf. It is a list rather than a
   second table: the questions asked of an archived row are when it left and
   how to get it back, not how it compares with the rows above. */
.profiles__archive {
  margin-top: var(--rv-space-4);
}

.profiles__archive-body {
  margin: 0 0 var(--rv-space-3);
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
}

.profiles__archive-list {
  display: flex;
  flex-direction: column;
  gap: var(--rv-space-2);
  margin: 0;
  padding: 0;
  list-style: none;
}

.profiles__archive-row {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-2);
  align-items: baseline;
}

.profiles__archive-since {
  flex: 1 1 auto;
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
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
  .profiles__cell-updated {
    display: grid;
    grid-template-columns: minmax(6rem, 0.4fr) minmax(0, 1fr);
    gap: var(--rv-space-3);
    white-space: normal;
  }

  .profiles__cell-outputs::before,
  .profiles__cell-updated::before {
    color: var(--rv-color-ink-tertiary);
    font-size: var(--rv-text-meta);
    content: attr(data-label);
  }

  .profiles__cell-actions {
    grid-row: 2 / span 2;
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
