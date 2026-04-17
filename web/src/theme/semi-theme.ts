export type SemiThemeId =
  | 'doucreator'
  | 'douyin'
  | 'feishu'
  | 'volcengine'
  | 'a11y'

export type ThemeMode = 'light' | 'dark' | 'system'

interface SemiThemeOption {
  id: SemiThemeId
  label: string
  cssHref: string
}

const THEME_LINK_ID = 'kc-semi-theme-stylesheet'
const PREFERENCES_STORAGE_KEY = 'kubecrux-prefs'

export const DEFAULT_SEMI_THEME_ID: SemiThemeId = 'doucreator'
export const DEFAULT_THEME_MODE: ThemeMode = 'light'

export const semiThemeOptions: SemiThemeOption[] = [
  { id: 'doucreator', label: '抖音创作服务', cssHref: '/semi-themes/doucreator.css' },
  { id: 'douyin', label: '抖音', cssHref: '/semi-themes/douyin.css' },
  { id: 'feishu', label: '飞书', cssHref: '/semi-themes/feishu.css' },
  { id: 'volcengine', label: '火山引擎', cssHref: '/semi-themes/volcengine.css' },
  { id: 'a11y', label: 'A11y 无障碍', cssHref: '/semi-themes/a11y.css' },
]

export const themeModeOptions: Array<{ value: ThemeMode; label: string }> = [
  { value: 'light', label: '浅色' },
  { value: 'dark', label: '深色' },
  { value: 'system', label: '跟随系统' },
]

function getThemeHref(themeId: SemiThemeId): string {
  return semiThemeOptions.find((theme) => theme.id === themeId)?.cssHref ?? '/semi-themes/doucreator.css'
}

export function resolveThemeMode(themeMode: ThemeMode): 'light' | 'dark' {
  if (themeMode !== 'system') return themeMode
  if (typeof window === 'undefined') return 'light'
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

export function getSemiThemeLabel(themeId: SemiThemeId): string {
  return semiThemeOptions.find((theme) => theme.id === themeId)?.label ?? '抖音创作服务'
}

export function applySemiTheme(themeId: SemiThemeId, themeMode: ThemeMode) {
  if (typeof document === 'undefined') return

  let link = document.getElementById(THEME_LINK_ID) as HTMLLinkElement | null
  if (!link) {
    link = document.createElement('link')
    link.id = THEME_LINK_ID
    link.rel = 'stylesheet'
    document.head.prepend(link)
  }

  const themeHref = getThemeHref(themeId)
  if (link.getAttribute('href') !== themeHref) {
    link.setAttribute('href', themeHref)
  }

  const resolvedMode = resolveThemeMode(themeMode)
  document.body.setAttribute('theme-mode', resolvedMode)
  document.documentElement.dataset.semiTheme = themeId
  document.documentElement.dataset.themeMode = resolvedMode
}

export function watchSystemThemeMode(onChange: () => void) {
  if (typeof window === 'undefined') return () => {}

  const media = window.matchMedia('(prefers-color-scheme: dark)')
  const handler = () => onChange()
  media.addEventListener('change', handler)
  return () => media.removeEventListener('change', handler)
}

export function readStoredThemePreference(): {
  themeId: SemiThemeId
  themeMode: ThemeMode
} {
  if (typeof window === 'undefined') {
    return {
      themeId: DEFAULT_SEMI_THEME_ID,
      themeMode: DEFAULT_THEME_MODE,
    }
  }

  try {
    const raw = window.localStorage.getItem(PREFERENCES_STORAGE_KEY)
    if (!raw) {
      return {
        themeId: DEFAULT_SEMI_THEME_ID,
        themeMode: DEFAULT_THEME_MODE,
      }
    }

    const parsed = JSON.parse(raw)
    const state = parsed?.state ?? parsed
    const themeId = semiThemeOptions.some((theme) => theme.id === state?.themeId)
      ? state.themeId
      : DEFAULT_SEMI_THEME_ID
    const themeMode = ['light', 'dark', 'system'].includes(state?.themeMode)
      ? state.themeMode
      : DEFAULT_THEME_MODE

    return { themeId, themeMode }
  } catch {
    return {
      themeId: DEFAULT_SEMI_THEME_ID,
      themeMode: DEFAULT_THEME_MODE,
    }
  }
}
