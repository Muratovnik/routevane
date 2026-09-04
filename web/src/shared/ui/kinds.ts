/** Shapes the shared primitives accept. They live outside the components
 * because a single-file component's setup block is not a module that can
 * export them, and a host needs the type to build its own data. */

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
