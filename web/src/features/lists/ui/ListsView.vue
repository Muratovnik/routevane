<script setup lang="ts">
import RvTable from '@/shared/ui/RvTable.vue'
import RvPageHeader from '@/shared/ui/RvPageHeader.vue'
import { useWorkspaceInspection } from '@/shared/model/useWorkspacePane'
import { useSortable } from '@vueuse/integrations/useSortable'
import { computed, onMounted, ref, useTemplateRef, watch } from 'vue'

import CategoryFilters from '@/entities/profile-composition/ui/CategoryFilters.vue'
import { matchesCategories } from '@/entities/profile-composition/model/categoryFilter'
import CategoryLabel from '@/entities/profile-composition/ui/CategoryLabel.vue'
import ListDetailDialog from '@/entities/profile-composition/ui/ListDetailDialog.vue'
import { useLists } from '@/features/lists/model/useLists'
import ListSheet from '@/features/lists/ui/ListSheet.vue'
import type {
  CategoryDetail,
  CategoryLists,
  ListDetail,
} from '@/shared/api/catalog'
import { useLocale } from '@/shared/i18n/useLocale'
import { libraryPageHash, parseLibraryPageHash } from '@/shared/lib/listsHash'
import type { ChoiceOption, MenuItem, SegmentOption } from '@/shared/ui/types'
import { useTableDragGeometry } from '@/shared/model/useTableDragGeometry'
import RvButton from '@/shared/ui/RvButton.vue'
import RvDialog from '@/shared/ui/RvDialog.vue'
import RvField from '@/shared/ui/RvField.vue'
import RvIcon from '@/shared/ui/RvIcon.vue'
import RvMenu from '@/shared/ui/RvMenu.vue'
import RvSegmented from '@/shared/ui/RvSegmented.vue'
import RvStateNotice from '@/shared/ui/RvStateNotice.vue'
import RvTextInput from '@/shared/ui/RvTextInput.vue'

/**
 * «Списки»: the flow that curates the catalog (ADR 0029).
 *
 * Everything global lives here — what a category holds, what a list holds,
 * where its entries come from, and whether either exists at all — so that
 * composing a route can write nothing but the route. The catalog table also
 * owns the default order; membership and content edits remain library actions.
 */
const inspectWorkspace = useWorkspaceInspection()
const { t, tc, tor } = useLocale()
const library = useLists()
const route = useRoute()
const router = useRouter()

// The row for lists no category claims. Unlike the composer's, it is always
// drawn: it is where a detached list goes, so it is a place rather than a
// leftover.
const UNCATEGORIZED_ID = 'rv:uncategorized'

type CategoryRow = {
  id: string
  label: string
  // The category this row stands for, or null for the computed last row.
  category: CategoryDetail | null
  members: ListDetail[]
}

const activeCategoryIDs = ref<string[]>([])
const activeCategoryID = computed(() =>
  activeCategoryIDs.value.length === 1 ? activeCategoryIDs.value[0]! : '',
)
const activeListID = ref('')
const categoriesOpen = ref(false)
const query = ref('')

const categoryForm = ref<'closed' | 'create' | 'rename'>('closed')
const categorySubject = ref<CategoryDetail | null>(null)
const categoryTitle = ref('')
const categoryTitleTouched = ref(false)
// A category created while the catalog is stale cannot be selected from the
// retained copy. Keep its server identity until a successful GET confirms that
// the row exists, then complete the selection.
const pendingCategoryID = ref('')
const listSheetOpen = ref(false)
const listSheetError = ref('')
const pendingCreatedListID = ref('')
const removingCategory = ref(false)
const categoryLists = ref<CategoryLists>('detach')
const removingList = ref<ListDetail | null>(null)
const priorityDraft = ref<string[]>([])
const priorityError = ref('')

// The control speaks in strings; only the two answers the request has are
// admitted back, so an unexpected value never reaches the endpoint.
const chosenDisposition = computed<string>({
  get: () => categoryLists.value,
  set: (value) => {
    categoryLists.value = value === 'delete' ? 'delete' : 'detach'
  },
})

const listsByID = computed(
  () => new Map(library.lists.value.map((list) => [list.id, list])),
)

const uncategorized = computed(() => {
  const claimed = new Set(
    library.categories.value.flatMap((category) => category.lists),
  )
  return library.lists.value.filter((list) => !claimed.has(list.id))
})

const rows = computed<CategoryRow[]>(() => [
  ...library.categories.value.map((category) => ({
    category,
    id: category.id,
    label: tor(`category.${category.id}`, category.title),
    members: category.lists
      .map((id) => listsByID.value.get(id))
      .filter((list): list is ListDetail => list !== undefined),
  })),
  {
    category: null,
    id: UNCATEGORIZED_ID,
    label: t('listPicker.other'),
    members: uncategorized.value,
  },
])

