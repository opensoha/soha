import { useQuery } from '@tanstack/react-query'
import { useAuthStore } from '@/stores/auth-store'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { readableQueryData } from '@/utils/query-feedback'
import type { BrandingSettings } from '@/types'
import {
  defaultBrandingSettings,
  normalizeBrandingSettings,
  readStoredBrandingSettings,
} from '@/utils/branding'
import { settingsQueries } from './queries'

export function useBrandingSettings() {
  const isAuthenticated = useAuthStore((state) => state.isAuthenticated())
  const fallback = readStoredBrandingSettings()
  const permissions = usePermissionSnapshot()
  const canRead = hasPermission(readableQueryData(permissions)?.data, 'settings.branding.view')
  const query = useQuery(settingsQueries.branding(isAuthenticated && canRead))
  return {
    ...query,
    data: { data: query.data ?? fallback ?? defaultBrandingSettings },
  }
}

export function getNormalizedBranding(value?: BrandingSettings | null) {
  return normalizeBrandingSettings(value ?? defaultBrandingSettings)
}
