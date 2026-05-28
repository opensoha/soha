import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import type { BusinessWorkspaceType } from '@/types'
import type { AppThemeId, ThemeMode } from '@/theme/app-theme'
import { DEFAULT_APP_THEME_ID, DEFAULT_THEME_MODE } from '@/theme/app-theme'

interface PreferencesState {
  sidebarCollapsed: boolean
  currentWorkspace: BusinessWorkspaceType | null
  themeId: AppThemeId
  themeMode: ThemeMode
  localeCode: 'zh_CN' | 'en_US'
  toggleSidebar: () => void
  setSidebarCollapsed: (collapsed: boolean) => void
  setCurrentWorkspace: (workspace: BusinessWorkspaceType | null) => void
  setThemeId: (themeId: AppThemeId) => void
  setThemeMode: (themeMode: ThemeMode) => void
  setLocaleCode: (localeCode: 'zh_CN' | 'en_US') => void
}

export const usePreferencesStore = create<PreferencesState>()(
  persist(
    (set) => ({
      sidebarCollapsed: false,
      currentWorkspace: null,
      themeId: DEFAULT_APP_THEME_ID,
      themeMode: DEFAULT_THEME_MODE,
      localeCode: 'zh_CN',
      toggleSidebar: () => set((s) => ({ sidebarCollapsed: !s.sidebarCollapsed })),
      setSidebarCollapsed: (collapsed) => set({ sidebarCollapsed: collapsed }),
      setCurrentWorkspace: (currentWorkspace) => set({ currentWorkspace }),
      setThemeId: (themeId) => set({ themeId }),
      setThemeMode: (themeMode) => set({ themeMode }),
      setLocaleCode: (localeCode) => set({ localeCode }),
    }),
    { name: 'soha-prefs' },
  ),
)
