import { history } from '@umijs/max'
import type { Settings as LayoutSettings } from '@ant-design/pro-components'
import type { BrandingSettings, PermissionSnapshot } from '@/types'
import defaultSettings from '../../../config/defaultSettings'
import { fetchBootstrapState, getStoredAccessToken } from './auth-api'

const loginPath = '/login'

export interface RuntimeInitialState {
  settings?: Partial<LayoutSettings>
  currentUser?: API.CurrentUser
  permissionSnapshot?: PermissionSnapshot
  branding?: BrandingSettings
  loading?: boolean
  fetchUserInfo?: () => Promise<API.CurrentUser | undefined>
  settingDrawerOpen?: boolean
}

function getRedirectTarget() {
  const { pathname, search, hash } = history.location
  return `${loginPath}?redirect=${encodeURIComponent(pathname + search + hash)}`
}

export function buildLayoutSettings(branding?: BrandingSettings): Partial<LayoutSettings> {
  return {
    ...defaultSettings,
    title: branding?.sidebarTitle || defaultSettings.title,
  } as Partial<LayoutSettings>
}

export async function loadRuntimeBootstrap(): Promise<RuntimeInitialState> {
  const bootstrap = await fetchBootstrapState(getStoredAccessToken())
  return {
    currentUser: bootstrap.currentUser,
    permissionSnapshot: bootstrap.permissionSnapshot,
    branding: bootstrap.branding,
    settings: buildLayoutSettings(bootstrap.branding),
    settingDrawerOpen: false,
  }
}

export async function fetchRuntimeUserInfo() {
  try {
    const bootstrap = await fetchBootstrapState(getStoredAccessToken())
    return bootstrap.currentUser
  } catch {
    history.replace(getRedirectTarget())
    return undefined
  }
}

export function redirectToLogin() {
  history.replace(getRedirectTarget())
}

export function buildAnonymousRuntimeState(): RuntimeInitialState {
  return {
    fetchUserInfo: fetchRuntimeUserInfo,
    settings: defaultSettings as Partial<LayoutSettings>,
    settingDrawerOpen: false,
  }
}

export function getRuntimeLoginPath() {
  return loginPath
}
