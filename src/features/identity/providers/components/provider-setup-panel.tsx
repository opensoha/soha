import { ManagementIconButton, ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { useI18n } from '@/i18n'
import { isApiError } from '@/services/api-error'
import { readableQueryData } from '@/utils/query-feedback'
import { InfoCircleOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { Alert, Button, Descriptions, Space, Typography } from 'antd'
import { identityProviderQueries } from '../queries'
import type { IdentityProvider } from '../types'
import { ProxySetupPanel } from './proxy-setup-panel'
const issues: Record<string, string> = {
  public_url_unconfigured: '尚未设置 Soha 公开访问地址，请在系统设置中配置。',
  remote_public_url_requires_https: '远程节点的登录回跳要求 Soha 公开访问地址使用 HTTPS。',
  outpost_identity_header_unsupported:
    '远程节点只支持默认 X-Soha 身份头及 X-Auth-Request-User、Email、Groups。请调整身份头映射。',
  oidc_client_missing: '尚未创建 OIDC 客户端。',
  saml_sp_configuration_incomplete: 'SAML 服务提供方配置不完整。',
  proxy_external_host_missing: '尚未配置受保护域名。',
  outpost_missing: '绑定的接入节点不存在，请重新选择。',
  outpost_forward_auth_url_missing: '请为远程节点配置完整的 Forward Auth 地址。',
  signing_key_unconfigured: '控制面尚未配置远程节点的签名密钥。',
}
const endpointLabels = {
  issuer: 'Issuer',
  discoveryUrl: 'OpenID configuration',
  authorizationUrl: 'Authorization endpoint',
  tokenUrl: 'Token endpoint',
  userInfoUrl: 'UserInfo endpoint',
  jwksUrl: 'Signing public keys (JWKS)',
  logoutUrl: 'Logout endpoint',
  samlMetadataUrl: 'SAML Metadata',
  samlSSOUrl: 'SAML SSO',
}
const endpointKeys = Object.keys(endpointLabels) as (keyof typeof endpointLabels)[]
export function ProviderSetupPanel({ provider }: { provider: IdentityProvider }) {
  const { t } = useI18n()
  const query = useQuery(identityProviderQueries.setup(provider.id))
  const visibleQueryData = query.isEnabled === false ? undefined : readableQueryData(query)
  if (query.isPending) return <ManagementState kind="loading" compact />
  if (!visibleQueryData)
    return (
      <>
        <ReadFeedback canRead={query.isEnabled} query={query} label="接入配置" />
        <Button
          disabled={query.isEnabled === false}
          loading={query.isFetching}
          onClick={() => void query.refetch()}
        >
          重新读取配置
        </Button>
      </>
    )
  const setup = visibleQueryData
  return (
    <div className="soha-identity-provider-setup">
      <ReadFeedback canRead={query.isEnabled} query={query} label="当前记录" />

      <div className="soha-identity-provider-setup-summary">
        <Space size={4} wrap>
          <StatusTag
            value={setup.configurationStatus === 'complete' ? 'success' : 'warning'}
            label={t(
              `identity.providers.setup.${setup.configurationStatus === 'complete' ? 'complete' : 'incomplete'}`,
            )}
          />
          <ManagementIconButton
            aria-label={t('identity.providers.setup.statusHelp')}
            tooltip={t('identity.providers.setup.statusHint')}
            icon={<InfoCircleOutlined />}
          />
          <Button
            disabled={query.isEnabled === false}
            loading={query.isFetching}
            onClick={() => void query.refetch()}
          >
            重新读取配置
          </Button>
        </Space>
      </div>
      <Alert
        showIcon
        type="info"
        title="真实接入验证需在业务应用中完成"
        description="此处检查已保存配置的完整性，不执行真实登录。请在业务应用验证登录、无权拒绝和注销后的会话；修改回调、证书或网关配置后需要重新验证。此页面不保存验证结果。"
      />
      {setup.issues.map((issue) => (
        <Typography.Paragraph key={issue} type="warning">
          {issues[issue] ?? issue}
        </Typography.Paragraph>
      ))}
      {setup.migrationRequired ? (
        <Alert
          showIcon
          type="warning"
          title="Legacy Core Reverse Proxy：仍需迁移"
          description={
            <>
              {setup.endpoints.legacyReverseProxyUrl && (
                <Typography.Paragraph>
                  当前旧入口：
                  <Typography.Text copyable>
                    {setup.endpoints.legacyReverseProxyUrl}
                  </Typography.Text>
                </Typography.Paragraph>
              )}
              <ol>
                <li>保留原配置与旧入口，在独立入口部署以下 Forward Auth 配置。</li>
                <li>旁路验证登录、无权拒绝、伪造身份头、Cookie、长连接及节点断联。</li>
                <li>验证后再切换业务入口并观察；失败时恢复原入口和配置，重新核验当前权限。</li>
              </ol>
              读取或复制模板不会完成迁移，也不会关闭旧入口；切换与回退由应用和网关管理员执行。
            </>
          }
        />
      ) : null}
      {provider.type === 'proxy' ? (
        <ProxySetupPanel provider={provider} setup={setup} />
      ) : (
        <Descriptions
          bordered
          column={1}
          size="small"
          styles={{ label: { width: 160 }, content: { overflowWrap: 'anywhere' } }}
          items={endpointKeys
            .filter((key) => setup.endpoints[key])
            .map((key) => ({
              key,
              label: t(`identity.providers.endpoint.${key}`, endpointLabels[key]),
              children: (
                <Typography.Text copyable={{ text: setup.endpoints[key] }}>
                  {setup.endpoints[key]}
                </Typography.Text>
              ),
            }))}
        />
      )}
    </div>
  )
}
export function ProviderConfigurationStatus({ providerId }: { providerId: string }) {
  const { t } = useI18n()
  const query = useQuery(identityProviderQueries.setup(providerId))
  const visibleQueryData = query.isEnabled === false ? undefined : readableQueryData(query)
  if (query.isPending) return <Typography.Text type="secondary">检查中</Typography.Text>
  if (query.isError)
    return (
      <Typography.Text type="danger">
        {isApiError(query.error) && query.error.status === 403 ? '无查看权限' : '检查失败'}
      </Typography.Text>
    )
  if (!visibleQueryData) return <Typography.Text type="secondary">状态不可用</Typography.Text>
  const setup = visibleQueryData
  return (
    <Typography.Text type={setup.configurationStatus === 'complete' ? 'secondary' : 'warning'}>
      {t(
        `identity.providers.setup.${setup.configurationStatus === 'complete' ? 'complete' : 'incomplete'}`,
      )}
    </Typography.Text>
  )
}
