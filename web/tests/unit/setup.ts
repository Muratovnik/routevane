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
import {
  DialogContent,
  DialogDescription,
  DialogOverlay,
  DialogPortal,
  DialogRoot,
  DialogTitle,
  Label,
  SwitchRoot,
  SwitchThumb,
  TooltipContent,
  TooltipPortal,
  TooltipRoot,
  TooltipTrigger,
  VisuallyHidden,
} from 'reka-ui'
import { config } from 'vitest-browser-vue'
import {
  cloneVNode,
  computed,
  defineComponent,
  Fragment,
  getCurrentInstance,
  h,
  provide,
  ref,
  Teleport,
  useId,
  watch,
  type Component,
  type PropType,
  type VNode,
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
    color: String,
    disabled: Boolean,
    external: Boolean,
    href: String,
    // The library draws `label` as a truncating span when no content is given;
    // `ui` is class overrides, which have no rendered form here.
    label: String,
    loading: Boolean,
    size: String,
    to: String,
    type: String as PropType<'button' | 'submit'>,
    ui: Object as PropType<Record<string, string>>,
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
          'data-color': props.color,
          'data-external': isLink ? String(props.external) : undefined,
          'data-size': props.size,
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
        children([
          slots.leading?.(),
          slots.default?.() ??
            (props.label === undefined
              ? undefined
              : h('span', { 'data-slot': 'label' }, props.label)),
          slots.trailing?.(),
        ]),
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

// The centred panel. Unlike the other doubles this one keeps the library's
// mechanics rather than standing in for them: Nuxt UI's modal is a thin layer
// over Reka's dialog, and the focus trap, the focus handed back, the scroll
// lock and Escape are Reka's. So the double renders the same Reka parts the
// library does, wired the same way — the caller's `content` props on the
// panel, the dismissal refused while `dismissible` is false, the `ui` classes
// on each part and the library's `data-slot` names — leaving out only the
// library's theme, which this suite does not load. That the library itself
// keeps this wiring is proven by the browser suites against the built product.
const UModalDouble = defineComponent({
  inheritAttrs: false,
  props: {
    content: {
      type: Object as PropType<Record<string, unknown>>,
      default: () => ({}),
    },
    description: String,
    dismissible: { type: Boolean, default: true },
    open: Boolean,
    portal: { type: [Boolean, String], default: true },
    title: String,
    ui: {
      type: Object as PropType<Record<string, unknown>>,
      default: () => ({}),
    },
  },
  emits: { 'update:open': (_open: boolean) => true },
  setup(props, { emit, slots }) {
    const refuse = (event: Event) => event.preventDefault()
    return () =>
      h(
        DialogRoot,
        {
          open: props.open,
          'onUpdate:open': (open: boolean) => emit('update:open', open),
        },
        () =>
          h(
            DialogPortal,
            { to: typeof props.portal === 'string' ? props.portal : 'body' },
            () => [
              h(DialogOverlay, {
                class: props.ui.overlay,
                'data-slot': 'overlay',
              }),
              h(
                DialogContent,
                {
                  ...props.content,
                  class: props.ui.content,
                  'data-slot': 'content',
                  ...(props.dismissible
                    ? {}
                    : { onEscapeKeyDown: refuse, onInteractOutside: refuse }),
                },
                () =>
                  children([
                    props.description === undefined
                      ? h(VisuallyHidden, () => h(DialogDescription))
                      : undefined,
                    h(
                      'div',
                      { class: props.ui.header, 'data-slot': 'header' },
                      children([
                        h(
                          'div',
                          { class: props.ui.wrapper, 'data-slot': 'wrapper' },
                          children([
                            h(
                              DialogTitle,
                              { class: props.ui.title, 'data-slot': 'title' },
                              () => props.title,
                            ),
                            props.description === undefined
                              ? undefined
                              : h(
                                  DialogDescription,
                                  {
                                    class: props.ui.description,
                                    'data-slot': 'description',
                                  },
                                  () => props.description,
                                ),
                          ]),
                        ),
                        slots.actions?.(),
                      ]),
                    ),
                    slots.body
                      ? h(
                          'div',
                          { class: props.ui.body, 'data-slot': 'body' },
                          slots.body(),
                        )
                      : undefined,
                    slots.footer
                      ? h(
                          'div',
                          { class: props.ui.footer, 'data-slot': 'footer' },
                          slots.footer(),
                        )
                      : undefined,
                  ]),
              ),
            ],
          ),
      )
  },
})

// The switch. Like the modal's, this double keeps the library's mechanics:
// Nuxt UI's switch is Reka's switch root and thumb with a Reka label beside
// them, so the double renders those parts wired the same way — the caller's
// attributes and model on the root, the id the label points at, the `ui`
// classes and `data-slot` names on each part, and the description in its own
// paragraph or its slot — leaving out only the library's theme. That the
// library keeps this wiring is proven by the browser suites.
const USwitchDouble = defineComponent({
  inheritAttrs: false,
  props: {
    class: [String, Array, Object] as PropType<unknown>,
    description: String,
    disabled: Boolean,
    id: String,
    label: String,
    modelValue: Boolean,
    ui: {
      type: Object as PropType<Record<string, unknown>>,
      default: () => ({}),
    },
  },
  emits: { 'update:modelValue': (_value: boolean) => true },
  setup(props, { attrs, emit, slots }) {
    const fallbackId = useId()
    return () => {
      const id = props.id ?? fallbackId
      return h(
        'div',
        { class: [props.ui.root, props.class], 'data-slot': 'root' },
        children([
          h('div', { class: props.ui.container, 'data-slot': 'container' }, [
            h(
              SwitchRoot,
              {
                ...attrs,
                id,
                class: props.ui.base,
                'data-slot': 'base',
                disabled: props.disabled,
                modelValue: props.modelValue,
                'onUpdate:modelValue': (value: unknown) =>
                  emit('update:modelValue', value === true),
              },
              () =>
                h(SwitchThumb, { class: props.ui.thumb, 'data-slot': 'thumb' }),
            ),
          ]),
          props.label === undefined && props.description === undefined
            ? undefined
            : h(
                'div',
                { class: props.ui.wrapper, 'data-slot': 'wrapper' },
                children([
                  props.label === undefined
                    ? undefined
                    : h(
                        Label,
                        {
                          for: id,
                          class: props.ui.label,
                          'data-slot': 'label',
                        },
                        () => props.label,
                      ),
                  props.description === undefined && !slots.description
                    ? undefined
                    : h(
                        'p',
                        {
                          class: props.ui.description,
                          'data-slot': 'description',
                        },
                        slots.description?.() ?? props.description,
                      ),
                ]),
              ),
        ]),
      )
    }
  },
})

// The tooltip. Nuxt UI's tooltip is Reka's tooltip with its own defaults, so
// the double renders the same Reka parts: the trigger as the caller's own
// element, the panel portalled with the caller's `content` props over the
// library's defaults, the `ui` classes on the panel and its text, and no panel
// at all when there is nothing to say. Opening, closing and the description it
// gives its trigger are Reka's, as they are in production; the browser suites
// prove the library keeps that wiring.
const UTooltipDouble = defineComponent({
  inheritAttrs: false,
  props: {
    content: {
      type: Object as PropType<Record<string, unknown>>,
      default: () => ({}),
    },
    delayDuration: Number,
    disabled: Boolean,
    text: String,
    ui: {
      type: Object as PropType<Record<string, unknown>>,
      default: () => ({}),
    },
  },
  setup(props, { attrs, slots }) {
    return () =>
      h(
        TooltipRoot,
        {
          delayDuration: props.delayDuration,
          disabled: props.text === undefined || props.disabled,
        },
        () => [
          h(TooltipTrigger, { ...attrs, asChild: true }, () =>
            slots.default?.(),
          ),
          h(TooltipPortal, () =>
            h(
              TooltipContent,
              {
                collisionPadding: 8,
                side: 'bottom',
                sideOffset: 8,
                ...props.content,
                class: props.ui.content,
                'data-slot': 'content',
              },
              () =>
                h(
                  'span',
                  { class: props.ui.text, 'data-slot': 'text' },
                  props.text,
                ),
            ),
          ),
        ],
      )
  },
})

// The field family. Each double renders the element the library renders and
// states, on it, the variant it was asked for. The form field double provides
// the same two injections as the library's own, from the library's own module,
// so a facade that registers its control through `useFormField` is exercised
// as it is in production: the label names whichever id was registered, the
// description stands between the label and the control, and an error is added
// under the control. That the library itself
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
          placeholder: props.placeholder,
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
    description: String,
    error: { type: [String, Boolean], default: undefined },
    label: String,
    // Class overrides have no rendered form here.
    ui: Object,
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
        description: props.description,
        error: props.error,
      })) as never,
    )
    return () => {
      // The library draws the error's element for a non-empty error and also
      // whenever the field is given an error slot, empty or not.
      const error =
        (typeof props.error === 'string' && props.error !== '') || slots.error
          ? h(
              'div',
              { id: `${ariaId}-error` },
              slots.error?.({ error: props.error }) ?? props.error,
            )
          : undefined
      return h(
        'div',
        children([
          props.label
            ? h('div', { 'data-slot': 'labelWrapper' }, [
                h(
                  'label',
                  { for: id.value },
                  slots.label?.({ label: props.label }) ?? props.label,
                ),
                // Beside the label and outside it, as the library draws it.
                slots.hint ? h('span', slots.hint({})) : undefined,
              ])
            : undefined,
          props.description
            ? h('p', { id: `${ariaId}-description` }, props.description)
            : undefined,
          h('div', children([slots.default?.(), error])),
        ]),
      )
    }
  },
})

