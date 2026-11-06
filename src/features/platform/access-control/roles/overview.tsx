import { Button, Card, Tooltip } from 'antd'
import { useI18n } from '@/i18n'
import { AdminTable } from '@/components/admin-table'
import { formatDateTime } from '@/utils/time'
import { MetadataTag } from '@/components/status-tag'
import {
  WorkloadOverview,
  WorkloadOverviewConfiguration,
  WorkloadOverviewMetadata,
} from '../../workloads/shared/workload-overview'
import type { RoleConfiguration, PolicyRule } from './detail-model'
import type { RoleDetail } from './types'

export function RoleOverview({
  detail,
  configuration,
  onTab,
}: {
  detail: RoleDetail
  configuration?: RoleConfiguration
  onTab: (tab: string) => void
}) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const count = configuration?.rules.length ?? detail.rules
  return (
    <WorkloadOverview
      aside={
        <Card className="soha-detail-card" title={zh ? '资源信息' : 'Resource information'}>
          <WorkloadOverviewConfiguration
            items={[
              { label: zh ? '作用域' : 'Scope', children: zh ? '命名空间' : 'Namespace' },
              {
                label: zh ? '命名空间' : 'Namespace',
                children: (
                  <Tooltip title={detail.namespace} trigger={['hover', 'focus', 'click']}>
                    <span className="soha-role-namespace" tabIndex={0}>
                      <MetadataTag label={detail.namespace} tone="blue" title={detail.namespace} />
                    </span>
                  </Tooltip>
                ),
              },
              { label: zh ? '规则数' : 'Rules', children: count },
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
        className="soha-detail-card"
        title={zh ? '当前权限规则' : 'Current permission rules'}
        extra={
          <Button type="link" onClick={() => onTab('rules')}>
            {zh ? '查看规则' : 'View rules'}
          </Button>
        }
      >
        {configuration ? (
          <AdminTable
            rowKey="key"
            tableSize="small"
            pagination={false}
            enableColumnSelection={false}
            enableDensity={false}
            columnSettingPlacement="hidden"
            dataSource={configuration.rules
              .slice(0, 3)
              .map((rule, index) => ({ ...rule, key: String(index) }))}
            empty={zh ? '当前没有权限规则' : 'No current permission rules'}
            columns={[
              {
                key: 'apiGroups',
                title: zh ? 'API 组' : 'API groups',
                width: 160,
                render: (_: unknown, rule: PolicyRule) =>
                  rule.apiGroups?.map((group) => (group === '' ? 'core ("")' : group)).join(', ') ||
                  '—',
              },
              {
                key: 'resources',
                title: zh ? '资源 / URL' : 'Resources / URLs',
                width: 240,
                render: (_: unknown, rule: PolicyRule) =>
                  [...(rule.resources ?? []), ...(rule.nonResourceURLs ?? [])].join(', ') || '—',
              },
              {
                key: 'verbs',
                title: zh ? '操作' : 'Verbs',
                width: 160,
                render: (_: unknown, rule: PolicyRule) => rule.verbs.join(', ') || '—',
              },
            ]}
            scroll={{ x: 560 }}
          />
        ) : (
          <ul className="soha-role-rule-preview">
            {detail.ruleSummaries?.slice(0, 3).map((summary, index) => (
              <li key={index}>{summary}</li>
            ))}
            {!detail.ruleSummaries?.length ? (
              <li>{zh ? '权限规则配置尚不可用' : 'Permission rule configuration unavailable'}</li>
            ) : null}
          </ul>
        )}
        <div className="soha-workload-overview-facts-footer">
          <Button type="link" onClick={() => onTab('bindings')}>
            {zh ? '查看引用此角色的绑定' : 'View bindings referencing this role'}
          </Button>
        </div>
      </Card>
      <WorkloadOverviewMetadata labels={detail.labels} annotations={detail.annotations} />
    </WorkloadOverview>
  )
}
