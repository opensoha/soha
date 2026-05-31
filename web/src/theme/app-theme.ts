import { theme as antdTheme } from 'antd'
import type { ThemeConfig } from 'antd'

export type AppThemeId = 'soha'

export type ThemeMode = 'light' | 'dark' | 'system'
export type ResolvedThemeMode = 'light' | 'dark'

export interface ThemePalette {
  primaryRgb: string
  primary: string
  primaryHover: string
  primaryActive: string
  primaryLightDefault: string
  primaryLightHover: string
  primaryLightActive: string
  primaryDisabled: string
  colorSuccess: string
  colorWarning: string
  colorDanger: string
  colorInfo: string
  controlOutline: string
  colorBgBase: string
  colorBgLayout: string
  colorBgContainer: string
  colorBgElevated: string
  colorBgMuted: string
  colorBorder: string
  colorBorderSecondary: string
  colorFill: string
  colorFillSecondary: string
  colorFillTertiary: string
  colorFillQuaternary: string
  colorText: string
  colorTextSecondary: string
  colorTextTertiary: string
  colorTextQuaternary: string
  colorCodeBg: string
  colorCodeText: string
  colorPrimaryBg: string
  colorPrimaryBgHover: string
  colorPrimaryBorder: string
  colorPrimaryBorderHover: string
  boxShadow: string
  boxShadowSecondary: string
}

const PREFERENCES_STORAGE_KEY = 'soha-prefs'

export const DEFAULT_APP_THEME_ID: AppThemeId = 'soha'
export const DEFAULT_THEME_MODE: ThemeMode = 'light'

export const appThemeOptions: Array<{ id: AppThemeId; label: string }> = [
  { id: 'soha', label: 'Soha' },
]

export const themeModeOptions: Array<{ value: ThemeMode; label: string }> = [
  { value: 'light', label: '浅色' },
  { value: 'dark', label: '深色' },
  { value: 'system', label: '跟随系统' },
]

const APP_FONT_FAMILY = "'Inter', 'Segoe UI', 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', 'Helvetica Neue', Arial, sans-serif"
const APP_FONT_SIZE = 12
const APP_LINE_HEIGHT = 20 / APP_FONT_SIZE

const THEME_PALETTES: Record<ResolvedThemeMode, ThemePalette> = {
  light: {
    primaryRgb: '17, 24, 39',
    primary: '#111827',
    primaryHover: '#1f2937',
    primaryActive: '#0f172a',
    primaryLightDefault: '#eef3f8',
    primaryLightHover: '#e3ebf4',
    primaryLightActive: '#d7e2ee',
    primaryDisabled: '#cdd8e5',
    colorSuccess: '#22c55e',
    colorWarning: '#f97316',
    colorDanger: '#ef4444',
    colorInfo: '#111827',
    controlOutline: 'rgba(17, 24, 39, 0.12)',
    colorBgBase: '#ffffff',
    colorBgLayout: '#ffffff',
    colorBgContainer: '#ffffff',
    colorBgElevated: '#ffffff',
    colorBgMuted: '#eef3f8',
    colorBorder: '#dbe4ee',
    colorBorderSecondary: '#e8eef5',
    colorFill: '#dfe7f0',
    colorFillSecondary: '#edf3f8',
    colorFillTertiary: '#f4f7fb',
    colorFillQuaternary: '#f8fbfd',
    colorText: '#111827',
    colorTextSecondary: '#4b5563',
    colorTextTertiary: '#6b7280',
    colorTextQuaternary: '#9ca3af',
    colorCodeBg: '#111827',
    colorCodeText: '#fafafa',
    colorPrimaryBg: '#edf3f8',
    colorPrimaryBgHover: '#e2eaf3',
    colorPrimaryBorder: '#d2deea',
    colorPrimaryBorderHover: '#b8c8da',
    boxShadow: '0 1px 3px 0 rgba(0, 0, 0, 0.1), 0 1px 2px -1px rgba(0, 0, 0, 0.1)',
    boxShadowSecondary:
      '0 10px 24px rgba(15, 23, 42, 0.06)',
  },
  dark: {
    primaryRgb: '250, 250, 250',
    primary: '#fafafa',
    primaryHover: '#e4e4e7',
    primaryActive: '#d4d4d8',
    primaryLightDefault: 'rgba(244, 244, 245, 0.12)',
    primaryLightHover: 'rgba(244, 244, 245, 0.18)',
    primaryLightActive: 'rgba(244, 244, 245, 0.24)',
    primaryDisabled: 'rgba(161, 161, 170, 0.35)',
    colorSuccess: '#4ade80',
    colorWarning: '#fb923c',
    colorDanger: '#f87171',
    colorInfo: '#fafafa',
    controlOutline: 'rgba(244, 244, 245, 0.24)',
    colorBgBase: '#09090b',
    colorBgLayout: '#09090b',
    colorBgContainer: '#18181b',
    colorBgElevated: '#18181b',
    colorBgMuted: '#27272a',
    colorBorder: '#27272a',
    colorBorderSecondary: '#3f3f46',
    colorFill: '#3f3f46',
    colorFillSecondary: '#27272a',
    colorFillTertiary: '#1f1f23',
    colorFillQuaternary: '#27272a',
    colorText: '#fafafa',
    colorTextSecondary: '#d4d4d8',
    colorTextTertiary: '#a1a1aa',
    colorTextQuaternary: '#71717a',
    colorCodeBg: '#000000',
    colorCodeText: '#fafafa',
    colorPrimaryBg: 'rgba(244, 244, 245, 0.12)',
    colorPrimaryBgHover: 'rgba(244, 244, 245, 0.18)',
    colorPrimaryBorder: 'rgba(244, 244, 245, 0.24)',
    colorPrimaryBorderHover: 'rgba(244, 244, 245, 0.32)',
    boxShadow: '0 12px 30px rgba(0, 0, 0, 0.45)',
    boxShadowSecondary: '0 8px 20px rgba(0, 0, 0, 0.32)',
  },
}

