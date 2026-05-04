import type { ReactNode } from 'react'
import {
  AlertOutlined,
  AppstoreOutlined,
  BellOutlined,
  CommentOutlined,
  DashboardOutlined,
  DeploymentUnitOutlined,
  FileOutlined,
  GlobalOutlined,
  HddOutlined,
  MenuOutlined,
  OrderedListOutlined,
  SafetyOutlined,
  SendOutlined,
  SettingOutlined,
  TeamOutlined,
  UserOutlined,
} from '@ant-design/icons'

const ICONS: Record<string, ReactNode> = {
  gauge: <DashboardOutlined />,
  server: <HddOutlined />,
  puzzle: <DeploymentUnitOutlined />,
  shield: <SafetyOutlined />,
  boxes: <AppstoreOutlined />,
  cog: <SettingOutlined />,
  network: <SendOutlined />,
  waves: <HddOutlined />,
  globe: <GlobalOutlined />,
  bell: <BellOutlined />,
  users: <TeamOutlined />,
  bot: <CommentOutlined />,
  blocks: <AppstoreOutlined />,
  activity: <SendOutlined />,
  'panels-top-left': <SettingOutlined />,
  megaphone: <BellOutlined />,
  'menu-square': <MenuOutlined />,
  'clipboard-list': <OrderedListOutlined />,
  'file-clock': <FileOutlined />,
  siren: <AlertOutlined />,
  user: <UserOutlined />,
}

export const MENU_ICON_OPTIONS = [
  { value: 'gauge', label: 'Dashboard' },
  { value: 'server', label: 'Server' },
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
  { value: 'blocks', label: 'Blocks' },
  { value: 'activity', label: 'Activity' },
  { value: 'panels-top-left', label: 'Panels' },
  { value: 'megaphone', label: 'Megaphone' },
  { value: 'menu-square', label: 'Menu' },
  { value: 'clipboard-list', label: 'Clipboard' },
  { value: 'file-clock', label: 'File Clock' },
  { value: 'siren', label: 'Alert' },
  { value: 'user', label: 'User' },
].map((item) => ({ ...item, preview: ICONS[item.value] ?? <MenuOutlined /> }))

export function resolveMenuIcon(iconKey?: string) {
  return ICONS[String(iconKey || '').trim()] ?? <MenuOutlined />
}

export function isKnownMenuIcon(iconKey?: string) {
  return Object.prototype.hasOwnProperty.call(ICONS, String(iconKey || '').trim())
}
