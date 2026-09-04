import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { defineComponent, h } from 'vue'

import RvButton from '@/shared/ui/RvButton.vue'

// Plain Vitest does not run Nuxt's component auto-import transform. This small
// contract double models the UButton boundary that RvButton relies on: element
// selection and refusal of a disabled link click remain library-owned.
const UButtonStub = defineComponent({
  inheritAttrs: false,
  props: {
    disabled: Boolean,
    external: Boolean,
    href: String,
    loading: Boolean,
    to: String,
    type: String,
    variant: String,
  },
  setup(props, { attrs, slots }) {
    return () => {
      const link = props.href !== undefined || props.to !== undefined
      const click = attrs.onClick
      return h(
        link ? 'a' : 'button',
        {
          ...attrs,
          'aria-disabled': link && props.disabled ? 'true' : undefined,
          disabled: link ? undefined : props.disabled,
          href: link && !props.disabled ? (props.href ?? props.to) : undefined,
          onClick: (event: MouseEvent) => {
            if (props.disabled) {
              event.preventDefault()
              event.stopPropagation()
              return
            }
            if (typeof click === 'function') click(event)
          },
          type: link ? undefined : props.type,
        },
        [slots.leading?.(), slots.default?.()],
      )
    }
  },
})

const library = { components: { UButton: UButtonStub } }

describe('RvButton', () => {
  it('forwards pending state while keeping one accessible progress mark', () => {
    const wrapper = mount(RvButton, {
      props: { loading: true, loadingLabel: 'Saving route' },
      slots: { default: 'Save' },
      global: library,
    })

    const button = wrapper.get<HTMLButtonElement>('button')
    expect(wrapper.getComponent(UButtonStub).props('disabled')).toBe(true)
    expect(wrapper.getComponent(UButtonStub).props('loading')).toBe(true)
    expect(button.attributes('aria-busy')).toBe('true')
    expect(button.attributes('aria-label')).toBe('Saving route')
    expect(button.text()).toContain('Save')
    expect(button.find('.rv-button__spinner').exists()).toBe(true)
  })

  it('forwards an href as an external native-link request', () => {
    const wrapper = mount(RvButton, {
      props: { disabled: true, href: '/artifact' },
      slots: { default: 'Download' },
      global: library,
    })

    expect(wrapper.getComponent(UButtonStub).props()).toMatchObject({
      disabled: true,
      external: true,
      href: '/artifact',
      to: undefined,
    })
  })

  it('forwards internal destinations to the router with the visual variant', () => {
    const wrapper = mount(RvButton, {
      props: { disabled: true, to: '/lists/new', variant: 'primary' },
      slots: { default: 'Create' },
      global: library,
    })

    expect(wrapper.getComponent(UButtonStub).props()).toMatchObject({
      disabled: true,
      external: false,
      href: undefined,
      to: '/lists/new',
      variant: 'link',
    })
  })
})
