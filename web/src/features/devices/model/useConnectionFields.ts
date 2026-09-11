import { computed, type Ref } from 'vue'

import type { ConnectionRequirements } from '@/shared/api/deploy'
import { useLocale } from '@/shared/i18n/useLocale'

/**
 * The field contract of one connection: what each field is called for the
 * chosen target, and which of them that target's deployer actually needs.
 *
 * The address field means a different thing per deployer — a router URL for
 * Keenetic, a configuration path for sing-box — so the dictionary names it per
 * target and follows the chosen language. A plugin target the dictionary does
 * not know keeps its own caption rather than getting a wrong generic one.
 *
 * Creation, editing and the page's own validity all read this one derivation,
 * so they cannot disagree about which fields a target has.
 */
export const useConnectionFields = (
  requirements: Ref<ConnectionRequirements | null>,
  targetID: Ref<string>,
) => {
  const { t, tor } = useLocale()

  return {
    addressLabel: computed(() =>
      tor(
        `deploy.field.address.${targetID.value}`,
        requirements.value?.addressLabel ?? t('devices.field.address'),
      ),
    ),
    addressPlaceholder: computed(
      () => requirements.value?.addressExample ?? '',
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
    needsAccount: computed(() => requirements.value?.needsCredential === true),
    needsInterface: computed(() => requirements.value?.needsInterface === true),
  }
}
