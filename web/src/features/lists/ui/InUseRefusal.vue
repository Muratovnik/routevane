<script setup lang="ts">
import { computed } from 'vue'

import type { HoldingProfile } from '@/features/lists/model/useLists'
import { useLocale } from '@/shared/i18n/useLocale'
import RvStateNotice from '@/shared/ui/RvStateNotice.vue'

/**
 * The refusal to delete a list or a category that profiles still name. It says
 * which profiles, each one a link to where it can be changed, and what to do
 * next for the kind of profile it is: a live one is edited, an archived one
 * cannot be and is deleted from the archive (ADR 0051).
 */
const props = defineProps<{
  profiles: HoldingProfile[]
  subject: 'category' | 'list'
}>()

const { t } = useLocale()
const hasLive = computed(() => props.profiles.some((entry) => !entry.archived))
const hasArchived = computed(() =>
  props.profiles.some((entry) => entry.archived),
)
</script>

<template>
  <RvStateNotice live :title="t(`lists.${subject}.inUse`)" tone="failed">
    <p>{{ t(`lists.${subject}.inUse.lead`) }}</p>
    <ul class="in-use__profiles">
      <li v-for="entry in profiles" :key="entry.id">
        <NuxtLink :to="`/profiles/${entry.id}`">{{ entry.title }}</NuxtLink>
        <span v-if="entry.archived" class="in-use__archived">{{
          t('lists.inUse.archived')
        }}</span>
      </li>
    </ul>
    <p v-if="hasLive">{{ t(`lists.${subject}.inUse.next`) }}</p>
    <p v-if="hasArchived">{{ t('lists.inUse.archived.next') }}</p>
  </RvStateNotice>
</template>

<style scoped>
.in-use__profiles {
  display: grid;
  gap: var(--rv-space-1);
}

.in-use__profiles a {
  color: var(--rv-color-accent-ink);
}

/* A template drops the space before the mark, so the gap is stated here. */
.in-use__archived {
  margin-inline-start: var(--rv-space-1);
}
</style>