// The choice family. A select, a select menu and a command palette in a
// popover each render the roles the library renders — a combobox trigger for
// the select, a button for the menu, a named listbox of options, the menu's
// search as a combobox inside its panel, the palette's search as a textbox —
// open on a press, close on Escape and on a single choice, and report a
// choice the way the library does. Their panels open in place rather than in a
// portal, so they stay inside a modal layer the way the library's nested layers
// do; portalling is the library's own and proven by the browser suites. Keyboard traversal, focus return and the
// drawn look are the library's own and are proven by the browser suites.
type ChoiceItem = {
  disabled?: boolean
  label?: string
  type?: string
  value?: string
  [field: string]: unknown
}

const itemGroups = (items: unknown): ChoiceItem[][] => {
  const list = (items ?? []) as ChoiceItem[] | ChoiceItem[][]
  return Array.isArray(list[0])
    ? (list as ChoiceItem[][])
    : [list as ChoiceItem[]]
}

const chosenAfter = (
  multiple: boolean,
  current: unknown,
  value: string,
): string | string[] => {
  if (!multiple) return value
  const values = Array.isArray(current) ? (current as string[]) : []
  return values.includes(value)
    ? values.filter((entry) => entry !== value)
    : [...values, value]
}

// A panel's search takes the focus as it opens, the way the library's does.
const focusOnMount = (vnode: VNode): void => (vnode.el as HTMLElement).focus()

