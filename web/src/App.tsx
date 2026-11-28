import { useLayoutEffect } from 'react'
import { AppErrorBoundary } from './components/app-error-boundary'
import { AppRouter } from './routes'
import { usePreferencesStore } from './stores/preferences-store'
import {
  applyAppTheme,
  DEFAULT_APP_THEME_ID,
  resolveThemeMode,
  watchSystemThemeMode,
} from './theme/app-theme'

export default function App() {
  const themeMode = usePreferencesStore((state) => state.themeMode)
  const accentColor = usePreferencesStore((state) => state.accentColor)
  const lightCanvasColor = usePreferencesStore((state) => state.lightCanvasColor)
  const darkCanvasColor = usePreferencesStore((state) => state.darkCanvasColor)
  const paperGrain = usePreferencesStore((state) => state.paperGrain)
  const overviewHeroStyle = usePreferencesStore((state) => state.overviewHeroStyle)
  const overviewGradientPreset = usePreferencesStore((state) => state.overviewGradientPreset)
  const overviewGradientStops = usePreferencesStore((state) => state.overviewGradientStops)
  const cornerRadius = usePreferencesStore((state) => state.cornerRadius)

  useLayoutEffect(() => {
    const resolved = resolveThemeMode(themeMode)
    const canvasColor = resolved === 'dark' ? darkCanvasColor : lightCanvasColor
    applyAppTheme(DEFAULT_APP_THEME_ID, themeMode, accentColor, canvasColor, {
      paperGrain,
      overviewHeroStyle,
      overviewGradientPreset,
      overviewGradientStops,
      cornerRadius,
    })
  }, [
    accentColor,
    cornerRadius,
    darkCanvasColor,
    lightCanvasColor,
    overviewGradientPreset,
    overviewGradientStops,
    overviewHeroStyle,
    paperGrain,
    themeMode,
  ])

  useLayoutEffect(() => {
    if (themeMode !== 'system') {
      return undefined
    }
    return watchSystemThemeMode(() => {
      const resolved = resolveThemeMode(themeMode)
      const canvasColor = resolved === 'dark' ? darkCanvasColor : lightCanvasColor
      applyAppTheme(DEFAULT_APP_THEME_ID, themeMode, accentColor, canvasColor, {
        paperGrain,
        overviewHeroStyle,
        overviewGradientPreset,
        overviewGradientStops,
        cornerRadius,
      })
    })
  }, [
    accentColor,
    cornerRadius,
    darkCanvasColor,
    lightCanvasColor,
    overviewGradientPreset,
    overviewGradientStops,
    overviewHeroStyle,
    paperGrain,
    themeMode,
  ])

  return (
    <AppErrorBoundary>
      <AppRouter />
    </AppErrorBoundary>
  )
}
