import { computed, type Ref } from 'vue'

import type { ConnectionRequirements } from '@/shared/api/deploy'
import { useLocale } from '@/shared/i18n/useLocale'
import { localDeviceAddress, validAddress } from '@/shared/lib/deviceAddress'

/** The fields of one connection, in the order the form draws them. */
export const FIELD_ORDER = [
  'name',
  'address',
  'account',
  'interfaceName',
] as const
export type FieldKey = (typeof FIELD_ORDER)[number]

/** What is wrong with one field. The words are the form's, not the model's. */
export type FieldProblem = 'required' | 'shape' | 'local'
export type FieldProblems = Partial<Record<FieldKey, FieldProblem>>

export type ConnectionDraft = Record<FieldKey, string>

const HTTP_ADDRESS = /^https?:\/\//i

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

  // A router is reached over http or https, and the service accepts only a
  // private or link-local IP literal for it. The deployer's own example says
  // which kind of address this target takes; with no answer yet, any http
  // address is held to the same rule.
  const httpDestination = (value: string): boolean =>
    HTTP_ADDRESS.test(value) ||
    HTTP_ADDRESS.test(requirements.value?.addressExample ?? '')

  /**
   * Every field of the draft that the service would refuse or that is left
   * empty. The draft is expected trimmed; an untouched optional field is not a
   * problem.
   */
  const problems = (draft: ConnectionDraft): FieldProblems => {
    const found: FieldProblems = {}
    if (draft.name === '') found.name = 'required'
    if (draft.address === '') found.address = 'required'
    else if (!validAddress(draft.address)) found.address = 'shape'
    else if (
      httpDestination(draft.address) &&
      !localDeviceAddress(draft.address)
    )
      found.address = 'local'
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
    // What the service will accept, said before it is typed rather than only
    // after it is refused. The rule belongs to the target's deployer, so the
    // dictionary carries one line per target and a target it does not know
    // gets no empty line under its field.
    addressHint: computed(() =>
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
    // A hint states an input format or a consequence, so a target the
    // dictionary carries no hint for gets no empty line under its field.
    interfaceHint: computed(() =>
      tor(`deploy.field.interface.${targetID.value}.hint`, ''),
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
