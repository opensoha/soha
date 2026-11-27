import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import type { BusinessWorkspaceType } from '@/types'
import type {
  AppThemeId,
  DrawerPresentationMode,
  DualRailStage,
  LightCanvasPreset,
  Overview3DMorphology,
  OverviewGradientPresetId,
  OverviewHeroStyle,
  ThemeMode,
} from '@/theme/app-theme'
import {
  DEFAULT_ACCENT_COLOR,
  DEFAULT_APP_THEME_ID,
  DEFAULT_CORNER_RADIUS,
  DEFAULT_DARK_CANVAS_COLOR,
  DEFAULT_LIGHT_CANVAS_COLOR,
  DEFAULT_OVERVIEW_GRADIENT_STOPS,
  DEFAULT_OVERVIEW_HERO_STYLE,
  DEFAULT_THEME_MODE,
  OVERVIEW_GRADIENT_PRESETS,
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
  overviewHeroStyle: OverviewHeroStyle
  overviewGradientPreset: OverviewGradientPresetId
  overviewGradientStops: [string, string, string]
  overview3dMorphology: Overview3DMorphology
  drawerPresentation: DrawerPresentationMode
  dualRailStage: DualRailStage
  cornerRadius: number
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
  setOverviewHeroStyle: (style: OverviewHeroStyle) => void
  setOverviewGradientPreset: (preset: OverviewGradientPresetId) => void
  setOverviewGradientStop: (index: 0 | 1 | 2, color: string) => void
  setOverview3dMorphology: (mode: Overview3DMorphology) => void
  setDrawerPresentation: (mode: DrawerPresentationMode) => void
  setDualRailStage: (stage: DualRailStage) => void
  setCornerRadius: (radius: number) => void
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
      overviewHeroStyle: DEFAULT_OVERVIEW_HERO_STYLE,
      overviewGradientPreset: 'workbench',
      overviewGradientStops: DEFAULT_OVERVIEW_GRADIENT_STOPS,
      overview3dMorphology: 'auto',
      drawerPresentation: 'split-card',
      dualRailStage: 'expanded',
      cornerRadius: DEFAULT_CORNER_RADIUS,
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
      setOverviewHeroStyle: (overviewHeroStyle) => set({ overviewHeroStyle }),
      setOverviewGradientPreset: (overviewGradientPreset) =>
        set((state) => {
          const matched = OVERVIEW_GRADIENT_PRESETS.find((item) => item.id === overviewGradientPreset)
          return {
            overviewGradientPreset,
            overviewGradientStops: matched ? [...matched.stops] : state.overviewGradientStops,
          }
        }),
      setOverviewGradientStop: (index, color) =>
        set((state) => {
          const next: [string, string, string] = [...state.overviewGradientStops]
          next[index] = color
          return {
            overviewGradientPreset: 'custom',
            overviewGradientStops: next,
          }
        }),
      setOverview3dMorphology: (overview3dMorphology) => set({ overview3dMorphology }),
      setDrawerPresentation: (drawerPresentation) => set({ drawerPresentation }),
      setDualRailStage: (dualRailStage) => set({ dualRailStage }),
      setCornerRadius: (cornerRadius) => set({ cornerRadius }),
      resetAppearanceDefaults: () =>
        set({
          accentColor: DEFAULT_ACCENT_COLOR,
          lightCanvas: 'apple',
          lightCanvasColor: DEFAULT_LIGHT_CANVAS_COLOR,
          darkCanvasColor: DEFAULT_DARK_CANVAS_COLOR,
          paperGrain: true,
          overviewHeroStyle: DEFAULT_OVERVIEW_HERO_STYLE,
          overviewGradientPreset: 'workbench',
          overviewGradientStops: DEFAULT_OVERVIEW_GRADIENT_STOPS,
          overview3dMorphology: 'auto',
          drawerPresentation: 'split-card',
          dualRailStage: 'expanded',
          cornerRadius: DEFAULT_CORNER_RADIUS,
        }),
      setLocaleCode: (localeCode) => set({ localeCode }),
      setCompanionMode: (companionMode) => set({ companionMode }),
      setCompanionBubbleEnabled: (companionBubbleEnabled) => set({ companionBubbleEnabled }),
      setSelectedCompanionPluginId: (selectedCompanionPluginId) =>
        set({ selectedCompanionPluginId }),
    }),
    {
      name: 'soha-prefs',
      version: 5,
      migrate: (persistedState, version) => {
        const prev = (persistedState ?? {}) as Partial<PreferencesState> & {
          dualRailStage?: string
        }
        if (version < 5) {
          return {
            ...prev,
            overviewHeroStyle: prev.overviewHeroStyle ?? 'prism',
            overviewGradientPreset: prev.overviewGradientPreset ?? 'workbench',
            overviewGradientStops: prev.overviewGradientStops ?? DEFAULT_OVERVIEW_GRADIENT_STOPS,
            drawerPresentation: prev.drawerPresentation ?? 'split-card',
            dualRailStage: prev.dualRailStage === 'rail-only' ? 'rail-only' : 'expanded',
            cornerRadius: DEFAULT_CORNER_RADIUS,
          }
        }
        return prev as PreferencesState
      },
    },
  ),
)
