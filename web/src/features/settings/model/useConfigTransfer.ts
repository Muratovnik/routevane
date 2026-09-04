import { computed, ref } from 'vue'

import {
  applyConfigTransfer,
  configTransferMaximumFileBytes,
  downloadConfigTransfer,
  parseConfigTransferDocument,
  previewConfigTransfer,
  type ConfigTransferApplyResult,
  type ConfigTransferDocument,
  type ConfigTransferPreview,
} from '@/shared/api/configTransfer'

const maximumFileBytes = configTransferMaximumFileBytes

type TransferState =
  'idle' | 'reading' | 'selected' | 'previewing' | 'ready' | 'failed'

type DownloadState = 'idle' | 'downloading' | 'failed'
type ApplyState = 'idle' | 'applying' | 'failed'
type TransferFailure = 'fileType' | 'fileSize' | 'fileRead' | 'preview' | ''

export function useConfigTransfer() {
  const state = ref<TransferState>('idle')
  const downloadState = ref<DownloadState>('idle')
  const applyState = ref<ApplyState>('idle')
  const document = ref<ConfigTransferDocument | null>(null)
  const fileName = ref('')
  const preview = ref<ConfigTransferPreview | null>(null)
  const applied = ref<ConfigTransferApplyResult | null>(null)
  const failure = ref<TransferFailure>('')

  // A chosen file invalidates both the answer and any promise still working on
  // the previous one. A late preview can never re-enable Apply for bytes the
  // operator has already replaced.
  let generation = 0

  const canPreview = computed(
    () => document.value !== null && state.value !== 'reading',
  )
  const canApply = computed(
    () =>
      preview.value !== null &&
      state.value === 'ready' &&
      applyState.value !== 'applying',
  )

  async function choose(file: File): Promise<void> {
    generation += 1
    const currentGeneration = generation
    preview.value = null
    applied.value = null
    applyState.value = 'idle'
    failure.value = ''
    document.value = null
    // The visible picker keeps the operator's choice while validation and
    // reading run. A failure belongs to that named file, not to an anonymous
    // state notice below an empty native control.
    fileName.value = file.name

    if (!file.name.toLowerCase().endsWith('.json')) {
      failure.value = 'fileType'
      state.value = 'failed'
      return
    }
    if (file.size > maximumFileBytes) {
      failure.value = 'fileSize'
      state.value = 'failed'
      return
    }

    state.value = 'reading'
    try {
      const bytes = await file.arrayBuffer()
      // Keep a leading UTF-8 BOM visible to JSON validation instead of
      // silently removing bytes from the document the server would see.
      const text = new TextDecoder('utf-8', {
        fatal: true,
        ignoreBOM: true,
      }).decode(bytes)
      const parsed = parseConfigTransferDocument(text)
      if (currentGeneration !== generation) return
      document.value = parsed
      state.value = 'selected'
    } catch {
      if (currentGeneration !== generation) return
      failure.value = 'fileRead'
      state.value = 'failed'
    }
  }

  async function previewSelected(): Promise<boolean> {
    const transfer = document.value
    if (transfer === null || state.value === 'reading') return false

    generation += 1
    const currentGeneration = generation
    preview.value = null
    applied.value = null
    failure.value = ''
    state.value = 'previewing'
    try {
      const nextPreview = await previewConfigTransfer(transfer)
      if (currentGeneration !== generation || document.value !== transfer)
        return false
      preview.value = nextPreview
      state.value = 'ready'
      return true
    } catch {
      if (currentGeneration !== generation || document.value !== transfer)
        return false
      failure.value = 'preview'
      state.value = 'failed'
      return false
    }
  }

  async function apply(): Promise<boolean> {
    const currentPreview = preview.value
    const transfer = document.value
    if (
      currentPreview === null ||
      transfer === null ||
      state.value !== 'ready' ||
      applyState.value === 'applying'
    )
      return false

    const currentGeneration = generation
    applyState.value = 'applying'
    try {
      const result = await applyConfigTransfer(currentPreview.digest, transfer)
      // A new file selection invalidates the preview while this request is in
      // flight. Do not let a late response claim that the replacement was
      // applied, or navigate away from the newly selected file.
      if (currentGeneration !== generation || document.value !== transfer) {
        applyState.value = 'idle'
        return false
      }
      applied.value = result
      applyState.value = 'idle'
      return true
    } catch {
      if (currentGeneration !== generation || document.value !== transfer) {
        applyState.value = 'idle'
        return false
      }
      applyState.value = 'failed'
      return false
    }
  }

  async function download(): Promise<boolean> {
    if (downloadState.value === 'downloading') return false
    downloadState.value = 'downloading'
    try {
      await downloadConfigTransfer()
      downloadState.value = 'idle'
      return true
    } catch {
      downloadState.value = 'failed'
      return false
    }
  }

  return {
    applied,
    apply,
    applyState,
    canApply,
    canPreview,
    choose,
    document,
    download,
    downloadState,
    fileName,
    failure,
    maximumFileBytes,
    preview,
    previewSelected,
    state,
  }
}
