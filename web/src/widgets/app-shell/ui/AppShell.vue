<script setup lang="ts">
import { computed } from 'vue'

import { useLocale } from '@/shared/i18n/useLocale'
import type { IconName } from '@/shared/ui/kinds'
import RvIcon from '@/shared/ui/RvIcon.vue'

/**
 * The chrome: the product's name and its sections. Sections mirror the object
 * types the product holds — lists, connections, settings — and nothing here is
 * a step: every section is reachable at any time. Preferences and the
 * service's own address live on the settings screen, not in the chrome.
 */
const { locale, t } = useLocale()
const route = useRoute()

// The document's language is part of the head, not a one-time DOM write: a page
// re-rendering its title would otherwise restore the build-time default and
// announce English copy as Russian.
useHead({ htmlAttrs: { lang: locale } })

const sections = computed<
  { id: string; icon: IconName; label: string; to: string; active: boolean }[]
>(() => {
  const path = route.path
  return [
    {
      id: 'library',
      icon: 'library',
      label: t('shell.nav.library'),
      to: '/',
      active: path === '/' || path.startsWith('/lists'),
    },
    {
      id: 'lists',
      // The list glyph belongs to the routes shelf above; a stack of sheets is
      // the nearest thing this set has to "the material a route is made of".
      icon: 'copy',
      label: t('shell.nav.lists'),
      to: '/library',
      active: path.startsWith('/library'),
    },
    {
      id: 'connections',
      icon: 'targets',
      label: t('shell.nav.connections'),
      to: '/connections',
      // The retired address still resolves, and the link it lands on is the
      // one that should look current while it does.
      active: path.startsWith('/connections') || path.startsWith('/devices'),
    },
    {
      id: 'settings',
      icon: 'settings',
      label: t('shell.nav.settings'),
      to: '/settings',
      active: path.startsWith('/settings'),
    },
  ]
})
</script>

<template>
  <div class="shell">
    <a class="shell__skip" href="#content">{{ t('shell.skip') }}</a>
    <header class="shell__side">
      <p class="shell__product">{{ t('shell.product') }}</p>
      <nav :aria-label="t('shell.nav')" class="shell__nav">
        <NuxtLink
          v-for="section in sections"
          :key="section.id"
          :aria-current="section.active ? 'page' : undefined"
          class="shell__nav-link"
          :class="{ 'shell__nav-link--active': section.active }"
          :to="section.to"
        >
          <RvIcon class="shell__nav-icon" :name="section.icon" />
          {{ section.label }}
        </NuxtLink>
      </nav>
    </header>

    <main id="content" class="shell__main">
      <div class="shell__measure">
        <slot />
      </div>
    </main>
  </div>
</template>

<style scoped>
.shell {
  display: grid;
  grid-template-columns: var(--rv-sidebar-width) minmax(0, 1fr);
  min-height: 100vh;
}

.shell__skip {
  position: absolute;
  left: -100vw;
  z-index: 2;
  padding: var(--rv-space-2) var(--rv-space-4);
  color: var(--rv-color-ink);
  background: var(--rv-color-surface);
}

.shell__skip:focus {
  top: var(--rv-space-2);
  left: var(--rv-space-2);
}

/* The chrome band is a dark zone in both themes, so it redefines the ink and
   surface roles for everything inside it. A primitive placed here keeps its
   contrast without knowing where it sits. */
.shell__side {
  --rv-color-accent: var(--rv-color-chrome-accent);
  --rv-color-ink: var(--rv-color-chrome-ink);
  --rv-color-ink-muted: var(--rv-color-chrome-ink-muted);
  --rv-color-ink-tertiary: var(--rv-color-chrome-ink-tertiary);
  --rv-color-surface-muted: var(--rv-color-chrome-raised);
  --rv-color-surface-hover: var(--rv-color-chrome-hover);
  --rv-color-rule: var(--rv-color-chrome-rule);
  --rv-color-rule-strong: var(--rv-color-chrome-rule-strong);

  position: sticky;
  top: 0;
  display: flex;
  flex-direction: column;
  gap: var(--rv-space-8);
  height: 100vh;
  padding: var(--rv-space-8) var(--rv-space-5);
  color: var(--rv-color-ink);
  background: var(--rv-color-chrome);
  border-right: var(--rv-border-hair) solid var(--rv-color-rule);
}

.shell__product {
  padding: 0 var(--rv-space-3);
  font-weight: 700;
  font-size: var(--rv-text-module);
  letter-spacing: var(--rv-tracking-title);
}

.shell__nav {
  display: grid;
  gap: var(--rv-space-2);
}

.shell__nav-link {
  display: flex;
  gap: var(--rv-space-3);
  align-items: center;
  min-height: var(--rv-control-touch);
  padding: 0 var(--rv-space-3);
  color: var(--rv-color-ink-muted);
  font-weight: 600;
  font-size: var(--rv-text-emphasis);
  text-decoration: none;
  border-radius: var(--rv-radius-md);
}

.shell__nav-icon {
  color: var(--rv-color-ink-tertiary);
}

.shell__nav-link:hover {
  color: var(--rv-color-ink);
  background: var(--rv-color-chrome-raised);
}

.shell__nav-link--active {
  color: var(--rv-color-ink);
  background: var(--rv-color-chrome-raised);
}

.shell__nav-link--active .shell__nav-icon {
  color: var(--rv-color-accent);
}

.shell__main {
  min-width: 0;
}

/* One measure for every screen, owned here, so moving between sections never
   changes the width the content sits in. --rv-page-inline mirrors the inline
   padding for descendants that bleed to the page edge (a sticky action bar);
   a bleed wider than this padding scrolls the whole page sideways. */
.shell__measure {
  display: grid;
  gap: var(--rv-space-12);
  width: min(var(--rv-measure-workspace), 100%);
  margin: 0 auto;
  padding: var(--rv-space-10) var(--rv-page-inline) var(--rv-space-12);
}

@media (width <= 64rem) {
  .shell {
    grid-template-columns: 1fr;
  }

  .shell__side {
    position: static;
    flex-flow: row wrap;
    align-items: center;
    gap: var(--rv-space-3) var(--rv-space-5);
    height: auto;
    padding: var(--rv-space-4) var(--rv-space-5);
    border-right: 0;
    border-bottom: var(--rv-border-hair) solid var(--rv-color-rule);
  }

  .shell__product {
    padding: 0;
  }

  .shell__nav {
    display: flex;
    flex-wrap: wrap;
    gap: var(--rv-space-1);
  }

  .shell__nav-link {
    min-height: var(--rv-control-default);
    padding: 0 var(--rv-space-3);
    font-size: var(--rv-text-interface);
  }

  .shell__measure {
    --rv-page-inline: var(--rv-space-6);

    padding: var(--rv-space-8) var(--rv-page-inline) var(--rv-space-10);
  }
}

@media (width <= 40rem) {
  .shell__side {
    padding-inline: var(--rv-space-4);
  }

  .shell__product {
    width: 100%;
  }

  .shell__nav {
    width: 100%;
  }

  .shell__nav-link {
    flex: 1 1 auto;
    justify-content: center;
  }

  .shell__measure {
    --rv-page-inline: var(--rv-space-4);

    gap: var(--rv-space-10);
    padding: var(--rv-space-6) var(--rv-page-inline) var(--rv-space-8);
  }
}
</style>
