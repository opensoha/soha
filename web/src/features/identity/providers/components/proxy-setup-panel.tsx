import { App, Button, Input, Tabs, Tag, Typography } from 'antd'
import { CopyOutlined } from '@ant-design/icons'
import { useI18n } from '@/i18n'
import type { IdentityProvider, IdentityProviderSetup } from '../types'
import {
  proxySetupContext,
  proxySetupSnippet,
  proxySetupTargets,
  type ProxySetupTarget,
} from '../proxy-setup-model'

const { Paragraph, Text } = Typography

const targetLabels: Record<ProxySetupTarget, string> = {
  'nginx-ingress': 'Nginx (Ingress)',
  'nginx-proxy-manager': 'Nginx (Proxy Manager)',
  'nginx-standalone': 'Nginx (Standalone)',
  'traefik-ingress': 'Traefik (Ingress)',
  'traefik-compose': 'Traefik (Compose)',
  'traefik-standalone': 'Traefik (Standalone)',
  'caddy-standalone': 'Caddy (Standalone)',
}

export function ProxySetupPanel({
  provider,
  setup,
}: {
  provider: IdentityProvider
  setup: IdentityProviderSetup
}) {
  const { message } = App.useApp()
  const { t } = useI18n()
  const context = proxySetupContext(provider, setup)
  if (!context) return null
  const endpoint = context.authURL

  const copy = async (value: string) => {
    await navigator.clipboard.writeText(value)
    void message.success(t('identity.proxySetup.copied', '已复制配置'))
  }

  return (
    <div className="soha-proxy-setup-panel">
      <div className="soha-proxy-setup-heading">
        <div>
          <Text strong>{t('identity.proxySetup.title', '接入配置')}</Text>
          <Paragraph type="secondary">
            {t(
              'identity.proxySetup.edgeDescription',
              '边缘代理转发业务流量，并向下列地址鉴权。请替换模板中的业务上游，并按部署环境配置 TLS。',
            )}
            {setup.requiresOutpostToken ? (
              <Text>
                远程节点需要独立的 Agent HTTP token；请在代理端注入凭据占位符。它与节点向 Soha
                注册的 token 不同。共享登录需配置受信任的 cookie 域和回跳地址。
              </Text>
            ) : null}
          </Paragraph>
        </div>
        <Tag color="blue">forward-auth</Tag>
      </div>
      <div className="soha-proxy-endpoint-row">
        <Input readOnly value={endpoint} />
        <Button
          aria-label={t('identity.proxySetup.copyEndpoint', '复制认证地址')}
          icon={<CopyOutlined />}
          onClick={() => copy(endpoint)}
        />
      </div>
      {
        <Tabs
          items={proxySetupTargets.map((target) => {
            const snippet = proxySetupSnippet(target, context)
            return {
              key: target,
              label: targetLabels[target],
              children: (
                <div className="soha-proxy-snippet">
                  <div className="soha-proxy-snippet-toolbar">
                    <Text type="secondary">
                      {t('identity.proxySetup.generated', '已按当前 Provider 参数生成')}
                    </Text>
                    <Button icon={<CopyOutlined />} size="small" onClick={() => copy(snippet)}>
                      {t('common.copy', '复制')}
                    </Button>
                  </div>
                  <pre>{snippet}</pre>
                </div>
              ),
            }
          })}
          size="small"
        />
      }
    </div>
  )
}
