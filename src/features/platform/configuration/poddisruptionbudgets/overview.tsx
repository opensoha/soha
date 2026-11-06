import { AdminTable } from '@/components/admin-table'
import { ManagementState } from '@/components/management-list'
import { MetadataTag } from '@/components/status-tag'
import { TableCellText } from '@/components/table-cell-content'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { buildRelatedResourcePath } from '@/features/platform/workloads-model'
import { useI18n } from '@/i18n'
import type { ScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import { QuestionCircleOutlined } from '@ant-design/icons'
import { Button, Card, Tooltip, type TableColumnsType } from 'antd'
import { Link } from 'react-router-dom'
import {
  WorkloadOverview,
  WorkloadOverviewConfiguration,
  WorkloadOverviewFacts,
  WorkloadOverviewMetadata,
} from '../../workloads/shared/workload-overview'
import { WorkloadConditions } from '../../workloads/shared/workload-conditions'
import { WorkloadPodsTable } from '../../workloads/shared/workload-pods-table'
import type { PDBConfiguration } from './pdb-model'
import type { PodDisruptionBudgetResource } from './types'
function Help({ title, help }: { title: string; help: string }) {
  return (
    <span className="soha-pdb-card-title">
      {title}
      <Tooltip title={help} trigger={['hover', 'focus', 'click']}>
        <Button
          type="text"
          size="small"
          icon={<QuestionCircleOutlined />}
          aria-label={title + ' info'}
        />
      </Tooltip>
    </span>
  )
}
export function PDBPods({
  detail,
  configuration,
  scope,
  compact = false,
  onShowAll,
}: {
  detail: PodDisruptionBudgetResource
  configuration?: PDBConfiguration
  scope: ScopeKey
  compact?: boolean
  onShowAll?: () => void
}) {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  if (!hasPermission(permissions, 'platform.pods.view'))
    return (
      <ManagementState
        kind="no-permission"
        description={
          zh
            ? '查看受保护 Pods 需要 Pod 查看权限'
            : 'Pod view permission is required to inspect selected Pods'
        }
      />
    )
  if (!configuration)
    return (
      <ManagementState
        kind="empty"
        title={zh ? '选择范围待确认' : 'Selection not confirmed'}
        description={
          zh
            ? '读取 PDB 选择器后才能确认受保护范围。'
            : 'The PDB selector must be read before showing selected Pods.'
        }
      />
    )
  if (configuration.selectorMode !== 'none' && detail.pods == null)
    return (
      <ManagementState
        kind="empty"
        title={zh ? '未返回可见 Pods' : 'Visible Pods were not returned'}
        description={
          zh
            ? '当前响应未提供 Pod 列表，无法确认为没有匹配。'
            : 'The response did not include a Pod list; this does not confirm an empty selection.'
        }
      />
    )
  const pods =
    configuration.selectorMode === 'none'
      ? []
      : (detail.pods || []).filter((pod) => pod.namespace === detail.namespace)
  return (
    <WorkloadPodsTable
      key={detail.namespace + '/' + detail.name + '/' + configuration.selectorMode}
      pods={pods}
      scope={scope}
      compact={compact}
      onShowAll={onShowAll}
    />
  )
}
function WorkloadLink({ detail, scope }: { detail: PodDisruptionBudgetResource; scope: ScopeKey }) {
  const permissions = readableQueryData(usePermissionSnapshot())?.data,
    ref = detail.workload
  if (!ref || !hasPermission(permissions, 'platform.pods.view')) return <>—</>
  const permission = (
    {
      Deployment: 'platform.deployment.view',
      StatefulSet: 'platform.workloads.stateful-sets.view',
      DaemonSet: 'platform.workloads.daemon-sets.view',
      ReplicaSet: 'platform.workloads.replica-sets.view',
      ReplicationController: 'platform.workloads.replication-controllers.view',
      Job: 'platform.workloads.jobs.view',
      CronJob: 'platform.workloads.cron-jobs.view',
    } as Record<string, string>
  )[ref.kind]
  const path =
    permission &&
    (!ref.namespace || ref.namespace === detail.namespace) &&
    hasPermission(permissions, permission)
      ? buildRelatedResourcePath(ref, detail.namespace, scope.clusterId)
      : null
  return path ? (
    <Link to={path}>
      <TableCellText value={ref.kind + '/' + ref.name} />
    </Link>
  ) : (
    <TableCellText value={ref.kind + '/' + ref.name} />
  )
}
export function PDBOverview({
  detail,
  configuration,
  scope,
  onShowPods,
}: {
  detail: PodDisruptionBudgetResource
  configuration?: PDBConfiguration
  scope: ScopeKey
  onShowPods: () => void
}) {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const current = configuration ? configuration.currentHealthy : detail.currentHealthy,
    desired = configuration ? configuration.desiredHealthy : detail.desiredHealthy,
    allowed = configuration ? configuration.disruptionsAllowed : detail.disruptionsAllowed
  const disruptions = configuration?.disruptedPods || []
  const columns: TableColumnsType<{ name: string; time?: string }> = [
    {
      title: 'Pod',
      dataIndex: 'name',
      width: 280,
      render: (name: string) => <TableCellText value={name} />,
    },
    {
      title: zh ? '驱逐受理时间' : 'Eviction accepted',
      dataIndex: 'time',
      width: 190,
      render: (value?: string) => (value ? formatDateTime(value) : '—'),
    },
  ]
  return (
    <WorkloadOverview
      aside={
        <Card title={zh ? '预算配置' : 'Budget configuration'}>
          <WorkloadOverviewConfiguration
            items={[
              {
                label: zh ? '命名空间' : 'Namespace',
                children: (
                  <Tooltip title={detail.namespace} trigger={['hover', 'focus', 'click']}>
                    <span className="soha-pdb-namespace-trigger" tabIndex={0}>
                      <MetadataTag
                        tone="blue"
                        className="soha-pdb-namespace"
                        label={<span>{detail.namespace}</span>}
                      />
                    </span>
                  </Tooltip>
                ),
              },
              {
                label: 'minAvailable',
                children: configuration
                  ? (configuration.minAvailable ?? '—')
                  : detail.minAvailable || '—',
              },
              {
                label: 'maxUnavailable',
                children: configuration
                  ? (configuration.maxUnavailable ?? '—')
                  : detail.maxUnavailable || '—',
              },
              {
                label: zh ? '非健康 Pod 策略' : 'Unhealthy Pod policy',
                children: configuration ? (
                  <>
                    {configuration.unhealthyPodEvictionPolicy}
                    {configuration.policyDefaulted ? (
                      <small className="soha-pdb-muted">{zh ? '（默认）' : ' (default)'}</small>
                    ) : null}
                  </>
                ) : (
                  '—'
                ),
              },
              { label: zh ? '配置代次' : 'Generation', children: configuration?.generation ?? '—' },
              {
                label: zh ? '已观测代次' : 'Observed generation',
                children: configuration?.observedGeneration ?? '—',
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
        title={
          <Help
            title={zh ? '可驱逐预算' : 'Disruption budget'}
            help={
              zh
                ? '这是自愿驱逐预算，不是 CPU / 内存用量，也不保证所有故障下可用。0 表示没有可用预算，不等于应用故障。状态代次必须与配置代次一致才是当前预算；非健康 Pod 的例外规则见策略配置。'
                : 'This budget applies to voluntary evictions, not resource usage or all failures. Zero budget is not application failure. Status must observe the current generation. Unhealthy Pod policy can make exceptions.'
            }
          />
        }
      >
        <WorkloadOverviewFacts
          facts={[
            {
              key: 'allowed',
              label: configuration?.observed
                ? zh
                  ? '允许驱逐'
                  : 'Disruptions allowed'
                : zh
                  ? '返回允许驱逐'
                  : 'Returned disruptions allowed',
              value: allowed,
            },
            {
              key: 'current',
              label: configuration?.observed
                ? zh
                  ? '当前健康'
                  : 'Current healthy'
                : zh
                  ? '返回健康数'
                  : 'Returned healthy count',
              value: current,
            },
            { key: 'desired', label: zh ? '期望健康' : 'Desired healthy', value: desired },
            {
              key: 'expected',
              label: zh ? '预计 Pods' : 'Expected Pods',
              value: configuration?.expectedPods,
            },
          ]}
        />
      </Card>
      <Card
        title={
          <Help
            title={zh ? 'Pod 选择器' : 'Pod selector'}
            help={
              zh
                ? 'policy/v1 中 selector 未设置或 null 不匹配 Pod，{} 匹配当前命名空间全部 Pod；所有匹配条件同时满足。'
                : 'In policy/v1 a null or omitted selector matches no Pods, while {} matches all Pods in the namespace. All selector requirements apply together.'
            }
          />
        }
      >
        {configuration ? (
          <>
            <div className="soha-pdb-selector">
              {configuration.selectorMode === 'none' ? (
                <span>{zh ? '未选择任何 Pod' : 'Selects no Pods'}</span>
              ) : configuration.selectorMode === 'all' ? (
                <MetadataTag
                  tone="gold"
                  label={zh ? '当前命名空间全部 Pod' : 'All Pods in this namespace'}
                />
              ) : (
                configuration.terms.map((term, index) => <MetadataTag key={index} label={term} />)
              )}
            </div>
            {configuration.selectorMode !== 'none' && detail.workload ? (
              <div className="soha-pdb-workload">
                <span>{zh ? '共同工作负载' : 'Common workload'}</span>
                <WorkloadLink detail={detail} scope={scope} />
              </div>
            ) : null}
          </>
        ) : (
          <TableCellText value={detail.selector || '—'} />
        )}
      </Card>
      <PDBPods
        detail={detail}
        configuration={configuration}
        scope={scope}
        compact
        onShowAll={onShowPods}
      />
      <WorkloadConditions
        conditions={configuration?.conditions ?? detail.conditions}
        title={zh ? '预算条件' : 'Budget conditions'}
        showTransitionTime
      />
      {disruptions.length ? (
        <Card
          title={
            <Help
              title={zh ? '已受理的驱逐' : 'Accepted disruptions'}
              help={
                zh
                  ? 'API Server 已受理、PDB 控制器尚未观察到 Pod 删除的记录；不是历史事件列表。'
                  : 'Evictions accepted by the API server but not yet observed as Pod deletions by the PDB controller; this is not event history.'
              }
            />
          }
        >
          <AdminTable
            columns={columns}
            dataSource={disruptions}
            rowKey="name"
            pagination={false}
            enableColumnSelection={false}
            tableSize="small"
            scroll={{ x: 470 }}
          />
        </Card>
      ) : null}
      <WorkloadOverviewMetadata labels={detail.labels} annotations={detail.annotations} copyable />
    </WorkloadOverview>
  )
}
