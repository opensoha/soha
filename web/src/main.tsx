import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ConfigProvider } from '@douyinfe/semi-ui'
import zhCN from '@douyinfe/semi-ui/lib/es/locale/source/zh_CN'
import enUS from '@douyinfe/semi-ui/lib/es/locale/source/en_US'
import App from './App'
import { I18nProvider } from './i18n'
import { usePreferencesStore } from './stores/preferences-store'
import { applySemiTheme, readStoredThemePreference } from './theme/semi-theme'
import './styles/globals.css'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      refetchOnWindowFocus: false,
      staleTime: 30_000,
    },
  },
})

const initialThemePreference = readStoredThemePreference()
applySemiTheme(initialThemePreference.themeId, initialThemePreference.themeMode)

function AppProviders() {
  const localeCode = usePreferencesStore((state) => state.localeCode)
  return (
    <ConfigProvider locale={localeCode === 'en_US' ? enUS : zhCN}>
      <I18nProvider>
        <App />
      </I18nProvider>
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
