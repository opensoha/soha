import { AdminTable } from '@/components/admin-table'
import { ManagementState } from '@/components/management-list'
import { MetadataTag } from '@/components/status-tag'
import { useI18n } from '@/i18n'
import { Card, Typography } from 'antd'
import { type ReactNode } from 'react'
import { WorkloadOverviewConfiguration } from '../../workloads/shared/workload-overview'
import { GatewayConditions } from './gateway-panels'
import type { BackendTLSConfiguration, PolicyReference } from './backend-tls-configuration'
import type { BackendTLSPolicyDetail } from './types'
import './backend-tls.css'

export type PolicyReferenceRenderer = (ref: PolicyReference) => ReactNode
export function PolicyReferences({
  title,
  references,
  labels,
  renderReference,
}: {
  title: string
  references?: readonly PolicyReference[]
  labels?: readonly string[]
  renderReference: PolicyReferenceRenderer
}) {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const matched = new Set(references?.map((r) => r.sourceLabel))
  return (
    <Card className="soha-detail-card" title={title}>
      <AdminTable
        pagination={false}
        enableColumnSelection={false}
        rowKey={(r: PolicyReference) => JSON.stringify([r.sourceLabel, r.sectionName])}
        dataSource={[...(references || [])]}
        scroll={{ x: 680 }}
        columns={[
          {
            title: zh ? '资源' : 'Resource',
            key: 'name',
            width: 220,
            render: (_: unknown, r: PolicyReference) => renderReference(r),
          },
          {
            title: zh ? '命名空间' : 'Namespace',
            dataIndex: 'namespace',
            width: 180,
            render: (v: string) => (
              <span className="soha-gateway-tags">
                <MetadataTag tone="blue" label={v} />
              </span>
            ),
          },
          {
            title: 'Kind / group',
            key: 'kind',
            width: 230,
            render: (_: unknown, r: PolicyReference) => (
              <span className="soha-gateway-wrap">
                {r.kind} · {r.group || 'core'}
              </span>
            ),
          },
          {
            title: 'SectionName',
            dataIndex: 'sectionName',
            width: 130,
            render: (v?: string) => v || '—',
          },
        ]}
        empty={
          <ManagementState
            compact
            kind="empty"
            description={
              labels?.length
                ? zh
                  ? '完整引用配置尚未读取，以下为接口摘要。'
                  : 'Full references unavailable; API summary below.'
                : zh
                  ? '本次未返回可查看的引用'
                  : 'No readable references returned'
            }
          />
        }
      />
      {(labels || [])
        .filter((label) => !matched.has(label))
        .map((label) => (
          <div className="soha-gateway-wrap" key={label}>
            {label}
          </div>
        ))}
    </Card>
  )
}
export function PolicyValidation({
  detail,
  config,
  renderReference,
}: {
  detail: BackendTLSPolicyDetail
  config?: BackendTLSConfiguration
  renderReference: PolicyReferenceRenderer
}) {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const hostname = config?.hostname ?? detail.hostname,
    system = config?.systemCA ?? detail.wellKnownCACertificates
  return (
    <div className="soha-detail-stack soha-backend-tls-validation">
      <Card className="soha-detail-card" title={zh ? 'TLS 校验规则' : 'TLS validation'}>
        <WorkloadOverviewConfiguration
          items={[
            {
              label: 'SNI / Hostname',
              children: (
                <Typography.Text className="soha-gateway-wrap" copyable={Boolean(hostname)}>
                  {hostname || '—'}
                </Typography.Text>
              ),
            },
            {
              label: zh ? '证书身份匹配' : 'Certificate identity',
              children: config ? (
                config.sans.length ? (
                  <div className="soha-gateway-values">
                    {config.sans.map((s) => (
                      <span className="soha-gateway-wrap" key={s}>
                        {s}
                      </span>
                    ))}
                  </div>
                ) : (
                  hostname || '—'
                )
              ) : zh ? (
                '完整配置未返回'
              ) : (
                'Full configuration not returned'
              ),
            },
            {
              label: zh ? 'CA 来源' : 'CA source',
              children: system
                ? `${system}${zh ? '（取决于控制器支持）' : ' (implementation support required)'}`
                : detail.caCertificateRefs?.length
                  ? zh
                    ? '引用 CA'
                    : 'CA references'
                  : '—',
            },
            ...(config?.sans.length
              ? [
                  {
                    label: zh ? '校验方式' : 'SAN matching',
                    children: zh
                      ? '按配置的 SAN 校验后端证书；Hostname 保留为 SNI。'
                      : 'Match the configured SANs; Hostname remains the SNI.',
                  },
                ]
              : []),
          ]}
        />
      </Card>
      {!system && (
        <PolicyReferences
          title={zh ? 'CA 证书引用' : 'CA certificate references'}
          references={config?.caRefs}
          labels={detail.caCertificateRefs}
          renderReference={renderReference}
        />
      )}
      {config && Object.keys(config.options).length > 0 && (
        <Card className="soha-detail-card" title={zh ? '实现选项' : 'Implementation options'}>
          <WorkloadOverviewConfiguration
            items={Object.entries(config.options).map(([label, value]) => ({
              label,
              children: (
                <code className="soha-gateway-wrap">
                  {typeof value === 'string' ? value : JSON.stringify(value)}
                </code>
              ),
            }))}
          />
        </Card>
      )}
    </div>
  )
}
export function PolicyConditions({
  detail,
  config,
  renderReference,
}: {
  detail: BackendTLSPolicyDetail
  config?: BackendTLSConfiguration
  renderReference: PolicyReferenceRenderer
}) {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  if (config?.ancestors.length)
    return (
      <div className="soha-detail-stack">
        {config.ancestors.map((a) => (
          <GatewayConditions
            key={a.key}
            title={zh ? '控制器条件' : 'Controller conditions'}
            conditions={a.conditions}
            generation={config.generation}
          >
            <WorkloadOverviewConfiguration
              items={[
                {
                  label: zh ? '观测祖先引用' : 'Observed ancestor',
                  children: renderReference(a.ref),
                },
                ...(a.ref.sectionName
                  ? [{ label: 'SectionName', children: a.ref.sectionName }]
                  : []),
                {
                  label: 'Controller',
                  children: <span className="soha-gateway-wrap">{a.controller || '—'}</span>,
                },
              ]}
            />
          </GatewayConditions>
        ))}
      </div>
    )
  return (
    <GatewayConditions
      title={zh ? '控制器条件' : 'Controller conditions'}
      conditions={detail.conditions || []}
      generation={config?.generation}
    />
  )
}
