import type { ProfileCard } from '@/shared/api/profiles'

const createdTime = (card: ProfileCard): number => {
  const parsed = Date.parse(card.createdAt)
  return Number.isNaN(parsed) ? 0 : parsed
}

/**
 * A profile that returns to a shelf goes back where its age puts it: the shelf
 * is newest first with the identity as the tie-break, the order the server pages
 * in, and the profile is no newer than it was.
 */
export const insertNewestFirst = (
  list: ProfileCard[],
  card: ProfileCard,
): ProfileCard[] =>
  [...list, card].sort(
    (left, right) =>
      createdTime(right) - createdTime(left) || left.id.localeCompare(right.id),
  )
