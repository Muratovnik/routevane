/** Shapes the shared primitives accept. A component could export them from a
 * plain `<script lang="ts">` block of its own, but a host needs the contract to
 * build its own data without importing the component, and a type import that
 * reaches into a `.vue` file is fragile in the editor and build tooling. So the
 * contract is a module and the components import it too. */

export type StatusTone = 'ready' | 'warning' | 'failed' | 'busy' | 'waiting'

export type Fact = {
  key: string
  label: string
  mono?: boolean
  value: string
}

export type SegmentOption = {
  label: string
  value: string
}

/** One choice in a select or a combobox. */
export type ChoiceOption = {
  value: string
  label: string
  /** Compared character by character — a file extension, an identifier. */
  mono?: string
  /** One short fact about the choice, read under its name. */
  note?: string
  /** A stated limit of this choice, in words rather than in colour alone. */
  warning?: string
  disabled?: boolean
}

/** A named run of choices inside one list. */
export type ChoiceGroup = {
  key: string
  label: string
  options: ChoiceOption[]
}

export type IconName =
  | 'archive'
  | 'check'
  | 'chevron'
  | 'close'
  | 'copy'
  | 'dots'
  | 'download'
  | 'drag'
  | 'edit'
  | 'external'
  | 'file'
  | 'info'
  | 'library'
  | 'plus'
  | 'refresh'
  | 'search'
  | 'send'
  | 'settings'
  | 'targets'
  | 'trash'
  | 'warning'

export type MenuItem = {
  key: string
  label: string
  icon?: IconName
  to?: string
  href?: string
  download?: boolean
  disabled?: boolean
  separatorBefore?: boolean
  children?: MenuItem[]
}

export type TabItem = {
  id: string
  label: string
}
