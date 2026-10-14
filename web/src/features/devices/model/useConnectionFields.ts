import { computed, type Ref } from 'vue'

import type { ConnectionRequirements } from '@/shared/api/deploy'
import { useLocale } from '@/shared/i18n/useLocale'
import { validAddress } from '@/shared/lib/deviceAddress'

/** The fields of one connection, in the order the form draws them. */
export const FIELD_ORDER = [
  'name',
  'address',
  'account',
  'interfaceName',
] as const
export type FieldKey = (typeof FIELD_ORDER)[number]

/** What is wrong with one field. The words are the form's, not the model's. */
export type FieldProblem = 'required' | 'shape'
export type FieldProblems = Partial<Record<FieldKey, FieldProblem>>

export type ConnectionDraft = Record<FieldKey, string>

/**
 * The field contract of one connection: what each field is called for the
 * chosen target, which of them that target's deployer actually needs, and what
 * is wrong with a draft of them.
 *
 * The address field means a different thing per deployer — a router URL for
 * Keenetic, a configuration path for sing-box — so the dictionary names it per
 * target and follows the chosen language. A plugin target the dictionary does
 * not know keeps its own caption rather than getting a wrong generic one.
 *
 * Creation, editing and the page's own validity all read this one derivation,
 * so they cannot disagree about which fields a target has or when a draft is
 * complete.
 */
export const useConnectionFields = (
  requirements: Ref<ConnectionRequirements | null>,
  targetID: Ref<string>,
) => {
  const { t, tor } = useLocale()

  const needsAccount = computed(
    () => requirements.value?.needsCredential === true,
  )
  const needsInterface = computed(
    () => requirements.value?.needsInterface === true,
  )

  /**
   * Every field of the draft that is left empty or is not shaped like what the
   * field takes. The draft is expected trimmed; an untouched optional field is
   * not a problem. Which networks an address may name is the service's policy,
   * stated at the field when the service refuses it, never checked here.
   */
  const problems = (draft: ConnectionDraft): FieldProblems => {
    const found: FieldProblems = {}
    if (draft.name === '') found.name = 'required'
    if (draft.address === '') found.address = 'required'
    else if (!validAddress(draft.address)) found.address = 'shape'
    if (needsAccount.value && draft.account === '') found.account = 'required'
    if (needsInterface.value && draft.interfaceName === '')
      found.interfaceName = 'required'
    return found
  }

  const addressExample = computed(
    () => requirements.value?.addressExample ?? '',
  )

  return {
    addressLabel: computed(() =>
      tor(
        `deploy.field.address.${targetID.value}`,
        requirements.value?.addressLabel ?? t('devices.field.address'),
      ),
    ),
    addressPlaceholder: addressExample,
    // What the service meant by refusing the address, in the words of the
    // target's own kind of address; a target the dictionary does not know gets
    // the plain statement that the address was not accepted.
    addressRefusal: computed(() =>
      tor(
        `deploy.field.address.${targetID.value}.rejected`,
        t('devices.address.refused'),
      ),
    ),
    // What the address field takes, explained behind the informer beside its
    // label rather than under it. The rule belongs to the target's deployer,
    // so the dictionary carries one text per target, and a target it does not
    // know gets no informer. An example goes in the placeholder, not here.
    addressInfo: computed(() =>
      tor(`deploy.field.address.${targetID.value}.hint`, ''),
    ),
    accountLabel: computed(() =>
      tor(`deploy.field.account.${targetID.value}`, t('devices.field.account')),
    ),
    interfaceLabel: computed(() =>
      tor(
        `deploy.field.interface.${targetID.value}`,
        requirements.value?.interfaceLabel ?? t('devices.field.interface'),
      ),
    ),
    interfaceInfo: computed(() =>
      tor(`deploy.field.interface.${targetID.value}.hint`, ''),
    ),
    // The shape of an interface name, shown in the field until one is typed.
    interfacePlaceholder: computed(() =>
      tor(`deploy.field.interface.${targetID.value}.placeholder`, ''),
    ),
    passwordLabel: computed(() =>
      tor(
        `deploy.field.password.${targetID.value}`,
        t('devices.field.credential'),
      ),
    ),
    needsAccount,
    needsInterface,
    problems,
  }
}
