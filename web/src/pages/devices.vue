<script setup lang="ts">
/**
 * The section this address named is now Connections. A bookmark and a link in
 * someone's notes still resolve: the router replaces the address before the
 * page renders, so going back does not return to a page that no longer exists
 * and no empty document is drawn in between.
 */
definePageMeta({ redirect: '/connections' })
</script>

<template>
  <div />
</template>