const isChosen = (current: unknown, value: string | undefined): boolean =>
  Array.isArray(current) ? current.includes(value) : current === value

const choiceProps = {
  color: String,
  content: {
    type: Object as PropType<Record<string, unknown>>,
    default: () => ({}),
  },
  descriptionKey: { type: String, default: 'description' },
  disabled: Boolean,
  highlight: Boolean,
  id: String,
  items: { type: Array, default: () => [] },
  loading: Boolean,
  loadingIcon: [Object, Function] as PropType<Component>,
  modelValue: { type: [String, Array], default: undefined },
  multiple: Boolean,
  open: Boolean,
  placeholder: String,
  selectedIcon: [Object, Function] as PropType<Component>,
  size: String,
  trailingIcon: [Object, Function] as PropType<Component>,
  variant: String,
}

// The options of one open panel, with the item slots the facades fill.
const renderOptions = (
  groups: ChoiceItem[][],
  current: unknown,
  descriptionKey: string,
  slots: Record<
    string,
    ((props: Record<string, unknown>) => VNodeChild) | undefined
  >,
  choose: (item: ChoiceItem) => void,
) =>
  groups.map((group) =>
    h(
      'div',
      { role: 'group' },
      group.map((entry) =>
        entry.type === 'label'
          ? h('div', { role: 'presentation' }, entry.label)
          : h(
              'div',
              {
                'aria-disabled': entry.disabled ? 'true' : undefined,
                'aria-selected': String(isChosen(current, entry.value)),
                'data-disabled': entry.disabled ? '' : undefined,
                role: 'option',
                onClick: () => {
                  if (!entry.disabled) choose(entry)
                },
              },
              children([
                slots['item-leading']?.({ item: entry }),
                h(
                  'span',
                  children([
                    slots['item-label']?.({ item: entry }) ?? entry.label,
                  ]),
                ),
                typeof entry[descriptionKey] === 'string'
                  ? h('span', entry[descriptionKey] as string)
                  : undefined,
                slots['item-trailing']?.({ item: entry }),
              ]),
            ),
      ),
    ),
  )

