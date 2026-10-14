<script setup lang="ts">
import type { UncoveredList } from '@/shared/api/outputs'
import { useLocale } from '@/shared/i18n/useLocale'
import { libraryPageHash } from '@/shared/lib/listsHash'

/**
 * The lists a refused build could not cover, each a link to the list in the
 * library, where its sources and entries are curated. The address is the
 * library's own fragment, because the service refuses a query string.
 */
defineProps<{ lists: UncoveredList[] }>()

const { t } = useLocale()
</script>

<template>
  <p>{{ t('profile.notice.build.partial_coverage.lists') }}</p>
  <ul class="uncovered">
    <li v-for="list in lists" :key="list.id">
      <NuxtLink :to="`/lists${libraryPageHash({ list: list.id })}`">{{
        list.title
      }}</NuxtLink>
    </li>
  </ul>
  <p>{{ t('profile.notice.build.partial_coverage.next') }}</p>
</template>

<style scoped>
.uncovered {
  display: grid;
  gap: var(--rv-space-1);
}

.uncovered a {
  color: var(--rv-color-accent-ink);
}
</style>
