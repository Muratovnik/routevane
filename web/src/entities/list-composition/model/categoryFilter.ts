import type { CategoryDetail } from '@/shared/api/catalog'

/** Empty selection includes every list; any selected category may contain it. */
export function matchesCategories(
  serviceID: string,
  selected: string[],
  categories: CategoryDetail[],
): boolean {
  return (
    selected.length === 0 ||
    selected.some((id) =>
      id === 'rv:uncategorized'
        ? !categories.some((category) => category.services.includes(serviceID))
        : categories.some(
            (category) =>
              category.id === id && category.services.includes(serviceID),
          ),
    )
  )
}
