import { useState } from 'react'
import { useNavigate, useLocation } from 'react-router-dom'
import { Form, Button, Toast, Divider, Typography } from '@douyinfe/semi-ui'
import { IconLock, IconUser } from '@douyinfe/semi-icons'
import { api } from '@/services/api-client'
import { useAuthStore } from '@/stores/auth-store'
import type { ApiResponse, AuthResult } from '@/types'

const { Title, Text } = Typography

const BASE_URL = '/api/v1'

export function LoginPage() {
  const navigate = useNavigate()
  const location = useLocation()
  const { setTokens, setUser } = useAuthStore()
  const [loading, setLoading] = useState(false)
  const [oidcLoading, setOidcLoading] = useState(false)

  const from = (location.state as { from?: { pathname: string } })?.from?.pathname ?? '/'

  const handleLogin = async (values: { username: string; password: string }) => {
    setLoading(true)
    try {
      const authRes = await api.post<ApiResponse<AuthResult>>(
        '/auth/login',
        { login: values.username, password: values.password },
      )
      setTokens(authRes.data.tokens.accessToken, authRes.data.tokens.refreshToken)
      setUser(authRes.data.user)

      Toast.success('登录成功')
      navigate(from, { replace: true })
    } catch (err: any) {
      Toast.error(err?.message ?? '登录失败')
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
          <span className="kc-auth-pill">Semi Official Themes</span>
          <Title heading={1} style={{ marginTop: 18, marginBottom: 12 }}>
            多套官方主题可切换的
            <br />
            KubeCrux 控制台
          </Title>
          <Text style={{ fontSize: 16, lineHeight: 1.8 }}>
            把多集群、交付、观测、权限与 AI 助手收在一个更统一的创作服务式界面里。
            现在已接入 Semi 官方主题包，并支持在运行时切换官方品牌主题与明暗模式。
          </Text>

          <div className="kc-auth-feature-grid">
            <div className="kc-auth-feature">
              <span className="kc-auth-feature-title">平台治理</span>
              <Text type="tertiary">集群、工作负载、网络与存储使用更统一的面板和列表语言。</Text>
            </div>
            <div className="kc-auth-feature">
              <span className="kc-auth-feature-title">交付协同</span>
              <Text type="tertiary">应用、工作流、发布与镜像仓库放在同一条业务操作路径里。</Text>
            </div>
            <div className="kc-auth-feature">
              <span className="kc-auth-feature-title">观测与 AI</span>
              <Text type="tertiary">监控、告警、事件和 Copilot 共用同一套品牌化反馈设计。</Text>
            </div>
            <div className="kc-auth-feature">
              <span className="kc-auth-feature-title">Semi 主题化</span>
              <Text type="tertiary">支持抖音创作服务、抖音、飞书、火山引擎与 A11y 官方主题切换。</Text>
            </div>
          </div>
        </section>

        <section className="kc-auth-panel">
          <div className="kc-auth-panel-inner">
            <div className="kc-auth-brand">
              <div className="kc-auth-mark">KC</div>
              <div className="kc-auth-brand-copy">
                <Title heading={3} style={{ margin: 0 }}>KubeCrux</Title>
                <Text type="tertiary">Kubernetes 多集群管理平台</Text>
              </div>
            </div>

            <Form onSubmit={handleLogin} labelPosition="inset">
              <Form.Input
                field="username"
                label="用户名"
                placeholder="请输入用户名"
                prefix={<IconUser />}
                rules={[{ required: true, message: '请输入用户名' }]}
                showClear
              />
              <Form.Input
                field="password"
                label="密码"
                placeholder="请输入密码"
                mode="password"
                prefix={<IconLock />}
                rules={[{ required: true, message: '请输入密码' }]}
              />

              <Button
                type="primary"
                theme="solid"
                htmlType="submit"
                loading={loading}
                block
                className="mt-4"
              >
                登录控制台
              </Button>
            </Form>

            <Divider margin={24}>
              <Text type="tertiary" size="small">或</Text>
            </Divider>

            <Button
              theme="light"
              block
              loading={oidcLoading}
              onClick={handleOIDCLogin}
            >
              OIDC 单点登录
            </Button>

            <div className="kc-auth-hint">
              建议使用管理员账号进入后，继续调整平台菜单、品牌配置与接入信息。
            </div>
          </div>
        </section>
      </div>
    </div>
  )
}
