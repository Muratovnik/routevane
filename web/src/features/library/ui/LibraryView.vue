<script setup lang="ts">
import { computed, onMounted } from 'vue'

import { useLibrary } from '@/features/library/model/useLibrary'
import { useLocale } from '@/shared/i18n/useLocale'
import type { ExportFormat } from '@/shared/api/exports'
import type { ListCard } from '@/shared/api/lists'
import type { OutputCard } from '@/shared/api/outputs'
import { legacyImportedOrdinal } from '@/shared/lib/legacyList'
import { listPageHash, parseLegacyListHash } from '@/shared/lib/listHash'
import type { MenuItem } from '@/shared/ui/kinds'
import RvButton from '@/shared/ui/RvButton.vue'
import RvIcon from '@/shared/ui/RvIcon.vue'
import RvDisclosure from '@/shared/ui/RvDisclosure.vue'
import RvMenu from '@/shared/ui/RvMenu.vue'
import RvStateNotice from '@/shared/ui/RvStateNotice.vue'

const { dateTime, t, tc, tor } = useLocale()
const library = useLibrary()
const route = useRoute()
const router = useRouter()

function openRoute(event: MouseEvent, card: ListCard): void {
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
  void router.push(listHref(card))
}

const copyMessage = computed(() => {
  if (library.copiedID.value !== '') return t('library.copied')
  if (library.copyFailedID.value !== '') return t('library.copyFailed')
  if (library.exportFailedID.value !== '') return t('library.export.failed')
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
  // lands on the list's page instead of an empty shelf.
  const legacyID = parseLegacyListHash(route.hash)
  if (legacyID !== '') {
    void router.replace(`/lists/${legacyID}`)
    return
  }
  void library.initialize()
})

function updatedAt(card: ListCard): string {
  const output = publishedOutputs(card).sort((left, right) =>
    (right.latest?.contentCreatedAt ?? '').localeCompare(
      left.latest?.contentCreatedAt ?? '',
    ),
  )[0]
  if (output?.latest === null || output === undefined)
    return t('library.noArtifact')
  const parsed = new Date(output.latest.contentCreatedAt)
  return Number.isNaN(parsed.valueOf()) ? '—' : dateTime.value.format(parsed)
}

function publishedOutputs(card: ListCard): OutputCard[] {
  return card.outputs.filter((output) => output.latest !== null)
}

function displayName(card: ListCard): string {
  const ordinal = legacyImportedOrdinal(card.name)
  return ordinal === null
    ? card.name
    : t('library.imported.name', { number: ordinal })
}

function outputSummary(card: ListCard): string {
  if (card.outputs.length === 0) return t('library.noOutputs')
  return card.outputs.map(library.outputTitle).join(', ')
}

function exportLabel(format: ExportFormat): string {
  return tor(
    `export.format.${format.rendererID}`,
    format.fileExtension.toUpperCase(),
  )
}

function listHref(card: ListCard, tab = ''): string {
  return `/lists/${card.id}${listPageHash({ tab })}`
}

function archivedSince(card: ListCard): string {
  const parsed = new Date(card.archivedAt)
  return Number.isNaN(parsed.valueOf())
    ? '—'
    : t('library.archived.since', { time: dateTime.value.format(parsed) })
}

function menuItems(card: ListCard): MenuItem[] {
  const items: MenuItem[] = [
    {
      icon: 'edit',
      key: 'open',
      label: t('library.configure'),
      to: listHref(card),
    },
  ]
  items.push({
    icon: 'download',
    key: 'downloads',
    label: t('library.download.menu'),
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
    label: t('library.sendTarget', { target: library.outputTitle(output) }),
    to: `/lists/${card.id}/send/${output.id}`,
  }))
  if (deployItems.length === 1) items.push(deployItems[0] as MenuItem)
  if (deployItems.length > 1) {
    items.push({
      children: deployItems,
      icon: 'send',
      key: 'send',
      label: t('library.send'),
    })
  }
  items.push({
    icon: 'plus',
    key: 'connections',
    label: t('library.connections'),
    to: listHref(card, 'outputs'),
  })
  if (library.downloadable(card) !== null) {
    items.push({ icon: 'copy', key: 'copy', label: t('library.copy') })
  }
  items.push({
    icon: 'archive',
    key: card.archivedAt === '' ? 'archive' : 'restore',
    label: card.archivedAt === '' ? t('library.archive') : t('library.restore'),
    separatorBefore: true,
  })
  return items
}