const activeRow = computed<CategoryRow | null>(
  () => rows.value.find((entry) => entry.id === activeCategoryID.value) ?? null,
)

const activeList = computed(
  () => listsByID.value.get(activeListID.value) ?? null,
)

// A refusal belongs to the act that caused it: while a confirmation is open it
// is stated there, because that is where the operator is standing.
const confirming = computed(
  () => removingCategory.value || removingList.value !== null,
)
const paneRefusal = computed(() =>
  confirming.value ? null : library.refusal.value,
)

// What a category can be asked to do. The catalog ships the words and the
// operator owns what is inside; the title belongs only to a category they
// created, and the operator may remove either kind (ADR 0029).
const categoryActions = (entry: CategoryRow): MenuItem[] => {
  const category = entry.category
  if (category === null) return []
  const items: MenuItem[] = []
  if (category.custom)
    items.push({
      disabled: library.busy.value,
      icon: 'edit',
      key: 'rename',
      label: t('lists.category.rename'),
    })
  items.push({
    disabled: library.busy.value,
    icon: 'trash',
    key: 'remove',
    label: t('lists.category.remove'),
    separatorBefore: items.length > 0,
  })
  return items
}

const profileActions = (list: ListDetail): MenuItem[] => {
  const items: MenuItem[] = []
  if (list.custom === true)
    items.push({
      disabled: library.busy.value,
      icon: 'edit',
      key: 'rename',
      label: t('lists.list.rename'),
    })
  const memberships = listCategories(list)
  if (activeRow.value?.category)
    items.push({
      disabled: library.stale.value || library.busy.value,
      key: 'detach',
      label: t('lists.list.detach'),
    })
  else if (memberships.length > 0)
    items.push({
      key: 'detach-category',
      label: t('lists.list.detach'),
      children: memberships.map((entry) => ({
        key: `detach:${entry.id}`,
        label: entry.label,
        disabled: library.stale.value || library.busy.value,
      })),
    })
  items.push({
    disabled: library.busy.value,
    icon: 'trash',
    key: 'remove',
    label: t('lists.list.remove'),
    separatorBefore: items.length > 0,
  })
  return items
}

// Every list this category does not already hold, named the way the rows name
// it. The combobox is the search, so the whole catalog can be offered.
const pickableLists = computed<ChoiceOption[]>(() => {
  const category = activeRow.value?.category ?? null
  if (category === null) return []
  const held = new Set(category.lists)
  return library.lists.value
    .filter((list) => !held.has(list.id))
    .map((list) => ({ label: list.title, value: list.id }))
})

const priorityDirty = computed(
  () =>
    priorityDraft.value.join('\0') !== library.defaultPriority.value.join('\0'),
)

const dispositions = computed<SegmentOption[]>(() => [
  { label: t('lists.category.remove.detach'), value: 'detach' },
  { label: t('lists.category.remove.delete'), value: 'delete' },
])

const categoryTitleError = computed(() =>
  categoryTitleTouched.value && categoryTitle.value.trim() === ''
    ? t('lists.category.invalid')
    : '',
)

onMounted(() => {
  const opened = parseLibraryPageHash(route.hash)
  activeCategoryIDs.value = opened.category
  activeListID.value = opened.list
  void library.initialize()
})

watch(
  () => library.stale.value,
  (isStale, wasStale) => {
    if (wasStale && !isStale) settlePendingCategory()
  },
)

const select = (categoryID: string): void => {
  activeCategoryIDs.value = categoryID ? [categoryID] : []
  library.clearRefusal()
  writeLocation()
}

const showCategory = (categoryID: string): void => {
  select(categoryID)
  categoriesOpen.value = false
}

const openList = (listID: string): void => {
  inspectWorkspace(true, () => {
    activeListID.value = listID
    writeLocation()
  })
}

const openRow = (event: MouseEvent, listID: string): void => {
  if (tableDisabled.value || event.defaultPrevented) return
  const target = event.target
  if (target instanceof Element && target.closest('button, a, input, select'))
    return
  openList(listID)
}

const startCreateList = (): void => {
  if (library.stale.value) return
  listSheetError.value = ''
  pendingCreatedListID.value = ''
  listSheetOpen.value = true
}

const closeList = (): void => {
  inspectWorkspace(false, () => {
    activeListID.value = ''
    writeLocation()
  })
}

// The address states where the operator is, so a refresh, a bookmark and the
// composing card's link all land on the same category and the same open list.
const writeLocation = (): void => {
  void router.replace(
    `/lists${libraryPageHash({
      category: activeCategoryIDs.value,
      list: activeListID.value,
    })}`,
  )
}