const displayed = (groups: ChoiceItem[][], current: unknown) =>
  groups
    .flat()
    .filter((entry) => entry.type !== 'label' && isChosen(current, entry.value))
    .map((entry) => entry.label)
    .join(', ')

const openState = (
  props: { open: boolean },
  emit: (event: 'update:open', value: boolean) => void,
) => {
  const open = ref(props.open)
  watch(
    () => props.open,
    (value) => {
      open.value = value
    },
  )
  const setOpen = (value: boolean) => {
    open.value = value
    emit('update:open', value)
  }
  return { open, setOpen }
}

const USelectDouble = defineComponent({
  inheritAttrs: false,
  props: choiceProps,
  emits: { 'update:modelValue': null, 'update:open': null },
  setup(props, { attrs, emit, slots }) {
    const { open, setOpen } = openState(props, emit)
    return () => {
      const groups = itemGroups(props.items)
      const shown = displayed(groups, props.modelValue)
      return [
        h(
          'button',
          {
            ...attrs,
            ...variantAttrs(props),
            'aria-expanded': String(open.value),
            'data-placeholder': shown === '' ? '' : undefined,
            disabled: props.disabled,
            id: props.id,
            role: 'combobox',
            type: 'button',
            onClick: () => setOpen(!open.value),
            onKeydown: (event: KeyboardEvent) => {
              if (['Enter', ' ', 'ArrowDown'].includes(event.key)) {
                event.preventDefault()
                setOpen(true)
              }
            },
          },
          children([slots.leading?.(), shown || props.placeholder]),
        ),
        open.value
          ? h(Fragment, [
              h(
                'div',
                {
                  ...props.content,
                  role: 'listbox',
                  onKeydown: (event: KeyboardEvent) => {
                    if (event.key === 'Escape') setOpen(false)
                  },
                },
                renderOptions(
                  groups,
                  props.modelValue,
                  props.descriptionKey,
                  slots,
                  (entry) => {
                    emit('update:modelValue', entry.value)
                    setOpen(false)
                  },
                ),
              ),
            ])
          : undefined,
      ]
    }
  },
})

const USelectMenuDouble = defineComponent({
  inheritAttrs: false,
  props: {
    ...choiceProps,
    ignoreFilter: Boolean,
    searchInput: { type: [Object, Boolean], default: true },
    searchTerm: { type: String, default: '' },
    valueKey: String,
  },
  emits: {
    'update:modelValue': null,
    'update:open': null,
    'update:searchTerm': null,
  },
  setup(props, { attrs, emit, slots }) {
    const { open, setOpen: setShown } = openState(props, emit)
    // The library clears its search as the panel closes.
    const setOpen = (value: boolean) => {
      setShown(value)
      if (!value) emit('update:searchTerm', '')
    }
    return () => {
      const groups = itemGroups(props.items)
      const shown = displayed(groups, props.modelValue)
      const search = (
        typeof props.searchInput === 'object' ? props.searchInput : {}
      ) as Record<string, unknown>
      const options = groups.flat().filter((entry) => entry.type !== 'label')
      return [
        h(
          'button',
          {
            ...attrs,
            ...variantAttrs(props),
            'aria-expanded': String(open.value),
            'aria-haspopup': 'listbox',
            disabled: props.disabled,
            id: props.id,
            type: 'button',
            onClick: () => setOpen(!open.value),
          },
          children([slots.leading?.(), shown || props.placeholder]),
        ),
        open.value
          ? h(Fragment, [
              h(
                'div',
                {
                  ...props.content,
                  role: 'listbox',
                  onKeydown: (event: KeyboardEvent) => {
                    if (event.key === 'Escape') setOpen(false)
                  },
                },
                children([
                  h('input', {
                    'aria-label': search['aria-label'],
                    placeholder: search.placeholder,
                    role: 'combobox',
                    value: props.searchTerm,
                    onInput: (event: Event) =>
                      emit(
                        'update:searchTerm',
                        (event.target as HTMLInputElement).value,
                      ),
                    onVnodeMounted: focusOnMount,
                  }),
                  options.length === 0
                    ? h('div', children([slots.empty?.()]))
                    : undefined,
                  ...renderOptions(
                    groups,
                    props.modelValue,
                    props.descriptionKey,
                    slots,
                    (entry) => {
                      emit(
                        'update:modelValue',
                        chosenAfter(
                          props.multiple,
                          props.modelValue,
                          entry.value!,
                        ),
                      )
                      if (!props.multiple) setOpen(false)
                    },
                  ),
                ]),
              ),
            ])
          : undefined,
      ]
    }
  },
})