function onMenu(card: ListCard, key: string): void {
  if (key.startsWith('export:'))
    void library.exportFile(card, key.slice('export:'.length))
  if (key === 'copy') void library.copyContents(card)
  if (key === 'archive') void library.setArchived(card, true)
  if (key === 'restore') void library.setArchived(card, false)
}
</script>

<template>
  <section aria-labelledby="library-title" class="library">
    <header class="library__header">
      <h1 id="library-title" class="library__title">
        {{ t('library.title') }}
      </h1>
      <p class="library__copy-message" role="status">{{ copyMessage }}</p>
      <RvButton to="/lists/new" variant="primary">
        <RvIcon name="plus" />
        {{ t('library.new') }}
      </RvButton>
    </header>

    <RvStateNotice
      v-if="library.state.value === 'ready' && importedCount > 0"
      :body="t('library.imported.body')"
      :title="tc('library.imported.title', importedCount)"
      tone="waiting"
    />

    <RvStateNotice
      v-if="library.state.value === 'loading'"
      live
      :title="t('library.loading')"
      tone="busy"
    />
    <RvStateNotice
      v-else-if="library.state.value === 'failed'"
      :body="t('library.failed.body')"
      live
      :title="t('library.failed')"
      tone="failed"
    >
      <template #action>
        <RvButton @click="library.initialize">
          {{ t('action.retry') }}
        </RvButton>
      </template>
    </RvStateNotice>
    <div v-else-if="library.state.value === 'empty'" class="library__empty">
      <p class="library__empty-title">{{ t('library.empty') }}</p>
      <p class="library__empty-body">{{ t('library.empty.body') }}</p>
      <RvButton to="/lists/new" variant="secondary">
        <RvIcon name="plus" />
        {{ t('library.new') }}
      </RvButton>
    </div>

    <div v-else-if="library.rows.value.length > 0" class="library__scroll">
      <table class="library__table">
        <thead>
          <tr>
            <th scope="col">{{ t('library.column.name') }}</th>
            <th scope="col">{{ t('library.column.outputs') }}</th>
            <th scope="col">{{ t('library.column.updated') }}</th>
            <th scope="col">
              <span class="library__visually-hidden">
                {{ t('library.column.actions') }}
              </span>
            </th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="card in library.rows.value"
            :key="card.id"
            class="library__row"
            @click="openRoute($event, card)"
          >
            <td class="library__cell-name">
              <NuxtLink class="library__link" :to="listHref(card)">
                {{ displayName(card) }}
              </NuxtLink>
              <span
                v-if="library.composition(card) !== displayName(card)"
                class="library__services"
              >
                {{ library.composition(card) }}
              </span>
            </td>
            <td
              class="library__cell-outputs"
              :data-label="t('library.column.outputs')"
              :class="{
                'library__cell-outputs--none': card.outputs.length === 0,
              }"
            >
              {{ outputSummary(card) }}
            </td>
            <td
              class="library__cell-updated"
              :data-label="t('library.column.updated')"
              :class="{
                'library__cell-updated--none':
                  library.downloadable(card) === null,
              }"
            >
              {{ updatedAt(card) }}
            </td>
            <td
              class="library__cell-actions"
              :data-label="t('library.column.actions')"
            >
              <div class="library__actions">
                <RvMenu
                  :items="menuItems(card)"
                  :label="t('library.menu', { name: displayName(card) })"
                  @select="(key) => onMenu(card, key)"
                />
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <RvStateNotice
      v-else
      :body="t('library.allArchived.body')"
      :title="t('library.allArchived')"
      tone="waiting"
    />

    <!-- An archived list leaves the shelf, not the library: its file and its
         subscription are still working, so the row stays findable. -->
    <RvDisclosure
      v-if="
        library.state.value === 'ready' && library.archived.value.length > 0
      "
      class="library__archive"
      :hint="tc('library.archived.count', library.archived.value.length)"
      :summary="t('library.archived')"
    >
      <p class="library__archive-body">{{ t('library.archived.body') }}</p>
      <ul class="library__archive-list">
        <li
          v-for="card in library.archived.value"
          :key="card.id"
          class="library__archive-row"
        >
          <NuxtLink class="library__link" :to="listHref(card)">
            {{ displayName(card) }}
          </NuxtLink>
          <span class="library__archive-since">{{ archivedSince(card) }}</span>
          <RvMenu
            :items="menuItems(card)"
            :label="t('library.menu', { name: displayName(card) })"
            @select="(key) => onMenu(card, key)"
          />
        </li>
      </ul>
    </RvDisclosure>
  </section>
</template>

<style scoped src="./LibraryView.css"></style>