const startCreateCategory = (): void => {
  categoriesOpen.value = false
  if (library.stale.value) return
  library.clearRefusal()
  categorySubject.value = null
  categoryTitle.value = ''
  categoryTitleTouched.value = false
  categoryForm.value = 'create'
}

const closeCategoryForm = (): void => {
  if (library.busy.value) return
  categoryForm.value = 'closed'
  categorySubject.value = null
  library.clearRefusal()
}

const closeCategoryRemoval = (): void => {
  if (library.busy.value) return
  removingCategory.value = false
  categorySubject.value = null
}

const closeListRemoval = (): void => {
  if (library.busy.value) return
  removingList.value = null
}

const committed = (status: string): boolean =>
  status === 'saved' || status === 'stale'

const settlePendingCategory = (): void => {
  const created = pendingCategoryID.value
  if (created === '' || library.stale.value) return
  pendingCategoryID.value = ''
  if (rows.value.some((entry) => entry.id === created)) {
    select(created)
    return
  }
  // A successful read is authoritative. If the server omitted the created
  // row, stay on an existing safe context instead of writing a bogus URL.
  if (!rows.value.some((entry) => entry.id === activeCategoryID.value))
    select(rows.value[0]?.id ?? UNCATEGORIZED_ID)
}

const retryLibraryRead = async (): Promise<void> => {
  if (await library.refresh()) settlePendingCategory()
}

const onCategoryAction = (entry: CategoryRow, key: string): void => {
  categoriesOpen.value = false
  const category = entry.category
  if (category === null || library.busy.value) return
  if (key === 'rename') {
    library.clearRefusal()
    categorySubject.value = category
    categoryTitle.value = category.title
    categoryTitleTouched.value = false
    categoryForm.value = 'rename'
    return
  }
  if (key === 'remove') {
    categorySubject.value = category
    categoryLists.value = 'detach'
    library.clearRefusal()
    removingCategory.value = true
  }
}

const onProfileAction = (list: ListDetail, key: string): void => {
  if (library.busy.value) return
  const category = activeRow.value?.category ?? null
  if (key === 'rename' && list.custom === true) {
    openList(list.id)
    return
  }
  if (key.startsWith('detach:') && !library.stale.value) {
    const from = library.categories.value.find(
      (entry) => entry.id === key.slice(7),
    )
    if (from) void library.detachList(from, list.id)
    return
  }
  if (key === 'detach' && category !== null && !library.stale.value) {
    void library.detachList(category, list.id)
    return
  }
  if (key === 'remove') startRemoveList(list)
}

const startRemoveList = (list: ListDetail): void => {
  if (library.busy.value) return
  library.clearRefusal()
  removingList.value = list
}

const submitCategoryTitle = async (): Promise<void> => {
  categoryTitleTouched.value = true
  const title = categoryTitle.value.trim()
  if (title === '') return
  const category = categorySubject.value
  if (categoryForm.value === 'rename') {
    if (category === null) return
    const result = await library.renameCategory(category.id, title)
    if (committed(result.status)) {
      categoryForm.value = 'closed'
      categorySubject.value = null
    }
    return
  }
  if (library.stale.value) return
  const result = await library.addCategory(title)
  if (!committed(result.status) || result.value === undefined) return
  categoryForm.value = 'closed'
  if (result.status === 'saved') {
    // A category made to be filled has to be the one on screen.
    select(result.value.id)
  } else {
    // The write is committed, but this old copy cannot contain the new row.
    // Retry GET completes the selection without repeating the POST.
    pendingCategoryID.value = result.value.id
  }
}

const submitPickedLists = async (listID: string): Promise<void> => {
  const category = activeRow.value?.category ?? null
  if (category === null || listID === '') return
  const result = await library.addList(category, listID)
  if (committed(result.status)) {
    pendingCreatedListID.value = ''
    listSheetOpen.value = false
    listSheetError.value = ''
    return
  }
  if (result.status === 'failed')
    listSheetError.value = t('lists.list.attach.failed')
}

const confirmRemoveCategory = async (): Promise<void> => {
  const category = categorySubject.value
  if (category === null) return
  const result = await library.deleteCategory(category.id, categoryLists.value)
  if (committed(result.status)) {
    removingCategory.value = false
    categorySubject.value = null
    // Only the category that was on screen needs a safe replacement. Removing
    // another row from its overflow menu must not move the operator.
    if (activeCategoryID.value === category.id) select(UNCATEGORIZED_ID)
  }
}

const confirmRemoveList = async (): Promise<void> => {
  const list = removingList.value
  if (list === null) return
  const result = await library.deleteList(list.id)
  if (committed(result.status)) {
    removingList.value = null
    if (activeListID.value === list.id) closeList()
  }
}

