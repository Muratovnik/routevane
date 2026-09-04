<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'

import ServiceDetailDialog from '@/entities/list-composition/ui/ServiceDetailDialog.vue'
import { useListLibrary } from '@/features/list-library/model/useListLibrary'
import type {
  CategoryDetail,
  CategoryLists,
  ServiceDetail,
} from '@/shared/api/catalog'
import { useLocale } from '@/shared/i18n/useLocale'
import { libraryPageHash, parseLibraryPageHash } from '@/shared/lib/libraryHash'
import type { ChoiceOption, MenuItem, SegmentOption } from '@/shared/ui/kinds'
import RvButton from '@/shared/ui/RvButton.vue'
import RvCombobox from '@/shared/ui/RvCombobox.vue'
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
 * composing a route can write nothing but the route. The geometry is the
 * composer's, and deliberately so: the same two columns, without the
 * checkboxes, because this screen is not selecting anything.
 */
const { t, tc, tor } = useLocale()
const library = useListLibrary()
const route = useRoute()
const router = useRouter()

// The row for lists no category claims. Unlike the composer's, it is always
// drawn: it is where a detached list goes, so it is a place rather than a
// leftover.
const uncategorizedID = 'rv:uncategorized'

type CategoryRow = {
  id: string
  label: string
  // The category this row stands for, or null for the computed last row.
  category: CategoryDetail | null
  members: ServiceDetail[]
}

const activeCategoryID = ref('')
const activeServiceID = ref('')
const creatingService = ref(false)
const mobilePane = ref<'collections' | 'details'>('collections')

const categoryForm = ref<'closed' | 'create' | 'rename'>('closed')
const categoryTitle = ref('')
const categoryTitleTouched = ref(false)
// A category created while the catalog is stale cannot be selected from the
// retained copy. Keep its server identity until a successful GET confirms that
// the row exists, then complete the selection.
const pendingCategoryID = ref('')
const pickingList = ref(false)
const pickedListID = ref('')
const removingCategory = ref(false)
const categoryLists = ref<CategoryLists>('detach')
const removingService = ref<ServiceDetail | null>(null)

// The control speaks in strings; only the two answers the request has are
// admitted back, so an unexpected value never reaches the endpoint.
const chosenDisposition = computed<string>({
  get: () => categoryLists.value,
  set: (value) => {
    categoryLists.value = value === 'delete' ? 'delete' : 'detach'
  },
})

const servicesByID = computed(
  () => new Map(library.services.value.map((service) => [service.id, service])),
)

const uncategorized = computed(() => {
  const claimed = new Set(
    library.categories.value.flatMap((category) => category.services),
  )
  return library.services.value.filter((service) => !claimed.has(service.id))
})

const rows = computed<CategoryRow[]>(() => [
  ...library.categories.value.map((category) => ({
    category,
    id: category.id,
    label: tor(`category.${category.id}`, category.title),
    members: category.services
      .map((id) => servicesByID.value.get(id))
      .filter((service): service is ServiceDetail => service !== undefined),
  })),
  {
    category: null,
    id: uncategorizedID,
    label: t('servicePicker.other'),
    members: uncategorized.value,
  },
])

const activeRow = computed<CategoryRow | null>(
  () =>
    rows.value.find((entry) => entry.id === activeCategoryID.value) ??
    rows.value[0] ??
    null,
)

const activeService = computed(
  () => servicesByID.value.get(activeServiceID.value) ?? null,
)

// A refusal belongs to the act that caused it: while a confirmation is open it
// is stated there, because that is where the operator is standing.
const confirming = computed(
  () => removingCategory.value || removingService.value !== null,
)
const paneRefusal = computed(() =>
  confirming.value ? null : library.refusal.value,
)

// What a category can be asked to do. The catalog ships the words and the
// operator owns what is inside; the title belongs only to a category they
// created, and the operator may remove either kind (ADR 0029).
const categoryActions = computed<MenuItem[]>(() => {
  const category = activeRow.value?.category ?? null
  if (category === null) return []
  const items: MenuItem[] = [
    {
      disabled: library.stale.value || library.busy.value,
      icon: 'plus',
      key: 'add',
      label: t('lists.category.addList'),
    },
  ]
  if (category.custom) {
    items.push({
      disabled: library.busy.value,
      icon: 'edit',
      key: 'rename',
      label: t('lists.category.rename'),
      separatorBefore: true,
    })
  }
  items.push({
    disabled: library.busy.value,
    icon: 'trash',
    key: 'remove',
    label: t('lists.category.remove'),
    separatorBefore: !category.custom,
  })
  return items
})

