import { useLayoutEffect } from 'react'
import { AppRouter } from './routes'
import { usePreferencesStore } from './stores/preferences-store'
import { applySemiTheme, DEFAULT_SEMI_THEME_ID, watchSystemThemeMode } from './theme/semi-theme'

export default function App() {
  const themeMode = usePreferencesStore((state) => state.themeMode)

  useLayoutEffect(() => {
    applySemiTheme(DEFAULT_SEMI_THEME_ID, themeMode)
  }, [themeMode])

  useLayoutEffect(() => {
    if (themeMode !== 'system') {
      return undefined
    }
    return watchSystemThemeMode(() => applySemiTheme(DEFAULT_SEMI_THEME_ID, themeMode))
  }, [themeMode])

  return <AppRouter />
}
