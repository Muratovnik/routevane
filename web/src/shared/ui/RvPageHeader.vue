<script setup lang="ts">
/**
 * A page's title row. A page that sits under another object names the way back
 * as a trail of links above its title, in compact metadata type, so the title
 * keeps its leading alignment and the way back is at the top of the page.
 */
defineProps<{
  title: string
  titleId: string
  /** The objects this page sits under, outermost first. */
  breadcrumbs?: { label: string; to: string }[]
  /** The accessible name of the trail. */
  breadcrumbLabel?: string
}>()
</script>

<template>
  <header class="rv-page-header">
    <nav
      v-if="breadcrumbs !== undefined && breadcrumbs.length > 0"
      :aria-label="breadcrumbLabel"
      class="rv-page-header__trail"
    >
      <ol class="rv-page-header__crumbs">
        <li
          v-for="crumb in breadcrumbs"
          :key="crumb.to"
          class="rv-page-header__crumb"
        >
          <NuxtLink class="rv-page-header__link" :to="crumb.to">
            {{ crumb.label }}
          </NuxtLink>
        </li>
      </ol>
    </nav>
    <h1 :id="titleId" class="rv-page-header__title">{{ title }}</h1>
    <div v-if="$slots.default" class="rv-page-header__actions"><slot /></div>
  </header>
</template>

<style scoped>
.rv-page-header {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: var(--rv-space-4);
  min-height: var(--rv-control-touch);
}

.rv-page-header__trail {
  flex-basis: 100%;
  margin-bottom: calc(var(--rv-space-3) * -1);
}

.rv-page-header__crumbs {
  display: flex;
  flex-wrap: wrap;
  gap: var(--rv-space-2);
  margin: 0;
  padding: 0;
  list-style: none;
}

/* The separator is drawn, not read: the list already says where one crumb ends. */
.rv-page-header__crumb + .rv-page-header__crumb::before {
  margin-inline-end: var(--rv-space-2);
  color: var(--rv-color-ink-tertiary);
  content: '›' / '';
}

.rv-page-header__link {
  color: var(--rv-color-ink-muted);
  font-size: var(--rv-text-meta);
  text-decoration: none;
  overflow-wrap: anywhere;
}

.rv-page-header__link:hover {
  color: var(--rv-color-ink);
  text-decoration: underline;
}

.rv-page-header__link:focus-visible {
  outline: var(--rv-border-mark) solid var(--rv-color-focus);
  outline-offset: var(--rv-focus-offset);
}

.rv-page-header__title {
  min-width: 0;
  font-size: var(--rv-text-page);
  line-height: var(--rv-leading-tight);
  letter-spacing: var(--rv-tracking-title);
  overflow-wrap: anywhere;
}

.rv-page-header__actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--rv-space-3);
}
</style>