/**
 * A list created while a category is open joins that category, in one edit
 * after the one that created it. The computed row holds nothing, so a list made
 * there is simply a list no category claims.
 */
const createList = async (draft: {
  domains: string[]
  title: string
}): Promise<void> => {
  listSheetError.value = ''
  const created = await library.addCustomList(draft.title, draft.domains)
  if (created.status !== 'saved' || created.value === undefined) {
    if (created.status === 'failed')
      listSheetError.value = t('listCard.save.failed')
    return
  }
  const category = activeRow.value?.category ?? null
  if (category === null) {
    pendingCreatedListID.value = ''
    listSheetOpen.value = false
    return
  }
  pendingCreatedListID.value = created.value.id
  await submitPickedLists(created.value.id)
}

const retryCreatedListAttachment = async (): Promise<void> => {
  if (pendingCreatedListID.value === '') return
  await submitPickedLists(pendingCreatedListID.value)
}

const onListUpdated = (detail: ListDetail): void => {
  library.acceptUpdatedList(detail)
}

const closeListSheet = (): void => {
  if (library.busy.value) return
  listSheetOpen.value = false
  listSheetError.value = ''
  pendingCreatedListID.value = ''
}

const resetPriority = (): void => {
  priorityDraft.value = [...library.defaultPriority.value]
  priorityError.value = ''
}

const filter = computed({
  get: () => activeCategoryIDs.value,
  set: (value: string[]) => {
    activeCategoryIDs.value = value
    library.clearRefusal()
    writeLocation()
  },
})
const tableDisabled = computed(() => library.busy.value || library.stale.value)
watch(
  () => library.defaultPriority.value,
  (ids, previous) => {
    if (
      priorityDraft.value.length === 0 ||
      priorityDraft.value.join('\0') === previous?.join('\0')
    )
      priorityDraft.value = [...ids]
    else
      priorityDraft.value = [
        ...priorityDraft.value.filter((id) => ids.includes(id)),
        ...ids.filter((id) => !priorityDraft.value.includes(id)),
      ]
  },
  { immediate: true },
)
const listCategories = (list: ListDetail): CategoryRow[] =>
  rows.value.filter(
    (row) =>
      row.category !== null &&
      row.members.some((member) => member.id === list.id),
  )
const visibleLists = computed(() => {
  const needle = query.value.trim().toLocaleLowerCase()
  return priorityDraft.value
    .map((id) => listsByID.value.get(id))
    .filter((list): list is ListDetail => list !== undefined)
    .filter(
      (list) =>
        matchesCategories(
          list.id,
          activeCategoryIDs.value,
          library.categories.value,
        ) &&
        [
          list.title,
          list.id,
          ...listCategories(list).map((category) => category.label),
        ].some((label) => label.toLocaleLowerCase().includes(needle)),
    )
})
const tableBody = useTemplateRef<HTMLElement>('tableBody')
const visibleOrder = computed({
  get: () => visibleLists.value.map((list) => list.id),
  set: (ids: string[]) => {
    if (tableDisabled.value) return
    const visible = new Set(ids)
    let index = 0
    priorityDraft.value = priorityDraft.value.map((id) =>
      visible.has(id) ? ids[index++]! : id,
    )
    priorityError.value = ''
  },
})
const sortable = useSortable(tableBody, visibleOrder, {
  handle: '.lists__handle:not(:disabled)',
  filter: '.lists__handle:disabled',
  preventOnFilter: false,
  ...useTableDragGeometry(),
  animation: 200,
  forceFallback: true,
  fallbackOnBody: true,
  watchElement: true,
  disabled: tableDisabled.value,
})
watch(tableDisabled, (disabled) => sortable.option('disabled', disabled), {
  flush: 'sync',
})
const movePriority = (id: string, offset: number): void => {
  if (tableDisabled.value) return
  const ids = [...visibleOrder.value]
  const from = ids.indexOf(id)
  const to = from + offset
  if (from < 0 || to < 0 || to >= ids.length) return
  ids.splice(from, 1)
  ids.splice(to, 0, id)
  visibleOrder.value = ids
}

const submitPriority = async (): Promise<void> => {
  if (!priorityDirty.value) return
  const result = await library.setDefaultPriority(priorityDraft.value)
  if (result.status === 'saved') {
    priorityError.value = ''
    return
  }
  if (result.status === 'failed')
    priorityError.value = t('lists.priority.failed.body')
}
</script>

