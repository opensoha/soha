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
  terminalBg: string
  terminalFg: string
  terminalCursor: string
  terminalBorder: string
  terminalMuted: string
  terminalRowDivider: string
  terminalOverlayBg: string
  colorPrimaryBg: string
  colorPrimaryBgHover: string
  colorPrimaryBorder: string
  colorPrimaryBorderHover: string
  boxShadow: string
  boxShadowSecondary: string
  accentBlue: string
  accentBlueRgb: string
  accentCyan: string
  accentCyanRgb: string
  accentViolet: string
  accentTeal: string
  accentTealRgb: string
  gradientPrimary: string
  gradientSubtle: string
  gradientPanel: string
  glassBg: string
  glassBgStrong: string
  glassBorder: string
  glassShadow: string
  workflowCanvasBg: string
  workflowCanvasGrid: string
  workflowCanvasGlow: string
  workflowNodeBg: string
  workflowNodeBorder: string
  workflowNodeSelectedBorder: string
  workflowNodeSelectedRing: string
  workflowEdgeDefault: string
  workflowEdgeSuccess: string
  workflowEdgeFailure: string
  workflowInspectorBg: string
  graphScope: string
  graphService: string
  graphSpan: string
  graphLog: string
  graphMetric: string
  graphHypothesis: string
  graphMuted: string
  graphRecommendation: string
  dataPanelBg: string
  dataPanelBorder: string
  dataRowHoverBg: string
  dataRowSelectedBg: string
  listItemActiveBg: string
  listItemActiveBorder: string
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

const APP_FONT_FAMILY =
  "'Inter', 'Segoe UI', 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', 'Helvetica Neue', Arial, sans-serif"
const APP_FONT_SIZE = 14
const APP_LINE_HEIGHT_PX = 22
const APP_LINE_HEIGHT = APP_LINE_HEIGHT_PX / APP_FONT_SIZE
const APP_SUPPORTING_FONT_SIZE = 12
const APP_SUPPORTING_LINE_HEIGHT_PX = 18
const APP_CONTROL_FONT_SIZE = 13
const APP_SECTION_FONT_SIZE = 16
const APP_SECTION_LINE_HEIGHT_PX = 24
const APP_METRIC_FONT_SIZE = 34
const APP_SUMMARY_FONT_SIZE = 22
const APP_CARD_PADDING = 20
// Shared by the overview canvas and its CSS slot; not a global chart size.
export const OVERVIEW_COMPACT_CHART_SIZE = 64
const APP_CONTROL_HEIGHT = 32
const APP_CONTROL_RADIUS = 6
const APP_PANEL_RADIUS = 8
const APP_LARGE_RADIUS = 14
// The app sidebar is compact; other menus retain the component's standard height.
const APP_SIDEBAR_ITEM_HEIGHT = 31
const GLASS_BLUR = '16px'

