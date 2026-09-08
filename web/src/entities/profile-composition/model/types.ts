/** The records this slice presents, named once for everything inside it.
 *
 * The transport declares these shapes and stays their single owner; the slice
 * re-states them here so a component depends on the slice's own model rather
 * than on the module that fetches them, and so there is never a second copy to
 * keep in step.
 */

export type {
  CategoryDetail,
  ListContents,
  ListContentsRow,
  ListDetail,
} from '@/shared/api/catalog'

export type { ProfileComposition, TargetForecast } from '@/shared/api/profiles'
