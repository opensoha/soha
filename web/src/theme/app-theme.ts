import { theme as antdTheme } from 'antd'
import type { ThemeConfig } from 'antd'

export type AppThemeId = 'soha'

export type ThemeMode = 'light' | 'dark' | 'system'
export type ResolvedThemeMode = 'light' | 'dark'
export type LightCanvasPreset = 'apple' | 'google' | 'anthropic' | 'mist'

export interface AccentPresetOption {
  id: string
  labelZh: string
  labelEn: string
  descZh: string
  lightHex: string
  darkHex: string
}

export interface CanvasPresetOption {
  id: string
  labelZh: string
  labelEn: string
  hex: string
}

export const ACCENT_PRESETS: AccentPresetOption[] = [
  {
    id: 'indigo',
    labelZh: '星际靛蓝',
    labelEn: 'Astral Indigo',
    descZh: '官方推荐 · K8s 云原生蓝与 AI 编排感，与健康绿零冲突',
    lightHex: '#364fc7',
    darkHex: '#6366f1',
  },
  {
    id: 'obsidian',
    labelZh: '曜石黑钛',
    labelEn: 'Obsidian Pro',
    descZh: 'Apple Pro / Vercel 极简金属黑，零色相干扰',
    lightHex: '#1d1d1f',
    darkHex: '#38bdf8',
  },
  {
    id: 'sapphire',
    labelZh: '峡湾深蓝',
    labelEn: 'Fjord Sapphire',
    descZh: 'Tailscale / Cloudflare 硬核基础设施深海蓝',
    lightHex: '#0f5288',
    darkHex: '#38bdf8',
  },
  {
    id: 'violet',
    labelZh: '极光紫罗兰',
    labelEn: 'Copilot Violet',
    descZh: '突出 Heya AI Agent 原生智能辨识度',
    lightHex: '#5b3fd6',
    darkHex: '#a78bfa',
  },
  {
    id: 'teal',
    labelZh: '翡翠青松',
    labelEn: 'Botanical Teal',
    descZh: '温润青松自然科技感',
    lightHex: '#0f6a5c',
    darkHex: '#2dd4bf',
  },
]

export const LIGHT_CANVAS_PRESETS: CanvasPresetOption[] = [
  { id: 'apple', labelZh: 'Apple 钛银', labelEn: 'Apple Mist', hex: '#f5f5f7' },
  { id: 'google', labelZh: 'Google 冰雾', labelEn: 'Cloud Ice', hex: '#f0f4f9' },
  { id: 'anthropic', labelZh: 'Anthropic 暖石', labelEn: 'Warm Stone', hex: '#f5f4ef' },
  { id: 'mist', labelZh: '晨青雾白', labelEn: 'Botanical Mist', hex: '#eef4f2' },
]

export const DARK_CANVAS_PRESETS: CanvasPresetOption[] = [
  { id: 'titanium', labelZh: '曜石冷钛', labelEn: 'Carbon Titanium', hex: '#0b0e13' },
  { id: 'oled', labelZh: '纯黑 OLED', labelEn: 'Pure OLED', hex: '#050507' },
  { id: 'navy', labelZh: '星夜深蓝', labelEn: 'Deep Space', hex: '#09101d' },
  { id: 'graphite', labelZh: '暖碳石墨', labelEn: 'Warm Graphite', hex: '#121210' },
]

export const DEFAULT_ACCENT_COLOR = '#364fc7'
export const DEFAULT_LIGHT_CANVAS_COLOR = '#f5f5f7'
export const DEFAULT_DARK_CANVAS_COLOR = '#0b0e13'

