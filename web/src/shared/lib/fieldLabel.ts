import type { ComputedRef, InjectionKey } from 'vue'

/**
 * What a field tells the choice drawn inside it about its own label.
 *
 * A `<label for>` names a button by the label alone, so a choice that is a
 * button would never announce what is chosen. The field draws its label text
 * under an id and hands that id to the control it labels, which can then name
 * itself by the label and its own content. A control outside any field is never
 * told one, so it cannot point at a label that was not drawn.
 */
export type FieldLabel = {
  /** The control the label names. */
  inputId: string
  /** The id of the label's text. */
  labelId: string
  /** The words of the label, for a panel that cannot point at them. */
  label: string
}

export const fieldLabelKey: InjectionKey<ComputedRef<FieldLabel>> =
  Symbol('rv-field-label')

export const fieldLabelId = (inputId: string): string => `${inputId}-label`