const listActions = computed<MenuItem[]>(() => {
  const items: MenuItem[] = []
  if ((activeRow.value?.category ?? null) !== null)
    items.push({
      disabled: library.stale.value || library.busy.value,
      key: 'detach',
      label: t('lists.list.detach'),
    })
  items.push({
    disabled: library.busy.value,
    icon: 'trash',
    key: 'remove',
    label: t('lists.list.remove'),
    separatorBefore: items.length > 0,
  })
  return items
})

// Every list this category does not already hold, named the way the rows name
// it. The combobox is the search, so the whole catalog can be offered.
const pickableLists = computed<ChoiceOption[]>(() => {
  const category = activeRow.value?.category ?? null
  if (category === null) return []
  const held = new Set(category.services)
  return library.services.value
    .filter((service) => !held.has(service.id))
    .map((service) => ({ label: service.title, value: service.id }))
})

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
  activeCategoryID.value = opened.category
  activeServiceID.value = opened.list
  if (opened.list !== '') mobilePane.value = 'details'
  void library.initialize()
})

watch(
  () => library.stale.value,
  (isStale, wasStale) => {
    if (wasStale && !isStale) settlePendingCategory()
  },
)

function select(categoryID: string): void {
  activeCategoryID.value = categoryID
  library.clearRefusal()
  writeLocation()
}

function showCategory(categoryID: string): void {
  select(categoryID)
  mobilePane.value = 'details'
}

function showCollections(): void {
  mobilePane.value = 'collections'
}

function openService(serviceID: string): void {
  activeServiceID.value = serviceID
  writeLocation()
}

function startCreateService(): void {
  if (library.stale.value) return
  activeServiceID.value = ''
  creatingService.value = true
}

function closeService(): void {
  activeServiceID.value = ''
  creatingService.value = false
  writeLocation()
}

// The address states where the operator is, so a refresh, a bookmark and the
// composing card's link all land on the same category and the same open list.
function writeLocation(): void {
  void router.replace(
    `/library${libraryPageHash({
      category: activeCategoryID.value,
      list: activeServiceID.value,
    })}`,
  )
}

function startCreateCategory(): void {
  if (library.stale.value) return
  categoryTitle.value = ''
  categoryTitleTouched.value = false
  categoryForm.value = 'create'
}

function committed(status: string): boolean {
  return status === 'saved' || status === 'stale'
}

function settlePendingCategory(): void {
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
    select(rows.value[0]?.id ?? uncategorizedID)
}

async function retryLibraryRead(): Promise<void> {
  if (await library.refresh()) settlePendingCategory()
}

function onCategoryAction(key: string): void {
  const category = activeRow.value?.category ?? null
  if (category === null || library.busy.value) return
  if (key === 'add') {
    if (library.stale.value) return
    pickedListID.value = ''
    pickingList.value = true
    return
  }
  if (key === 'rename') {
    categoryTitle.value = category.title
    categoryTitleTouched.value = false
    categoryForm.value = 'rename'
    return
  }
  if (key === 'remove') {
    categoryLists.value = 'detach'
    library.clearRefusal()
    removingCategory.value = true
  }
}

function onListAction(service: ServiceDetail, key: string): void {
  if (library.busy.value) return
  const category = activeRow.value?.category ?? null
  if (key === 'detach' && category !== null && !library.stale.value) {
    void library.detachList(category, service.id)
    return
  }
  if (key === 'remove') startRemoveService(service)
}

function startRemoveService(service: ServiceDetail): void {
  if (library.busy.value) return
  library.clearRefusal()
  removingService.value = service
}

