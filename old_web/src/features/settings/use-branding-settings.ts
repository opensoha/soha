import { useQuery } from '@tanstack/react-query'
import { api } from '@/services/api-client'
import { useAuthStore } from '@/stores/auth-store'
import type { ApiResponse, BrandingSettings } from '@/types'
import { defaultBrandingSettings, normalizeBrandingSettings, readStoredBrandingSettings } from '@/utils/branding'

export function useBrandingSettings() {
  const isAuthenticated = useAuthStore((state) => state.isAuthenticated())
  const fallback = readStoredBrandingSettings()
  const query = useQuery({
    queryKey: ['settings-branding'],
    queryFn: () => api.get<ApiResponse<BrandingSettings>>('/settings/branding'),
    enabled: isAuthenticated,
  })
  return {
    ...query,
    data: query.data ?? { data: fallback || defaultBrandingSettings },
  }
}

export function getNormalizedBranding(value?: BrandingSettings | null) {
  return normalizeBrandingSettings(value ?? defaultBrandingSettings)
}
