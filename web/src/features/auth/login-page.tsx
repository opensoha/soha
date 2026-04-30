import { useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { App, Button, Card, Divider, Form, Input, Space, Typography } from 'antd'
import { LockOutlined, SafetyCertificateOutlined, UserOutlined } from '@ant-design/icons'
import { commitAuthResult, fetchAuthProviders, loginWithPassword } from '@/features/auth/auth-api'
import { readStoredBrandingSettings } from '@/utils/branding'

const { Title, Text } = Typography

interface LoginFormValues {
  password: string
  username: string
}

function getProviderLabel(type: string, name: string) {
  if (type === 'oidc') {
    return name || 'OIDC'
  }
  return name || type.toUpperCase()
}

function getProviderIcon(type: string) {
  if (type === 'oidc') {
    return <SafetyCertificateOutlined />
  }
  return <UserOutlined />
}

export function LoginPage() {
  const navigate = useNavigate()
  const location = useLocation()
  const { message } = App.useApp()
  const [loading, setLoading] = useState(false)
  const [oidcLoading, setOidcLoading] = useState(false)

  const branding = readStoredBrandingSettings()
  const from = (location.state as { from?: { pathname: string } })?.from?.pathname ?? '/'
  const providersQuery = useQuery({
    queryKey: ['auth-providers'],
    queryFn: fetchAuthProviders,
    staleTime: 60_000,
  })
  const thirdPartyProviders = (providersQuery.data ?? []).filter((item) => item.enabled !== false && item.type === 'oidc')
  const appTitle = branding.sidebarTitle || branding.appTitle || 'KubeCrux'

  const handleLogin = async (values: LoginFormValues) => {
    setLoading(true)
    try {
      const authResult = await loginWithPassword(values.username, values.password)
      commitAuthResult(authResult)
      message.success('登录成功')
      navigate(from, { replace: true })
    } catch (err: any) {
      message.error(err?.message ?? '登录失败')
    } finally {
      setLoading(false)
    }
  }

  const handleOIDCLogin = () => {
    setOidcLoading(true)
    window.location.href = '/api/v1/auth/oidc/login'
  }

  return (
    <div className="kc-auth-shell">
      <div className="kc-auth-layout kc-auth-layout--pro">
        <section className="kc-auth-hero kc-auth-hero--pro">
          <Space direction="vertical" size={20}>
            {branding.loginLogoUrl ? (
              <div className="kc-auth-hero-logo">
                <img src={branding.loginLogoUrl} alt={branding.appTitle || 'Logo'} className="kc-auth-hero-logo-img" />
              </div>
            ) : null}
            <span className="kc-auth-pill">KubeCrux Console</span>
            <div>
              <Title level={1} style={{ marginTop: 0, marginBottom: 12 }}>
                平台、交付、观测与权限
                <br />
                在同一个控制台里协同运行
              </Title>
              <Text style={{ fontSize: 16, lineHeight: 1.8 }}>
                {appTitle} 以统一的 antd 控制台壳承载平台、交付、观测与权限能力，
                保持当前 Vite 前端和 Gin 后端的稳定协同方式。
              </Text>
            </div>

            <div className="kc-auth-feature-grid">
              <div className="kc-auth-feature">
                <span className="kc-auth-feature-title">多集群平台治理</span>
                <Text type="secondary">聚合查看集群、工作负载、网络、存储与扩展能力。</Text>
              </div>
              <div className="kc-auth-feature">
                <span className="kc-auth-feature-title">交付与策略协同</span>
                <Text type="secondary">环境、流程、发布、权限和菜单策略沿同一信息架构运行。</Text>
              </div>
              <div className="kc-auth-feature">
                <span className="kc-auth-feature-title">统一运维反馈</span>
                <Text type="secondary">观测、告警、事件和 AI 分析在同一操作上下文中联动。</Text>
              </div>
              <div className="kc-auth-feature">
                <span className="kc-auth-feature-title">统一控制台体验</span>
                <Text type="secondary">导航、主题、权限和业务工作区共享同一套控制台结构与运行时上下文。</Text>
              </div>
            </div>
          </Space>
        </section>

        <Card className="kc-auth-panel kc-auth-panel--pro" bordered={false}>
          <div className="kc-auth-panel-inner">
            <div className="kc-auth-brand">
              {branding.expandedLogoUrl ? (
                <img src={branding.expandedLogoUrl} alt={branding.sidebarTitle || 'Logo'} className="kc-auth-brand-logo-img" />
              ) : (
                <div className="kc-auth-mark">KC</div>
              )}
              <div className="kc-auth-brand-copy">
                <Title level={3} style={{ margin: 0 }}>{appTitle}</Title>
                <Text type="secondary">Kubernetes 多集群管理平台</Text>
              </div>
            </div>

            <div className="kc-auth-panel-copy">
              <Title level={4} style={{ marginTop: 0, marginBottom: 8 }}>登录控制台</Title>
              <Text type="secondary">
                使用本地账号或单点登录进入当前工作区。认证成功后将返回你原本访问的页面。
              </Text>
            </div>

            <Form<LoginFormValues> layout="vertical" onFinish={handleLogin}>
              <Form.Item<LoginFormValues>
                name="username"
                label="用户名"
                rules={[{ required: true, message: '请输入用户名' }]}
              >
                <Input prefix={<UserOutlined />} placeholder="请输入用户名" allowClear size="large" />
              </Form.Item>
              <Form.Item<LoginFormValues>
                name="password"
                label="密码"
                rules={[{ required: true, message: '请输入密码' }]}
              >
                <Input.Password prefix={<LockOutlined />} placeholder="请输入密码" size="large" />
              </Form.Item>

              <Button type="primary" htmlType="submit" loading={loading} block size="large" className="mt-4">
                登录控制台
              </Button>
            </Form>

            <div className="kc-auth-provider-slot" aria-hidden={thirdPartyProviders.length === 0}>
              {thirdPartyProviders.length > 0 ? (
                <>
                  <Divider style={{ marginBlock: 24 }}>
                    <Text type="secondary">第三方登录</Text>
                  </Divider>

                  <div className="kc-auth-provider-list">
                    {thirdPartyProviders.map((provider) => (
                      <Button
                        key={provider.type}
                        block
                        size="large"
                        loading={oidcLoading}
                        onClick={handleOIDCLogin}
                        className="kc-auth-provider-button"
                      >
                        <span className="kc-auth-provider-button__content">
                          <span className="kc-auth-provider-button__icon">
                            {getProviderIcon(provider.type)}
                          </span>
                          <span className="kc-auth-provider-button__label">
                            使用 {getProviderLabel(provider.type, provider.name)} 登录
                          </span>
                        </span>
                      </Button>
                    ))}
                  </div>
                </>
              ) : null}
            </div>
          </div>
        </Card>
      </div>
    </div>
  )
}
