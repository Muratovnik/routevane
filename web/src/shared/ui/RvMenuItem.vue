<script setup lang="ts">
import { DropdownMenuItem } from 'reka-ui'
import { computed, resolveComponent } from 'vue'

import type { MenuItem } from '@/shared/ui/types'
import RvIcon from '@/shared/ui/RvIcon.vue'

const props = defineProps<{ item: MenuItem }>()
const emit = defineEmits<{ select: [] }>()
const routerLink = resolveComponent('NuxtLink')
const element = computed(() => {
  if (props.item.disabled === true) return 'div'
  if (props.item.to !== undefined) return routerLink
  return props.item.href !== undefined ? 'a' : 'div'
})
const linkAttributes = computed(() => {
  if (props.item.disabled === true) return {}
  if (props.item.to !== undefined) return { to: props.item.to }
  if (props.item.href !== undefined)
    return {
      href: props.item.href,
      ...(props.item.download === true ? { download: '' } : {}),
    }
  return {}
})
</script>

<template>
  <DropdownMenuItem
    as-child
    :disabled="item.disabled === true"
    @select="emit('select')"
  >
    <component
      :is="element"
      class="rv-menu__item"
      :class="{ 'rv-menu__item--separated': item.separatorBefore }"
      :data-menu-key="item.key"
      v-bind="linkAttributes"
    >
      <RvIcon v-if="item.icon !== undefined" :name="item.icon" />
      <span class="rv-menu__item-label">{{ item.label }}</span>
    </component>
  </DropdownMenuItem>
</template>
