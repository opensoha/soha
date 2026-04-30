import { useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { Alert, App, Button, Divider, Form, Input, Typography } from 'antd'
import { LockOutlined, UserOutlined } from '@ant-design/icons'
import { api } from '@/services/api-client'
import { useAuthStore } from '@/stores/auth-store'
import { readStoredBrandingSettings } from '@/utils/branding'
import type { ApiResponse, AuthResult } from '@/types'

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
  const { setTokens, setUser } = useAuthStore()
  const [loading, setLoading] = useState(false)
  const [oidcLoading, setOidcLoading] = useState(false)

  const branding = readStoredBrandingSettings()
  const from = (location.state as { from?: { pathname: string } })?.from?.pathname ?? '/'

  const handleLogin = async (values: LoginFormValues) => {
    setLoading(true)
    try {
      const authRes = await api.post<ApiResponse<AuthResult>>('/auth/login', {
        login: values.username,
        password: values.password,
      })
      setTokens(authRes.data.tokens.accessToken, authRes.data.tokens.refreshToken)
      setUser(authRes.data.user)
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
      <div className="kc-auth-layout">
        <section className="kc-auth-hero">
          {branding.loginLogoUrl ? (
            <div className="kc-auth-hero-logo">
              <img src={branding.loginLogoUrl} alt={branding.appTitle || 'Logo'} className="kc-auth-hero-logo-img" />
            </div>
          ) : null}
          <span className="kc-auth-pill">Ant Design Console</span>
          <Title level={1} style={{ marginTop: 18, marginBottom: 12 }}>
            面向多集群平台运营的
            <br />
            {branding.sidebarTitle || 'KubeCrux'} 控制台
          </Title>
          <Text style={{ fontSize: 16, lineHeight: 1.8 }}>
            把多集群、交付、观测、权限与 AI 助手收在一个统一的运维操作界面里，
            以清晰的信息架构和更标准化的组件语义承接日常平台工作流。
          </Text>

          <div className="kc-auth-feature-grid">
            <div className="kc-auth-feature">
              <span className="kc-auth-feature-title">平台治理</span>
              <Text type="secondary">聚合管理集群、工作负载、网络与存储资源。</Text>
            </div>
            <div className="kc-auth-feature">
              <span className="kc-auth-feature-title">交付协同</span>
              <Text type="secondary">应用、工作流、发布与仓库配置走同一条操作路径。</Text>
            </div>
            <div className="kc-auth-feature">
              <span className="kc-auth-feature-title">观测与 AI</span>
              <Text type="secondary">监控、告警、事件和 Copilot 共用统一交互反馈。</Text>
            </div>
            <div className="kc-auth-feature">
              <span className="kc-auth-feature-title">标准化组件</span>
              <Text type="secondary">前端已切换到 antd 体系，减少非标准组件语义负担。</Text>
            </div>
          </div>
        </section>

        <section className="kc-auth-panel">
          <div className="kc-auth-panel-inner">
            <div className="kc-auth-brand">
              {branding.expandedLogoUrl ? (
                <img src={branding.expandedLogoUrl} alt={branding.sidebarTitle || 'Logo'} className="kc-auth-brand-logo-img" />
              ) : (
                <div className="kc-auth-mark">KC</div>
              )}
              <div className="kc-auth-brand-copy">
                <Title level={3} style={{ margin: 0 }}>{branding.sidebarTitle || 'KubeCrux'}</Title>
                <Text type="secondary">Kubernetes 多集群管理平台</Text>
              </div>
            </div>

            <Form<LoginFormValues> layout="vertical" onFinish={handleLogin}>
              <Form.Item<LoginFormValues>
                name="username"
                label="用户名"
                rules={[{ required: true, message: '请输入用户名' }]}
              >
                <Input prefix={<UserOutlined />} placeholder="请输入用户名" allowClear />
              </Form.Item>
              <Form.Item<LoginFormValues>
                name="password"
                label="密码"
                rules={[{ required: true, message: '请输入密码' }]}
              >
                <Input.Password prefix={<LockOutlined />} placeholder="请输入密码" />
              </Form.Item>

              <Button type="primary" htmlType="submit" loading={loading} block className="mt-4">
                登录控制台
              </Button>
            </Form>

            <Divider style={{ marginBlock: 24 }}>
              <Text type="secondary">或</Text>
            </Divider>

            <Button block loading={oidcLoading} onClick={handleOIDCLogin}>
              OIDC 单点登录
            </Button>

            <Alert
              className="mt-4"
              title="建议使用管理员账号进入后，继续调整平台菜单、品牌配置与接入信息。"
              type="info"
              showIcon
            />
          </div>
        </section>
      </div>
    </div>
  )
}
