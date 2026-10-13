import { profileHref } from '@/features/profiles/lib/profileHref'
import type { useProfiles } from '@/features/profiles/model/useProfiles'
import { useLocale } from '@/shared/i18n/useLocale'
import type { ExportFormat } from '@/shared/api/exports'
import type { ProfileCard } from '@/shared/api/profiles'
import type { MenuItem } from '@/shared/ui/types'

/**
 * The action set of one profile row. A profile's menu lists what can happen to
 * it now: what it cannot do — send a format that was never published, copy a
 * file that does not exist — is absent rather than disabled. A live profile and
 * an archived one are different sets, because an archived profile is frozen and
 * its way out is deletion.
 */
export const useProfileMenu = (library: ReturnType<typeof useProfiles>) => {
  const { t, tor } = useLocale()

  const exportLabel = (format: ExportFormat): string =>
    tor(
      `export.format.${format.rendererID}`,
      format.fileExtension.toUpperCase(),
    )

  const exportItem = (): MenuItem[] =>
    library.exportFormats.value.length === 0
      ? []
      : [
          {
            children: library.exportFormats.value.map((format) => ({
              disabled: library.exporting.value !== '',
              key: `export:${format.id}`,
              label: exportLabel(format),
            })),
            icon: 'download',
            key: 'export',
            label: t('profiles.menu.export'),
          },
        ]

  const sendItem = (card: ProfileCard): MenuItem[] => {
    const children = library.deployableOutputs(card).map((output) => {
      const format = library.outputTitle(output)
      const connection = library.connectionName(output)
      return {
        key: `send:${output.id}`,
        label:
          connection === ''
            ? format
            : t('profiles.sendTo', { connection, format }),
        to: `/profiles/${card.id}/send/${output.id}`,
      }
    })
    return children.length === 0
      ? []
      : [
          {
            children,
            icon: 'send',
            key: 'send',
            label: t('profiles.menu.send'),
          },
        ]
  }

  // Which file lands on the clipboard is the operator's choice: one published
  // format names itself, several are a submenu.
  const copyItem = (card: ProfileCard): MenuItem[] => {
    const copyable = library.publishedOutputs(card)
    const [only] = copyable
    if (copyable.length === 1 && only !== undefined) {
      return [
        {
          icon: 'copy',
          key: `copy:${only.id}`,
          label: t('profiles.copy.format', {
            format: library.outputTitle(only),
          }),
        },
      ]
    }
    if (copyable.length === 0) return []
    return [
      {
        children: copyable.map((output) => ({
          key: `copy:${output.id}`,
          label: library.outputTitle(output),
        })),
        icon: 'copy',
        key: 'copy',
        label: t('profiles.copy'),
      },
    ]
  }

  const archivedItems = (): MenuItem[] => [
    {
      disabled: library.shelving.value !== '',
      icon: 'archive',
      key: 'restore',
      label: t('profiles.restore'),
    },
    {
      disabled: library.deleting.value !== '',
      icon: 'trash',
      key: 'delete',
      label: t('profiles.delete'),
      separatorBefore: true,
    },
  ]

  const rebuildItem = (card: ProfileCard): MenuItem[] =>
    library.rebuildable(card)
      ? [
          {
            disabled: library.rebuilding.value !== '',
            icon: 'refresh',
            key: 'rebuild',
            label: t('profiles.rebuild'),
          },
        ]
      : []

  const liveItems = (card: ProfileCard): MenuItem[] => [
    ...sendItem(card),
    {
      key: 'formats',
      label: t('profiles.addFormat'),
      to: profileHref(card, 'outputs'),
    },
    ...copyItem(card),
    ...rebuildItem(card),
    // One shelf change at a time: the archive answer rearranges the shelf, so a
    // second request is withheld until the first one has been answered.
    {
      disabled: library.shelving.value !== '',
      icon: 'archive',
      key: 'archive',
      label: t('profiles.archive'),
      separatorBefore: true,
    },
  ]

  const menuItems = (card: ProfileCard): MenuItem[] => [
    { key: 'open', label: t('profiles.configure'), to: profileHref(card) },
    ...exportItem(),
    ...(card.archivedAt === '' ? liveItems(card) : archivedItems()),
  ]

  return { menuItems }
}
