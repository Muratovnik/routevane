import type { ProfileCard } from '@/shared/api/profiles'
import { profilePageHash } from '@/shared/lib/profileHash'

/** The address of a profile's page, optionally on one of its tabs. */
export const profileHref = (card: ProfileCard, tab = ''): string =>
  `/profiles/${card.id}${profilePageHash({ tab })}`
