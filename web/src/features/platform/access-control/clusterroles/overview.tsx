import { Button, Card, Tooltip } from 'antd'
import { QuestionCircleOutlined } from '@ant-design/icons'
import { useI18n } from '@/i18n'
import { AdminTable } from '@/components/admin-table'
import { formatDateTime } from '@/utils/time'
import { MetadataTag } from '@/components/status-tag'
import {
  WorkloadOverview,
  WorkloadOverviewConfiguration,
  WorkloadOverviewMetadata,
} from '../../workloads/shared/workload-overview'
import type { ClusterRoleConfiguration, PolicyRule } from './detail-model'
import type { ClusterRoleDetail } from './types'

export function ClusterRoleOverview({
  detail,
  configuration,
  onTab,
}: {
  detail: ClusterRoleDetail
  configuration?: ClusterRoleConfiguration
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
              { label: zh ? '作用域' : 'Scope', children: zh ? '集群' : 'Cluster' },
              { label: zh ? '规则数' : 'Rules', children: count },
              {
                label: zh ? '选择器数' : 'Selectors',
                children: configuration?.selectors.length ?? detail.aggregationRules,
              },
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
          <ul className="soha-clusterrole-rule-preview">
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
      <Card
        className="soha-detail-card"
        title={
          <span className="soha-clusterrole-section-title">
            {zh ? '聚合配置' : 'Aggregation configuration'}
            <Tooltip
              trigger={['hover', 'focus', 'click']}
              title={
                zh
                  ? '选择器之间为 OR；同一选择器内的条件为 AND。选择器不代表已解析的来源角色，当前规则以 rules 为准。'
                  : 'Selectors are OR; requirements within one selector are AND. Selectors do not identify resolved source roles. Current permissions are shown in rules.'
              }
            >
              <Button
                type="text"
                size="small"
                icon={<QuestionCircleOutlined />}
                aria-label={zh ? '聚合配置说明' : 'About aggregation'}
              />
            </Tooltip>
          </span>
        }
      >
        {!configuration ? (
          <span>{zh ? '补充配置尚不可用' : 'Configuration unavailable'}</span>
        ) : !configuration.aggregated ? (
          <span>{zh ? '未配置聚合' : 'Aggregation is not configured'}</span>
        ) : (
          <>
            <MetadataTag label={zh ? '聚合角色' : 'Aggregated role'} tone="purple" />
            {configuration.selectors.length === 0 ? (
              <p>{zh ? '没有聚合选择器' : 'No aggregation selectors'}</p>
            ) : (
              configuration.selectors.map((selector, index) => (
                <div className="soha-clusterrole-selector" key={index}>
                  <strong>{`${zh ? '选择器' : 'Selector'} ${index + 1}${index > 0 ? ' · OR' : ''}`}</strong>
                  <ul>
                    {Object.entries(selector.matchLabels || {}).map(([key, value]) => (
                      <li key={key}>{`${key} = ${JSON.stringify(value)}`}</li>
                    ))}
                    {selector.matchExpressions?.map((expression, expressionIndex) => (
                      <li
                        key={expressionIndex}
                      >{`${expression.key} ${expression.operator}${expression.values?.length ? ` (${expression.values.map((value) => JSON.stringify(value)).join(', ')})` : ''}`}</li>
                    ))}
                    {Object.keys(selector.matchLabels || {}).length === 0 &&
                    !selector.matchExpressions?.length ? (
                      <li>
                        {zh
                          ? '空选择器：匹配所有 ClusterRole'
                          : 'Empty selector: matches all ClusterRoles'}
                      </li>
                    ) : null}
                  </ul>
                  {Object.keys(selector.matchLabels || {}).length +
                    (selector.matchExpressions?.length ?? 0) >
                  1 ? (
                    <small>
                      {zh ? '以上条件须同时满足（AND）' : 'All requirements must match (AND)'}
                    </small>
                  ) : null}
                </div>
              ))
            )}
          </>
        )}
      </Card>
      <WorkloadOverviewMetadata labels={detail.labels} annotations={detail.annotations} />
    </WorkloadOverview>
  )
}
