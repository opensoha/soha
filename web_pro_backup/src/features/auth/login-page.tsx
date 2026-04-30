import { useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { Alert, App, Button, Card, Divider, Form, Input, Space, Typography } from 'antd'
import { LockOutlined, SafetyCertificateOutlined, UserOutlined } from '@ant-design/icons'
import { commitAuthResult, fetchAuthProviders, loginWithPassword } from '@/features/auth/auth-api'
import { readStoredBrandingSettings } from '@/utils/branding'

const { Title, Text } = Typography

const BASE_URL = '/api/v1'

interface LoginFormValues {
  password: string
  username: string
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
  const hasOIDCProvider = (providersQuery.data ?? []).some((item) => item.type === 'oidc' && item.enabled !== false)
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
    window.location.href = `${BASE_URL}/auth/oidc/login`
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
            <span className="kc-auth-pill">Ant Design Pro Console</span>
            <div>
              <Title level={1} style={{ marginTop: 0, marginBottom: 12 }}>
                平台、交付、观测与权限
                <br />
                在同一个控制台里协同运行
              </Title>
              <Text style={{ fontSize: 16, lineHeight: 1.8 }}>
                {appTitle} 正在迁移到 ant-design-pro 脚手架基线。登录页与业务工作台保持现有行为，
                同时向更统一的页面容器、导航语义与运行时结构收敛。
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
                <span className="kc-auth-feature-title">Pro 容器收敛</span>
                <Text type="secondary">页面逐步对齐更标准的 Pro 布局与内容容器，而不是保留旧壳层样式。</Text>
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

            {hasOIDCProvider ? (
              <>
                <Divider style={{ marginBlock: 24 }}>
                  <Text type="secondary">或</Text>
                </Divider>

                <Button block loading={oidcLoading} onClick={handleOIDCLogin} size="large" icon={<SafetyCertificateOutlined />}>
                  OIDC 单点登录
                </Button>
              </>
            ) : null}

            <Alert
              className="mt-4"
              message="管理员首次进入后，可继续调整品牌、菜单、身份接入和权限策略。"
              type="info"
              showIcon
            />
          </div>
        </Card>
      </div>
    </div>
  )
}
