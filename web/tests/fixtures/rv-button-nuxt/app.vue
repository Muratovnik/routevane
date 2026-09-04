<script setup lang="ts">
import { ref } from 'vue'

import RvButton from '../../../src/shared/ui/RvButton.vue'

const activations = ref(0)
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
    </main>
  </UApp>
</template>