<template>
  <section aria-labelledby="lists-title" class="lists">
    <RvPageHeader title-id="lists-title" :title="t('lists.title')">
      <!-- Renaming a category or retiring one is rare and belongs to no single
           row, so it is named here rather than hidden behind the plus beside
           the filters — that plus is where a new category is made, which is
           what a plus means everywhere else on this screen. -->
      <RvButton
        :disabled="library.state.value !== 'ready' || library.busy.value"
        @click="categoriesOpen = true"
        >{{ t('lists.manageCategories') }}</RvButton
      >
      <RvButton
        :disabled="
          library.state.value !== 'ready' ||
          library.busy.value ||
          library.stale.value
        "
        variant="primary"
        @click="startCreateList"
        ><RvIcon name="plus" />{{ t('lists.addList') }}</RvButton
      >
    </RvPageHeader>

    <RvStateNotice
      v-if="library.state.value === 'loading'"
      live
      :title="t('lists.loading')"
      tone="busy"
    />
    <RvStateNotice
      v-else-if="library.state.value === 'failed'"
      :body="t('lists.failed.body')"
      live
      :title="t('lists.failed')"
      tone="failed"
    >
      <template #action>
        <RvButton @click="library.initialize">{{ t('action.retry') }}</RvButton>
      </template>
    </RvStateNotice>

    <template v-else>
      <RvStateNotice
        v-if="library.stale.value"
        :body="t('lists.stale.body')"
        class="lists__notice"
        live
        :title="t('lists.stale')"
        tone="warning"
      >
        <template #action>
          <RvButton :disabled="library.busy.value" @click="retryLibraryRead">
            {{
              library.refreshing.value
                ? t('lists.refresh.busy')
                : t('lists.refresh')
            }}
          </RvButton>
        </template>
      </RvStateNotice>
      <CategoryFilters
        v-model="filter"
        v-model:query="query"
        :categories="library.categories.value"
        :lists="library.lists.value"
        :disabled="library.busy.value"
        :action-disabled="library.stale.value"
        :action-label="t('lists.addCategory')"
        @action="startCreateCategory"
      />
      <div v-if="priorityDirty" class="lists__order-bar">
        <RvButton
          v-if="priorityDirty"
          :disabled="tableDisabled"
          size="compact"
          variant="quiet"
          @click="resetPriority"
          >{{ t('lists.priority.cancel') }}</RvButton
        >
        <RvButton
          v-if="priorityDirty"
          :disabled="tableDisabled"
          size="compact"
          variant="primary"
          :loading="library.busy.value"
          @click="submitPriority"
          >{{ t('lists.priority.save') }}</RvButton
        >
      </div>
      <RvStateNotice
        v-if="paneRefusal !== null || priorityError !== ''"
        :title="
          t(
            priorityError
              ? 'lists.priority.failed'
              : paneRefusal?.kind === 'inUse'
                ? 'lists.category.inUse'
                : 'lists.category.failed',
          )
        "
        :body="
          priorityError ||
          (paneRefusal?.kind === 'inUse'
            ? t('lists.category.inUse.body', {
                profiles: paneRefusal.profiles.join(', '),
              })
            : t('lists.category.failed.body'))
        "
        live
        tone="failed"
      />
      <!-- The pane the table scrolls inside. It has no role and no name of its
           own, and a test reads the geometry and the scroll extent it owns, so
           it carries a test hook. -->
      <RvTable
        class="lists__workspace lists__pane-body"
        data-testid="rv-lists-workspace"
        dense
        sticky-header
      >
        <thead>
          <tr>
            <th class="lists__priority-column" scope="col">
              <span class="lists__visually-hidden">{{
                t('lists.priority.column')
              }}</span>
            </th>
            <th scope="col">{{ t('listPicker.column.list') }}</th>
            <th class="lists__category-column" scope="col">
              {{ t('listPicker.collections') }}
            </th>
            <th class="lists__action-column" scope="col">
              <span class="lists__visually-hidden">{{
                t('lists.manageCategories')
              }}</span>
            </th>
          </tr>
        </thead>
        <tbody ref="tableBody" class="lists__members">
          <tr
            v-for="list in visibleLists"
            :key="list.id"
            class="lists__list-row"
            :data-id="list.id"
            @click="openRow($event, list.id)"
          >
            <td class="lists__priority-column">
              <button
                type="button"
                class="lists__handle"
                :disabled="tableDisabled"
                :aria-label="
                  t('profile.priority.move.aria', {
                    list: list.title,
                    position: priorityDraft.indexOf(list.id) + 1,
                    total: priorityDraft.length,
                  })
                "
                @keydown.up.prevent="movePriority(list.id, -1)"
                @keydown.down.prevent="movePriority(list.id, 1)"
                @keydown.home.prevent="
                  movePriority(list.id, -visibleOrder.indexOf(list.id))
                "
                @keydown.end.prevent="
                  movePriority(
                    list.id,
                    visibleOrder.length - visibleOrder.indexOf(list.id) - 1,
                  )
                "
              >
                <RvIcon name="drag" />
              </button>
            </td>
            <th scope="row">
              <button
                type="button"
                class="lists__list-name"
                :disabled="library.busy.value"
                @click="openList(list.id)"
              >
                <span>{{ list.title }}</span>
              </button>
            </th>
            <td class="lists__category-column">
              <CategoryLabel
                v-for="category in listCategories(list)"
                :id="category.id"
                :key="category.id"
                :label="category.label"
              /><span v-if="listCategories(list).length === 0">{{
                t('listPicker.other')
              }}</span>
            </td>
            <td class="lists__action-column">
              <RvMenu
                :disabled="library.busy.value"
                :items="profileActions(list)"
                :label="t('lists.list.menu', { list: list.title })"
                @select="onProfileAction(list, $event)"
              />
              <button
                type="button"
                class="lists__open"
                :disabled="tableDisabled"
                :aria-label="t('listDetail.open.aria', { list: list.title })"
                @click="openList(list.id)"
              >
                <RvIcon name="chevron" class="lists__open-indicator" />
              </button>
            </td>
          </tr>
        </tbody>

        <p v-if="visibleLists.length === 0" class="lists__empty">
          {{
            query ? t('create.noMatches', { query }) : t('lists.category.empty')
          }}
        </p>
      </RvTable>
    </template>

    <RvDialog
      :open="categoriesOpen"
      :title="t('lists.manageCategories')"
      :close-label="t('action.close')"
      :dismissible="!library.busy.value"
      variant="panel"
      @update:open="categoriesOpen = $event"
    >
      <section aria-labelledby="lists-categories" class="lists__collections">
        <header class="lists__pane-header">
          <h2 id="lists-categories">{{ t('listPicker.collections') }}</h2>
        </header>
        <ul class="lists__groups">
          <li
            v-for="entry in rows"
            :key="entry.id"
            class="lists__group"
            :class="{ 'lists__group--active': activeRow?.id === entry.id }"
          >
            <div class="lists__category-row">
              <button
                :aria-current="activeRow?.id === entry.id ? 'true' : undefined"
                class="lists__category"
                :disabled="library.busy.value"
                type="button"
                @click="showCategory(entry.id)"
              >
                <span class="lists__category-copy">
                  <strong>{{ entry.label }}</strong>
                  <small>{{
                    tc('create.category.size', entry.members.length)
                  }}</small>
                </span>
                <RvIcon class="lists__category-mark" name="chevron" />
              </button>
              <RvMenu
                v-if="categoryActions(entry).length > 0"
                :disabled="library.busy.value"
                :items="categoryActions(entry)"
                :label="t('lists.category.menu', { category: entry.label })"
                @select="onCategoryAction(entry, $event)"
              />
            </div>
          </li>
        </ul>
      </section>
    </RvDialog>

    <RvDialog
      :close-label="t('action.close')"
      :dismissible="!library.busy.value"
      :open="categoryForm !== 'closed'"
      :title="
        categoryForm === 'rename'
          ? t('lists.category.rename.title')
          : t('lists.category.new')
      "
      variant="sheet"
      @update:open="$event === false && closeCategoryForm()"
    >
      <form class="lists__form" @submit.prevent="submitCategoryTitle">
        <RvField
          :error="categoryTitleError"
          input-id="lists-category-title"
          :label="t('lists.category.field')"
        >
          <template #default="{ describedBy, invalid }">
            <RvTextInput
              v-model="categoryTitle"
              :described-by="describedBy"
              :disabled="
                library.busy.value ||
                (library.stale.value && categoryForm === 'create')
              "
              input-id="lists-category-title"
              :invalid="invalid"
            />
          </template>
        </RvField>
        <RvStateNotice
          v-if="library.refusal.value !== null"
          :body="
            library.refusal.value.kind === 'inUse'
              ? t('lists.category.inUse.body', {
                  profiles: library.refusal.value.profiles.join(', '),
                })
              : t('lists.category.failed.body')
          "
          live
          :title="
            library.refusal.value.kind === 'inUse'
              ? t('lists.category.inUse')
              : t('lists.category.failed')
          "
          tone="failed"
        />
      </form>
      <template #footer>
        <RvButton
          :disabled="library.busy.value"
          variant="quiet"
          @click="closeCategoryForm"
        >
          {{ t('action.cancel') }}
        </RvButton>
        <RvButton
          :disabled="
            library.busy.value ||
            (library.stale.value && categoryForm === 'create')
          "
          :loading="library.busy.value"
          variant="primary"
          @click="submitCategoryTitle"
        >
          {{
            categoryForm === 'rename'
              ? t('lists.category.save')
              : t('lists.category.create')
          }}
        </RvButton>
      </template>
    </RvDialog>

    <ListSheet
      :busy="library.busy.value"
      :category-title="activeRow?.category?.title ?? null"
      :created-pending-attachment="pendingCreatedListID !== ''"
      :error="listSheetError"
      :existing="pickableLists"
      :open="listSheetOpen"
      @add="submitPickedLists"
      @close="closeListSheet"
      @create="createList"
      @retry-attachment="retryCreatedListAttachment"
    />

    <!-- Deleting a category asks the one question it has to ask: what becomes
         of the lists it holds. -->
    <RvDialog
      :close-label="t('action.close')"
      :dismissible="!library.busy.value"
      :open="removingCategory"
      :title="t('lists.category.remove')"
      variant="panel"
      @update:open="$event === false && closeCategoryRemoval()"
    >
      <div class="lists__form">
        <RvSegmented
          v-model="chosenDisposition"
          :disabled="library.busy.value"
          :label="t('lists.category.remove.question')"
          name="lists-category-disposition"
          :options="dispositions"
        />
        <RvStateNotice
          v-if="library.refusal.value !== null"
          :body="
            library.refusal.value.kind === 'inUse'
              ? t('lists.category.inUse.body', {
                  profiles: library.refusal.value.profiles.join(', '),
                })
              : t('lists.category.failed.body')
          "
          live
          :title="
            library.refusal.value.kind === 'inUse'
              ? t('lists.category.inUse')
              : t('lists.category.failed')
          "
          tone="failed"
        />
      </div>
      <template #footer>
        <RvButton
          :disabled="library.busy.value"
          variant="quiet"
          @click="closeCategoryRemoval"
        >
          {{ t('action.cancel') }}
        </RvButton>
        <RvButton
          :disabled="library.busy.value"
          :loading="library.busy.value"
          variant="primary"
          @click="confirmRemoveCategory"
        >
          {{ t('lists.category.remove.submit') }}
        </RvButton>
      </template>
    </RvDialog>

    <RvDialog
      :close-label="t('action.close')"
      :dismissible="!library.busy.value"
      :open="removingList !== null"
      :title="t('lists.list.remove')"
      variant="panel"
      @update:open="$event === false && closeListRemoval()"
    >
      <div class="lists__form">
        <p class="lists__form-note">
          {{
            t('lists.list.remove.body', {
              list: removingList?.title ?? '',
            })
          }}
        </p>
        <RvStateNotice
          v-if="library.refusal.value !== null"
          :body="
            library.refusal.value.kind === 'inUse'
              ? t('lists.list.inUse.body', {
                  profiles: library.refusal.value.profiles.join(', '),
                })
              : t('lists.category.failed.body')
          "
          live
          :title="
            library.refusal.value.kind === 'inUse'
              ? t('lists.list.inUse')
              : t('lists.category.failed')
          "
          tone="failed"
        />
      </div>
      <template #footer>
        <RvButton
          :disabled="library.busy.value"
          variant="quiet"
          @click="closeListRemoval"
        >
          {{ t('action.cancel') }}
        </RvButton>
        <RvButton
          :disabled="library.busy.value"
          :loading="library.busy.value"
          variant="primary"
          @click="confirmRemoveList"
        >
          {{ t('lists.list.remove.submit') }}
        </RvButton>
      </template>
    </RvDialog>

    <ListDetailDialog
      :disabled="library.busy.value"
      mode="library"
      :list="activeList"
      @close="closeList"
      @remove="startRemoveList"
      @updated="onListUpdated"
    />
  </section>
