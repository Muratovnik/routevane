<script setup lang="ts">
import { useLocalStorage } from '@vueuse/core'
import { computed } from 'vue'

import { useLocale } from '@/shared/i18n/useLocale'
import {
  checkService,
  retrySection,
  useServiceHealth,
} from '@/shared/model/useServiceHealth'
import type { IconName } from '@/shared/ui/types'
import RvWorkspace from '@/shared/ui/RvWorkspace.vue'
import RvTooltip from '@/shared/ui/RvTooltip.vue'
import RvIcon from '@/shared/ui/RvIcon.vue'
import RvButton from '@/shared/ui/RvButton.vue'
import RvStateNotice from '@/shared/ui/RvStateNotice.vue'
import ApplicationUpdate from '@/features/application-update/ui/ApplicationUpdate.vue'

/**
 * The chrome: the product's name and its sections. Sections mirror the object
 * types the product holds — lists, connections, settings — and nothing here is
 * a step: every section is reachable at any time. Preferences and the
 * service's own address live on the settings screen, not in the chrome.
 */
const { locale, t } = useLocale()
const route = useRoute()
const collapsed = useLocalStorage('rv.sidebarCollapsed', false, {
  writeDefaults: false,
  onError: () => {},
})
const workspace = computed(
  () => route.path.startsWith('/profiles/') || route.path === '/lists',
)

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
      label: t('shell.nav.profiles'),
      to: '/',
      active: path === '/' || path.startsWith('/profiles'),
    },
    {
      id: 'lists',
      // The list glyph belongs to the routes shelf above; a stack of sheets is
      // the nearest thing this set has to "the material a route is made of".
      icon: 'copy',
      label: t('shell.nav.lists'),
      to: '/lists',
      active: path.startsWith('/lists'),
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

/**
 * One message about the surface itself, above whatever the screen is saying
 * about its own work. It lives here because every screen is drawn inside this
 * shell, and there is exactly one of it: a service that is not answering is
 * also why a section did not open and why the development server went quiet, so
 * stacking all three would state one fact three times. They are ordered by how
 * much they explain.
 */
const { devDisconnected, probing, sectionFailure, unreachable } =
  useServiceHealth()

const SECTION_NOTICE = 'shell.notice.section'

const notice = computed(() => {
  if (unreachable.value) return 'shell.notice.unreachable'
  if (sectionFailure.value !== '') return SECTION_NOTICE
  // Only a development build ever reports this one, so nothing here has to ask
  // which build it is running in.
  if (devDisconnected.value) return 'shell.notice.dev'
  return ''
})

/**
 * Retrying a section asks for that section again, and what that takes belongs
 * to whoever registered the recovery — this is a press, not a decision about
 * documents. Retrying anything else asks the service whether it is back; a
 * development server that is still gone answers that question too, and the
 * message becomes the larger one.
 */
const retry = async (): Promise<void> => {
  if (notice.value === SECTION_NOTICE) {
    await retrySection()
    return
  }
  await checkService()
}
</script>

<template>
  <div class="shell-frame">
    <!-- The two columns and the transition between their widths. It has no
         role and no name of its own, so a test hook is the only way to read
         the geometry it owns. -->
    <div
      class="shell"
      :class="{ 'shell--collapsed': collapsed }"
      data-testid="rv-shell-grid"
    >
      <a class="shell__skip" href="#content">{{ t('shell.skip') }}</a>
      <header class="shell__side">
        <p class="shell__product">
          <!-- Decoration beside the product name, so it is hidden from the
               accessibility tree and carries a test hook instead of a role. -->
          <span
            aria-hidden="true"
            class="shell__product-mark"
            data-testid="rv-shell-product-mark"
          />
          <span class="shell__product-name">{{ t('shell.product') }}</span>
        </p>
        <nav :aria-label="t('shell.nav')" class="shell__nav">
          <RvTooltip
            v-for="section in sections"
            :key="section.id"
            :text="section.label"
            :disabled="!collapsed"
          >
            <NuxtLink
              :aria-current="section.active ? 'page' : undefined"
              class="shell__nav-link"
              :class="{ 'shell__nav-link--active': section.active }"
              :to="section.to"
            >
              <RvIcon class="shell__nav-icon" :name="section.icon" />
              <span class="shell__nav-label">{{ section.label }}</span>
            </NuxtLink>
          </RvTooltip>
        </nav>
        <div class="shell__footer">
          <ApplicationUpdate :collapsed="collapsed" />
          <RvTooltip
            :text="t(collapsed ? 'shell.expand' : 'shell.collapse')"
            :disabled="!collapsed"
          >
            <button
              class="shell__collapse"
              type="button"
              :aria-expanded="!collapsed"
              :aria-label="t(collapsed ? 'shell.expand' : 'shell.collapse')"
              @click="collapsed = !collapsed"
            >
              <RvIcon name="chevron" /><span class="shell__nav-label">{{
                t('shell.collapse')
              }}</span>
            </button>
          </RvTooltip>
        </div>
      </header>

      <main
        id="content"
        class="shell__main"
        :class="{ 'shell__main--workspace': workspace }"
      >
        <div
          class="shell__measure"
          :class="{ 'shell__measure--noticed': notice !== '' }"
        >
          <div v-if="notice !== ''" class="shell__notice">
            <!-- The tone states what is true of the service, not what this
                 tab is doing about it. A check that flipped the tone would
                 hide the whole message for the reveal delay and announce it
                 again on the way back, once per scheduled check; the work
                 belongs to the control that does it. -->
            <RvStateNotice live :title="t(notice)" tone="failed">
              <template #action>
                <RvButton size="compact" :loading="probing" @click="retry">{{
                  t('action.retry')
                }}</RvButton>
              </template>
            </RvStateNotice>
          </div>
          <RvWorkspace><slot /></RvWorkspace>
        </div>
      </main>
    </div>
  </div>
</template>

<style scoped>
.shell-frame {
  container-type: inline-size;
}

.shell {
  display: grid;
  grid-template-columns: var(--rv-sidebar-width) minmax(0, 1fr);
  height: 100dvh;
  overflow: hidden;
  transition: grid-template-columns var(--rv-motion-normal)
    var(--rv-motion-ease-out);
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
  min-height: 0;
  overflow: hidden auto;
  overscroll-behavior: contain;
  transition: padding var(--rv-motion-normal) var(--rv-motion-ease-out);
  display: flex;
  flex-direction: column;
  gap: var(--rv-space-8);
  height: 100dvh;
  padding: var(--rv-space-8) var(--rv-space-5) 0;
  color: var(--rv-color-ink);
  background: var(--rv-color-chrome);
  border-right: var(--rv-border-hair) solid var(--rv-color-rule);
}

.shell__product {
  display: flex;
  gap: var(--rv-space-2);
  align-items: center;
  overflow: hidden;
  flex: none;
  padding: 0 var(--rv-space-3);
  font-weight: 700;
  font-size: var(--rv-text-section);
  letter-spacing: var(--rv-tracking-title);
}

.shell__product-mark {
  display: block;
  flex: 0 0 auto;
  width: var(--rv-brand-mark-size);
  min-width: var(--rv-brand-mark-size);
  max-width: var(--rv-brand-mark-size);
  height: var(--rv-brand-mark-size);
  min-height: var(--rv-brand-mark-size);
  max-height: var(--rv-brand-mark-size);
  background-color: currentColor;
  mask-image: url('/routevane-logo.svg');
  mask-position: center;
  mask-repeat: no-repeat;
  mask-size: contain;
}

.shell__nav {
  display: grid;
  gap: var(--rv-space-2);
}

.shell__nav-link {
  overflow: hidden;
  position: relative;
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
  position: relative;
  overflow: auto;
  scrollbar-gutter: stable;
  overscroll-behavior: contain;
  min-width: 0;
  min-height: 0;
}

/* The shell owns outer alignment; individual fields own their reading width. */
.shell__measure {
  display: grid;
  gap: var(--rv-space-12);
  width: 100%;
  margin: 0;
  padding: var(--rv-space-6) var(--rv-page-inline);
  min-height: 0;
  container-type: inline-size;
}

/* The message aligns with the content column rather than with the window, so
   it reads as part of the page it sits above at every width. */
.shell__notice {
  width: 100%;
  max-width: var(--rv-composition-width);
  margin-inline: auto;
}

@container (width <= 40rem) {
  .shell {
    grid-template-rows: auto minmax(0, 1fr);
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

@container (width <= 40rem) {
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

.shell--collapsed {
  grid-template-columns: var(--rv-sidebar-collapsed-width) minmax(0, 1fr);
}

.shell--collapsed .shell__side {
  padding-inline: var(--rv-space-2);
}

.shell--collapsed .shell__product {
  padding-inline: var(--rv-space-3);
}

.shell__product-name,
.shell__nav-label {
  white-space: nowrap;
  flex: none;
  transition:
    opacity var(--rv-motion-normal) var(--rv-motion-ease-out),
    transform var(--rv-motion-normal) var(--rv-motion-ease-out);
}

.shell--collapsed .shell__product-name,
.shell--collapsed .shell__nav-label {
  opacity: 0;
  transform: translateX(var(--rv-space-2));
  pointer-events: none;
}

.shell__collapse {
  overflow: hidden;
  flex: none;
  white-space: nowrap;
  position: relative;
  display: flex;
  align-items: center;
  gap: var(--rv-space-3);
  margin-top: auto;
  min-height: var(--rv-control-touch);
  padding: var(--rv-space-3);
  color: var(--rv-color-ink-muted);
  background: transparent;
  border: 0;
  border-radius: var(--rv-radius-md);
  cursor: pointer;
  text-align: start;
}

.shell__footer {
  display: flex;
  flex-direction: column;
  gap: var(--rv-space-1);
  margin-top: auto;
  flex: none;
}

.shell__collapse > .rv-icon {
  transition: transform var(--rv-motion-normal) var(--rv-motion-ease-out);
  transform: rotate(90deg);
  flex: none;
}

.shell--collapsed .shell__collapse > .rv-icon {
  transform: rotate(-90deg);
}

.shell--collapsed .shell__nav-link,
.shell--collapsed .shell__collapse {
  padding-inline: var(--rv-space-3);
}

@media (width > 64rem) and (height > 36rem) {
  .shell__main--workspace {
    height: 100dvh;
  }

  /* A floor rather than a fixed height: each section inside states the
     standing height of its own data region, so this column fills the window
     when the page fits and grows when something is added above or below that
     region — carrying its closing inset down with it. */
  .shell__main--workspace .shell__measure {
    min-height: 100%;
    grid-template-rows: minmax(0, 1fr);
  }

  /* A workspace fills the window, so the message it is given has to take its
     own height out of that fill rather than push the frame past it. */
  .shell__main--workspace .shell__measure--noticed {
    grid-template-rows: auto minmax(0, 1fr);
  }
}

@container (width <= 40rem) {
  .shell--collapsed {
    grid-template-columns: 1fr;
  }

  .shell__collapse {
    display: none;
  }

  .shell--collapsed .shell__nav-label {
    position: static;
    width: auto;
    height: auto;
    overflow: visible;
    clip-path: none;
    padding: 0;
    border: 0;
    opacity: 1;
    pointer-events: auto;
  }
}
</style>
