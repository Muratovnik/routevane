import type { IconName } from '@/shared/ui/types'

const TARGET_ICONS: Record<string, IconName> = {
  amnezia: 'amnezia',
  keenetic: 'keenetic',
  'keenetic-dns': 'keenetic',
  mikrotik: 'mikrotik',
  openwrt: 'openwrt',
  singbox: 'sing-box',
}

export const targetIcon = (targetID: string): IconName =>
  TARGET_ICONS[targetID] ?? 'targets'