</template>

<style scoped>
.lists {
  container-type: inline-size;
  view-transition-name: library-content;
  display: flex;
  flex-direction: column;
  gap: var(--rv-space-4);
  width: 100%;
  min-width: 0;
  min-height: 0;
  height: 100%;
}

.lists__order-bar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--rv-space-3);
  font-size: var(--rv-text-meta);
  color: var(--rv-color-ink-muted);
}

.lists__details-title {
  font-size: var(--rv-text-dense);
}

.lists__priority-help {
  flex: 1 1 var(--rv-panel-width);
  min-width: 0;
  overflow-wrap: anywhere;
}

/* The table's standing height comes from the window rather than from what the
   header and the filters leave over: a definite height is what keeps the rows
   scrolling inside this frame, so a notice or the order bar lengthens the page
   instead of shortening the table. */
.lists__workspace {
  position: relative;
  flex: none;
  height: var(--rv-library-height);
  min-width: 0;
  min-height: 0;
  overflow: auto;
  overscroll-behavior: contain;

  --rv-table-min-width: var(--rv-measure-field);
  --rv-table-layout: fixed;
}

.lists__workspace thead th {
  color: var(--rv-color-ink-muted);
  font-weight: 600;
}

.lists__workspace .lists__priority-column {
  width: var(--rv-table-action-width);
}

