import type { BrandingSettings, PermissionSnapshot } from '@/types'
import { fetchBootstrapState, getStoredAccessToken } from './auth-api'

export interface RuntimeBootstrapState {
  currentUser?: API.CurrentUser
  permissionSnapshot?: PermissionSnapshot
  branding?: BrandingSettings
}

export async function loadRuntimeBootstrap(): Promise<RuntimeBootstrapState> {
  const bootstrap = await fetchBootstrapState(getStoredAccessToken())
  return {
    currentUser: bootstrap.currentUser,
    permissionSnapshot: bootstrap.permissionSnapshot,
    branding: bootstrap.branding,
  }
}
