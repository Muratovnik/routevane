<script setup lang="ts">
import { ref } from 'vue'

import RvButton from '../../../src/shared/ui/RvButton.vue'
import RvFilePicker from '../../../src/shared/ui/RvFilePicker.vue'
import RvSegmented from '../../../src/shared/ui/RvSegmented.vue'
import RvSelect from '../../../src/shared/ui/RvSelect.vue'
import RvSwitch from '../../../src/shared/ui/RvSwitch.vue'
import RvTextarea from '../../../src/shared/ui/RvTextarea.vue'
import RvTextInput from '../../../src/shared/ui/RvTextInput.vue'
import RvTooltip from '../../../src/shared/ui/RvTooltip.vue'

const activations = ref(0)
const text = ref('Typed value')
const notes = ref('Typed notes')
const chosen = ref('daily')
const segment = ref('auto')
const on = ref(true)
const off = ref(false)
const kept = ref(false)

const OPTIONS = [
  { label: 'Daily', value: 'daily' },
  { label: 'Weekly', value: 'weekly' },
]
const LONG_HINT =
  'Rebuilds every file of this profile from the lists it holds now, and keeps the files it published before.'
const SEGMENTS = [
  { label: 'Automatic', value: 'auto' },
  { label: 'By hand', value: 'manual' },
]
</script>

<template>
  <UApp>
    <main>
      <RvButton
        data-testid="disabled-href"
        disabled
        href="/probe-href"
        @click="activations += 1"
      >
        Disabled download
      </RvButton>
      <RvButton
        data-testid="disabled-to"
        disabled
        to="/probe-to"
        @click="activations += 1"
      >
        Disabled route
      </RvButton>
      <output aria-label="Activation count" data-testid="activations">
        {{ activations }}
      </output>

      <!-- The final confirmation of an act that cannot be taken back. -->
      <RvButton variant="danger">Delete permanently</RvButton>

      <!-- Every control that can be unavailable, once available and once not,
           drawn by the real library: the disabled look is read off these. -->
      <section aria-label="Controls">
        <RvButton variant="secondary">Open button</RvButton>
        <RvButton disabled variant="secondary">Closed button</RvButton>
        <RvTextInput v-model="text" aria-label="Open field" />
        <RvTextInput v-model="text" aria-label="Closed field" disabled />
        <RvTextarea v-model="notes" aria-label="Open editor" input-id="open" />
        <RvTextarea
          v-model="notes"
          aria-label="Closed editor"
          disabled
          input-id="closed"
        />
        <span id="open-select-label">Open select</span>
        <RvSelect
          v-model="chosen"
          input-id="open-select"
          labelled-by="open-select-label"
          :options="OPTIONS"
          placeholder="Choose"
        />
        <span id="closed-select-label">Closed select</span>
        <RvSelect
          v-model="chosen"
          disabled
          input-id="closed-select"
          labelled-by="closed-select-label"
          :options="OPTIONS"
          placeholder="Choose"
        />
        <RvSwitch v-model="off" label="Open switch" />
        <RvSwitch v-model="off" disabled label="Closed switch" />
        <RvSwitch v-model="on" label="Open switch on" />
        <RvSwitch v-model="on" disabled label="Closed switch on" />
        <RvSwitch
          v-model="kept"
          description="Kept on this computer only."
          label="Described switch"
        />
        <RvSegmented
          v-model="segment"
          label="Open choice"
          name="open"
          :options="SEGMENTS"
        />
        <RvSegmented
          v-model="segment"
          disabled
          label="Closed choice"
          name="closed"
          :options="SEGMENTS"
        />
        <RvFilePicker
          action-label="Choose"
          empty-label="No file selected"
          hint="JSON, up to 64 MiB"
          input-id="open-file"
          label="Open file"
        />
        <RvFilePicker
          action-label="Unavailable"
          disabled
          empty-label="No file selected"
          hint="JSON, up to 64 MiB"
          input-id="closed-file"
          label="Closed file"
        />
      </section>

      <!-- A tooltip longer than one line, drawn by the real library. -->
      <section aria-label="Tooltips">
        <RvTooltip :text="LONG_HINT">
          <button type="button">Explained action</button>
        </RvTooltip>
      </section>
    </main>
  </UApp>
</template>