.lists__workspace .lists__action-column {
  width: auto;
  min-width: calc(var(--rv-table-action-width) * 2);
  padding-inline: var(--rv-space-1);
  text-align: end;
  white-space: nowrap;
}

.lists__workspace th:nth-child(2) {
  width: var(--rv-catalog-name-column);
}

.lists__category-column {
  width: var(--rv-catalog-category-column);
}

.lists__list-row {
  cursor: pointer;
}

.lists__list-row:hover {
  background: var(--rv-color-surface-hover);
}

.lists__open-indicator {
  flex: none;
  transform: rotate(-90deg);
  color: var(--rv-color-ink-muted);
}

.lists__handle {
  display: inline-flex;
  align-items: center;
  gap: var(--rv-space-2);
  width: 100%;
  min-height: var(--rv-control-compact);
  padding: 0;
  color: var(--rv-color-ink-muted);
  font: inherit;
  font-variant-numeric: tabular-nums;
  background: transparent;
  border: 0;
  cursor: grab;
  touch-action: none;
}

.lists__handle:disabled {
  cursor: not-allowed;
  opacity: var(--rv-disabled-opacity);
}

.lists__list-name {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--rv-space-2);
  width: 100%;
  min-height: var(--rv-control-default);
  padding: var(--rv-space-2) 0;
  font: inherit;
  color: var(--rv-color-ink);
  text-align: start;
  overflow-wrap: anywhere;
  background: transparent;
  border: 0;
  cursor: pointer;
}

