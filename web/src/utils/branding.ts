import type { BrandingSettings } from '@/types'

const BRANDING_STORAGE_KEY = 'soha-branding'

export const defaultBrandingSettings: BrandingSettings = {
  appTitle: 'Soha',
  sidebarTitle: 'Soha',
  loginLogoUrl: '',
  expandedLogoUrl: '',
  collapsedLogoUrl: '',
  faviconUrl: '',
}

export function normalizeBrandingSettings(value?: Partial<BrandingSettings> | null): BrandingSettings {
  return {
    appTitle: String(value?.appTitle ?? defaultBrandingSettings.appTitle).trim() || defaultBrandingSettings.appTitle,
    sidebarTitle: String(value?.sidebarTitle ?? value?.appTitle ?? defaultBrandingSettings.sidebarTitle).trim() || defaultBrandingSettings.sidebarTitle,
    loginLogoUrl: String(value?.loginLogoUrl ?? '').trim(),
    expandedLogoUrl: String(value?.expandedLogoUrl ?? '').trim(),
    collapsedLogoUrl: String(value?.collapsedLogoUrl ?? '').trim(),
    faviconUrl: String(value?.faviconUrl ?? '').trim(),
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
