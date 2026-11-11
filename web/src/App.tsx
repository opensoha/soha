import { useLayoutEffect } from 'react'
import { AppErrorBoundary } from './components/app-error-boundary'
import { AppRouter } from './routes'
import { usePreferencesStore } from './stores/preferences-store'
import { applyAppTheme, DEFAULT_APP_THEME_ID, watchSystemThemeMode } from './theme/app-theme'

export default function App() {
  const themeMode = usePreferencesStore((state) => state.themeMode)

  useLayoutEffect(() => {
    applyAppTheme(DEFAULT_APP_THEME_ID, themeMode)
  }, [themeMode])

  useLayoutEffect(() => {
    if (themeMode !== 'system') {
      return undefined
    }
    return watchSystemThemeMode(() => applyAppTheme(DEFAULT_APP_THEME_ID, themeMode))
  }, [themeMode])

  return (
    <AppErrorBoundary>
      <AppRouter />
    </AppErrorBoundary>
  )
}