const UPopoverDouble = defineComponent({
  props: {
    content: {
      type: Object as PropType<Record<string, unknown>>,
      default: () => ({}),
    },
    open: Boolean,
    // Class overrides have no rendered form here.
    ui: Object,
  },
  emits: { 'update:open': null },
  setup(props, { emit, slots }) {
    const { open, setOpen } = openState(props, emit)
    const trigger = ref<HTMLElement | null>(null)
    const close = () => {
      setOpen(false)
      trigger.value?.focus()
    }
    return () => {
      const [anchor] = (slots.default?.() ?? []) as VNode[]
      return [
        anchor
          ? cloneVNode(anchor, {
              'aria-expanded': String(open.value),
              'aria-haspopup': 'dialog',
              onClick: () => setOpen(!open.value),
              onVnodeMounted: (vnode: VNode) => {
                trigger.value = vnode.el as HTMLElement
              },
            })
          : undefined,
        open.value
          ? h(Fragment, [
              h(
                'div',
                {
                  ...props.content,
                  role: 'dialog',
                  onKeydown: (event: KeyboardEvent) => {
                    if (event.key === 'Escape') close()
                  },
                },
                children([slots.content?.({ close })]),
              ),
            ])
          : undefined,
      ]
    }
  },
})

const UCommandPaletteDouble = defineComponent({
  inheritAttrs: false,
  props: {
    by: String,
    groups: { type: Array, default: () => [] },
    input: { type: [Object, Boolean], default: true },
    modelValue: { type: [String, Array, Object], default: undefined },
    multiple: Boolean,
    placeholder: String,
    searchTerm: { type: String, default: '' },
  },
  emits: { 'update:modelValue': null, 'update:searchTerm': null },
  setup(props, { attrs, emit, slots }) {
    // Without a value key the palette holds and reports whole entries, compared
    // by the field it is told to compare them by.
    const key = (entry: unknown) =>
      props.by && entry !== undefined && entry !== null
        ? (entry as ChoiceItem)[props.by]
        : entry
    const held = (): unknown[] => {
      if (Array.isArray(props.modelValue)) return props.modelValue
      return props.modelValue === undefined ? [] : [props.modelValue]
    }
    const chosenValues = () =>
      props.multiple ? held().map(key) : key(props.modelValue)
    const chosenEntries = (entry: ChoiceItem) => {
      if (!props.multiple) return entry
      const kept = held().filter((other) => key(other) !== key(entry))
      return kept.length < held().length ? kept : [...kept, entry]
    }
    return () => {
      const groups = props.groups as { items: ChoiceItem[]; label?: string }[]
      const input = (
        typeof props.input === 'object' ? props.input : {}
      ) as Record<string, unknown>
      return h(
        'div',
        { class: attrs.class },
        children([
          h('input', {
            'aria-label': input['aria-label'],
            placeholder: props.placeholder,
            value: props.searchTerm,
            onInput: (event: Event) =>
              emit(
                'update:searchTerm',
                (event.target as HTMLInputElement).value,
              ),
            onVnodeMounted: focusOnMount,
          }),
          groups.length === 0
            ? h('div', children([slots.empty?.()]))
            : h(
                'div',
                {
                  'aria-multiselectable': String(props.multiple),
                  role: 'listbox',
                },
                renderOptions(
                  groups.map((group) => [
                    ...(group.label
                      ? [{ type: 'label', label: group.label }]
                      : []),
                    ...group.items,
                  ]),
                  chosenValues(),
                  'description',
                  slots,
                  (entry) => emit('update:modelValue', chosenEntries(entry)),
                ),
              ),
        ]),
      )
    }
  },
})

config.global.components = {
  ...config.global.components,
  UButton: UButtonDouble,
  UCommandPalette: UCommandPaletteDouble,
  UFormField: UFormFieldDouble,
  UInput: UInputDouble,
  UModal: UModalDouble,
  UPopover: UPopoverDouble,
  USelect: USelectDouble,
  USelectMenu: USelectMenuDouble,
  USlideover: USlideoverDouble,
  USwitch: USwitchDouble,
  UTextarea: UTextareaDouble,
  UTooltip: UTooltipDouble,
}
