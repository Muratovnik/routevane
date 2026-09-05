import { config } from '@vue/test-utils'
import {
  defineComponent,
  getCurrentInstance,
  h,
  Teleport,
  type PropType,
  type VNodeChild,
} from 'vue'

function children(parts: Array<VNodeChild | VNodeChild[] | undefined>) {
  return parts.flat().filter((part) => part !== undefined)
}

// Production gets these components from Nuxt's auto-import transform. The
// unit suite intentionally runs as plain Vite, so it needs one behavioral
// boundary double rather than dozens of feature-local blank stubs. This keeps
// the contracts tests rely on: honest element types, disabled-link refusal,
// named slots and update events.
const UButtonDouble = defineComponent({
  inheritAttrs: false,
  props: {
    block: Boolean,
    disabled: Boolean,
    external: Boolean,
    href: String,
    loading: Boolean,
    to: String,
    type: String as PropType<'button' | 'submit'>,
    variant: String,
  },
  setup(props, { attrs, slots }) {
    return () => {
      const isLink = props.href !== undefined || props.to !== undefined
      const unavailable = props.disabled || props.loading
      const click = attrs.onClick
      return h(
        isLink ? 'a' : 'button',
        {
          ...attrs,
          'aria-disabled': isLink && unavailable ? 'true' : undefined,
          disabled: isLink ? undefined : unavailable,
          href: isLink && !unavailable ? (props.href ?? props.to) : undefined,
          onClick: (event: MouseEvent) => {
            if (unavailable) {
              event.preventDefault()
              event.stopPropagation()
              return
            }
            if (typeof click === 'function') click(event)
            if (Array.isArray(click))
              for (const listener of click) listener(event)
          },
          type: isLink ? undefined : (props.type ?? 'button'),
        },
        children([slots.leading?.(), slots.default?.(), slots.trailing?.()]),
      )
    }
  },
})

const USlideoverDouble = defineComponent({
  inheritAttrs: false,
  props: {
    description: String,
    dismissible: { type: Boolean, default: true },
    open: Boolean,
    side: String,
    title: String,
  },
  emits: { 'update:open': (_open: boolean) => true },
  setup(props, { emit, slots }) {
    const id = `test-slideover-${getCurrentInstance()?.uid ?? 0}`
    return () => {
      if (!props.open) return null
      const close = () => {
        if (props.dismissible) emit('update:open', false)
      }
      return h(Teleport, { to: 'body' }, [
        h('div', {
          'data-testid': 'slideover-overlay',
          onClick: close,
        }),
        h(
          'section',
          {
            'aria-describedby': props.description
              ? `${id}-description`
              : undefined,
            'aria-labelledby': `${id}-title`,
            'data-side': props.side,
            role: 'dialog',
          },
          children([
            h('h2', { id: `${id}-title` }, props.title),
            props.description
              ? h('p', { id: `${id}-description` }, props.description)
              : undefined,
            slots.actions?.(),
            slots.close
              ? h('span', { onClick: close }, slots.close())
              : undefined,
            slots.body?.(),
            slots.footer?.(),
          ]),
        ),
      ])
    }
  },
})

config.global.components = {
  ...config.global.components,
  UButton: UButtonDouble,
  USlideover: USlideoverDouble,
}