.lists__list-name > span {
  min-width: 0;
}

.lists__list-row.sortable-ghost {
  opacity: var(--rv-disabled-opacity);
}

.lists__collections {
  display: grid;
  min-height: 0;
}

.lists__pane-header {
  padding: var(--rv-space-3) var(--rv-space-5);
}

.lists__pane-header h2 {
  font-size: var(--rv-text-interface);
}

.lists__groups {
  max-height: var(--rv-overlay-height);
  overflow-y: auto;
}

.lists__group {
  border-bottom: var(--rv-border-hair) solid var(--rv-color-rule);
}

.lists__group--active {
  background: var(--rv-color-surface-muted);
}

.lists__category-row {
  display: flex;
  align-items: center;
  padding-inline-end: var(--rv-space-2);
}

.lists__category {
  display: flex;
  flex: 1;
  align-items: center;
  gap: var(--rv-space-3);
  justify-content: space-between;
  min-width: 0;
  min-height: var(--rv-row-dense);
  padding: var(--rv-space-2) var(--rv-space-5);
  font: inherit;
  color: var(--rv-color-ink);
  text-align: start;
  background: transparent;
  border: 0;
  cursor: pointer;
}

.lists__category-copy {
  display: grid;
  gap: var(--rv-space-1);
}

.lists__category-copy small {
  color: var(--rv-color-ink-muted);
}

.lists__category-mark {
  transform: rotate(-90deg);
}

.lists__form {
  display: grid;
  gap: var(--rv-space-4);
  padding: var(--rv-space-5) var(--rv-space-6);
}

.lists__empty {
  padding: var(--rv-space-5);
  color: var(--rv-color-ink-muted);
}

.lists__form-note {
  color: var(--rv-color-ink-muted);
}

.lists__visually-hidden {
  position: absolute;
  width: var(--rv-border-hair);
  height: var(--rv-border-hair);
  overflow: hidden;
  clip-path: inset(50%);
}

@media (width <= 64rem), (height <= 36rem) {
  .lists {
    view-transition-name: library-content;
    height: auto;
  }

  .lists__workspace {
    flex: none;
    height: var(--rv-picker-mobile-height);
  }
}

@container (width <= 40rem) {
  .lists__workspace .lists__priority-column {
    width: var(--rv-picker-priority-width);
    padding-inline: var(--rv-space-1);
  }

  .lists__workspace .lists__priority-column[scope='col'] {
    font-size: var(--rv-text-meta);
    overflow-wrap: anywhere;
  }
}

/* The fallback is portalled outside its table; retain table column geometry. */
.lists__list-row.sortable-fallback {
  display: table;
  table-layout: fixed;
  border-collapse: collapse;
  background: var(--rv-color-surface-muted);
}

.lists__open {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: var(--rv-control-compact);
  height: var(--rv-control-compact);
  padding: 0;
  color: var(--rv-color-ink-muted);
  background: transparent;
  border: 0;
  border-radius: var(--rv-radius-sm);
  cursor: pointer;
}

.lists__open:hover {
  background: var(--rv-color-surface-hover);
}
</style>