async function submitCategoryTitle(): Promise<void> {
  categoryTitleTouched.value = true
  const title = categoryTitle.value.trim()
  if (title === '') return
  const category = activeRow.value?.category ?? null
  if (categoryForm.value === 'rename') {
    if (category === null) return
    const result = await library.renameCategory(category.id, title)
    if (committed(result.status)) categoryForm.value = 'closed'
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

async function submitPickedList(): Promise<void> {
  const category = activeRow.value?.category ?? null
  if (category === null || pickedListID.value === '') return
  const result = await library.addList(category, pickedListID.value)
  if (committed(result.status)) pickingList.value = false
}

async function confirmRemoveCategory(): Promise<void> {
  const category = activeRow.value?.category ?? null
  if (category === null) return
  const result = await library.deleteCategory(category.id, categoryLists.value)
  if (committed(result.status)) {
    removingCategory.value = false
    // The uncategorized row is a computed safe context and remains valid even
    // while the deleted category is still present in the retained copy.
    select(uncategorizedID)
  }
}

async function confirmRemoveService(): Promise<void> {
  const service = removingService.value
  if (service === null) return
  const result = await library.deleteList(service.id)
  if (committed(result.status)) {
    removingService.value = null
    if (activeServiceID.value === service.id) closeService()
  }
}

/**
 * A list created while a category is open joins that category, in one edit
 * after the one that created it. The computed row holds nothing, so a list made
 * there is simply a list no category claims.
 */
async function onServiceCreated(detail: ServiceDetail): Promise<void> {
  creatingService.value = false
  const category = activeRow.value?.category ?? null
  if (category === null) {
    await library.refresh()
    return
  }
  await library.addList(category, detail.id)
}

function onServiceUpdated(): void {
  void library.refresh()
}
</script>

<template>
  <section aria-labelledby="lists-title" class="lists">
    <header class="lists__header">
      <h1 id="lists-title" class="lists__title">{{ t('lists.title') }}</h1>
    </header>

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
      <div class="lists__workspace" :class="`lists__workspace--${mobilePane}`">
        <section aria-labelledby="lists-categories" class="lists__collections">
          <header class="lists__pane-header">
            <h2 id="lists-categories">{{ t('servicePicker.collections') }}</h2>
          </header>
          <ul class="lists__groups">
            <li
              v-for="entry in rows"
              :key="entry.id"
              class="lists__group"
              :class="{ 'lists__group--active': activeRow?.id === entry.id }"
            >
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
            </li>
          </ul>
          <footer class="lists__collections-footer">
            <RvButton
              block
              :disabled="library.busy.value || library.stale.value"
              size="compact"
              type="button"
              variant="quiet"
              @click="startCreateCategory"
            >
              <RvIcon name="plus" />
              {{ t('lists.addCategory') }}
            </RvButton>
          </footer>
        </section>

        <section
          v-if="activeRow !== null"
          aria-labelledby="lists-details-title"
          class="lists__details"
        >
          <header class="lists__details-header">
            <button class="lists__back" type="button" @click="showCollections">
              <RvIcon name="chevron" />
              {{ t('servicePicker.back') }}
            </button>
            <h2 id="lists-details-title" class="lists__details-title">
              {{ activeRow.label }}
            </h2>
            <strong class="lists__details-count">
              {{ tc('create.category.size', activeRow.members.length) }}
            </strong>
            <RvMenu
              v-if="categoryActions.length > 0"
              :disabled="library.busy.value"
              :items="categoryActions"
              :label="t('lists.category.menu', { category: activeRow.label })"
              @select="onCategoryAction"
            />
          </header>
          <div class="lists__pane-body">
            <RvStateNotice
              v-if="paneRefusal !== null"
              :body="
                paneRefusal.kind === 'inUse'
                  ? t('lists.category.inUse.body', {
                      routes: paneRefusal.routes.join(', '),
                    })
                  : t('lists.category.failed.body')
              "
              class="lists__notice"
              live
              :title="
                paneRefusal.kind === 'inUse'
                  ? t('lists.category.inUse')
                  : t('lists.category.failed')
              "
              tone="failed"
            />
            <ul class="lists__members">
              <li
                v-for="service in activeRow.members"
                :key="service.id"
                class="lists__list-row"
              >
                <span class="lists__list-name">{{ service.title }}</span>
                <button
                  :aria-label="
                    t('serviceDetail.open.aria', { service: service.title })
                  "
                  class="lists__open"
                  :disabled="library.busy.value"
                  type="button"
                  @click="openService(service.id)"
                >
                  <RvIcon name="chevron" />
                </button>
                <RvMenu
                  :disabled="library.busy.value"
                  :items="listActions"
                  :label="t('lists.list.menu', { list: service.title })"
                  @select="onListAction(service, $event)"
                />
              </li>
            </ul>
            <p v-if="activeRow.members.length === 0" class="lists__empty">
              {{ t('lists.category.empty') }}
            </p>
          </div>
          <footer class="lists__details-footer">
            <RvButton
              block
              :disabled="library.busy.value || library.stale.value"
              size="compact"
              type="button"
              variant="quiet"
              @click="startCreateService"
            >
              <RvIcon name="plus" />
              {{ t('lists.addList') }}
            </RvButton>
          </footer>
        </section>
      </div>
    </template>

    <RvDialog
      :close-label="t('action.close')"
      :dismissible="!library.busy.value"
      :open="categoryForm !== 'closed'"
      :title="
        categoryForm === 'rename'
          ? t('lists.category.rename.title')
          : t('lists.category.new')
      "
      variant="panel"
      @update:open="categoryForm = 'closed'"
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
      </form>
      <template #footer>
        <RvButton
          :disabled="library.busy.value"
          variant="quiet"
          @click="categoryForm = 'closed'"
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

    <RvDialog
      :close-label="t('action.close')"
      :dismissible="!library.busy.value"
      :open="pickingList"
      :title="t('lists.category.addList')"
      variant="panel"
      @update:open="pickingList = false"
    >
      <div class="lists__form">
        <RvCombobox
          v-if="pickableLists.length > 0"
          v-model="pickedListID"
          :disabled="library.busy.value || library.stale.value"
          :empty-label="t('lists.category.pick.empty')"
          input-id="lists-category-list"
          :options="pickableLists"
          :placeholder="t('lists.category.pick.placeholder')"
          :loading="library.busy.value"
          :toggle-label="t('lists.category.pick.toggle')"
        />
        <p v-else class="lists__form-note">
          {{ t('lists.category.pick.none') }}
        </p>
      </div>
      <template #footer>
        <RvButton
          :disabled="library.busy.value"
          variant="quiet"
          @click="pickingList = false"
        >
          {{ t('action.cancel') }}
        </RvButton>
        <RvButton
          :disabled="
            library.busy.value || library.stale.value || pickedListID === ''
          "
          :loading="library.busy.value"
          variant="primary"
          @click="submitPickedList"
        >
          {{ t('lists.category.add') }}
        </RvButton>
      </template>
    </RvDialog>

    <!-- Deleting a category asks the one question it has to ask: what becomes
         of the lists it holds. -->
    <RvDialog
      :close-label="t('action.close')"
      :dismissible="!library.busy.value"
      :open="removingCategory"
      :title="t('lists.category.remove')"
      variant="panel"
      @update:open="removingCategory = false"
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
                  routes: library.refusal.value.routes.join(', '),
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
          @click="removingCategory = false"
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
      :open="removingService !== null"
      :title="t('lists.list.remove')"
      variant="panel"
      @update:open="removingService = null"
    >
      <div class="lists__form">
        <p class="lists__form-note">
          {{
            t('lists.list.remove.body', {
              list: removingService?.title ?? '',
            })
          }}
        </p>
        <RvStateNotice
          v-if="library.refusal.value !== null"
          :body="
            library.refusal.value.kind === 'inUse'
              ? t('lists.list.inUse.body', {
                  routes: library.refusal.value.routes.join(', '),
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
          @click="removingService = null"
        >
          {{ t('action.cancel') }}
        </RvButton>
        <RvButton
          :disabled="library.busy.value"
          :loading="library.busy.value"
          variant="primary"
          @click="confirmRemoveService"
        >
          {{ t('lists.list.remove.submit') }}
        </RvButton>
      </template>
    </RvDialog>

    <ServiceDetailDialog
      :creating="creatingService"
      :disabled="library.busy.value"
      mode="library"
      :service="activeService"
      @close="closeService"
      @created="onServiceCreated"
      @remove="startRemoveService"
      @updated="onServiceUpdated"
    />
  </section>
</template>

<style scoped src="./ListLibraryView.css"></style>
