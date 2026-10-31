import type { BrandingSettings } from '@/types'
import { appBrowserPath, defaultBrandAsset } from './app-base'

const BRANDING_STORAGE_KEY = 'soha-branding'

export const defaultBrandingSettings: BrandingSettings = {
  appTitle: 'Soha',
  sidebarTitle: 'Soha',
  slogan: 'Soha 是一种能力！',
  loginLogoUrl: appBrowserPath('/logo.svg'),
  expandedLogoUrl: appBrowserPath('/logo.svg'),
  collapsedLogoUrl: appBrowserPath('/logo.svg'),
  darkExpandedLogoUrl: '',
  darkCollapsedLogoUrl: '',
  faviconUrl: appBrowserPath('/logo.svg'),
}

export function normalizeBrandingSettings(
  value?: Partial<BrandingSettings> | null,
): BrandingSettings {
  const result = {
    appTitle:
      String(value?.appTitle ?? defaultBrandingSettings.appTitle).trim() ||
      defaultBrandingSettings.appTitle,
    sidebarTitle:
      String(
        value?.sidebarTitle ?? value?.appTitle ?? defaultBrandingSettings.sidebarTitle,
      ).trim() || defaultBrandingSettings.sidebarTitle,
    slogan:
      String(value?.slogan ?? defaultBrandingSettings.slogan).trim() ||
      defaultBrandingSettings.slogan,
    loginLogoUrl:
      String(value?.loginLogoUrl ?? defaultBrandingSettings.loginLogoUrl).trim() ||
      defaultBrandingSettings.loginLogoUrl,
    expandedLogoUrl:
      String(value?.expandedLogoUrl ?? defaultBrandingSettings.expandedLogoUrl).trim() ||
      defaultBrandingSettings.expandedLogoUrl,
    collapsedLogoUrl:
      String(value?.collapsedLogoUrl ?? defaultBrandingSettings.collapsedLogoUrl).trim() ||
      defaultBrandingSettings.collapsedLogoUrl,
    darkExpandedLogoUrl: String(value?.darkExpandedLogoUrl ?? '').trim(),
    darkCollapsedLogoUrl: String(value?.darkCollapsedLogoUrl ?? '').trim(),
    faviconUrl:
      String(value?.faviconUrl ?? defaultBrandingSettings.faviconUrl).trim() ||
      defaultBrandingSettings.faviconUrl,
  }
  for (const key of [
    'loginLogoUrl',
    'expandedLogoUrl',
    'collapsedLogoUrl',
    'faviconUrl',
  ] as const) {
    result[key] = defaultBrandAsset(result[key])
  }
  return result
}

export function resolveBrandingLogos(branding: BrandingSettings, themeMode: 'light' | 'dark') {
  const expandedLogoUrl = branding.expandedLogoUrl || branding.loginLogoUrl
  const collapsedLogoUrl = branding.collapsedLogoUrl
  return {
    expandedLogoUrl:
      themeMode === 'dark' ? branding.darkExpandedLogoUrl || expandedLogoUrl : expandedLogoUrl,
    collapsedLogoUrl:
      themeMode === 'dark' ? branding.darkCollapsedLogoUrl || collapsedLogoUrl : collapsedLogoUrl,
  }
}

export function readStoredBrandingSettings(): BrandingSettings {
  if (typeof window === 'undefined') {
    return defaultBrandingSettings
  }
  try {
    const raw = window.localStorage.getItem(BRANDING_STORAGE_KEY)
    if (!raw) {
      return defaultBrandingSettings
    }
    return normalizeBrandingSettings(JSON.parse(raw) as Partial<BrandingSettings>)
  } catch {
    return defaultBrandingSettings
  }
}

export function persistBrandingSettings(value: BrandingSettings) {
  if (typeof window === 'undefined') {
    return
  }
  try {
    window.localStorage.setItem(BRANDING_STORAGE_KEY, JSON.stringify(value))
  } catch {
    // Ignore storage failures so branding bootstrap cannot break initial render.
  }
}

export function applyBrandingSettings(value: BrandingSettings) {
  if (typeof document === 'undefined') {
    return
  }
  document.title = value.appTitle
  if (!value.faviconUrl) {
    return
  }
  let favicon = document.querySelector<HTMLLinkElement>("link[rel*='icon']")
  if (!favicon) {
    favicon = document.createElement('link')
    favicon.rel = 'icon'
    document.head.appendChild(favicon)
  }
  favicon.href = value.faviconUrl
}
