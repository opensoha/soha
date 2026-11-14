import { MetadataTag } from '@/components/status-tag'
import { Card, Collapse, Descriptions, Space, Typography } from 'antd'
import type { PluginManifest, PluginPermissionRequest } from '../plugin-model'
import { requiredSecretValues } from '../plugin-model'
import { compactPluginTags, pluginJsonBlock } from './formatters'
import { ExtensionPointList } from './extension-list'

const { Text } = Typography
const groupLabels: Record<string, string> = {
  skills: 'Skills',
  mcpPresets: 'MCP Presets',
  connectors: '连接器',
  aiProviderAdapters: 'AI Provider 适配器',
  agentProfiles: 'Agent Profiles',
  gatewayPolicyPacks: 'Gateway 策略包',
  tools: '工具',
  resources: '资源',
  prompts: '提示词',
}

export function PermissionReview({
  permissions,
}: {
  permissions?: PluginPermissionRequest | null
}) {
  const required = permissions?.required ?? []
  const domain = permissions?.domain ?? []
  if (!required.length && !domain.length) {
    return <Text type="secondary">未声明权限需求</Text>
  }
  return (
    <Space orientation="vertical" size={8} className="soha-plugin-values">
      {required.length ? (
        <div>
          <Text type="secondary">Gateway</Text>
          <div>{compactPluginTags(required, required.length)}</div>
        </div>
      ) : null}
      {domain.length ? (
        <div>
          <Text type="secondary">Domain</Text>
          <div>{compactPluginTags(domain, domain.length)}</div>
        </div>
      ) : null}
    </Space>
  )
}

export function PluginManifestSections({ manifest }: { manifest?: PluginManifest | null }) {
  if (!manifest) return null
  const secrets = requiredSecretValues(manifest.secrets)
  const content = [
    { title: '资产文件', values: manifest.assets },
    { title: '声明能力', values: manifest.capabilities },
  ]
  return (
    <>
      <div className="soha-plugin-detail-grid">
        <Card title={<h2 className="soha-plugin-section-title">扩展与资产</h2>} size="small">
          <section className="soha-plugin-section">
            <h3>扩展点</h3>
            <ExtensionPointList points={manifest.extensionPoints} />
          </section>
          {content.map(({ title, values }) => (
            <section className="soha-plugin-section" key={title}>
              <h3>{title}</h3>
              {Object.values(values ?? {}).some((items) => items.length) ? (
                <dl className="soha-plugin-value-groups">
                  {Object.entries(values ?? {})
                    .filter(([, items]) => items.length)
                    .map(([key, items]) => (
                      <div key={key}>
                        <dt>{groupLabels[key] ?? key}</dt>
                        <dd>
                          <ul>
                            {items.map((item: string) => (
                              <li key={item}>{item}</li>
                            ))}
                          </ul>
                        </dd>
                      </div>
                    ))}
                </dl>
              ) : (
                <Text type="secondary">未声明{title}</Text>
              )}
            </section>
          ))}
        </Card>
        <Card title={<h2 className="soha-plugin-section-title">安装要求</h2>} size="small">
          <section className="soha-plugin-section">
            <h3>运行与兼容性</h3>
            <Descriptions
              column={1}
              size="small"
              items={[
                {
                  key: 'runtime',
                  label: '运行方式',
                  children: manifest.runtime?.mode || 'manifest-only',
                },
                { key: 'soha', label: 'Soha', children: manifest.compatibility?.soha || '未声明' },
                {
                  key: 'contracts',
                  label: 'Contracts',
                  children: manifest.compatibility?.contracts || '未声明',
                },
                { key: 'homepage', label: '主页', children: manifest.homepage || '未声明' },
              ]}
            />
          </section>
          <section className="soha-plugin-section">
            <h3>申请权限</h3>
            <PermissionReview permissions={manifest.permissions} />
            <p className="soha-plugin-permission-note">安装不会自动授予访问权限。</p>
          </section>
          <section className="soha-plugin-section">
            <h3>密钥需求</h3>
            {secrets.length ? (
              <ul className="soha-plugin-secret-list">
                {secrets.map((item) => (
                  <li key={item.name}>
                    <Space wrap size={6}>
                      <Text strong>{item.name}</Text>
                      <MetadataTag
                        label={item.required === false ? '可选' : '必需'}
                        tone={item.required === false ? 'default' : 'gold'}
                      />
                    </Space>
                    {item.description ? (
                      <div>
                        <Text type="secondary">{item.description}</Text>
                      </div>
                    ) : null}
                  </li>
                ))}
              </ul>
            ) : (
              <Text type="secondary">无需提供密钥</Text>
            )}
          </section>
        </Card>
      </div>
      <Collapse
        size="small"
        items={[
          { key: 'manifest', label: '原始 Manifest', children: pluginJsonBlock(manifest) },
          {
            key: 'schema',
            label: '配置 Schema',
            children: manifest.configSchema ? (
              pluginJsonBlock(manifest.configSchema)
            ) : (
              <Text type="secondary">未声明配置 Schema</Text>
            ),
          },
        ]}
      />
    </>
  )
}
