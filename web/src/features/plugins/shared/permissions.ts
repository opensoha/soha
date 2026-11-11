import { hasPermission } from '@/features/auth'
import type { PermissionSnapshot } from '@/types'

export function canInstallPlugin(snapshot?: PermissionSnapshot) {
  return hasPermission(snapshot, 'plugin.install')
}

export function canConfigurePlugins(snapshot?: PermissionSnapshot) {
  return hasPermission(snapshot, 'plugin.configure')
}

export function canChangePluginLifecycle(snapshot?: PermissionSnapshot) {
  return hasPermission(snapshot, 'plugin.lifecycle')
}

export function canRemovePlugins(snapshot?: PermissionSnapshot) {
  return hasPermission(snapshot, 'plugin.remove')
}

export function canUpgradePlugins(snapshot?: PermissionSnapshot) {
  return hasPermission(snapshot, 'plugin.upgrade')
}

export function canConfigurePluginSecrets(snapshot?: PermissionSnapshot) {
  return hasPermission(snapshot, 'plugin.configure_secrets')
}