function normalizeHexColor(input?: string, fallback: string = DEFAULT_ACCENT_COLOR): string {
  const value = (input || '').trim()
  if (/^#[0-9a-fA-F]{6}$/.test(value)) return value.toLowerCase()
  return fallback.toLowerCase()
}

function hexToRgbTuple(
  hex: string,
  fallback: string = DEFAULT_ACCENT_COLOR,
): [number, number, number] {
  const clean = normalizeHexColor(hex, fallback).slice(1)
  return [
    Number.parseInt(clean.slice(0, 2), 16),
    Number.parseInt(clean.slice(2, 4), 16),
    Number.parseInt(clean.slice(4, 6), 16),
  ]
}

function mixRgb(
  base: [number, number, number],
  target: [number, number, number],
  weight: number,
): string {
  const clamp = (n: number) => Math.max(0, Math.min(255, Math.round(n)))
  const r = clamp(base[0] * (1 - weight) + target[0] * weight)
  const g = clamp(base[1] * (1 - weight) + target[1] * weight)
  const b = clamp(base[2] * (1 - weight) + target[2] * weight)
  return `#${[r, g, b].map((c) => c.toString(16).padStart(2, '0')).join('')}`
}

export function deriveAccentTokens(accentHex: string | undefined, mode: ResolvedThemeMode) {
  const normalized = normalizeHexColor(accentHex)
  const preset = ACCENT_PRESETS.find((item) => item.lightHex.toLowerCase() === normalized)
  const effectiveHex =
    mode === 'dark'
      ? preset
        ? preset.darkHex
        : mixRgb(hexToRgbTuple(normalized), [255, 255, 255], 0.28)
      : normalized
  const rgb = hexToRgbTuple(effectiveHex)
  const rgbString = `${rgb[0]}, ${rgb[1]}, ${rgb[2]}`
  const buttonPrimary =
    mode === 'dark'
      ? preset?.id === 'obsidian'
        ? '#2563eb'
        : mixRgb(hexToRgbTuple(normalized), [255, 255, 255], 0.12)
      : normalized
  const buttonRgb = hexToRgbTuple(buttonPrimary)
  return {
    primary: effectiveHex,
    primaryRgb: rgbString,
    primaryHover:
      mode === 'dark' ? mixRgb(rgb, [255, 255, 255], 0.16) : mixRgb(rgb, [255, 255, 255], 0.14),
    primaryActive: mixRgb(rgb, [0, 0, 0], 0.16),
    primaryLightDefault:
      mode === 'dark' ? `rgba(${rgbString}, 0.14)` : mixRgb(rgb, [255, 255, 255], 0.91),
    primaryLightHover:
      mode === 'dark' ? `rgba(${rgbString}, 0.20)` : mixRgb(rgb, [255, 255, 255], 0.85),
    primaryLightActive:
      mode === 'dark' ? `rgba(${rgbString}, 0.26)` : mixRgb(rgb, [255, 255, 255], 0.78),
    buttonPrimary,
    buttonHover: mixRgb(buttonRgb, [255, 255, 255], 0.13),
    buttonActive: mixRgb(buttonRgb, [0, 0, 0], 0.15),
    buttonRgbString: `${buttonRgb[0]}, ${buttonRgb[1]}, ${buttonRgb[2]}`,
  }
}

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
  "-apple-system, BlinkMacSystemFont, 'PingFang SC', 'Hiragino Sans GB', 'Noto Sans SC', sans-serif"
const APP_SERIF_FONT_FAMILY = "'Songti SC', 'STSong', 'Noto Serif SC', Georgia, serif"
const APP_MONO_FONT_FAMILY =
  "'SFMono-Regular', ui-monospace, Menlo, Monaco, Consolas, 'Liberation Mono', monospace"
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
const APP_CONTROL_RADIUS = 10
const APP_PANEL_RADIUS = 18
const APP_LARGE_RADIUS = 24
// The app sidebar is compact; other menus retain the component's standard height.
const APP_SIDEBAR_ITEM_HEIGHT = 31
const GLASS_BLUR = '20px'

const THEME_PALETTES: Record<ResolvedThemeMode, ThemePalette> = {
  light: {
    primaryRgb: '15, 106, 92',
    primary: '#0f6a5c',
    primaryHover: '#14806f',
    primaryActive: '#0b5247',
    primaryLightDefault: '#e2f3ef',
    primaryLightHover: '#cdebe4',
    primaryLightActive: '#bce3da',
    primaryDisabled: '#9dc2bb',
    colorSuccess: '#128064',
    colorWarning: '#b8741a',
    colorDanger: '#c94f38',
    colorInfo: '#0f6a5c',
    controlOutline: 'rgba(15, 106, 92, 0.16)',
    colorBgBase: '#f5f5f7',
    colorBgLayout: '#f5f5f7',
    colorBgContainer: '#ffffff',
    colorBgElevated: '#ffffff',
    colorBgMuted: '#ebedf0',
    colorBorder: '#dfe2e7',
    colorBorderSecondary: '#e8eaee',
    colorFill: '#c8cdd5',
    colorFillSecondary: '#dadfe6',
    colorFillTertiary: '#ebedf0',
    colorFillQuaternary: 'rgba(29, 29, 31, 0.036)',
    colorText: '#1d1d1f',
    colorTextSecondary: '#515156',
    colorTextTertiary: '#76767d',
    colorTextQuaternary: '#9a9aa1',
    colorCodeBg: '#0f171e',
    colorCodeText: '#f1f5f9',
    terminalBg: '#0c1218',
    terminalFg: '#e2e8f0',
    terminalCursor: '#2dd4bf',
    terminalBorder: 'rgba(15, 35, 42, 0.14)',
    terminalMuted: 'rgba(148, 163, 184, 0.88)',
    terminalRowDivider: 'rgba(255, 255, 255, 0.06)',
    terminalOverlayBg: 'rgba(12, 18, 24, 0.92)',
    colorPrimaryBg: '#e2f3ef',
    colorPrimaryBgHover: '#cdebe4',
    colorPrimaryBorder: 'rgba(15, 106, 92, 0.24)',
    colorPrimaryBorderHover: 'rgba(15, 106, 92, 0.42)',
    boxShadow: '0 24px 60px -18px rgba(15, 20, 28, 0.075), 0 4px 16px -4px rgba(15, 20, 28, 0.03)',
    boxShadowSecondary:
      '0 18px 42px -12px rgba(15, 20, 28, 0.09), 0 2px 8px -2px rgba(15, 20, 28, 0.035)',
    accentBlue: '#0f6a5c',
    accentBlueRgb: '15, 106, 92',
    accentCyan: '#0d8a78',
    accentCyanRgb: '13, 138, 120',
    accentViolet: '#0284c7',
    accentTeal: '#14806f',
    accentTealRgb: '20, 128, 111',
    gradientPrimary: 'linear-gradient(150deg, #127868 0%, #0b4f45 100%)',
    gradientSubtle:
      'linear-gradient(135deg, rgba(15, 106, 92, 0.05) 0%, rgba(235, 237, 240, 0.7) 100%)',
    gradientPanel: 'linear-gradient(180deg, #ffffff 0%, #fafafa 100%)',
    glassBg: 'rgba(255, 255, 255, 0.84)',
    glassBgStrong: 'rgba(255, 255, 255, 0.94)',
    glassBorder: 'rgba(29, 29, 31, 0.08)',
    glassShadow: '0 18px 42px -14px rgba(15, 20, 28, 0.08), 0 2px 8px -2px rgba(15, 20, 28, 0.03)',
    workflowCanvasBg: 'linear-gradient(180deg, #f8f8fa 0%, #ebedf0 100%)',
    workflowCanvasGrid: 'rgba(29, 29, 31, 0.06)',
    workflowCanvasGlow:
      'radial-gradient(circle at top left, rgba(15, 106, 92, 0.08), transparent 40%)',
    workflowNodeBg: 'rgba(255, 255, 255, 0.96)',
    workflowNodeBorder: 'rgba(29, 29, 31, 0.10)',
    workflowNodeSelectedBorder: '#0f6a5c',
    workflowNodeSelectedRing: 'rgba(15, 106, 92, 0.16)',
    workflowEdgeDefault: '#9a9aa1',
    workflowEdgeSuccess: '#128064',
    workflowEdgeFailure: '#c94f38',
    workflowInspectorBg: 'rgba(250, 250, 252, 0.94)',
    graphScope: '#0f6a5c',
    graphService: '#0d8a78',
    graphSpan: '#76767d',
    graphLog: '#b8741a',
    graphMetric: '#14806f',
    graphHypothesis: '#c94f38',
    graphMuted: '#76767d',
    graphRecommendation: '#0f6a5c',
    dataPanelBg: '#ffffff',
    dataPanelBorder: '#dfe2e7',
    dataRowHoverBg: 'rgba(15, 106, 92, 0.038)',
    dataRowSelectedBg: '#eaf6f3',
    listItemActiveBg: '#0f6a5c',
    listItemActiveBorder: 'rgba(15, 106, 92, 0.25)',
  },
  dark: {
    primaryRgb: '45, 212, 191',
    primary: '#14b8a6',
    primaryHover: '#2dd4bf',
    primaryActive: '#0d9488',
    primaryLightDefault: 'rgba(45, 212, 191, 0.12)',
    primaryLightHover: 'rgba(45, 212, 191, 0.18)',
    primaryLightActive: 'rgba(45, 212, 191, 0.24)',
    primaryDisabled: 'rgba(100, 116, 139, 0.34)',
    colorSuccess: '#34d399',
    colorWarning: '#fbbf24',
    colorDanger: '#f87171',
    colorInfo: '#2dd4bf',
    controlOutline: 'rgba(45, 212, 191, 0.22)',
    colorBgBase: '#0b0e13',
    colorBgLayout: '#0b0e13',
    colorBgContainer: '#12161d',
    colorBgElevated: '#181d26',
    colorBgMuted: '#0e1218',
    colorBorder: '#222936',
    colorBorderSecondary: '#1b212c',
    colorFill: '#333d4f',
    colorFillSecondary: '#252d3b',
    colorFillTertiary: '#1a202b',
    colorFillQuaternary: 'rgba(226, 232, 240, 0.045)',
    colorText: '#f1f5f9',
    colorTextSecondary: '#94a3b8',
    colorTextTertiary: '#64748b',
    colorTextQuaternary: '#475569',
    colorCodeBg: '#080a0e',
    colorCodeText: '#f1f5f9',
    terminalBg: '#080a0e',
    terminalFg: '#e2e8f0',
    terminalCursor: '#2dd4bf',
    terminalBorder: 'rgba(148, 163, 184, 0.14)',
    terminalMuted: 'rgba(148, 163, 184, 0.85)',
    terminalRowDivider: 'rgba(255, 255, 255, 0.06)',
    terminalOverlayBg: 'rgba(8, 10, 14, 0.92)',
    colorPrimaryBg: 'rgba(45, 212, 191, 0.11)',
    colorPrimaryBgHover: 'rgba(45, 212, 191, 0.17)',
    colorPrimaryBorder: 'rgba(45, 212, 191, 0.28)',
    colorPrimaryBorderHover: 'rgba(45, 212, 191, 0.44)',
    boxShadow: '0 18px 42px -12px rgba(0, 0, 0, 0.62)',
    boxShadowSecondary: '0 24px 56px -14px rgba(0, 0, 0, 0.72), 0 2px 8px rgba(0, 0, 0, 0.45)',
    accentBlue: '#f1f5f9',
    accentBlueRgb: '241, 245, 249',
    accentCyan: '#2dd4bf',
    accentCyanRgb: '45, 212, 191',
    accentViolet: '#38bdf8',
    accentTeal: '#14b8a6',
    accentTealRgb: '20, 184, 166',
    gradientPrimary: 'linear-gradient(150deg, #115e59 0%, #0f3d3e 100%)',
    gradientSubtle:
      'linear-gradient(135deg, rgba(45, 212, 191, 0.08) 0%, rgba(18, 22, 29, 0.7) 100%)',
    gradientPanel: 'linear-gradient(180deg, #161b24 0%, #12161d 100%)',
    glassBg: 'rgba(18, 22, 29, 0.84)',
    glassBgStrong: 'rgba(24, 29, 38, 0.94)',
    glassBorder: 'rgba(148, 163, 184, 0.12)',
    glassShadow: '0 24px 56px -12px rgba(0, 0, 0, 0.72)',
    workflowCanvasBg: 'linear-gradient(180deg, #0b0e13 0%, #12161d 100%)',
    workflowCanvasGrid: 'rgba(148, 163, 184, 0.07)',
    workflowCanvasGlow:
      'radial-gradient(circle at top left, rgba(45, 212, 191, 0.10), transparent 40%)',
    workflowNodeBg: 'rgba(24, 29, 38, 0.94)',
    workflowNodeBorder: 'rgba(148, 163, 184, 0.14)',
    workflowNodeSelectedBorder: '#2dd4bf',
    workflowNodeSelectedRing: 'rgba(45, 212, 191, 0.22)',
    workflowEdgeDefault: '#475569',
    workflowEdgeSuccess: '#34d399',
    workflowEdgeFailure: '#f87171',
    workflowInspectorBg: 'rgba(24, 29, 38, 0.94)',
    graphScope: '#f1f5f9',
    graphService: '#2dd4bf',
    graphSpan: '#94a3b8',
    graphLog: '#fbbf24',
    graphMetric: '#34d399',
    graphHypothesis: '#f87171',
    graphMuted: '#64748b',
    graphRecommendation: '#2dd4bf',
    dataPanelBg: '#12161d',
    dataPanelBorder: '#222936',
    dataRowHoverBg: 'rgba(45, 212, 191, 0.05)',
    dataRowSelectedBg: 'rgba(45, 212, 191, 0.11)',
    listItemActiveBg: '#115e59',
    listItemActiveBorder: 'rgba(45, 212, 191, 0.28)',
  },
}

export function resolveThemeMode(themeMode: ThemeMode): ResolvedThemeMode {
  if (themeMode !== 'system') return themeMode
  if (typeof window === 'undefined') return 'light'
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

export function deriveCanvasTokens(canvasHex: string | undefined, mode: ResolvedThemeMode) {
  const fallback = mode === 'dark' ? DEFAULT_DARK_CANVAS_COLOR : DEFAULT_LIGHT_CANVAS_COLOR
  const bg = normalizeHexColor(canvasHex, fallback)
  const rgb = hexToRgbTuple(bg, fallback)
  if (mode === 'dark') {
    const container =
      bg === DEFAULT_DARK_CANVAS_COLOR ? '#12161d' : mixRgb(rgb, [226, 232, 240], 0.036)
    const elevated =
      bg === DEFAULT_DARK_CANVAS_COLOR ? '#181d26' : mixRgb(rgb, [226, 232, 240], 0.068)
    const muted = bg === DEFAULT_DARK_CANVAS_COLOR ? '#0e1218' : mixRgb(rgb, [226, 232, 240], 0.018)
    const border = bg === DEFAULT_DARK_CANVAS_COLOR ? '#222936' : mixRgb(rgb, [148, 163, 184], 0.14)
    const borderSecondary =
      bg === DEFAULT_DARK_CANVAS_COLOR ? '#1b212c' : mixRgb(rgb, [148, 163, 184], 0.095)
    const containerRgb = hexToRgbTuple(container, '#12161d')
    const elevatedRgb = hexToRgbTuple(elevated, '#181d26')
    return {
      colorBgBase: bg,
      colorBgLayout: bg,
      colorBgContainer: container,
      colorBgElevated: elevated,
      colorBgMuted: muted,
      colorBorder: border,
      colorBorderSecondary: borderSecondary,
      bgOat: mixRgb(rgb, [226, 232, 240], 0.052),
      bgSurfaceSoft: mixRgb(rgb, [226, 232, 240], 0.044),
      glassBg: `rgba(${containerRgb[0]}, ${containerRgb[1]}, ${containerRgb[2]}, 0.84)`,
      glassBgStrong: `rgba(${elevatedRgb[0]}, ${elevatedRgb[1]}, ${elevatedRgb[2]}, 0.94)`,
      dataPanelBg: container,
      dataPanelBorder: border,
    }
  }
  const muted = bg === DEFAULT_LIGHT_CANVAS_COLOR ? '#ebedf0' : mixRgb(rgb, [15, 23, 42], 0.042)
  const border = bg === DEFAULT_LIGHT_CANVAS_COLOR ? '#dfe2e7' : mixRgb(rgb, [15, 23, 42], 0.092)
  const borderSecondary =
    bg === DEFAULT_LIGHT_CANVAS_COLOR ? '#e8eaee' : mixRgb(rgb, [15, 23, 42], 0.056)
  return {
    colorBgBase: bg,
    colorBgLayout: bg,
    colorBgContainer: '#ffffff',
    colorBgElevated: '#ffffff',
    colorBgMuted: muted,
    colorBorder: border,
    colorBorderSecondary: borderSecondary,
    bgOat: bg === DEFAULT_LIGHT_CANVAS_COLOR ? '#e6e8ec' : mixRgb(rgb, [15, 23, 42], 0.064),
    bgSurfaceSoft: '#fafafa',
    glassBg: 'rgba(255, 255, 255, 0.84)',
    glassBgStrong: 'rgba(255, 255, 255, 0.94)',
    dataPanelBg: '#ffffff',
    dataPanelBorder: border,
  }
}

export function getThemePalette(
  themeMode: ThemeMode | ResolvedThemeMode,
  accentColor?: string,
  canvasColor?: string,
): ThemePalette {
  const resolvedMode = themeMode === 'system' ? resolveThemeMode(themeMode) : themeMode
  const base = THEME_PALETTES[resolvedMode]
  const accent = deriveAccentTokens(accentColor, resolvedMode)
  const canvas = deriveCanvasTokens(canvasColor, resolvedMode)
  return {
    ...base,
    colorBgBase: canvas.colorBgBase,
    colorBgLayout: canvas.colorBgLayout,
    colorBgContainer: canvas.colorBgContainer,
    colorBgElevated: canvas.colorBgElevated,
    colorBgMuted: canvas.colorBgMuted,
    colorBorder: canvas.colorBorder,
    colorBorderSecondary: canvas.colorBorderSecondary,
    glassBg: canvas.glassBg,
    glassBgStrong: canvas.glassBgStrong,
    dataPanelBg: canvas.dataPanelBg,
    dataPanelBorder: canvas.dataPanelBorder,
    primary: accent.primary,
    primaryRgb: accent.primaryRgb,
    primaryHover: accent.primaryHover,
    primaryActive: accent.primaryActive,
    primaryLightDefault: accent.primaryLightDefault,
    primaryLightHover: accent.primaryLightHover,
    primaryLightActive: accent.primaryLightActive,
    controlOutline: `rgba(${accent.primaryRgb}, 0.20)`,
    colorInfo: accent.primary,
    colorPrimaryBg: accent.primaryLightDefault,
    colorPrimaryBgHover: accent.primaryLightHover,
    colorPrimaryBorder: `rgba(${accent.primaryRgb}, 0.26)`,
    colorPrimaryBorderHover: `rgba(${accent.primaryRgb}, 0.44)`,
    accentBlue: resolvedMode === 'dark' ? base.colorText : accent.primary,
    accentBlueRgb: resolvedMode === 'dark' ? '241, 245, 249' : accent.primaryRgb,
    accentCyan: accent.primary,
    accentCyanRgb: accent.primaryRgb,
    listItemActiveBg: accent.buttonPrimary,
    listItemActiveBorder: `rgba(${accent.primaryRgb}, 0.28)`,
    dataRowHoverBg:
      resolvedMode === 'dark'
        ? `rgba(${accent.primaryRgb}, 0.055)`
        : `rgba(${accent.primaryRgb}, 0.036)`,
    dataRowSelectedBg: accent.primaryLightDefault,
  }
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

export function getAntdTheme(
  themeMode: ThemeMode | ResolvedThemeMode,
  accentColor?: string,
  canvasColor?: string,
): ThemeConfig {
  const resolvedMode = themeMode === 'system' ? resolveThemeMode(themeMode) : themeMode
  const palette = getThemePalette(resolvedMode, accentColor, canvasColor)
  const accent = deriveAccentTokens(accentColor, resolvedMode)

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
        itemSelectedColor: palette.colorText,
        itemSelectedBg: palette.primaryLightDefault,
        subMenuItemSelectedColor: palette.colorText,
        groupTitleColor: palette.colorTextTertiary,
        darkItemBg: 'transparent',
        darkSubMenuItemBg: 'transparent',
        darkPopupBg: palette.colorBgElevated,
        darkItemColor: palette.colorTextSecondary,
        darkItemHoverColor: palette.colorText,
        darkItemHoverBg: palette.colorFillQuaternary,
        darkItemSelectedColor: palette.colorText,
        darkItemSelectedBg: palette.primaryLightDefault,
        darkGroupTitleColor: palette.colorTextTertiary,
        itemBorderRadius: 7,
        subMenuItemBorderRadius: 7,
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
        rowHoverBg: palette.dataRowHoverBg,
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
        colorPrimary: accent.buttonPrimary,
        colorPrimaryHover: accent.buttonHover,
        colorPrimaryActive: accent.buttonActive,
        primaryShadow:
          resolvedMode === 'dark'
            ? 'inset 0 1px 0 rgba(255, 255, 255, 0.22), 0 8px 20px -6px rgba(0, 0, 0, 0.55)'
            : `inset 0 1px 0 rgba(255, 255, 255, 0.24), 0 10px 24px -8px rgba(${accent.buttonRgbString}, 0.34)`,
        defaultShadow: 'inset 0 1px 0 rgba(255, 255, 255, 0.8), 0 1px 2px rgba(23, 33, 28, 0.04)',
        dangerShadow: 'none',
        defaultBorderColor: palette.colorBorder,
        defaultColor: palette.colorText,
        defaultBg: palette.colorBgContainer,
        defaultHoverBg: palette.colorFillTertiary,
        defaultHoverBorderColor: palette.colorPrimaryBorder,
        defaultHoverColor: palette.colorText,
        defaultActiveBg: palette.colorFillSecondary,
        defaultActiveBorderColor: palette.colorPrimaryBorder,
        defaultActiveColor: palette.colorText,
        primaryColor: '#f7f4ee',
        solidTextColor: '#f7f4ee',
        dangerColor: '#ffffff',
        fontWeight: 600,
        borderRadius: 999,
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

export function applyAppTheme(
  _themeId: AppThemeId,
  themeMode: ThemeMode,
  accentColor?: string,
  canvasColor?: string,
  options?: {
    paperGrain?: boolean
  },
) {
  if (typeof document === 'undefined') return
  const resolvedMode = resolveThemeMode(themeMode)
  const palette = getThemePalette(resolvedMode, accentColor, canvasColor)
  const accent = deriveAccentTokens(accentColor, resolvedMode)
  const canvas = deriveCanvasTokens(canvasColor, resolvedMode)
  const root = document.documentElement
  delete root.dataset.lightCanvas
  delete root.dataset.tableStyle
  root.dataset.paperGrain = options?.paperGrain === false ? 'off' : 'on'
  const motion = antdTheme.getDesignToken(getAntdTheme(resolvedMode, accentColor, canvasColor))
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
  root.style.setProperty('--soha-table-header-bg', palette.colorBgContainer)
  root.style.setProperty('--soha-table-header-sort-bg', palette.colorFillQuaternary)
  root.style.setProperty('--soha-table-row-hover-bg', palette.dataRowHoverBg)
  root.style.setProperty('--soha-table-row-selected-bg', palette.colorPrimaryBg)
  root.style.setProperty('--soha-table-row-selected-hover-bg', palette.colorPrimaryBgHover)
  root.style.setProperty('--soha-table-link-color', palette.colorText)
  root.style.setProperty('--soha-table-link-hover-color', accent.primary)
  root.style.setProperty('--soha-menu-item-hover-bg', palette.colorFillQuaternary)
  root.style.setProperty('--soha-menu-item-selected-bg', palette.listItemActiveBg)
  root.style.setProperty('--soha-menu-item-selected-color', '#f8fafc')
  root.style.setProperty('--soha-brand-teal', accent.primary)
  root.style.setProperty('--soha-brand-teal-rgb', accent.primaryRgb)
  root.style.setProperty('--soha-brand-teal-soft', accent.primaryLightDefault)
  root.style.setProperty('--soha-bg-oat', canvas.bgOat)
  root.style.setProperty('--soha-bg-surface-soft', canvas.bgSurfaceSoft)
  root.style.setProperty(
    '--soha-hairline',
    resolvedMode === 'dark' ? 'rgba(148, 163, 184, 0.10)' : 'rgba(29, 29, 31, 0.065)',
  )
  root.style.setProperty(
    '--soha-hairline-strong',
    resolvedMode === 'dark' ? 'rgba(148, 163, 184, 0.16)' : 'rgba(29, 29, 31, 0.105)',
  )
  root.style.setProperty(
    '--soha-bezel-bg',
    resolvedMode === 'dark' ? 'rgba(148, 163, 184, 0.055)' : 'rgba(29, 29, 36, 0.042)',
  )
  root.style.setProperty(
    '--soha-inner-highlight',
    resolvedMode === 'dark'
      ? 'inset 0 1px 0 rgba(255, 255, 255, 0.065)'
      : 'inset 0 1px 0 rgba(255, 255, 255, 0.92)',
  )
  root.style.setProperty('--soha-ease-fluid', 'cubic-bezier(0.32, 0.72, 0, 1)')
  root.style.setProperty('--soha-ease-expo', 'cubic-bezier(0.16, 1, 0.3, 1)')
  root.style.setProperty('--soha-font-serif', APP_SERIF_FONT_FAMILY)
  root.style.setProperty('--soha-font-mono', APP_MONO_FONT_FAMILY)
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