export function resolveThemeMode(themeMode: ThemeMode): ResolvedThemeMode {
  if (themeMode !== 'system') return themeMode
  if (typeof window === 'undefined') return 'light'
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

export function getThemePalette(themeMode: ThemeMode | ResolvedThemeMode): ThemePalette {
  const resolvedMode = themeMode === 'system' ? resolveThemeMode(themeMode) : themeMode
  return THEME_PALETTES[resolvedMode]
}

export function getAntdTheme(themeMode: ThemeMode | ResolvedThemeMode): ThemeConfig {
  const resolvedMode = themeMode === 'system' ? resolveThemeMode(themeMode) : themeMode
  const palette = getThemePalette(resolvedMode)

  return {
    algorithm: resolvedMode === 'dark' ? antdTheme.darkAlgorithm : antdTheme.defaultAlgorithm,
    token: {
      colorPrimary: palette.primary,
      colorSuccess: palette.colorSuccess,
      colorWarning: palette.colorWarning,
      colorError: palette.colorDanger,
      colorInfo: palette.colorInfo,
      colorLink: palette.primary,
      colorLinkHover: palette.primaryHover,
      colorLinkActive: palette.primaryActive,
      colorTextBase: palette.colorText,
      colorText: palette.colorText,
      colorTextSecondary: palette.colorTextSecondary,
      colorTextTertiary: palette.colorTextTertiary,
      colorTextQuaternary: palette.colorTextQuaternary,
      colorTextDisabled: palette.colorTextQuaternary,
      colorBgBase: palette.colorBgBase,
      colorBgContainer: palette.colorBgContainer,
      colorBgElevated: palette.colorBgElevated,
      colorBgLayout: palette.colorBgLayout,
      colorBgSpotlight: resolvedMode === 'dark' ? 'rgba(24, 24, 27, 0.92)' : 'rgba(38, 38, 38, 0.85)',
      colorBgMask: resolvedMode === 'dark' ? 'rgba(9, 9, 11, 0.72)' : 'rgba(38, 38, 38, 0.45)',
      colorPrimaryBg: palette.colorPrimaryBg,
      colorPrimaryBgHover: palette.colorPrimaryBgHover,
      colorPrimaryBorder: palette.colorPrimaryBorder,
      colorPrimaryBorderHover: palette.colorPrimaryBorderHover,
      colorPrimaryHover: palette.primaryHover,
      colorPrimaryActive: palette.primaryActive,
      colorPrimaryText: palette.primary,
      colorPrimaryTextHover: palette.primaryHover,
      colorPrimaryTextActive: palette.primaryActive,
      colorSuccessBg: resolvedMode === 'dark' ? 'rgba(74, 222, 128, 0.16)' : '#f0fdf4',
      colorSuccessBgHover: resolvedMode === 'dark' ? 'rgba(74, 222, 128, 0.24)' : '#dcfce7',
      colorSuccessBorder: resolvedMode === 'dark' ? 'rgba(74, 222, 128, 0.32)' : '#bbf7d0',
      colorSuccessBorderHover: resolvedMode === 'dark' ? 'rgba(74, 222, 128, 0.42)' : '#86efac',
      colorSuccessHover: resolvedMode === 'dark' ? '#86efac' : '#16a34a',
      colorSuccessActive: resolvedMode === 'dark' ? '#4ade80' : '#15803d',
      colorSuccessText: resolvedMode === 'dark' ? '#86efac' : '#16a34a',
      colorSuccessTextHover: resolvedMode === 'dark' ? '#86efac' : '#16a34a',
      colorSuccessTextActive: resolvedMode === 'dark' ? '#4ade80' : '#15803d',
      colorWarningBg: resolvedMode === 'dark' ? 'rgba(251, 146, 60, 0.16)' : '#fff7ed',
      colorWarningBgHover: resolvedMode === 'dark' ? 'rgba(251, 146, 60, 0.24)' : '#fed7aa',
      colorWarningBorder: resolvedMode === 'dark' ? 'rgba(251, 146, 60, 0.32)' : '#fdba74',
      colorWarningBorderHover: resolvedMode === 'dark' ? 'rgba(251, 146, 60, 0.42)' : '#fb923c',
      colorWarningHover: resolvedMode === 'dark' ? '#fdba74' : '#ea580c',
      colorWarningActive: resolvedMode === 'dark' ? '#fb923c' : '#c2410c',
      colorWarningText: resolvedMode === 'dark' ? '#fdba74' : '#ea580c',
      colorWarningTextHover: resolvedMode === 'dark' ? '#fdba74' : '#ea580c',
      colorWarningTextActive: resolvedMode === 'dark' ? '#fb923c' : '#c2410c',
      colorErrorBg: resolvedMode === 'dark' ? 'rgba(248, 113, 113, 0.16)' : '#fef2f2',
      colorErrorBgHover: resolvedMode === 'dark' ? 'rgba(248, 113, 113, 0.24)' : '#fecaca',
      colorErrorBorder: resolvedMode === 'dark' ? 'rgba(248, 113, 113, 0.32)' : '#fca5a5',
      colorErrorBorderHover: resolvedMode === 'dark' ? 'rgba(248, 113, 113, 0.42)' : '#f87171',
      colorErrorHover: resolvedMode === 'dark' ? '#fca5a5' : '#dc2626',
      colorErrorActive: resolvedMode === 'dark' ? '#f87171' : '#b91c1c',
      colorErrorText: resolvedMode === 'dark' ? '#fca5a5' : '#dc2626',
      colorErrorTextHover: resolvedMode === 'dark' ? '#fca5a5' : '#dc2626',
      colorErrorTextActive: resolvedMode === 'dark' ? '#f87171' : '#b91c1c',
      colorInfoBg: palette.colorPrimaryBg,
      colorInfoBgHover: palette.colorPrimaryBgHover,
      colorInfoBorder: palette.colorPrimaryBorder,
      colorInfoBorderHover: palette.colorPrimaryBorderHover,
      colorInfoHover: palette.primaryHover,
      colorInfoActive: palette.primaryActive,
      colorInfoText: palette.primary,
      colorInfoTextHover: palette.primaryHover,
      colorInfoTextActive: palette.primaryActive,
      colorFill: palette.colorFill,
      colorFillSecondary: palette.colorFillSecondary,
      colorFillTertiary: palette.colorFillTertiary,
      colorFillQuaternary: palette.colorFillQuaternary,
      colorBorder: palette.colorBorder,
      colorBorderSecondary: palette.colorBorderSecondary,
      colorBgTextHover: palette.colorFillQuaternary,
      colorBgTextActive: palette.colorPrimaryBg,
      controlItemBgHover: palette.colorFillQuaternary,
      controlItemBgActive: palette.colorPrimaryBg,
      controlItemBgActiveHover: palette.colorPrimaryBgHover,
      controlOutline: palette.controlOutline,
      fontFamily: APP_FONT_FAMILY,
      fontSize: APP_FONT_SIZE,
      fontSizeSM: 11,
      fontSizeLG: 14,
      fontSizeXL: 16,
      fontSizeHeading1: 30,
      fontSizeHeading2: 24,
      fontSizeHeading3: 20,
      fontSizeHeading4: 16,
      fontSizeHeading5: 14,
      lineHeight: APP_LINE_HEIGHT,
      lineHeightSM: 18 / 11,
      lineHeightLG: 22 / 14,
      lineHeightHeading1: 38 / 30,
      lineHeightHeading2: 32 / 24,
      lineHeightHeading3: 28 / 20,
      lineHeightHeading4: 24 / 16,
      lineHeightHeading5: 22 / 14,
      borderRadius: 10,
      borderRadiusXS: 2,
      borderRadiusSM: 6,
      borderRadiusLG: 14,
      padding: 16,
      paddingSM: 12,
      paddingLG: 24,
      margin: 16,
      marginSM: 12,
      marginLG: 24,
      boxShadow: palette.boxShadow,
      boxShadowSecondary: palette.boxShadowSecondary,
    },
    components: {
      Layout: {
        bodyBg: palette.colorBgLayout,
        headerBg: palette.colorBgContainer,
        headerColor: palette.colorText,
        lightSiderBg: palette.colorBgContainer,
        siderBg: palette.colorBgContainer,
        triggerBg: palette.colorBgContainer,
        triggerColor: palette.colorTextSecondary,
        lightTriggerBg: palette.colorBgContainer,
        lightTriggerColor: palette.colorTextSecondary,
      },
      Menu: {
        itemBg: 'transparent',
        subMenuItemBg: 'transparent',
        popupBg: palette.colorBgElevated,
        itemColor: palette.colorTextSecondary,
        itemHoverColor: palette.colorText,
        itemHoverBg: palette.colorFillQuaternary,
        itemActiveBg: palette.colorFillQuaternary,
        itemSelectedColor: palette.primary,
        itemSelectedBg: palette.colorPrimaryBg,
        subMenuItemSelectedColor: palette.primary,
        groupTitleColor: palette.colorTextTertiary,
        darkItemBg: 'transparent',
        darkSubMenuItemBg: 'transparent',
        darkPopupBg: palette.colorBgElevated,
        darkItemColor: palette.colorTextSecondary,
        darkItemHoverColor: palette.colorText,
        darkItemHoverBg: palette.colorFillQuaternary,
        darkItemSelectedColor: palette.primary,
        darkItemSelectedBg: palette.colorPrimaryBg,
        darkGroupTitleColor: palette.colorTextTertiary,
        itemBorderRadius: 10,
        subMenuItemBorderRadius: 10,
        itemMarginInline: 8,
        itemHeight: 38,
        groupTitleFontSize: APP_FONT_SIZE,
        groupTitleLineHeight: '20px',
      },
      Table: {
        borderColor: palette.colorBorder,
        headerBg: palette.colorBgMuted,
        headerColor: palette.colorTextSecondary,
        headerSplitColor: palette.colorBorderSecondary,
        headerSortActiveBg: palette.colorFillQuaternary,
        headerSortHoverBg: palette.colorFillQuaternary,
        rowHoverBg: palette.colorFillQuaternary,
        rowSelectedBg: palette.colorPrimaryBg,
        rowSelectedHoverBg: palette.colorPrimaryBgHover,
        bodySortBg: palette.colorBgContainer,
        rowExpandedBg: palette.colorBgElevated,
        footerBg: palette.colorBgMuted,
        footerColor: palette.colorTextSecondary,
        fixedHeaderSortActiveBg: palette.colorFillQuaternary,
        stickyScrollBarBg: resolvedMode === 'dark' ? 'rgba(113, 113, 122, 0.32)' : 'rgba(161, 161, 170, 0.24)',
      },
      Card: {
        headerBg: 'transparent',
        actionsBg: palette.colorFillQuaternary,
        extraColor: palette.colorTextSecondary,
        headerPadding: 20,
        bodyPadding: 20,
      },
      Button: {
        primaryShadow: 'none',
        defaultShadow: 'none',
        dangerShadow: 'none',
        defaultBorderColor: palette.colorBorder,
        defaultColor: palette.colorText,
        defaultBg: palette.colorBgContainer,
        defaultHoverBg: palette.colorFillQuaternary,
        defaultHoverBorderColor: palette.colorPrimaryBorder,
        defaultHoverColor: palette.colorText,
        defaultActiveBg: palette.colorFillSecondary,
        defaultActiveBorderColor: palette.colorPrimaryBorder,
        defaultActiveColor: palette.colorText,
        primaryColor: resolvedMode === 'dark' ? '#18181b' : '#ffffff',
        solidTextColor: resolvedMode === 'dark' ? '#18181b' : '#ffffff',
        dangerColor: '#ffffff',
        fontWeight: 500,
        borderRadius: 6,
      },
      Input: {
        activeBg: palette.colorBgContainer,
        hoverBg: palette.colorBgContainer,
        addonBg: palette.colorBgMuted,
        activeShadow: 'none',
        errorActiveShadow: 'none',
        warningActiveShadow: 'none',
        hoverBorderColor: palette.colorPrimaryBorderHover,
        activeBorderColor: palette.primary,
        borderRadius: 6,
      },
      Select: {
        selectorBg: palette.colorBgContainer,
        hoverBorderColor: palette.colorPrimaryBorderHover,
        activeBorderColor: palette.primary,
        activeOutlineColor: palette.controlOutline,
        optionSelectedBg: palette.colorPrimaryBg,
        optionActiveBg: palette.colorFillQuaternary,
        optionSelectedColor: palette.colorText,
        optionSelectedFontWeight: 500,
        multipleItemBg: palette.colorFillQuaternary,
        multipleItemBorderColor: palette.colorBorder,
        borderRadius: 6,
      },
      Alert: {
        borderRadiusLG: 8,
      },
      Modal: {
        borderRadiusLG: 12,
        headerBg: palette.colorBgContainer,
        contentBg: palette.colorBgContainer,
        footerBg: palette.colorBgContainer,
        titleColor: palette.colorText,
      },
      Progress: {
        defaultColor: palette.primary,
        remainingColor: palette.colorFillQuaternary,
        lineBorderRadius: 999,
      },
      Steps: {
        iconSize: 32,
      },
      Switch: {
        trackHeight: 24,
        trackMinWidth: 44,
        innerMinMargin: 4,
        innerMaxMargin: 24,
      },
      Slider: {
        railBg: palette.colorFillQuaternary,
        railHoverBg: palette.colorFillSecondary,
        trackBg: palette.colorFillQuaternary,
        trackHoverBg: palette.colorFillSecondary,
        handleColor: palette.primary,
        handleActiveColor: palette.primaryHover,
        handleActiveOutlineColor: palette.controlOutline,
        handleSize: 18,
        handleSizeHover: 20,
        railSize: 6,
      },
      ColorPicker: {
        borderRadius: 6,
      },
    },
  }
}

export function getAppThemeLabel(): string {
  return 'Soha'
}

export function applyAppTheme(_themeId: AppThemeId, themeMode: ThemeMode) {
  if (typeof document === 'undefined') return
  const resolvedMode = resolveThemeMode(themeMode)
  const palette = getThemePalette(resolvedMode)
  const root = document.documentElement

  root.style.setProperty('--soha-primary-rgb', palette.primaryRgb)
  root.style.setProperty('--soha-primary', palette.primary)
  root.style.setProperty('--soha-primary-hover', palette.primaryHover)
  root.style.setProperty('--soha-primary-active', palette.primaryActive)
  root.style.setProperty('--soha-primary-light-default', palette.primaryLightDefault)
  root.style.setProperty('--soha-primary-light-hover', palette.primaryLightHover)
  root.style.setProperty('--soha-primary-light-active', palette.primaryLightActive)
  root.style.setProperty('--soha-primary-disabled', palette.primaryDisabled)
  root.style.setProperty('--soha-bg-base', palette.colorBgBase)
  root.style.setProperty('--soha-bg-canvas', palette.colorBgLayout)
  root.style.setProperty('--soha-bg-layout', palette.colorBgLayout)
  root.style.setProperty('--soha-bg-surface', palette.colorBgContainer)
  root.style.setProperty('--soha-bg-surface-elevated', palette.colorBgElevated)
  root.style.setProperty('--soha-bg-surface-muted', palette.colorBgMuted)
  root.style.setProperty('--soha-surface', palette.colorBgContainer)
  root.style.setProperty('--soha-surface-elevated', palette.colorBgElevated)
  root.style.setProperty('--soha-surface-muted', palette.colorBgMuted)
  root.style.setProperty('--soha-fill-strong', palette.colorFill)
  root.style.setProperty('--soha-fill-medium', palette.colorFillSecondary)
  root.style.setProperty('--soha-fill-weak', palette.colorFillQuaternary)
  root.style.setProperty('--soha-fill-subtle', palette.colorFillQuaternary)
  root.style.setProperty('--soha-border-color', palette.colorBorder)
  root.style.setProperty('--soha-border-color-strong', palette.colorBorderSecondary)
  root.style.setProperty('--soha-border', palette.colorBorder)
  root.style.setProperty('--soha-border-strong', palette.colorBorderSecondary)
  root.style.setProperty('--soha-text-primary', palette.colorText)
  root.style.setProperty('--soha-text-secondary', palette.colorTextSecondary)
  root.style.setProperty('--soha-text-tertiary', palette.colorTextTertiary)
  root.style.setProperty('--soha-text-quaternary', palette.colorTextQuaternary)
  root.style.setProperty('--soha-text-inverse', '#ffffff')
  root.style.setProperty('--soha-code-bg', palette.colorCodeBg)
  root.style.setProperty('--soha-code-fg', palette.colorCodeText)
  root.style.setProperty('--soha-shadow-soft', palette.boxShadowSecondary)
  root.style.setProperty('--soha-shadow-strong', palette.boxShadow)
  root.style.setProperty('--soha-shadow-panel', palette.boxShadowSecondary)
  root.style.setProperty('--soha-danger', palette.colorDanger)
  root.style.setProperty('--soha-table-header-bg', palette.colorBgMuted)
  root.style.setProperty('--soha-table-header-sort-bg', palette.colorFillSecondary)
  root.style.setProperty('--soha-table-row-hover-bg', palette.colorFillQuaternary)
  root.style.setProperty('--soha-table-row-selected-bg', palette.colorPrimaryBg)
  root.style.setProperty('--soha-table-row-selected-hover-bg', palette.colorPrimaryBgHover)
  root.style.setProperty('--soha-menu-item-hover-bg', palette.colorFillQuaternary)
  root.style.setProperty('--soha-menu-item-selected-bg', palette.colorPrimaryBg)
  root.style.setProperty('color-scheme', resolvedMode)

  document.body.setAttribute('theme-mode', resolvedMode)
  root.dataset.themeMode = resolvedMode
}

export function watchSystemThemeMode(onChange: () => void) {
  if (typeof window === 'undefined') return () => {}
  const media = window.matchMedia('(prefers-color-scheme: dark)')
  const handler = () => onChange()
  if (typeof media.addEventListener === 'function') {
    media.addEventListener('change', handler)
    return () => media.removeEventListener('change', handler)
  }
  media.addListener(handler)
  return () => media.removeListener(handler)
}

export function readStoredThemePreference(): {
  themeId: AppThemeId
  themeMode: ThemeMode
} {
  if (typeof window === 'undefined') {
    return {
      themeId: DEFAULT_APP_THEME_ID,
      themeMode: DEFAULT_THEME_MODE,
    }
  }

  try {
    const raw = window.localStorage.getItem(PREFERENCES_STORAGE_KEY)
    if (!raw) {
      return {
        themeId: DEFAULT_APP_THEME_ID,
        themeMode: DEFAULT_THEME_MODE,
      }
    }

    const parsed = JSON.parse(raw)
    const state = parsed?.state ?? parsed
    const themeMode = ['light', 'dark', 'system'].includes(state?.themeMode)
      ? state.themeMode
      : DEFAULT_THEME_MODE

    return {
      themeId: DEFAULT_APP_THEME_ID,
      themeMode,
    }
  } catch {
    return {
      themeId: DEFAULT_APP_THEME_ID,
      themeMode: DEFAULT_THEME_MODE,
    }
  }
}
