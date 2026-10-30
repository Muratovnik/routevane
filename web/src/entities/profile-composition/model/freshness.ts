import { useLocale } from '@/shared/i18n/useLocale'

import type { ListFreshness } from './types'

export type FreshnessTone = 'ready' | 'waiting' | 'failed'

export type FreshnessText = { label: string; tone: FreshnessTone }

/**
 * How a list's latest source read reads to the operator: when it was and what
 * it changed. The card says it in a sentence and the library's column in a
 * cell, so both read the same facts the same way.
 */
export const useFreshnessText = () => {
  const { formatAgo, t } = useLocale()

  // What the latest successful read brought and withdrew. A withdrawn value
  // stays in the plans until its own observation expires, so it is named as
  // gone from the sources rather than gone from the list.
  const changes = (freshness: ListFreshness): string => {
    const parts: string[] = []
    if (freshness.added > 0)
      parts.push(t('listFreshness.added', { count: freshness.added }))
    if (freshness.removed > 0)
      parts.push(t('listFreshness.removed', { count: freshness.removed }))
    return parts.length === 0 ? t('listFreshness.unchanged') : parts.join(' · ')
  }

  /** The card's sentence about its sources. */
  const line = (freshness: ListFreshness, now: number): FreshnessText => {
    const ago = formatAgo(freshness.lastReadAt, now)
    switch (freshness.state) {
      case 'none':
        return { label: t('listFreshness.off'), tone: 'waiting' }
      case 'never':
        return { label: t('listFreshness.never'), tone: 'waiting' }
      case 'failed':
        return {
          label:
            freshness.lastReadAt === ''
              ? t('listFreshness.failed')
              : t('listFreshness.failed.since', { ago }),
          tone: 'failed',
        }
      case 'stale':
        return {
          label: [
            t('listFreshness.read', { ago }),
            t('listFreshness.stale'),
          ].join(' · '),
          tone: 'waiting',
        }
      case 'fresh':
        return {
          label: [t('listFreshness.read', { ago }), changes(freshness)].join(
            ' · ',
          ),
          tone: 'ready',
        }
    }
  }

  /** The library's cell: the column already says what the moment is. */
  const cell = (
    freshness: ListFreshness | undefined,
    now: number,
  ): FreshnessText => {
    if (freshness === undefined || freshness.state === 'none')
      return { label: '—', tone: 'waiting' }
    const ago = formatAgo(freshness.lastReadAt, now)
    switch (freshness.state) {
      case 'never':
        return { label: t('listFreshness.cell.never'), tone: 'waiting' }
      case 'failed':
        return { label: t('listFreshness.cell.failed'), tone: 'failed' }
      case 'stale':
        return {
          label: [ago, t('listFreshness.stale')].join(' · '),
          tone: 'waiting',
        }
      case 'fresh':
        return {
          label:
            freshness.added > 0
              ? [
                  ago,
                  t('listFreshness.added', { count: freshness.added }),
                ].join(' · ')
              : ago,
          tone: 'ready',
        }
    }
  }

  return { cell, line }
}
