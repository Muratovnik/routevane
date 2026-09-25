// A real browser needs real geometry. A control whose size is a design token
// measures zero without these, and a zero-sized control can be neither seen
// nor clicked. The Nuxt UI bridge stylesheet is deliberately absent: it pulls
// in the library build, and this suite replaces those components with the
// behavioral doubles below.
import '@/assets/styles/tokens.css'
import '@/assets/styles/global.css'

import {
  formFieldInjectionKey,
  inputIdInjectionKey,
} from '@nuxt/ui/composables/useFormField'
import { config } from 'vitest-browser-vue'
import {
  computed,
  defineComponent,
  getCurrentInstance,
  h,
  provide,
  ref,
  Teleport,
  useId,
  type Component,
  type PropType,
  type VNodeChild,
} from 'vue'

// Vitest can reuse a browser context for successive files. Storage belongs to
// each fixture, so a locale chosen by one file must not turn the next file's
// first visit into a returning visit. Cases within a file can still test reloads.
window.localStorage.clear()
window.sessionStorage.clear()

const children = (parts: Array<VNodeChild | VNodeChild[] | undefined>) =>
  parts.flat().filter((part) => part !== undefined)

// Production gets these components from Nuxt's auto-import transform. The
// unit suite intentionally runs as plain Vite, so it needs one behavioral
// boundary double rather than dozens of feature-local blank stubs. This keeps
// the contracts tests rely on: honest element types, disabled-link refusal,
// named slots and update events.
//
// Two decisions the library makes for itself in production — whether a
// destination is an external navigation or a router link, and which of its own
// variants it was asked for — have no rendered form of their own. The double
// states them on the element it renders, so a spec reads them from the DOM
// instead of reaching into the component instance.
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
          'data-external': isLink ? String(props.external) : undefined,
          'data-variant': props.variant,
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
    // The library applies the content classes it is handed to the panel it
    // renders, so the double does too, and states the entrance decision that
    // has no rendered form of its own.
    transition: { type: Boolean, default: true },
    ui: {
      type: Object as PropType<Record<string, unknown>>,
      default: () => ({}),
    },
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
            class: props.ui.content,
            'data-dismissible': String(props.dismissible),
            'data-side': props.side,
            'data-transition': String(props.transition),
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

// The field family. Each double renders the element the library renders and
// states, on it, the variant it was asked for. The form field double provides
// the same two injections as the library's own, from the library's own module,
// so a facade that registers its control through `useFormField` is exercised
// as it is in production: the label names whichever id was registered, and a
// help text gives way to an error in the same place. That the library itself
// keeps this contract is proven by the browser suites against the built
// product, not here.
const fieldControlProps = {
  color: String,
  disabled: Boolean,
  highlight: Boolean,
  id: String,
  leadingIcon: [Object, Function] as PropType<Component>,
  modelValue: String,
  placeholder: String,
  size: String,
  variant: String,
}

const variantAttrs = (props: {
  color?: string
  highlight: boolean
  size?: string
}) => ({
  'data-color': props.color,
  'data-highlight': String(props.highlight),
  'data-size': props.size,
})

const UInputDouble = defineComponent({
  inheritAttrs: false,
  props: { ...fieldControlProps, autocomplete: String, type: String },
  emits: { blur: (_event: FocusEvent) => true, 'update:modelValue': null },
  setup(props, { attrs, emit, expose }) {
    const inputRef = ref<HTMLInputElement | null>(null)
    expose({ inputRef })
    return () =>
      h('div', { class: attrs.class, ...variantAttrs(props) }, [
        h('input', {
          ...attrs,
          class: undefined,
          ref: inputRef,
          id: props.id,
          autocomplete: props.autocomplete,
          disabled: props.disabled,
          placeholder: props.placeholder,
          type: props.type ?? 'text',
          value: props.modelValue,
          onBlur: (event: FocusEvent) => emit('blur', event),
          onInput: (event: Event) =>
            emit('update:modelValue', (event.target as HTMLInputElement).value),
        }),
        props.leadingIcon
          ? h('span', { 'data-slot': 'leading' }, [h(props.leadingIcon)])
          : undefined,
      ])
  },
})

const UTextareaDouble = defineComponent({
  inheritAttrs: false,
  props: { ...fieldControlProps, rows: Number },
  emits: { 'update:modelValue': null },
  setup(props, { attrs, emit }) {
    return () =>
      h('div', { class: attrs.class, ...variantAttrs(props) }, [
        h('textarea', {
          ...attrs,
          class: undefined,
          id: props.id,
          disabled: props.disabled,
          rows: props.rows,
          value: props.modelValue,
          onInput: (event: Event) =>
            emit(
              'update:modelValue',
              (event.target as HTMLTextAreaElement).value,
            ),
        }),
      ])
  },
})

const UFormFieldDouble = defineComponent({
  props: {
    error: { type: [String, Boolean], default: undefined },
    help: String,
    label: String,
  },
  setup(props, { slots }) {
    const id = ref<string | undefined>(useId())
    const ariaId = id.value
    provide(inputIdInjectionKey, id)
    // Only the fields `useFormField` reads; the library's type also names the
    // validation options a Routevane field never sets.
    provide(
      formFieldInjectionKey,
      computed(() => ({
        ariaId,
        error: props.error,
        help: props.help,
      })) as never,
    )
    return () => {
      const error =
        typeof props.error === 'string' && props.error !== ''
          ? h('div', { id: `${ariaId}-error` }, props.error)
          : undefined
      const help =
        error === undefined && props.help
          ? h('div', { id: `${ariaId}-help` }, props.help)
          : undefined
      return h(
        'div',
        children([
          props.label ? h('label', { for: id.value }, props.label) : undefined,
          h('div', children([slots.default?.(), error, help])),
        ]),
      )
    }
  },
})

config.global.components = {
  ...config.global.components,
  UButton: UButtonDouble,
  UFormField: UFormFieldDouble,
  UInput: UInputDouble,
  USlideover: USlideoverDouble,
  UTextarea: UTextareaDouble,
}
