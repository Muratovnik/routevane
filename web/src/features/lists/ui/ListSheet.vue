<script setup lang="ts">
import { computed, ref, watch } from 'vue'

import { useLocale } from '@/shared/i18n/useLocale'
import { normalizeDomain } from '@/shared/lib/destinationList'
import type { ChoiceOption, SegmentOption } from '@/shared/ui/kinds'
import RvButton from '@/shared/ui/RvButton.vue'
import RvCombobox from '@/shared/ui/RvCombobox.vue'
import RvDialog from '@/shared/ui/RvDialog.vue'
import RvField from '@/shared/ui/RvField.vue'
import RvSegmented from '@/shared/ui/RvSegmented.vue'
import RvStateNotice from '@/shared/ui/RvStateNotice.vue'
import RvTextInput from '@/shared/ui/RvTextInput.vue'
import RvTextarea from '@/shared/ui/RvTextarea.vue'

const props = defineProps<{
  busy: boolean
  categoryTitle: string | null
  createdPendingAttachment?: boolean
  error?: string
  existing: ChoiceOption[]
  open: boolean
}>()

const emit = defineEmits<{
  add: [listID: string]
  close: []
  create: [draft: { domains: string[]; title: string }]
  'retry-attachment': []
}>()

const { t } = useLocale()
const mode = ref<'create' | 'existing'>('create')
const title = ref('')
const domains = ref('')
const picked = ref('')
const titleError = ref('')
const domainsError = ref('')

const modeValue = computed<string>({
  get: () => mode.value,
  set: (value) => {
    mode.value = value === 'existing' ? 'existing' : 'create'
    titleError.value = ''
    domainsError.value = ''
  },
})

const modes = computed<SegmentOption[]>(() => [
  { label: t('lists.list.flow.create'), value: 'create' },
  { label: t('lists.list.flow.existing'), value: 'existing' },
])

watch(
  () => props.open,
  (open) => {
    if (!open) return
    mode.value = 'create'
    title.value = ''
    domains.value = ''
    picked.value = ''
    titleError.value = ''
    domainsError.value = ''
  },
)

function parsedDomains(): string[] | null {
  const result: string[] = []
  const lines = domains.value.split(/\r?\n/)
  for (let index = 0; index < lines.length; index += 1) {
    const line = lines[index]?.trim() ?? ''
    if (line === '') continue
    const domain = normalizeDomain(line)
    if (domain === null) {
      domainsError.value = t('listCard.new.domains.invalid', {
        line: index + 1,
      })
      return null
    }
    result.push(domain)
  }
  const unique = [...new Set(result)].sort()
  if (unique.length === 0) {
    domainsError.value = t('listCard.domains.required')
    return null
  }
  if (unique.length > 64) {
    domainsError.value = t('listCard.new.domains.limit')
    return null
  }
  domainsError.value = ''
  return unique
}

function submit(): void {
  if (props.busy) return
  if (mode.value === 'existing') {
    if (picked.value !== '') emit('add', picked.value)
    return
  }
  const nextTitle = title.value.trim()
  titleError.value = nextTitle === '' ? t('listCard.title.invalid') : ''
  const nextDomains = parsedDomains()
  if (nextTitle === '' || nextDomains === null) return
  emit('create', { domains: nextDomains, title: nextTitle })
}
</script>

<template>
  <RvDialog
    :close-label="t('action.close')"
    :dismissible="!busy"
    :open="open"
    :title="t('lists.list.new')"
    variant="sheet"
    @update:open="$event === false && emit('close')"
  >
    <form class="list-sheet" @submit.prevent="submit">
      <RvSegmented
        v-if="categoryTitle !== null && !createdPendingAttachment"
        v-model="modeValue"
        :disabled="busy"
        :label="t('lists.list.flow.label')"
        name="library-list-flow"
        :options="modes"
      />

      <template v-if="mode === 'create' && !createdPendingAttachment">
        <RvField
          :error="titleError"
          input-id="library-list-title"
          :label="t('listCard.title.field')"
        >
          <template #default="{ describedBy, invalid }">
            <RvTextInput
              v-model="title"
              :described-by="describedBy"
              :disabled="busy"
              input-id="library-list-title"
              :invalid="invalid"
              maxlength="120"
            />
          </template>
        </RvField>
        <RvField
          :error="domainsError"
          :hint="t('listCard.new.domains.hint')"
          input-id="library-list-domains"
          :label="t('listCard.new.domains.field')"
        >
          <template #default="{ describedBy, invalid }">
            <RvTextarea
              v-model="domains"
              :described-by="describedBy"
              :disabled="busy"
              input-id="library-list-domains"
              :invalid="invalid"
              :rows="6"
            />
          </template>
        </RvField>
      </template>

      <template v-else-if="mode === 'existing' && !createdPendingAttachment">
        <RvCombobox
          v-if="existing.length > 0"
          v-model="picked"
          :disabled="busy"
          :empty-label="t('lists.category.pick.empty')"
          input-id="library-existing-list"
          :options="existing"
          :placeholder="t('lists.category.pick.placeholder')"
          :toggle-label="t('lists.category.pick.toggle')"
        />
        <p v-else class="list-sheet__note">
          {{ t('lists.category.pick.none') }}
        </p>
      </template>

      <RvStateNotice
        v-if="error !== undefined && error !== ''"
        :body="error"
        live
        :title="t('lists.category.failed')"
        tone="failed"
      />
    </form>
    <template #footer>
      <RvButton :disabled="busy" variant="quiet" @click="emit('close')">
        {{ t('action.cancel') }}
      </RvButton>
      <RvButton
        :disabled="busy || (mode === 'existing' && picked === '')"
        :loading="busy"
        type="button"
        variant="primary"
        @click="createdPendingAttachment ? emit('retry-attachment') : submit()"
      >
        {{
          createdPendingAttachment
            ? t('lists.list.attach.retry')
            : mode === 'existing'
              ? t('lists.category.add')
              : t('lists.list.create')
        }}
      </RvButton>
    </template>
  </RvDialog>
</template>

<style scoped>
.list-sheet {
  display: grid;
  gap: var(--rv-space-5);
  padding: var(--rv-space-5) var(--rv-space-6);
}

.list-sheet__note {
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-dense);
}
</style>
