import { AnimatedIcon, type AnimatedIconName } from '@/components/animated-icon'

const WORKBENCH_ICONS: Record<string, AnimatedIconName> = {
  home: 'home',
  platform: 'cluster',
  delivery: 'activity',
  monitoring: 'radio-tower',
  compute: 'server',
  ai: 'bot',
  security: 'shield',
  settings: 'settings',
  account: 'user',
}

const MENU_ICONS = [
  { value: 'home', label: 'Home' },
  { value: 'gauge', label: 'Dashboard' },
  { value: 'server', label: 'Server' },
  { value: 'desktop', label: 'Desktop' },
  { value: 'cluster', label: 'Cluster' },
  { value: 'puzzle', label: 'Puzzle' },
  { value: 'shield', label: 'Shield' },
  { value: 'boxes', label: 'Boxes' },
  { value: 'cog', label: 'Cog' },
  { value: 'network', label: 'Network' },
  { value: 'waves', label: 'Storage' },
  { value: 'globe', label: 'Globe' },
  { value: 'bell', label: 'Bell' },
  { value: 'users', label: 'Users' },
  { value: 'bot', label: 'Bot' },
  { value: 'image', label: 'Image' },
  { value: 'blocks', label: 'Blocks' },
  { value: 'flavor', label: 'Flavor' },
  { value: 'code', label: 'Code' },
  { value: 'activity', label: 'Activity' },
  { value: 'history', label: 'History' },
  { value: 'info', label: 'Info' },
  { value: 'sync', label: 'Sync' },
  { value: 'panels-top-left', label: 'Panels' },
  { value: 'megaphone', label: 'Megaphone' },
  { value: 'menu-square', label: 'Menu' },
  { value: 'clipboard-list', label: 'Clipboard' },
  { value: 'file-clock', label: 'File Clock' },
  { value: 'siren', label: 'Alert' },
  { value: 'user', label: 'User' },
  { value: 'docker', label: 'Docker' },
  { value: 'book', label: 'Book' },
  { value: 'inspect', label: 'Inspect' },
  { value: 'key', label: 'Key' },
  { value: 'link', label: 'Link' },
  { value: 'palette', label: 'Palette' },
  { value: 'radio-tower', label: 'Radio Tower' },
  { value: 'settings', label: 'Settings' },
  { value: 'storage', label: 'Storage' },
  { value: 'wrench', label: 'Wrench' },
] as const

export const MENU_ICON_OPTIONS = MENU_ICONS.map((item) => ({
  ...item,
  preview: resolveMenuIcon(item.value),
}))

export function resolveMenuIcon(iconKey?: string) {
  const key = String(iconKey || '').trim()
  return <AnimatedIcon name={isKnownMenuIcon(key) ? (key as AnimatedIconName) : 'menu-square'} />
}

export function resolveWorkbenchIcon(workbenchId: string) {
  return <AnimatedIcon name={WORKBENCH_ICONS[workbenchId] ?? 'workbench'} />
}

export function isKnownMenuIcon(iconKey?: string) {
  return MENU_ICONS.some((item) => item.value === String(iconKey || '').trim())
}
