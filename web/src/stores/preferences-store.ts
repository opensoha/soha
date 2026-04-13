import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import type { SemiThemeId, ThemeMode } from '@/theme/semi-theme'
import { DEFAULT_SEMI_THEME_ID, DEFAULT_THEME_MODE } from '@/theme/semi-theme'

interface PreferencesState {
  sidebarCollapsed: boolean
  themeId: SemiThemeId
  themeMode: ThemeMode
  localeCode: 'zh_CN' | 'en_US'
  toggleSidebar: () => void
  setSidebarCollapsed: (collapsed: boolean) => void
  setThemeId: (themeId: SemiThemeId) => void
  setThemeMode: (themeMode: ThemeMode) => void
  setLocaleCode: (localeCode: 'zh_CN' | 'en_US') => void
}

export const usePreferencesStore = create<PreferencesState>()(
  persist(
    (set) => ({
      sidebarCollapsed: false,
      themeId: DEFAULT_SEMI_THEME_ID,
      themeMode: DEFAULT_THEME_MODE,
      localeCode: 'zh_CN',
      toggleSidebar: () => set((s) => ({ sidebarCollapsed: !s.sidebarCollapsed })),
      setSidebarCollapsed: (collapsed) => set({ sidebarCollapsed: collapsed }),
      setThemeId: (themeId) => set({ themeId }),
      setThemeMode: (themeMode) => set({ themeMode }),
      setLocaleCode: (localeCode) => set({ localeCode }),
    }),
    { name: 'kubecrux-prefs' },
  ),
)
