import { useLayoutEffect } from 'react'
import { AppRouter } from './routes'
import { usePreferencesStore } from './stores/preferences-store'
import { applySemiTheme, watchSystemThemeMode } from './theme/semi-theme'

export default function App() {
  const themeId = usePreferencesStore((state) => state.themeId)
  const themeMode = usePreferencesStore((state) => state.themeMode)

  useLayoutEffect(() => {
    applySemiTheme(themeId, themeMode)

    if (themeMode !== 'system') {
      return
    }

    return watchSystemThemeMode(() => applySemiTheme(themeId, 'system'))
  }, [themeId, themeMode])

  return <AppRouter />
}
