import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import { App as AntdApp, ConfigProvider } from 'antd'
import zhCN from 'antd/locale/zh_CN'
import enUS from 'antd/locale/en_US'
import App from './App'
import { GlobalApiErrorHandler } from './components/global-api-error-handler'
import { SessionQueryProvider } from './components/session-query-provider'
import { useBrandingSettings } from '@/features/settings'
import { I18nProvider } from './i18n'
import { usePreferencesStore } from './stores/preferences-store'
import {
  applyAppTheme,
  DEFAULT_APP_THEME_ID,
  getAntdTheme,
  readStoredThemePreference,
  resolveThemeMode,
  watchSystemThemeMode,
  watchSystemMotion,
} from './theme/app-theme'
import {
  applyBrandingSettings,
  persistBrandingSettings,
  readStoredBrandingSettings,
} from './utils/branding'
import { installBrowserPopGuard } from './utils/browser-pop-guard'
import './styles/globals.css'
import './styles/shared-surfaces.css'

const storedThemePreference = readStoredThemePreference()

applyAppTheme(DEFAULT_APP_THEME_ID, storedThemePreference.themeMode)
applyBrandingSettings(readStoredBrandingSettings())

function AppProviders() {
  const localeCode = usePreferencesStore((state) => state.localeCode)
  const themeMode = usePreferencesStore((state) => state.themeMode)
  const brandingQuery = useBrandingSettings()
  const [systemThemeVersion, setSystemThemeVersion] = React.useState(0)

  React.useEffect(() => setSystemThemeVersion((current) => current + 1), [])

  React.useEffect(() => {
    if (!brandingQuery.isSuccess || !brandingQuery.data?.data) return
    const branding = brandingQuery.data.data
    applyBrandingSettings(branding)
    persistBrandingSettings(branding)
  }, [brandingQuery.data, brandingQuery.isSuccess])

  React.useEffect(() => {
    if (themeMode !== 'system') return undefined
    return watchSystemThemeMode(() => setSystemThemeVersion((current) => current + 1))
  }, [themeMode])

  React.useEffect(
    () =>
      watchSystemMotion(() => {
        applyAppTheme(DEFAULT_APP_THEME_ID, themeMode)
        setSystemThemeVersion((current) => current + 1)
      }),
    [themeMode],
  )

  const resolvedThemeMode = React.useMemo(
    () => resolveThemeMode(themeMode),
    [themeMode, systemThemeVersion],
  )
  const antdTheme = React.useMemo(() => {
    const theme = getAntdTheme(resolvedThemeMode)
    // Keep Antd's motion provider present from the first render to preserve page state.
    return systemThemeVersion === 0 ? { ...theme, token: { ...theme.token, motion: false } } : theme
  }, [resolvedThemeMode, systemThemeVersion])

  return (
    <ConfigProvider
      locale={localeCode === 'en_US' ? enUS : zhCN}
      theme={antdTheme}
      tooltip={{
        styles: {
          container: {
            border: '1px solid var(--soha-border-color)',
            boxShadow: 'var(--soha-shadow-soft)',
          },
        },
      }}
    >
      <AntdApp>
        <GlobalApiErrorHandler />
        <I18nProvider>
          <App />
        </I18nProvider>
      </AntdApp>
    </ConfigProvider>
  )
}

installBrowserPopGuard()

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <BrowserRouter basename={import.meta.env.BASE_URL}>
      <SessionQueryProvider>
        <AppProviders />
      </SessionQueryProvider>
    </BrowserRouter>
  </React.StrictMode>,
)
