import {
  commitAuthResult,
  exchangeOIDCCode,
  fetchPermissionSnapshot,
  logoutAuthSession,
} from '@/features/auth/auth-api'
import { normalizeLocalReturnTo, shouldUseDocumentNavigation } from '@/features/auth/return-to'
import { appBrowserPath, appRouterPath } from '@/utils/app-base'
import { findLandingPath } from '@/routes/meta'
import { usePreferencesStore } from '@/stores/preferences-store'
import { App, Spin, theme, Typography } from 'antd'
import { useEffect, useRef, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'

const { Title, Text } = Typography

export function OIDCCallbackPage() {
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const { message } = App.useApp()
  const { token } = theme.useToken()
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
        commitAuthResult(authResult)
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
      } catch (err: any) {
        setError(err?.message ?? 'OIDC 登录失败')
      }
    }

    exchangeCode(code)
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

  const handleBackToLogin = async (event: React.MouseEvent<HTMLAnchorElement>) => {
    event.preventDefault()
    await logoutAuthSession()
    navigate('/login', { replace: true })
  }

  return (
    <div
      className="min-h-screen flex items-center justify-center"
      style={{ backgroundColor: token.colorBgLayout }}
    >
      <div className="flex flex-col items-center gap-4">
        {error ? (
          <>
            <Title level={4}>登录失败</Title>
            <Text role="alert" type="danger">
              {error}
            </Text>
            <a
              href={appBrowserPath('/login')}
              className="mt-4"
              style={{ color: token.colorPrimary }}
              onClick={handleBackToLogin}
            >
              返回登录
            </a>
          </>
        ) : (
          <>
            <Spin size="large" />
            <Text type="secondary">正在完成登录...</Text>
          </>
        )}
      </div>
    </div>
  )
}
