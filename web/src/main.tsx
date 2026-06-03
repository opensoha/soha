import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App as AntdApp, ConfigProvider } from 'antd'
import zhCN from 'antd/locale/zh_CN'
import enUS from 'antd/locale/en_US'
import App from './App'
import { useBrandingSettings } from './features/settings/use-branding-settings'
import { I18nProvider } from './i18n'
import { usePreferencesStore } from './stores/preferences-store'
import {
  applyAppTheme,
  DEFAULT_APP_THEME_ID,
  getAntdTheme,
  readStoredThemePreference,
  resolveThemeMode,
  watchSystemThemeMode,
} from './theme/app-theme'
import { applyBrandingSettings, persistBrandingSettings, readStoredBrandingSettings } from './utils/branding'
import './styles/globals.css'
import './styles/shared-surfaces.css'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      refetchOnWindowFocus: false,
      staleTime: 30_000,
    },
  },
})

const storedThemePreference = readStoredThemePreference()

applyAppTheme(DEFAULT_APP_THEME_ID, storedThemePreference.themeMode)
applyBrandingSettings(readStoredBrandingSettings())

function AppProviders() {
  const localeCode = usePreferencesStore((state) => state.localeCode)
  const themeMode = usePreferencesStore((state) => state.themeMode)
  const brandingQuery = useBrandingSettings()
  const [systemThemeVersion, setSystemThemeVersion] = React.useState(0)

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

  const resolvedThemeMode = React.useMemo(() => resolveThemeMode(themeMode), [themeMode, systemThemeVersion])
  const antdTheme = React.useMemo(() => getAntdTheme(resolvedThemeMode), [resolvedThemeMode])

  return (
    <ConfigProvider
      locale={localeCode === 'en_US' ? enUS : zhCN}
      theme={antdTheme}
    >
      <AntdApp>
        <I18nProvider>
          <App />
        </I18nProvider>
      </AntdApp>
    </ConfigProvider>
  )
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <AppProviders />
      </BrowserRouter>
    </QueryClientProvider>
  </React.StrictMode>,
)
