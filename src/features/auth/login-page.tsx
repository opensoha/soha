import { ReadFeedback, useInlineApiError } from '@/components/request-feedback'
import {
  API_BASE_URL,
  commitAuthResult,
  fetchAuthProviders,
  fetchLoginOptions,
  fetchPermissionSnapshot,
  loginWithPassword,
  restoreAuthSession,
} from '@/features/auth/auth-api'
import {
  publishAuthSessionAvailable,
  subscribeAuthSessionAvailable,
} from '@/features/auth/auth-session-channel'
import { authKeys } from '@/features/auth/keys'
import { normalizeLocalReturnTo, shouldUseDocumentNavigation } from '@/features/auth/return-to'
import { appBrowserPath } from '@/utils/app-base'
import { findLandingPath } from '@/routes/meta'
import { useAuthStore } from '@/stores/auth-store'
import { usePreferencesStore } from '@/stores/preferences-store'
import { getThemePalette, readThemeCssVariable, resolveThemeMode } from '@/theme/app-theme'
import {
  applyBrandingSettings,
  persistBrandingSettings,
  readStoredBrandingSettings,
  resolveBrandingLogos,
} from '@/utils/branding'
import { readableQueryData } from '@/utils/query-feedback'
import {
  CheckCircleOutlined,
  LockOutlined,
  MoonOutlined,
  SafetyCertificateOutlined,
  SunOutlined,
  UserOutlined,
  WarningOutlined,
} from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { Alert, App, Button, Card, Divider, Form, Input, Slider, Spin, Typography } from 'antd'
import { useCallback, useEffect, useRef, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import './login-page.css'

const { Title, Text } = Typography

const LOGIN_COPYRIGHT_TEXT = '© 2026 Soha 版权所有，由项目贡献者设计与开发。'
const LOGIN_SESSION_RESTORE_RETRY_MS = 2_000

interface LoginFormValues {
  password: string
  username: string
}

interface StarNode {
  pulse: number
  radius: number
  tone: StarToneVariable
  vx: number
  vy: number
  x: number
  y: number
}

const STAR_TONES = [
  '--soha-primary',
  '--soha-accent-cyan',
  '--soha-accent-teal',
  '--soha-success',
] as const

type StarToneVariable = (typeof STAR_TONES)[number]

const CAPABILITY_QUESTIONS = [
  {
    question: '门户',
    answer: '统一入口、常用工作台与待办导航',
  },
  {
    question: 'k8s工作台',
    answer: '多集群、工作负载、网络、存储和 YAML 管理',
  },
  {
    question: '持续交付工作台',
    answer: '应用、服务、环境、工作流、测试与发布',
  },
  {
    question: '可观测性工作台',
    answer: '指标、链路、日志、告警、值班与自愈',
  },
  {
    question: '计算资源工作台',
    answer: '虚拟机、容器运行时、镜像模板与任务中心',
  },
  {
    question: 'AI工作台',
    answer: '对话、知识库、Agent、模型网关与评测',
  },
  {
    question: '内网工作台',
    answer: '应用目录、身份 Provider、Outpost 与访问策略',
  },
  {
    question: '设置中心',
    answer: '用户、角色、组织、系统集成、品牌与运行配置',
  },
]

function createStarNode(width: number, height: number): StarNode {
  const angle = Math.random() * Math.PI * 2
  const speed = 0.05 + Math.random() * 0.14

  return {
    pulse: Math.random() * Math.PI * 2,
    radius: 1 + Math.random() * 1.8,
    tone: STAR_TONES[Math.floor(Math.random() * STAR_TONES.length)]!,
    vx: Math.cos(angle) * speed,
    vy: Math.sin(angle) * speed,
    x: Math.random() * width,
    y: Math.random() * height,
  }
}

function fallbackStarToneColor(tone: StarToneVariable, themeMode: 'light' | 'dark') {
  const palette = getThemePalette(themeMode)
  switch (tone) {
    case '--soha-accent-cyan':
      return palette.accentCyan
    case '--soha-accent-teal':
      return palette.accentTeal
    case '--soha-success':
      return palette.colorSuccess
    case '--soha-primary':
    default:
      return palette.primary
  }
}

function LoginConstellationBackground() {
  const canvasRef = useRef<HTMLCanvasElement | null>(null)

  useEffect(() => {
    const canvas = canvasRef.current
    const context = canvas?.getContext('2d')

    if (!canvas || !context) {
      return undefined
    }

    let animationFrame = 0
    let width = 1
    let height = 1
    let lastTimestamp = 0
    let nodes: StarNode[] = []
    let currentThemeMode: 'light' | 'dark' = 'light'
    const motionMedia = window.matchMedia('(prefers-reduced-motion: reduce)')
    let reducedMotion = motionMedia.matches
    const readThemeMode = () => {
      currentThemeMode = document.documentElement.dataset.themeMode === 'dark' ? 'dark' : 'light'
    }

    const resize = () => {
      const rect = canvas.getBoundingClientRect()
      width = Math.max(1, rect.width)
      height = Math.max(1, rect.height)
      const ratio = Math.min(window.devicePixelRatio || 1, 2)

      canvas.width = Math.floor(width * ratio)
      canvas.height = Math.floor(height * ratio)
      context.setTransform(ratio, 0, 0, ratio, 0, 0)

      const nextCount = Math.round(Math.min(118, Math.max(58, (width * height) / 15000)))

      if (nodes.length > nextCount) {
        nodes = nodes.slice(0, nextCount)
      }
      while (nodes.length < nextCount) {
        nodes.push(createStarNode(width, height))
      }
      nodes = nodes.map((node) =>
        node.x > width || node.y > height ? createStarNode(width, height) : node,
      )
    }

    const draw = (timestamp: number) => {
      readThemeMode()
      const delta = lastTimestamp ? Math.min(32, timestamp - lastTimestamp) / 16.67 : 1
      lastTimestamp = timestamp

      const gradient = context.createLinearGradient(0, 0, width, height)
      const palette = getThemePalette(currentThemeMode)
      gradient.addColorStop(0, readThemeCssVariable('--soha-bg-layout', palette.colorBgLayout))
      gradient.addColorStop(
        0.52,
        readThemeCssVariable('--soha-bg-surface', palette.colorBgContainer),
      )
      gradient.addColorStop(
        1,
        readThemeCssVariable('--soha-bg-surface-muted', palette.colorBgMuted),
      )
      context.fillStyle = gradient
      context.fillRect(0, 0, width, height)

      context.save()
      context.strokeStyle = readThemeCssVariable('--soha-graph-muted', palette.graphMuted)
      context.globalAlpha = currentThemeMode === 'dark' ? 0.06 : 0.085
      context.lineWidth = 1
      for (let x = 0; x <= width; x += 72) {
        context.beginPath()
        context.moveTo(x, 0)
        context.lineTo(x, height)
        context.stroke()
      }
      for (let y = 0; y <= height; y += 72) {
        context.beginPath()
        context.moveTo(0, y)
        context.lineTo(width, y)
        context.stroke()
      }
      context.restore()

      const connectionDistance = Math.max(116, Math.min(168, width / 7))

      nodes.forEach((node) => {
        if (!reducedMotion) {
          node.x += node.vx * delta
          node.y += node.vy * delta
          node.pulse += 0.016 * delta

          if (node.x < -24) node.x = width + 24
          if (node.x > width + 24) node.x = -24
          if (node.y < -24) node.y = height + 24
          if (node.y > height + 24) node.y = -24
        }
      })

      for (let index = 0; index < nodes.length; index += 1) {
        const source = nodes[index]!
        for (let nextIndex = index + 1; nextIndex < nodes.length; nextIndex += 1) {
          const target = nodes[nextIndex]!
          const dx = source.x - target.x
          const dy = source.y - target.y
          const distance = Math.hypot(dx, dy)

          if (distance > connectionDistance) {
            continue
          }

          const alpha =
            (1 - distance / connectionDistance) * (currentThemeMode === 'dark' ? 0.32 : 0.2)
          context.strokeStyle = readThemeCssVariable(
            source.tone,
            fallbackStarToneColor(source.tone, currentThemeMode),
          )
          context.globalAlpha = alpha
          context.lineWidth = 1
          context.beginPath()
          context.moveTo(source.x, source.y)
          context.lineTo(target.x, target.y)
          context.stroke()
          context.globalAlpha = 1
        }
      }

      nodes.forEach((node) => {
        const pulse = 0.28 + Math.sin(node.pulse) * 0.12
        context.fillStyle = readThemeCssVariable(
          node.tone,
          fallbackStarToneColor(node.tone, currentThemeMode),
        )
        context.globalAlpha = currentThemeMode === 'dark' ? 0.54 + pulse : 0.42 + pulse * 0.62
        context.beginPath()
        context.arc(node.x, node.y, node.radius, 0, Math.PI * 2)
        context.fill()

        context.globalAlpha =
          currentThemeMode === 'dark' ? 0.06 + pulse * 0.12 : 0.035 + pulse * 0.08
        context.beginPath()
        context.arc(node.x, node.y, node.radius * 6.5, 0, Math.PI * 2)
        context.fill()
        context.globalAlpha = 1
      })

      if (!reducedMotion && !document.hidden) {
        animationFrame = window.requestAnimationFrame(draw)
      }
    }

    const resizeObserver = new ResizeObserver(() => {
      resize()
      if (reducedMotion) {
        draw(performance.now())
      }
    })

    resizeObserver.observe(canvas)
    const themeObserver = new MutationObserver(() => {
      if (reducedMotion) {
        draw(performance.now())
      }
    })
    themeObserver.observe(document.documentElement, {
      attributeFilter: ['data-theme-mode'],
      attributes: true,
    })
    const redraw = () => {
      window.cancelAnimationFrame(animationFrame)
      reducedMotion = motionMedia.matches
      lastTimestamp = 0
      draw(performance.now())
    }
    motionMedia.addEventListener('change', redraw)
    document.addEventListener('visibilitychange', redraw)
    resize()
    draw(performance.now())

    return () => {
      motionMedia.removeEventListener('change', redraw)
      document.removeEventListener('visibilitychange', redraw)
      resizeObserver.disconnect()
      themeObserver.disconnect()
      window.cancelAnimationFrame(animationFrame)
    }
  }, [])

  return <canvas ref={canvasRef} className="soha-auth-background" aria-hidden="true" />
}

function LoginCapabilityFlow({
  brandName,
  loginLogoUrl,
  slogan,
}: {
  brandName: string
  loginLogoUrl: string
  slogan: string
}) {
  return (
    <section className="soha-auth-flow" aria-label="Soha 支持的能力">
      <div className="soha-auth-flow-copy">
        {loginLogoUrl ? (
          <div className="soha-auth-hero-logo">
            <img src={loginLogoUrl} alt={brandName} className="soha-auth-hero-logo-img" />
          </div>
        ) : (
          <Title level={1} className="soha-auth-flow-title">
            {slogan}
          </Title>
        )}
      </div>

      <div className="soha-auth-flow-map">
        <svg
          className="soha-auth-flow-lines"
          viewBox="0 0 640 360"
          preserveAspectRatio="none"
          aria-hidden="true"
        >
          <defs>
            <linearGradient id="soha-auth-flow-gradient" x1="0" x2="1" y1="0" y2="0">
              <stop offset="0%" stopColor="var(--soha-graph-muted)" stopOpacity="0.18" />
              <stop offset="46%" stopColor="var(--soha-accent-cyan)" stopOpacity="0.72" />
              <stop offset="68%" stopColor="var(--soha-accent-teal)" stopOpacity="0.78" />
              <stop offset="100%" stopColor="var(--soha-graph-muted)" stopOpacity="0.18" />
            </linearGradient>
          </defs>
          <path className="soha-auth-flow-line" d="M80 72 C210 24 250 118 332 116" />
          <path className="soha-auth-flow-line is-delay-1" d="M332 116 C444 114 472 58 562 90" />
          <path className="soha-auth-flow-line is-delay-2" d="M332 116 C278 152 178 142 118 206" />
          <path className="soha-auth-flow-line is-delay-3" d="M118 206 C238 248 332 214 452 254" />
          <path className="soha-auth-flow-line is-delay-1" d="M562 90 C590 156 548 212 452 254" />
          <path className="soha-auth-flow-line is-delay-2" d="M452 254 C384 330 296 338 210 330" />
          <path className="soha-auth-flow-line is-muted" d="M80 72 C120 150 86 190 118 206" />
        </svg>

        {CAPABILITY_QUESTIONS.map((item, index) => (
          <article
            key={item.question}
            className={`soha-auth-flow-node soha-auth-flow-node--${index}`}
          >
            <span className="soha-auth-flow-node__dot" />
            <div>
              <div className="soha-auth-flow-node__question">{item.question}</div>
              <div className="soha-auth-flow-node__answer">{item.answer}</div>
            </div>
          </article>
        ))}
      </div>
    </section>
  )
}

function getProviderLabel(type: string, name: string) {
  if (type === 'oidc') {
    return name || 'OIDC'
  }
  if (type === 'feishu') return name || '飞书'
  if (type === 'dingtalk') return name || '钉钉'
  if (type === 'wecom') return name || '企业微信'
  if (type === 'saml') return name || 'SAML'
  return name || type.toUpperCase()
}

function getProviderIcon(type: string, iconUrl?: string) {
  if (iconUrl) {
    return <img src={iconUrl} alt="" className="soha-auth-provider-button__icon-image" />
  }
  const brandInitials: Record<string, string> = { feishu: '飞', dingtalk: '钉', wecom: '企' }
  if (brandInitials[type]) {
    return (
      <span className={`soha-auth-provider-button__brand is-${type}`}>{brandInitials[type]}</span>
    )
  }
  if (type === 'oidc' || type === 'saml') {
    return <SafetyCertificateOutlined />
  }
  return <UserOutlined />
}

function normalizeSliderValue(value: number | number[]) {
  return Array.isArray(value) ? (value[0] ?? 0) : value
}

function routeLocationPath(
  value:
    | {
        hash?: string
        pathname?: string
        search?: string
      }
    | undefined,
) {
  if (!value?.pathname) {
    return null
  }
  return `${value.pathname}${value.search ?? ''}${value.hash ?? ''}`
}

function providerLoginPath(provider: { id?: string; loginUrl?: string }) {
  if (provider.id) {
    return `${API_BASE_URL}/auth/providers/${encodeURIComponent(provider.id)}/login`
  }
  if (provider.loginUrl) {
    return `${API_BASE_URL}${provider.loginUrl}`
  }
  return null
}

export function LoginPage() {
  const navigate = useNavigate()
  const location = useLocation()
  const { message } = App.useApp()
  const accessToken = useAuthStore((state) => state.accessToken)
  const isAuthenticated = Boolean(accessToken)
  const [loginError, setLoginError] = useState<Error | null>(null)
  useInlineApiError(loginError)
  const loginFence = useRef(false)
  const [loading, setLoading] = useState(false)
  const [providerLoadingKey, setProviderLoadingKey] = useState<string | null>(null)
  const [checkingExistingSession, setCheckingExistingSession] = useState(!isAuthenticated)
  const [sessionRestoreUnavailable, setSessionRestoreUnavailable] = useState(false)
  const [sliderValue, setSliderValue] = useState(0)
  const [sliderVerified, setSliderVerified] = useState(false)
  const themeMode = usePreferencesStore((state) => state.themeMode)
  const setThemeMode = usePreferencesStore((state) => state.setThemeMode)

  const storedBranding = readStoredBrandingSettings()
  const stateReturnTo = normalizeLocalReturnTo(
    routeLocationPath(
      (
        location.state as
          | {
              from?: {
                hash?: string
                pathname?: string
                search?: string
              }
            }
          | undefined
      )?.from,
    ),
  )
  const queryReturnTo = normalizeLocalReturnTo(
    new URLSearchParams(location.search).get('return_to'),
  )
  const returnTo = queryReturnTo ?? stateReturnTo
  const providersQuery = useQuery({
    queryKey: authKeys.providers(),
    queryFn: fetchAuthProviders,
    enabled: !checkingExistingSession && !isAuthenticated,
    staleTime: 60_000,
  })
  const loginOptionsQuery = useQuery({
    queryKey: authKeys.loginOptions(),
    queryFn: fetchLoginOptions,
    enabled: !checkingExistingSession && !isAuthenticated,
    retry: false,
    staleTime: 60_000,
  })
  const loadedBranding = readableQueryData(loginOptionsQuery)?.branding
  const branding = loadedBranding ?? storedBranding
  useEffect(() => {
    if (!loadedBranding) return
    applyBrandingSettings(loadedBranding)
    persistBrandingSettings(loadedBranding)
  }, [loadedBranding])
  const thirdPartyProviders = (readableQueryData(providersQuery) ?? []).filter(
    (item) => item.enabled !== false && item.type !== 'password',
  )
  const appTitle = branding.sidebarTitle || branding.appTitle || 'Soha'
  const resolvedThemeMode = resolveThemeMode(themeMode)
  const logos = resolveBrandingLogos(branding, resolvedThemeMode)
  const sliderVerificationEnabled =
    readableQueryData(loginOptionsQuery)?.verification?.sliderEnabled === true
  const localPasswordLoginEnabled =
    readableQueryData(loginOptionsQuery)?.localPasswordLoginEnabled !== false
  const toggleThemeMode = () => {
    setThemeMode(resolvedThemeMode === 'dark' ? 'light' : 'dark')
  }
  const resolvePostLoginPath = useCallback(async (roles: string[], explicitPath?: string) => {
    if (explicitPath && explicitPath !== '/login') {
      return explicitPath
    }
    try {
      const snapshot = await fetchPermissionSnapshot()
      return (
        findLandingPath(snapshot, usePreferencesStore.getState().currentWorkspace, roles) ?? '/'
      )
    } catch {
      return '/'
    }
  }, [])
  const completeLoginNavigation = useCallback(
    (targetPath: string) => {
      if (shouldUseDocumentNavigation(targetPath)) {
        window.location.assign(appBrowserPath(targetPath))
        return
      }
      navigate(targetPath, { replace: true })
    },
    [navigate],
  )

  useEffect(() => {
    let cancelled = false
    let retryTimer: number | undefined

    const navigateWithCurrentSession = async () => {
      const currentUser = useAuthStore.getState().user
      const nextPath = await resolvePostLoginPath(currentUser?.roles ?? [], returnTo ?? undefined)
      if (!cancelled) {
        completeLoginNavigation(nextPath)
      }
    }

    const restore = async () => {
      if (accessToken) {
        setCheckingExistingSession(false)
        setSessionRestoreUnavailable(false)
        await navigateWithCurrentSession()
        return
      }

      setCheckingExistingSession(true)
      const result = await restoreAuthSession()
      if (cancelled) {
        return
      }

      if (result === 'authenticated') {
        setSessionRestoreUnavailable(false)
        await navigateWithCurrentSession()
        return
      }

      if (result === 'unavailable') {
        setSessionRestoreUnavailable(true)
        retryTimer = window.setTimeout(restore, LOGIN_SESSION_RESTORE_RETRY_MS)
        return
      }

      setSessionRestoreUnavailable(false)
      setCheckingExistingSession(false)
    }

    const unsubscribe = subscribeAuthSessionAvailable(() => {
      if (!useAuthStore.getState().accessToken) {
        if (retryTimer !== undefined) window.clearTimeout(retryTimer)
        void restore()
      }
    })
    void restore()

    return () => {
      cancelled = true
      unsubscribe()
      if (retryTimer !== undefined) {
        window.clearTimeout(retryTimer)
      }
    }
  }, [accessToken, completeLoginNavigation, resolvePostLoginPath, returnTo])

  const resetSliderVerification = () => {
    setSliderValue(0)
    setSliderVerified(false)
  }

  const handleSliderChange = (value: number | number[]) => {
    const nextValue = normalizeSliderValue(value)
    setSliderValue(nextValue)
    if (nextValue < 98 && sliderVerified) {
      setSliderVerified(false)
    }
  }

  const handleSliderComplete = (value: number | number[]) => {
    const nextValue = normalizeSliderValue(value)
    if (nextValue >= 98) {
      setSliderValue(100)
      setSliderVerified(true)
      return
    }
    resetSliderVerification()
  }

  const handleLogin = async (values: LoginFormValues) => {
    if (
      loginFence.current ||
      providerLoadingKey ||
      (!readableQueryData(loginOptionsQuery) &&
        (loginOptionsQuery.isLoading || loginOptionsQuery.isError))
    )
      return
    if (sliderVerificationEnabled && !sliderVerified) {
      message.warning('请先完成滑块验证')
      return
    }

    loginFence.current = true
    setLoginError(null)
    setLoading(true)
    try {
      const authResult = await loginWithPassword(values.username, values.password)
      commitAuthResult(authResult)
      publishAuthSessionAvailable()
      const nextPath = await resolvePostLoginPath(authResult.user.roles, returnTo ?? undefined)
      message.success('登录成功')
      completeLoginNavigation(nextPath)
    } catch (err) {
      if (sliderVerificationEnabled) {
        resetSliderVerification()
      }
      setLoginError(err instanceof Error ? err : new Error('登录失败'))
    } finally {
      loginFence.current = false
      setLoading(false)
    }
  }

  const handleProviderLogin = (provider: {
    id?: string
    type: string
    loginUrl?: string
    name: string
  }) => {
    if (loading || providerLoadingKey) return
    const loginPath = providerLoginPath(provider)
    if (provider.type === 'saml' || !loginPath) {
      message.warning('当前 SAML 登录配置已保存，但服务端回调链路尚未启用。')
      return
    }
    const loginURL = new URL(loginPath, window.location.origin)
    if (returnTo) {
      loginURL.searchParams.set('return_to', appBrowserPath(returnTo))
    }
    const target = `${loginURL.pathname}${loginURL.search}`
    setProviderLoadingKey(target)
    window.location.assign(target)
  }

  return (
    <div className="soha-auth-shell">
      <LoginConstellationBackground />
      <Button
        type="text"
        className="soha-auth-theme-toggle"
        icon={resolvedThemeMode === 'dark' ? <SunOutlined /> : <MoonOutlined />}
        aria-label={resolvedThemeMode === 'dark' ? '切换浅色模式' : '切换深色模式'}
        onClick={toggleThemeMode}
      />

      <div className="soha-auth-layout soha-auth-layout--floating">
        <LoginCapabilityFlow
          brandName={appTitle}
          loginLogoUrl={logos.expandedLogoUrl}
          slogan={branding.slogan || 'Soha 是一种能力！'}
        />

        <Card className="soha-auth-panel soha-auth-panel--floating" variant="borderless">
          <div className="soha-auth-panel-inner">
            <div className="soha-auth-brand">
              <img
                src={logos.collapsedLogoUrl || appBrowserPath('/logo.svg')}
                alt=""
                className="soha-auth-brand-logo-img"
              />
              <div className="soha-auth-brand-copy">
                <Title level={4} style={{ margin: 0 }}>
                  {appTitle}
                </Title>
                <Text type="secondary">多工作台统一控制台</Text>
              </div>
            </div>

            <div className="soha-auth-panel-copy">
              <Title level={5} style={{ marginTop: 0, marginBottom: 4 }}>
                {checkingExistingSession ? '恢复登录状态' : '登录控制台'}
              </Title>
              {checkingExistingSession ? (
                <Text type="secondary">
                  {sessionRestoreUnavailable
                    ? '后端服务暂时不可用，正在自动重试。'
                    : '正在检查当前浏览器会话。'}
                </Text>
              ) : null}
            </div>

            {checkingExistingSession ? (
              <div className="soha-auth-session-restore">
                <Spin />
              </div>
            ) : (
              <>
                <ReadFeedback query={loginOptionsQuery} label="登录选项" />
                <ReadFeedback query={providersQuery} label="第三方登录" />
                {loginError ? (
                  <Alert showIcon type="error" title="登录失败" description={loginError.message} />
                ) : null}
                {localPasswordLoginEnabled ? (
                  <Form<LoginFormValues>
                    disabled={
                      loading ||
                      Boolean(providerLoadingKey) ||
                      (!readableQueryData(loginOptionsQuery) &&
                        (loginOptionsQuery.isLoading || loginOptionsQuery.isError))
                    }
                    layout="vertical"
                    onFinish={handleLogin}
                  >
                    <Form.Item<LoginFormValues>
                      name="username"
                      label="用户名"
                      rules={[{ required: true, message: '请输入用户名' }]}
                    >
                      <Input
                        prefix={<UserOutlined />}
                        placeholder="请输入用户名"
                        allowClear
                        size="middle"
                      />
                    </Form.Item>
                    <Form.Item<LoginFormValues>
                      name="password"
                      label="密码"
                      rules={[{ required: true, message: '请输入密码' }]}
                    >
                      <Input.Password
                        prefix={<LockOutlined />}
                        placeholder="请输入密码"
                        size="middle"
                      />
                    </Form.Item>

                    {sliderVerificationEnabled ? (
                      <Form.Item>
                        <div className={`soha-auth-slider ${sliderVerified ? 'is-verified' : ''}`}>
                          <div className="soha-auth-slider-label">
                            <span className="soha-auth-slider-label__icon">
                              {sliderVerified ? (
                                <CheckCircleOutlined />
                              ) : (
                                <SafetyCertificateOutlined />
                              )}
                            </span>
                            <span>{sliderVerified ? '验证通过' : '拖动滑块完成验证'}</span>
                          </div>
                          <div className="soha-auth-slider-track-shell">
                            <span className="soha-auth-slider-shine" />
                            <span className="soha-auth-slider-track-copy">
                              {sliderVerified ? '身份环境检查完成' : '按住滑块向右拖动'}
                            </span>
                          </div>
                          <Slider
                            className="soha-auth-slider-control"
                            disabled={sliderVerified || loading}
                            max={100}
                            min={0}
                            onChange={handleSliderChange}
                            onChangeComplete={handleSliderComplete}
                            step={1}
                            tooltip={{ formatter: null }}
                            value={sliderValue}
                          />
                          <div className="soha-auth-slider-footer">
                            <span>{sliderVerified ? '可提交登录' : '滑到最右侧后解锁登录'}</span>
                            <span>{sliderVerified ? '100%' : `${Math.round(sliderValue)}%`}</span>
                          </div>
                        </div>
                      </Form.Item>
                    ) : null}

                    <Button
                      type="primary"
                      htmlType="submit"
                      loading={loading}
                      block
                      disabled={sliderVerificationEnabled && !sliderVerified}
                      className="soha-auth-submit"
                    >
                      登录控制台
                    </Button>
                  </Form>
                ) : null}

                {thirdPartyProviders.length > 0 ? (
                  <div className="soha-auth-provider-slot">
                    <Divider style={{ marginBlock: localPasswordLoginEnabled ? 18 : 0 }}>
                      <Text type="secondary">第三方登录</Text>
                    </Divider>

                    <div className="soha-auth-provider-list">
                      {thirdPartyProviders.map((provider) => (
                        <Button
                          key={`${provider.type}-${provider.id ?? provider.name}`}
                          block
                          disabled={loading || Boolean(providerLoadingKey)}
                          loading={Boolean(
                            providerLoginPath(provider) &&
                            providerLoadingKey?.startsWith(providerLoginPath(provider) ?? ''),
                          )}
                          onClick={() => handleProviderLogin(provider)}
                          className="soha-auth-provider-button"
                        >
                          <span className="soha-auth-provider-button__content">
                            <span className="soha-auth-provider-button__icon">
                              {provider.type === 'saml' && !provider.loginUrl ? (
                                <WarningOutlined />
                              ) : (
                                getProviderIcon(provider.type, provider.iconUrl)
                              )}
                            </span>
                            <span className="soha-auth-provider-button__label">
                              {getProviderLabel(provider.type, provider.name)}
                            </span>
                          </span>
                        </Button>
                      ))}
                    </div>
                  </div>
                ) : null}
              </>
            )}
          </div>
        </Card>
      </div>
      <div className="soha-auth-copyright">{LOGIN_COPYRIGHT_TEXT}</div>
    </div>
  )
}
