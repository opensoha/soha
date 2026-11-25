import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import type { BusinessWorkspaceType } from '@/types'
import type { AppThemeId, LightCanvasPreset, ThemeMode } from '@/theme/app-theme'
import {
  DEFAULT_ACCENT_COLOR,
  DEFAULT_APP_THEME_ID,
  DEFAULT_DARK_CANVAS_COLOR,
  DEFAULT_LIGHT_CANVAS_COLOR,
  DEFAULT_THEME_MODE,
} from '@/theme/app-theme'

export interface ApplicationListFilters {
  group?: string
  search?: string
  scope?: 'all' | 'favorites' | 'recent'
  view?: 'cards' | 'list'
}

interface PreferencesState {
  applicationListFilters: Record<string, ApplicationListFilters>
  setApplicationListFilters: (userId: string, filters: ApplicationListFilters) => void
  sidebarCollapsed: boolean
  currentWorkspace: BusinessWorkspaceType | null
  themeId: AppThemeId
  themeMode: ThemeMode
  accentColor: string
  lightCanvas: LightCanvasPreset
  lightCanvasColor: string
  darkCanvasColor: string
  paperGrain: boolean
  localeCode: 'zh_CN' | 'en_US'
  companionMode: 'companion' | 'icon'
  companionBubbleEnabled: boolean
  selectedCompanionPluginId: string
  applicationShortcuts: Record<string, { favorites: string[]; recent: string[] }>
  toggleFavoriteApplication: (userId: string, applicationId: string) => void
  visitApplication: (userId: string, applicationId: string) => void
  toggleSidebar: () => void
  setSidebarCollapsed: (collapsed: boolean) => void
  setCurrentWorkspace: (workspace: BusinessWorkspaceType | null) => void
  setThemeId: (themeId: AppThemeId) => void
  setThemeMode: (themeMode: ThemeMode) => void
  setAccentColor: (accentColor: string) => void
  setLightCanvas: (lightCanvas: LightCanvasPreset) => void
  setLightCanvasColor: (color: string) => void
  setDarkCanvasColor: (color: string) => void
  setPaperGrain: (enabled: boolean) => void
  resetAppearanceDefaults: () => void
  setLocaleCode: (localeCode: 'zh_CN' | 'en_US') => void
  setCompanionMode: (mode: 'companion' | 'icon') => void
  setCompanionBubbleEnabled: (enabled: boolean) => void
  setSelectedCompanionPluginId: (pluginId: string) => void
}

export const usePreferencesStore = create<PreferencesState>()(
  persist(
    (set) => ({
      sidebarCollapsed: false,
      currentWorkspace: null,
      themeId: DEFAULT_APP_THEME_ID,
      themeMode: DEFAULT_THEME_MODE,
      accentColor: DEFAULT_ACCENT_COLOR,
      lightCanvas: 'apple',
      lightCanvasColor: DEFAULT_LIGHT_CANVAS_COLOR,
      darkCanvasColor: DEFAULT_DARK_CANVAS_COLOR,
      paperGrain: true,
      localeCode: 'zh_CN',
      companionMode: 'companion',
      companionBubbleEnabled: true,
      selectedCompanionPluginId: 'builtin.soha-companion',
      applicationShortcuts: {},
      applicationListFilters: {},
      setApplicationListFilters: (userId, filters) =>
        set((state) => ({
          applicationListFilters: { ...state.applicationListFilters, [userId]: filters },
        })),
      toggleFavoriteApplication: (userId, applicationId) =>
        set((state) => {
          const current = state.applicationShortcuts[userId] ?? { favorites: [], recent: [] }
          return {
            applicationShortcuts: {
              ...state.applicationShortcuts,
              [userId]: {
                ...current,
                favorites: current.favorites.includes(applicationId)
                  ? current.favorites.filter((id) => id !== applicationId)
                  : [...current.favorites, applicationId],
              },
            },
          }
        }),
      visitApplication: (userId, applicationId) =>
        set((state) => {
          const current = state.applicationShortcuts[userId] ?? { favorites: [], recent: [] }
          if (current.recent[0] === applicationId) return state
          return {
            applicationShortcuts: {
              ...state.applicationShortcuts,
              [userId]: {
                ...current,
                recent: [
                  applicationId,
                  ...current.recent.filter((id) => id !== applicationId),
                ].slice(0, 12),
              },
            },
          }
        }),
      toggleSidebar: () => set((s) => ({ sidebarCollapsed: !s.sidebarCollapsed })),
      setSidebarCollapsed: (collapsed) => set({ sidebarCollapsed: collapsed }),
      setCurrentWorkspace: (currentWorkspace) => set({ currentWorkspace }),
      setThemeId: (themeId) => set({ themeId }),
      setThemeMode: (themeMode) => set({ themeMode }),
      setAccentColor: (accentColor) => set({ accentColor }),
      setLightCanvas: (lightCanvas) => set({ lightCanvas }),
      setLightCanvasColor: (lightCanvasColor) => set({ lightCanvasColor }),
      setDarkCanvasColor: (darkCanvasColor) => set({ darkCanvasColor }),
      setPaperGrain: (paperGrain) => set({ paperGrain }),
      resetAppearanceDefaults: () =>
        set({
          accentColor: DEFAULT_ACCENT_COLOR,
          lightCanvas: 'apple',
          lightCanvasColor: DEFAULT_LIGHT_CANVAS_COLOR,
          darkCanvasColor: DEFAULT_DARK_CANVAS_COLOR,
          paperGrain: true,
        }),
      setLocaleCode: (localeCode) => set({ localeCode }),
      setCompanionMode: (companionMode) => set({ companionMode }),
      setCompanionBubbleEnabled: (companionBubbleEnabled) => set({ companionBubbleEnabled }),
      setSelectedCompanionPluginId: (selectedCompanionPluginId) =>
        set({ selectedCompanionPluginId }),
    }),
    { name: 'soha-prefs' },
  ),
)
