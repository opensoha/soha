import {
  exchangeOIDCCode,
  fetchPermissionSnapshot,
  logoutAuthSession,
} from '@/features/auth/auth-api'
import { normalizeLocalReturnTo, shouldUseDocumentNavigation } from '@/features/auth/return-to'
import { findLandingPath } from '@/routes/meta'
import { usePreferencesStore } from '@/stores/preferences-store'
import { appBrowserPath, appRouterPath } from '@/utils/app-base'
import { App, Button, Spin, Typography } from 'antd'
import { useEffect, useRef, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import './browser-handoff-page.css'

const { Title, Text } = Typography

export function OIDCCallbackPage() {
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const { message } = App.useApp()
  const [error, setError] = useState<string | null>(null)
  const handledRef = useRef(false)

  useEffect(() => {
    if (handledRef.current) return
    handledRef.current = true

    const code = searchParams.get('code')
    const errorCode = searchParams.get('error')
    const errorMessage = searchParams.get('error_description') || searchParams.get('message')
    if (errorCode) {
      setError(errorMessage || errorCode)
      return
    }
    if (!code) {
      setError('缺少授权码参数')
      return
    }

    async function exchangeCode(authCode: string) {
      try {
        const returnTo = normalizeLocalReturnTo(searchParams.get('return_to'))
        const authResult = await exchangeOIDCCode(authCode)
        const snapshot = returnTo ? null : await fetchPermissionSnapshot().catch(() => null)
        const nextPath =
          returnTo ??
          findLandingPath(
            snapshot,
            usePreferencesStore.getState().currentWorkspace,
            authResult.user.roles,
          ) ??
          '/'
        message.success('登录成功')
        if (shouldUseDocumentNavigation(nextPath)) {
          window.location.assign(appBrowserPath(nextPath))
          return
        }
        navigate(nextPath, { replace: true })
      } catch (err: unknown) {
        setError(err instanceof Error && err.message ? err.message : 'OIDC 登录失败')
      }
    }

    void exchangeCode(code)
  }, [message, navigate, searchParams])

  useEffect(() => {
    return () => {
      const url = new URL(window.location.href)
      if (appRouterPath(url.pathname) === '/login/callback') {
        url.search = ''
        window.history.replaceState({}, document.title, url.toString())
      }
    }
  }, [])

  const handleBackToLogin = async (event: React.MouseEvent<HTMLElement>) => {
    event.preventDefault()
    await logoutAuthSession()
    navigate('/login', { replace: true, state: { skipSessionRestore: true } })
  }

  return (
    <main className="soha-browser-handoff-page">
      <div className="soha-browser-handoff-bezel">
        <section className="soha-browser-handoff-card">
          <div className="soha-browser-handoff-brand" style={{ justifyContent: 'center' }}>
            <img src={appBrowserPath('/logo.svg')} alt="" />
            <Text strong>Soha</Text>
          </div>
          <div className="soha-oidc-callback-status">
            {error ? (
              <>
                <Title level={4} style={{ margin: 0 }}>
                  登录失败
                </Title>
                <Text role="alert" type="danger">
                  {error}
                </Text>
                <Button
                  type="primary"
                  href={appBrowserPath('/login')}
                  onClick={handleBackToLogin}
                  style={{ marginTop: 8 }}
                >
                  返回登录
                </Button>
              </>
            ) : (
              <>
                <Spin size="large" />
                <Text type="secondary">正在完成登录...</Text>
              </>
            )}
          </div>
        </section>
      </div>
    </main>
  )
}
