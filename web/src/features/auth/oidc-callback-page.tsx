import { useEffect, useRef, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { Spin, Toast, Typography } from '@douyinfe/semi-ui'
import { api } from '@/services/api-client'
import { useAuthStore } from '@/stores/auth-store'
import type { ApiResponse, AuthResult } from '@/types'

const { Title, Text } = Typography

export function OIDCCallbackPage() {
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const { setTokens, setUser } = useAuthStore()
  const [error, setError] = useState<string | null>(null)
  const handledRef = useRef(false)

  useEffect(() => {
    if (handledRef.current) return
    handledRef.current = true

    const code = searchParams.get('code')
    if (!code) {
      setError('缺少授权码参数')
      return
    }

    async function exchangeCode(authCode: string) {
      try {
        const authRes = await api.post<ApiResponse<AuthResult>>(
          '/auth/oidc/exchange',
          { code: authCode },
        )
        setTokens(authRes.data.tokens.accessToken, authRes.data.tokens.refreshToken)
        setUser(authRes.data.user)

        Toast.success('登录成功')
        navigate('/', { replace: true })
      } catch (err: any) {
        setError(err?.message ?? 'OIDC 登录失败')
        Toast.error(err?.message ?? 'OIDC 登录失败')
      }
    }

    exchangeCode(code)
  }, [searchParams, navigate, setTokens, setUser])

  return (
    <div className="min-h-screen flex items-center justify-center" style={{ backgroundColor: 'var(--semi-color-bg-0)' }}>
      <div className="flex flex-col items-center gap-4">
        {error ? (
          <>
            <Title heading={4}>登录失败</Title>
            <Text type="danger">{error}</Text>
            <a
              href="/login"
              className="mt-4"
              style={{ color: 'var(--semi-color-primary)' }}
            >
              返回登录
            </a>
          </>
        ) : (
          <>
            <Spin size="large" />
            <Text type="tertiary">正在完成登录...</Text>
          </>
        )}
      </div>
    </div>
  )
}
