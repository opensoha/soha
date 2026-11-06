import { Button, Card, Tooltip } from 'antd'
import { MetadataTag } from '@/components/status-tag'
import { QuestionCircleOutlined } from '@ant-design/icons'
import { Link } from 'react-router-dom'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useI18n } from '@/i18n'
import { readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import {
  WorkloadOverview,
  WorkloadOverviewConfiguration,
  WorkloadOverviewMetadata,
} from '../../workloads/shared/workload-overview'
import type { ServiceAccountDetail } from './types'
import type { SaConfiguration } from './detail-model'

export function SaOverview({
  detail,
  configuration,
  clusterId,
  onTab,
}: {
  detail: ServiceAccountDetail
  configuration?: SaConfiguration
  clusterId: string
  onTab: (tab: string) => void
}) {
  const zh = useI18n().localeCode === 'zh_CN'
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const canSecret = hasPermission(permissions, 'platform.configuration.secrets.view')
  const references = (names?: string[]) =>
    names === undefined ? (
      <span className="soha-sa-muted">{zh ? '未提供' : 'Not provided'}</span>
    ) : names.length ? (
      <ul className="soha-sa-references">
        {names.map((name, index) => (
          <li key={`${name}-${index}`}>
            {canSecret && clusterId && detail.namespace ? (
              <Link
                to={`/configuration/secrets/${encodeURIComponent(name)}?${new URLSearchParams({ clusterId, namespace: detail.namespace })}`}
              >
                {name}
              </Link>
            ) : (
              name
            )}
          </li>
        ))}
      </ul>
    ) : (
      <span className="soha-sa-muted">{zh ? '未声明引用' : 'No references declared'}</span>
    )
  return (
    <WorkloadOverview
      className="soha-sa-overview"
      aside={
        <Card title={zh ? '身份信息' : 'Identity'}>
          <WorkloadOverviewConfiguration
            items={[
              {
                label: zh ? '命名空间' : 'Namespace',
                children: (
                  <Tooltip title={detail.namespace} trigger={['hover', 'focus', 'click']}>
                    <span tabIndex={0} className="soha-sa-namespace">
                      <MetadataTag tone="blue" label={detail.namespace} />
                    </span>
                  </Tooltip>
                ),
              },
              { label: zh ? 'API 版本' : 'API version', children: 'v1' },
              {
                label: zh ? '创建时间' : 'Created',
                children: detail.createdAt ? formatDateTime(detail.createdAt) : '—',
              },
            ]}
          />
        </Card>
      }
    >
      <Card
        title={
          <span className="soha-sa-section-title">
            {zh ? '账户配置' : 'Account configuration'}
            <Tooltip
              trigger={['hover', 'focus', 'click']}
              title={
                zh
                  ? 'automountServiceAccountToken 表示 ServiceAccount 的 Token 自动挂载声明。Pod 自身配置可以覆盖该值；未声明不等于禁用，也不证明 Token 已挂载。'
                  : 'automountServiceAccountToken is the ServiceAccount token automount declaration. Pod settings can override it. Omission does not mean disabled or prove a mounted token.'
              }
            >
              <Button
                type="text"
                size="small"
                icon={<QuestionCircleOutlined />}
                aria-label={zh ? '自动挂载说明' : 'Automount information'}
              />
            </Tooltip>
          </span>
        }
      >
        <WorkloadOverviewConfiguration
          items={[
            {
              label: zh ? '自动挂载 Token' : 'Token automount',
              children:
                configuration === undefined
                  ? zh
                    ? '未知'
                    : 'Unknown'
                  : configuration.automount === undefined
                    ? zh
                      ? '未声明'
                      : 'Not declared'
                    : configuration.automount
                      ? 'true'
                      : 'false',
            },
            {
              label: zh ? 'Secret 引用' : 'Secret references',
              children: references(detail.secrets),
            },
            {
              label: zh ? '镜像拉取 Secret' : 'Image pull secrets',
              children: references(detail.imagePullSecrets),
            },
          ]}
        />
        <div className="soha-sa-access-links">
          <Button type="link" onClick={() => onTab('bindings')}>
            {zh ? '查看绑定' : 'View bindings'}
          </Button>
          <Button type="link" onClick={() => onTab('effective-access')}>
            {zh ? '查看有效权限' : 'View effective access'}
          </Button>
        </div>
      </Card>
      <WorkloadOverviewMetadata labels={detail.labels} annotations={detail.annotations} copyable />
    </WorkloadOverview>
  )
}
