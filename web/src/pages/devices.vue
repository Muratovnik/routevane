<script setup lang="ts">
import { onMounted } from 'vue'

/**
 * The section this address named is now Connections. A bookmark and a link in
 * someone's notes still resolve: the address is replaced rather than pushed,
 * so going back does not return to a page that no longer exists.
 */
const router = useRouter()

onMounted(() => {
  void router.replace('/connections')
})
</script>

<template>
  <div />
</template>