const THEME_PALETTES: Record<ResolvedThemeMode, ThemePalette> = {
  light: {
    primaryRgb: '22, 119, 255',
    primary: '#1677ff',
    primaryHover: '#4096ff',
    primaryActive: '#0958d9',
    primaryLightDefault: '#e6f4ff',
    primaryLightHover: '#bae0ff',
    primaryLightActive: '#91caff',
    primaryDisabled: '#adc6ff',
    colorSuccess: '#22c55e',
    colorWarning: '#f97316',
    colorDanger: '#ef4444',
    colorInfo: '#0891b2',
    controlOutline: 'rgba(22, 119, 255, 0.16)',
    colorBgBase: '#ffffff',
    colorBgLayout: '#f6f8fb',
    colorBgContainer: '#ffffff',
    colorBgElevated: 'rgba(255, 255, 255, 0.94)',
    colorBgMuted: '#f9f9fb',
    colorBorder: '#e5e7eb',
    colorBorderSecondary: '#f0f2f5',
    colorFill: '#d1d5db',
    colorFillSecondary: '#e5e7eb',
    colorFillTertiary: '#f3f4f6',
    colorFillQuaternary: '#f9f9fb',
    colorText: '#111827',
    colorTextSecondary: '#4b5563',
    colorTextTertiary: '#6b7280',
    colorTextQuaternary: '#9ca3af',
    colorCodeBg: '#111827',
    colorCodeText: '#fafafa',
    terminalBg: '#0b1220',
    terminalFg: '#e5edf5',
    terminalCursor: '#4cbbff',
    terminalBorder: 'rgba(22, 119, 255, 0.18)',
    terminalMuted: 'rgba(148, 163, 184, 0.88)',
    terminalRowDivider: 'rgba(148, 163, 184, 0.08)',
    terminalOverlayBg: 'rgba(11, 18, 32, 0.92)',
    colorPrimaryBg: '#e6f4ff',
    colorPrimaryBgHover: '#bae0ff',
    colorPrimaryBorder: '#91caff',
    colorPrimaryBorderHover: '#69b1ff',
    boxShadow: '0 1px 3px 0 rgba(15, 23, 42, 0.08), 0 1px 2px -1px rgba(15, 23, 42, 0.08)',
    boxShadowSecondary: '0 14px 34px rgba(22, 119, 255, 0.08)',
    accentBlue: '#1677ff',
    accentBlueRgb: '22, 119, 255',
    accentCyan: '#13c2c2',
    accentCyanRgb: '19, 194, 194',
    accentViolet: '#7c3aed',
    accentTeal: '#52c41a',
    accentTealRgb: '82, 196, 26',
    gradientPrimary: 'linear-gradient(135deg, #1677ff 0%, #13c2c2 56%, #52c41a 100%)',
    gradientSubtle:
      'linear-gradient(135deg, rgba(22, 119, 255, 0.10) 0%, rgba(19, 194, 194, 0.08) 54%, rgba(82, 196, 26, 0.06) 100%)',
    gradientPanel:
      'linear-gradient(180deg, rgba(255, 255, 255, 0.98) 0%, rgba(249, 249, 251, 0.94) 100%)',
    glassBg: 'rgba(255, 255, 255, 0.72)',
    glassBgStrong: 'rgba(255, 255, 255, 0.88)',
    glassBorder: 'rgba(22, 119, 255, 0.24)',
    glassShadow: '0 18px 48px rgba(22, 119, 255, 0.10)',
    workflowCanvasBg:
      'linear-gradient(180deg, rgba(249, 249, 251, 0.98) 0%, rgba(230, 244, 255, 0.72) 100%)',
    workflowCanvasGrid: 'rgba(22, 119, 255, 0.14)',
    workflowCanvasGlow:
      'radial-gradient(circle at top left, rgba(19, 194, 194, 0.16), transparent 36%)',
    workflowNodeBg: 'rgba(255, 255, 255, 0.90)',
    workflowNodeBorder: 'rgba(22, 119, 255, 0.28)',
    workflowNodeSelectedBorder: '#1677ff',
    workflowNodeSelectedRing: 'rgba(22, 119, 255, 0.18)',
    workflowEdgeDefault: '#94a3b8',
    workflowEdgeSuccess: '#52c41a',
    workflowEdgeFailure: '#ef4444',
    workflowInspectorBg: 'rgba(255, 255, 255, 0.88)',
    graphScope: '#1677ff',
    graphService: '#13c2c2',
    graphSpan: '#7c3aed',
    graphLog: '#b45309',
    graphMetric: '#0891b2',
    graphHypothesis: '#dc2626',
    graphMuted: '#64748b',
    graphRecommendation: '#0f766e',
    dataPanelBg: 'rgba(255, 255, 255, 0.88)',
    dataPanelBorder: '#e5e7eb',
    dataRowHoverBg: '#f9f9fb',
    dataRowSelectedBg: '#e6f4ff',
    listItemActiveBg: 'linear-gradient(135deg, rgba(22, 119, 255, 0.12), rgba(19, 194, 194, 0.10))',
    listItemActiveBorder: 'rgba(22, 119, 255, 0.42)',
  },
  dark: {
    primaryRgb: '64, 150, 255',
    primary: '#4096ff',
    primaryHover: '#69b1ff',
    primaryActive: '#1677ff',
    primaryLightDefault: 'rgba(64, 150, 255, 0.16)',
    primaryLightHover: 'rgba(64, 150, 255, 0.24)',
    primaryLightActive: 'rgba(64, 150, 255, 0.30)',
    primaryDisabled: 'rgba(64, 150, 255, 0.34)',
    colorSuccess: '#4ade80',
    colorWarning: '#fb923c',
    colorDanger: '#f87171',
    colorInfo: '#69b1ff',
    controlOutline: 'rgba(64, 150, 255, 0.28)',
    colorBgBase: '#07111f',
    colorBgLayout: '#07111f',
    colorBgContainer: '#0d1726',
    colorBgElevated: '#101c2d',
    colorBgMuted: '#122238',
    colorBorder: '#1e334d',
    colorBorderSecondary: '#28415f',
    colorFill: '#2c4665',
    colorFillSecondary: '#172b44',
    colorFillTertiary: '#102035',
    colorFillQuaternary: '#0f1b2b',
    colorText: '#fafafa',
    colorTextSecondary: '#d4d4d8',
    colorTextTertiary: '#a1a1aa',
    colorTextQuaternary: '#71717a',
    colorCodeBg: '#000000',
    colorCodeText: '#fafafa',
    terminalBg: '#020817',
    terminalFg: '#e5edf5',
    terminalCursor: '#7dd3fc',
    terminalBorder: 'rgba(64, 150, 255, 0.22)',
    terminalMuted: 'rgba(148, 163, 184, 0.88)',
    terminalRowDivider: 'rgba(148, 163, 184, 0.10)',
    terminalOverlayBg: 'rgba(2, 8, 23, 0.92)',
    colorPrimaryBg: 'rgba(64, 150, 255, 0.16)',
    colorPrimaryBgHover: 'rgba(64, 150, 255, 0.24)',
    colorPrimaryBorder: 'rgba(64, 150, 255, 0.32)',
    colorPrimaryBorderHover: 'rgba(105, 177, 255, 0.44)',
    boxShadow: '0 12px 30px rgba(0, 0, 0, 0.45)',
    boxShadowSecondary: '0 18px 42px rgba(0, 0, 0, 0.34)',
    accentBlue: '#4096ff',
    accentBlueRgb: '64, 150, 255',
    accentCyan: '#36cfc9',
    accentCyanRgb: '54, 207, 201',
    accentViolet: '#a78bfa',
    accentTeal: '#73d13d',
    accentTealRgb: '115, 209, 61',
    gradientPrimary: 'linear-gradient(135deg, #4096ff 0%, #36cfc9 56%, #73d13d 100%)',
    gradientSubtle:
      'linear-gradient(135deg, rgba(64, 150, 255, 0.18) 0%, rgba(54, 207, 201, 0.14) 54%, rgba(115, 209, 61, 0.12) 100%)',
    gradientPanel:
      'linear-gradient(180deg, rgba(16, 28, 45, 0.92) 0%, rgba(13, 23, 38, 0.88) 100%)',
    glassBg: 'rgba(13, 23, 38, 0.72)',
    glassBgStrong: 'rgba(16, 28, 45, 0.88)',
    glassBorder: 'rgba(64, 150, 255, 0.24)',
    glassShadow: '0 20px 56px rgba(0, 0, 0, 0.36)',
    workflowCanvasBg:
      'linear-gradient(180deg, rgba(7, 17, 31, 0.98) 0%, rgba(13, 23, 38, 0.94) 100%)',
    workflowCanvasGrid: 'rgba(64, 150, 255, 0.18)',
    workflowCanvasGlow:
      'radial-gradient(circle at top left, rgba(54, 207, 201, 0.16), transparent 38%)',
    workflowNodeBg: 'rgba(16, 28, 45, 0.90)',
    workflowNodeBorder: 'rgba(64, 150, 255, 0.28)',
    workflowNodeSelectedBorder: '#4096ff',
    workflowNodeSelectedRing: 'rgba(64, 150, 255, 0.24)',
    workflowEdgeDefault: '#64748b',
    workflowEdgeSuccess: '#73d13d',
    workflowEdgeFailure: '#f87171',
    workflowInspectorBg: 'rgba(16, 28, 45, 0.88)',
    graphScope: '#4096ff',
    graphService: '#36cfc9',
    graphSpan: '#a78bfa',
    graphLog: '#fbbf24',
    graphMetric: '#22d3ee',
    graphHypothesis: '#f87171',
    graphMuted: '#94a3b8',
    graphRecommendation: '#5eead4',
    dataPanelBg: 'rgba(16, 28, 45, 0.88)',
    dataPanelBorder: '#1e334d',
    dataRowHoverBg: '#0f1b2b',
    dataRowSelectedBg: 'rgba(64, 150, 255, 0.16)',
    listItemActiveBg: 'linear-gradient(135deg, rgba(64, 150, 255, 0.18), rgba(54, 207, 201, 0.14))',
    listItemActiveBorder: 'rgba(64, 150, 255, 0.42)',
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

export function readThemeCssVariable(name: string, fallback: string): string {
  if (typeof window === 'undefined') return fallback
  const value = window.getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  return value || fallback
}

const THEME_CSS_VAR_COLOR_PATTERN = /^var\((--[^),]+)(?:,\s*(.+))?\)$/

export function resolveThemeColorReference(value: string, fallback?: string): string {
  const match = value.match(THEME_CSS_VAR_COLOR_PATTERN)
  if (!match) return value
  const [, variableName, inlineFallback] = match
  const resolved = readThemeCssVariable(variableName, inlineFallback?.trim() || fallback || value)
  if (THEME_CSS_VAR_COLOR_PATTERN.test(resolved) && resolved !== value) {
    return resolveThemeColorReference(resolved, fallback)
  }
  return resolved
}

export function readTerminalThemeColors() {
  const mode =
    typeof document !== 'undefined' && document.documentElement.dataset.themeMode === 'dark'
      ? 'dark'
      : 'light'
  const palette = getThemePalette(mode)

  return {
    background: readThemeCssVariable('--soha-terminal-bg', palette.terminalBg),
    foreground: readThemeCssVariable('--soha-terminal-fg', palette.terminalFg),
    cursor: readThemeCssVariable('--soha-terminal-cursor', palette.terminalCursor),
  }
}

export function getAntdTheme(themeMode: ThemeMode | ResolvedThemeMode): ThemeConfig {
  const resolvedMode = themeMode === 'system' ? resolveThemeMode(themeMode) : themeMode
  const palette = getThemePalette(resolvedMode)

  return {
    algorithm: resolvedMode === 'dark' ? antdTheme.darkAlgorithm : antdTheme.defaultAlgorithm,
    token: {
      motion: !prefersReducedMotion(),
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
      colorBgSpotlight:
        resolvedMode === 'dark' ? 'rgba(13, 23, 38, 0.94)' : 'rgba(15, 23, 42, 0.86)',
      colorBgMask: resolvedMode === 'dark' ? 'rgba(7, 17, 31, 0.72)' : 'rgba(15, 23, 42, 0.42)',
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
      controlHeight: APP_CONTROL_HEIGHT,
      fontFamily: APP_FONT_FAMILY,
      fontSize: APP_FONT_SIZE,
      fontSizeSM: APP_SUPPORTING_FONT_SIZE,
      fontSizeLG: APP_SECTION_FONT_SIZE,
      fontSizeXL: 20,
      fontSizeHeading1: 30,
      fontSizeHeading2: 24,
      fontSizeHeading3: 20,
      fontSizeHeading4: 16,
      fontSizeHeading5: 14,
      lineHeight: APP_LINE_HEIGHT,
      lineHeightSM: APP_SUPPORTING_LINE_HEIGHT_PX / APP_SUPPORTING_FONT_SIZE,
      lineHeightLG: APP_SECTION_LINE_HEIGHT_PX / APP_SECTION_FONT_SIZE,
      lineHeightHeading1: 38 / 30,
      lineHeightHeading2: 32 / 24,
      lineHeightHeading3: 28 / 20,
      lineHeightHeading4: 24 / 16,
      lineHeightHeading5: 22 / 14,
      borderRadius: 10,
      borderRadiusXS: 2,
      borderRadiusSM: APP_CONTROL_RADIUS,
      borderRadiusLG: APP_LARGE_RADIUS,
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
      // Tag uses semantic base colors as text; scope readable foregrounds to this component.
      Tag:
        resolvedMode === 'light'
          ? {
              colorSuccess: '#15803d',
              colorWarning: '#c2410c',
              green7: '#237804',
              gold7: '#874d00',
            }
          : {},
      Tooltip: {
        colorBgSpotlight: palette.colorBgElevated,
        colorTextLightSolid: palette.colorText,
        fontSize: APP_SUPPORTING_FONT_SIZE,
        lineHeight: APP_SUPPORTING_LINE_HEIGHT_PX / APP_SUPPORTING_FONT_SIZE,
        controlHeight: 28,
        paddingSM: 8,
        paddingXS: 8,
      },
      Breadcrumb: {
        fontSize: APP_SUPPORTING_FONT_SIZE,
        lineHeight: 20 / APP_SUPPORTING_FONT_SIZE,
      },
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
        fontSize: APP_SUPPORTING_FONT_SIZE,
        lineHeight: 20 / APP_SUPPORTING_FONT_SIZE,
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
        groupTitleFontSize: APP_SUPPORTING_FONT_SIZE,
        groupTitleLineHeight: '20px',
      },
      Table: {
        lineHeight: 20 / APP_SUPPORTING_FONT_SIZE,
        cellFontSize: APP_FONT_SIZE,
        cellFontSizeMD: APP_FONT_SIZE,
        cellFontSizeSM: APP_SUPPORTING_FONT_SIZE,
        borderColor: palette.colorBorder,
        headerBg: palette.colorBgMuted,
        headerColor: palette.colorText,
        headerSplitColor: palette.colorBorder,
        cellPaddingBlockSM: 8,
        cellPaddingBlockMD: 12,
        cellPaddingBlock: 12,
        cellPaddingInlineSM: 12,
        cellPaddingInlineMD: 12,
        cellPaddingInline: 12,
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
        stickyScrollBarBg:
          resolvedMode === 'dark' ? 'rgba(113, 113, 122, 0.32)' : 'rgba(161, 161, 170, 0.24)',
      },
      Card: {
        headerFontSize: APP_SECTION_FONT_SIZE,
        headerFontSizeSM: APP_FONT_SIZE,
        headerBg: 'transparent',
        actionsBg: palette.colorFillQuaternary,
        extraColor: palette.colorTextSecondary,
        headerPadding: APP_CARD_PADDING,
        bodyPadding: APP_CARD_PADDING,
      },
      Button: {
        contentFontSize: APP_CONTROL_FONT_SIZE,
        contentFontSizeSM: APP_SUPPORTING_FONT_SIZE,
        primaryShadow: '0 1px 2px rgba(var(--soha-primary-rgb), 0.12)',
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
        primaryColor: '#ffffff',
        solidTextColor: '#ffffff',
        dangerColor: '#ffffff',
        fontWeight: 500,
        borderRadius: APP_CONTROL_RADIUS,
      },
      Input: {
        inputFontSize: APP_CONTROL_FONT_SIZE,
        inputFontSizeSM: APP_SUPPORTING_FONT_SIZE,
        activeBg: palette.colorBgContainer,
        hoverBg: palette.colorBgContainer,
        addonBg: palette.colorBgMuted,
        activeShadow: 'none',
        errorActiveShadow: 'none',
        warningActiveShadow: 'none',
        hoverBorderColor: palette.colorPrimaryBorderHover,
        activeBorderColor: palette.primary,
        borderRadius: APP_CONTROL_RADIUS,
      },
      InputNumber: {
        inputFontSize: APP_CONTROL_FONT_SIZE,
        inputFontSizeSM: APP_SUPPORTING_FONT_SIZE,
      },
      DatePicker: {
        inputFontSize: APP_CONTROL_FONT_SIZE,
        inputFontSizeSM: APP_SUPPORTING_FONT_SIZE,
      },
      Select: {
        fontSize: APP_CONTROL_FONT_SIZE,
        lineHeight: APP_LINE_HEIGHT_PX / APP_CONTROL_FONT_SIZE,
        optionFontSize: APP_CONTROL_FONT_SIZE,
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
        borderRadius: APP_CONTROL_RADIUS,
      },
      Form: {
        labelFontSize: APP_CONTROL_FONT_SIZE,
        itemMarginBottom: 20,
      },
      Tabs: {
        titleFontSize: APP_FONT_SIZE,
        titleFontSizeSM: APP_CONTROL_FONT_SIZE,
        titleFontSizeLG: APP_SECTION_FONT_SIZE,
        horizontalItemGutter: 24,
      },
      Alert: {
        borderRadiusLG: APP_PANEL_RADIUS,
      },
      Drawer: {
        colorBgElevated: palette.colorBgContainer,
      },
      Modal: {
        titleFontSize: APP_SECTION_FONT_SIZE,
        titleLineHeight: APP_SECTION_LINE_HEIGHT_PX / APP_SECTION_FONT_SIZE,
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
        handleSize: 20,
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
        borderRadius: APP_CONTROL_RADIUS,
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
  const motion = antdTheme.getDesignToken(getAntdTheme(resolvedMode))
  root.style.setProperty(
    '--soha-motion-duration-fast',
    motion.motion ? motion.motionDurationFast : '0s',
  )
  root.style.setProperty(
    '--soha-motion-duration-mid',
    motion.motion ? motion.motionDurationMid : '0s',
  )
  root.style.setProperty(
    '--soha-motion-duration-slow',
    motion.motion ? motion.motionDurationSlow : '0s',
  )
  root.style.setProperty('--soha-motion-ease', motion.motionEaseInOut)
  root.style.setProperty('--soha-motion-ease-out', motion.motionEaseOut)
  root.style.setProperty('--soha-motion-activity', 'calc(var(--soha-motion-duration-mid) * 9)')

  root.style.setProperty('--soha-primary-rgb', palette.primaryRgb)
  root.style.setProperty('--soha-primary', palette.primary)
  root.style.setProperty('--soha-primary-hover', palette.primaryHover)
  root.style.setProperty('--soha-primary-active', palette.primaryActive)
  root.style.setProperty('--soha-primary-light-default', palette.primaryLightDefault)
  root.style.setProperty('--soha-primary-light-hover', palette.primaryLightHover)
  root.style.setProperty('--soha-primary-light-active', palette.primaryLightActive)
  root.style.setProperty('--soha-primary-disabled', palette.primaryDisabled)
  root.style.setProperty('--soha-accent-blue', palette.accentBlue)
  root.style.setProperty('--soha-accent-blue-rgb', palette.accentBlueRgb)
  root.style.setProperty('--soha-accent-cyan', palette.accentCyan)
  root.style.setProperty('--soha-accent-cyan-rgb', palette.accentCyanRgb)
  root.style.setProperty('--soha-accent-violet', palette.accentViolet)
  root.style.setProperty('--soha-accent-teal', palette.accentTeal)
  root.style.setProperty('--soha-accent-teal-rgb', palette.accentTealRgb)
  root.style.setProperty('--soha-gradient-primary', palette.gradientPrimary)
  root.style.setProperty('--soha-gradient-subtle', palette.gradientSubtle)
  root.style.setProperty('--soha-gradient-panel', palette.gradientPanel)
  root.style.setProperty('--soha-glass-bg', palette.glassBg)
  root.style.setProperty('--soha-glass-bg-strong', palette.glassBgStrong)
  root.style.setProperty('--soha-glass-border', palette.glassBorder)
  root.style.setProperty('--soha-glass-shadow', palette.glassShadow)
  root.style.setProperty('--soha-glass-blur', GLASS_BLUR)
  root.style.setProperty('--soha-bg-base', palette.colorBgBase)
  root.style.setProperty('--soha-bg-canvas', palette.colorBgLayout)
  root.style.setProperty('--soha-bg-layout', palette.colorBgLayout)
  root.style.setProperty('--soha-bg-surface', palette.colorBgContainer)
  root.style.setProperty('--soha-bg-surface-elevated', palette.colorBgElevated)
  root.style.setProperty('--soha-bg-surface-muted', palette.colorBgMuted)
  root.style.setProperty('--soha-fill-strong', palette.colorFill)
  root.style.setProperty('--soha-fill-medium', palette.colorFillSecondary)
  root.style.setProperty('--soha-fill-weak', palette.colorFillQuaternary)
  root.style.setProperty('--soha-fill-subtle', palette.colorFillQuaternary)
  root.style.setProperty('--soha-border-color', palette.colorBorder)
  root.style.setProperty('--soha-border-color-strong', palette.colorBorderSecondary)
  root.style.setProperty('--soha-text-primary', palette.colorText)
  root.style.setProperty('--soha-text-secondary', palette.colorTextSecondary)
  root.style.setProperty('--soha-text-tertiary', palette.colorTextTertiary)
  root.style.setProperty('--soha-text-quaternary', palette.colorTextQuaternary)
  root.style.setProperty('--soha-text-inverse', '#ffffff')
  root.style.setProperty('--soha-code-bg', palette.colorCodeBg)
  root.style.setProperty('--soha-code-fg', palette.colorCodeText)
  root.style.setProperty('--soha-terminal-bg', palette.terminalBg)
  root.style.setProperty('--soha-terminal-fg', palette.terminalFg)
  root.style.setProperty('--soha-terminal-cursor', palette.terminalCursor)
  root.style.setProperty('--soha-terminal-border', palette.terminalBorder)
  root.style.setProperty('--soha-terminal-muted', palette.terminalMuted)
  root.style.setProperty('--soha-terminal-row-divider', palette.terminalRowDivider)
  root.style.setProperty('--soha-terminal-overlay-bg', palette.terminalOverlayBg)
  root.style.setProperty('--soha-shadow-soft', palette.boxShadowSecondary)
  root.style.setProperty('--soha-shadow-strong', palette.boxShadow)
  root.style.setProperty('--soha-shadow-panel', palette.boxShadowSecondary)
  root.style.setProperty('--soha-danger', palette.colorDanger)
  root.style.setProperty('--soha-success', palette.colorSuccess)
  root.style.setProperty('--soha-warning', palette.colorWarning)
  root.style.setProperty('--soha-info', palette.colorInfo)
  // Quantitative resource bars use yellow between request and limit, with readable labels.
  root.style.setProperty('--soha-resource-success', resolvedMode === 'dark' ? '#73d13d' : '#237804')
  root.style.setProperty('--soha-resource-warning', resolvedMode === 'dark' ? '#d4b106' : '#fadb14')
  root.style.setProperty('--soha-resource-danger', resolvedMode === 'dark' ? '#ff7875' : '#cf1322')
  root.style.setProperty('--soha-resource-on-fill', resolvedMode === 'dark' ? '#101c2d' : '#ffffff')
  root.style.setProperty('--soha-resource-on-warning', '#101c2d')
  root.style.setProperty(
    '--soha-resource-request-on-fill',
    resolvedMode === 'dark' ? '#237804' : '#b7eb8f',
  )
  root.style.setProperty('--soha-resource-request-on-warning', '#237804')
  root.style.setProperty(
    '--soha-resource-limit-on-fill',
    resolvedMode === 'dark' ? '#a8071a' : '#ffccc7',
  )
  root.style.setProperty('--soha-table-header-bg', palette.colorBgMuted)
  root.style.setProperty('--soha-table-header-sort-bg', palette.colorFillSecondary)
  root.style.setProperty('--soha-table-row-hover-bg', palette.colorFillQuaternary)
  root.style.setProperty('--soha-table-row-selected-bg', palette.colorPrimaryBg)
  root.style.setProperty('--soha-table-row-selected-hover-bg', palette.colorPrimaryBgHover)
  root.style.setProperty('--soha-table-link-color', palette.primary)
  root.style.setProperty('--soha-table-link-hover-color', palette.primaryHover)
  root.style.setProperty('--soha-menu-item-hover-bg', palette.colorFillQuaternary)
  root.style.setProperty('--soha-menu-item-selected-bg', palette.colorPrimaryBg)
  root.style.setProperty('--soha-menu-item-selected-color', palette.primary)
  root.style.setProperty('--soha-overview-card-bg', palette.colorBgContainer)
  root.style.setProperty('--soha-overview-card-success-bg', palette.colorBgContainer)
  root.style.setProperty('--soha-overview-card-warning-bg', palette.colorBgContainer)
  root.style.setProperty('--soha-overview-card-danger-bg', palette.colorBgContainer)
  root.style.setProperty('--soha-overview-icon-bg', palette.colorBgContainer)
  root.style.setProperty('--soha-overview-brand-border', palette.colorBorder)
  root.style.setProperty('--soha-workflow-canvas-bg', palette.workflowCanvasBg)
  root.style.setProperty('--soha-workflow-canvas-grid', palette.workflowCanvasGrid)
  root.style.setProperty('--soha-workflow-canvas-glow', palette.workflowCanvasGlow)
  root.style.setProperty('--soha-workflow-node-bg', palette.workflowNodeBg)
  root.style.setProperty('--soha-workflow-node-border', palette.workflowNodeBorder)
  root.style.setProperty('--soha-workflow-node-selected-border', palette.workflowNodeSelectedBorder)
  root.style.setProperty('--soha-workflow-node-selected-ring', palette.workflowNodeSelectedRing)
  root.style.setProperty('--soha-workflow-edge-default', palette.workflowEdgeDefault)
  root.style.setProperty('--soha-workflow-edge-success', palette.workflowEdgeSuccess)
  root.style.setProperty('--soha-workflow-edge-failure', palette.workflowEdgeFailure)
  root.style.setProperty('--soha-workflow-inspector-bg', palette.workflowInspectorBg)
  root.style.setProperty('--soha-graph-scope', palette.graphScope)
  root.style.setProperty('--soha-graph-service', palette.graphService)
  root.style.setProperty('--soha-graph-span', palette.graphSpan)
  root.style.setProperty('--soha-graph-log', palette.graphLog)
  root.style.setProperty('--soha-graph-metric', palette.graphMetric)
  root.style.setProperty('--soha-graph-hypothesis', palette.graphHypothesis)
  root.style.setProperty('--soha-graph-muted', palette.graphMuted)
  root.style.setProperty('--soha-graph-recommendation', palette.graphRecommendation)
  root.style.setProperty('--soha-data-panel-bg', palette.dataPanelBg)
  root.style.setProperty('--soha-data-panel-border', palette.dataPanelBorder)
  root.style.setProperty('--soha-data-row-hover-bg', palette.dataRowHoverBg)
  root.style.setProperty('--soha-data-row-selected-bg', palette.dataRowSelectedBg)
  root.style.setProperty('--soha-list-item-active-bg', palette.listItemActiveBg)
  root.style.setProperty('--soha-list-item-active-border', palette.listItemActiveBorder)
  root.style.setProperty('--soha-font-family', APP_FONT_FAMILY)
  root.style.setProperty('--soha-font-size', `${APP_FONT_SIZE}px`)
  root.style.setProperty('--soha-line-height', `${APP_LINE_HEIGHT_PX}px`)
  root.style.setProperty('--soha-font-size-supporting', `${APP_SUPPORTING_FONT_SIZE}px`)
  root.style.setProperty('--soha-font-size-control', `${APP_CONTROL_FONT_SIZE}px`)
  root.style.setProperty('--soha-font-size-section', `${APP_SECTION_FONT_SIZE}px`)
  root.style.setProperty('--soha-line-height-supporting', `${APP_SUPPORTING_LINE_HEIGHT_PX}px`)
  root.style.setProperty('--soha-line-height-section', `${APP_SECTION_LINE_HEIGHT_PX}px`)
  root.style.setProperty('--soha-font-size-metric', `${APP_METRIC_FONT_SIZE}px`)
  root.style.setProperty('--soha-font-size-summary', `${APP_SUMMARY_FONT_SIZE}px`)
  root.style.setProperty('--soha-card-padding', `${APP_CARD_PADDING}px`)
  root.style.setProperty('--soha-overview-chart-size', `${OVERVIEW_COMPACT_CHART_SIZE}px`)
  root.style.setProperty('--soha-control-height', `${APP_CONTROL_HEIGHT}px`)
  root.style.setProperty('--soha-sidebar-item-height', `${APP_SIDEBAR_ITEM_HEIGHT}px`)
  root.style.setProperty('--soha-radius', `${APP_CONTROL_RADIUS}px`)
  root.style.setProperty('--soha-radius-control', `${APP_CONTROL_RADIUS}px`)
  root.style.setProperty('--soha-radius-panel', `${APP_PANEL_RADIUS}px`)
  root.style.setProperty('--soha-radius-lg', `${APP_LARGE_RADIUS}px`)
  root.style.setProperty('--soha-management-query-field-default-width', '300px')
  root.style.setProperty('--soha-management-toolbar-search-width', '300px')
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

function prefersReducedMotion() {
  return (
    typeof window !== 'undefined' &&
    typeof window.matchMedia === 'function' &&
    window.matchMedia('(prefers-reduced-motion: reduce)').matches
  )
}

export function watchSystemMotion(onChange: () => void) {
  if (typeof window === 'undefined') return () => {}
  const media = window.matchMedia('(prefers-reduced-motion: reduce)')
  media.addEventListener('change', onChange)
  return () => media.removeEventListener('change', onChange)
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
