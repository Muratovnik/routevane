<script setup lang="ts">
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
import type { MenuItem } from '@/shared/ui/kinds'
import RvButton from '@/shared/ui/RvButton.vue'
import RvIcon from '@/shared/ui/RvIcon.vue'
import RvDisclosure from '@/shared/ui/RvDisclosure.vue'
import RvMenu from '@/shared/ui/RvMenu.vue'
import RvStateNotice from '@/shared/ui/RvStateNotice.vue'

const { dateTime, t, tc, tor } = useLocale()
const library = useProfiles()
const route = useRoute()
const router = useRouter()

function openRoute(event: MouseEvent, card: ProfileCard): void {
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

function updatedAt(card: ProfileCard): string {
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

function publishedOutputs(card: ProfileCard): OutputCard[] {
  return card.outputs.filter((output) => output.latest !== null)
}

function displayName(card: ProfileCard): string {
  const ordinal = legacyImportedOrdinal(card.name)
  return ordinal === null
    ? card.name
    : t('profiles.imported.name', { number: ordinal })
}

function outputSummary(card: ProfileCard): string {
  if (card.outputs.length === 0) return t('profiles.noOutputs')
  return card.outputs.map(library.outputTitle).join(', ')
}

function exportLabel(format: ExportFormat): string {
  return tor(
    `export.format.${format.rendererID}`,
    format.fileExtension.toUpperCase(),
  )
}

function profileHref(card: ProfileCard, tab = ''): string {
  return `/profiles/${card.id}${profilePageHash({ tab })}`
}

function archivedSince(card: ProfileCard): string {
  const parsed = new Date(card.archivedAt)
  return Number.isNaN(parsed.valueOf())
    ? '—'
    : t('profiles.archived.since', { time: dateTime.value.format(parsed) })
}

function menuItems(card: ProfileCard): MenuItem[] {
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

function onMenu(card: ProfileCard, key: string): void {
  if (key.startsWith('export:'))
    void library.exportFile(card, key.slice('export:'.length))
  if (key === 'copy') void library.copyContents(card)
  if (key === 'archive') void library.setArchived(card, true)
  if (key === 'restore') void library.setArchived(card, false)
}
</script>

<template>
  <section aria-labelledby="profiles-title" class="profiles">
    <header class="profiles__header">
      <h1 id="profiles-title" class="profiles__title">
        {{ t('profiles.title') }}
      </h1>
      <p class="profiles__copy-message" role="status">{{ copyMessage }}</p>
      <RvButton to="/profiles/new" variant="primary">
        <RvIcon name="plus" />
        {{ t('profiles.new') }}
      </RvButton>
    </header>

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

    <div v-else-if="library.rows.value.length > 0" class="profiles__scroll">
      <table class="profiles__table">
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
      </table>
    </div>
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
  </section>
</template>

<style scoped src="./ProfilesView.css"></style>
