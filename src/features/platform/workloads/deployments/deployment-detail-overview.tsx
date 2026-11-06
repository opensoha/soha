import { useI18n } from '@/i18n'
import type { DeploymentDetail, DeploymentRolloutStatus, ScopeKey } from '@/types'
import { formatDateTime } from '@/utils/time'
import { Alert, Button, Card, Typography } from 'antd'
import {
  WorkloadOverview,
  WorkloadOverviewFacts,
  WorkloadOverviewConfiguration,
  WorkloadOverviewMetadata,
} from '../shared/workload-overview'
import { DeploymentDetailPods } from './deployment-detail-pods'
import { WorkloadConditions } from '../shared/workload-conditions'
import type { buildDeploymentFacts } from './deployment-detail-model'

export function deploymentDetailState(detail: DeploymentDetail, status?: DeploymentRolloutStatus) {
  const counts = [
    detail.desiredReplicas,
    detail.updatedReplicas,
    detail.readyReplicas,
    detail.availableReplicas,
  ]
  if (!counts.every((value) => Number.isFinite(value) && value >= 0)) return 'unknown'
  if (detail.desiredReplicas === 0) return detail.pods?.length ? 'progressing' : 'scaled-to-zero'
  if (status?.status && !['ready', 'healthy'].includes(status.status)) return status.status
  if (detail.updatedReplicas < detail.desiredReplicas) return 'progressing'
  if (
    detail.readyReplicas < detail.desiredReplicas ||
    detail.availableReplicas < detail.desiredReplicas
  )
    return 'degraded'
  return 'healthy'
}
export function DeploymentDetailOverview({
  detail,
  status,
  scope,
  model,
  onTabChange,
}: {
  detail: DeploymentDetail
  status?: DeploymentRolloutStatus
  scope: ScopeKey
  model: ReturnType<typeof buildDeploymentFacts>
  onTabChange: (key: string) => void
}) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const state = deploymentDetailState(detail, status)
  const zero = state === 'scaled-to-zero'
  const healthy = state === 'healthy'
  const unknown = state === 'unknown'
  const current = model.sets.find((set) => set.record?.revision === status?.revision)
  return (
    <WorkloadOverview
      className="soha-deployment-overview"
      aside={
        <>
          <Card className="soha-detail-card" title={zh ? '运行配置' : 'Configuration'}>
            <WorkloadOverviewConfiguration
              items={[
                { label: zh ? '命名空间' : 'Namespace', children: detail.namespace },
                { label: zh ? '更新策略' : 'Strategy', children: detail.strategy || '—' },
                {
                  label: zh ? '创建时间' : 'Created',
                  children: detail.createdAt ? formatDateTime(detail.createdAt) : '—',
                },
                {
                  label: zh ? '选择器' : 'Selector',
                  children:
                    Object.entries(detail.selector || {})
                      .map(([key, value]) => `${key}=${value}`)
                      .join(', ') || '—',
                },
              ]}
            />
          </Card>
          <Card className="soha-detail-card" title={zh ? '当前 ReplicaSet' : 'Current ReplicaSet'}>
            <p>{current?.node.resource.name || '—'}</p>
            <Typography.Text type="secondary">
              {current?.record
                ? `${current.record.replicas} ${zh ? '个副本' : 'replicas'} · ${current.record.readyReplicas} ${zh ? '个就绪' : 'ready'}`
                : zh
                  ? '当前版本信息尚未返回'
                  : 'Current revision information is unavailable'}
            </Typography.Text>
            <Button type="link" onClick={() => onTabChange('related-resources')}>
              {zh ? '查看关联资源' : 'View related resources'}
            </Button>
          </Card>
          <Card className="soha-detail-card" title={zh ? '交付来源' : 'Delivery source'}>
            <Typography.Text type="secondary">
              {zh
                ? '查看应用环境绑定及构建、工作流、应用发布记录。'
                : 'View application bindings, builds, workflows and releases.'}
            </Typography.Text>
            <Button type="link" onClick={() => onTabChange('delivery')}>
              {zh ? '查看交付联动' : 'View delivery integration'}
            </Button>
          </Card>
        </>
      }
    >
      <Alert
        showIcon
        type={zero || unknown ? 'info' : healthy ? 'success' : 'warning'}
        title={
          unknown
            ? zh
              ? '副本状态尚不完整'
              : 'Replica status is incomplete'
            : zero
              ? zh
                ? '已缩容至 0 个副本'
                : 'Scaled to zero replicas'
              : healthy
                ? zh
                  ? 'Deployment 当前副本已就绪'
                  : 'Deployment replicas are ready'
                : zh
                  ? 'Deployment 正在更新或仍有副本未就绪'
                  : 'Deployment is updating or replicas are not ready'
        }
        description={!healthy && !zero ? status?.message : undefined}
        action={
          <Button type="link" onClick={() => onTabChange(healthy ? 'revisions' : 'events')}>
            {healthy ? (zh ? '查看版本历史' : 'View revisions') : zh ? '查看事件' : 'View events'}
          </Button>
        }
      />
      <Card
        className="soha-detail-card"
        title={zh ? '副本与发布' : 'Replicas and rollout'}
        extra={
          <Typography.Text type="secondary">
            {zh ? '当前模板' : 'Current template'} · {status?.revision || '—'}
          </Typography.Text>
        }
      >
        <WorkloadOverviewFacts
          facts={[
            { key: 'desired', label: zh ? '期望' : 'Desired', value: detail.desiredReplicas },
            { key: 'updated', label: zh ? '已更新' : 'Updated', value: detail.updatedReplicas },
            { key: 'ready', label: zh ? '就绪' : 'Ready', value: detail.readyReplicas },
            { key: 'available', label: zh ? '可用' : 'Available', value: detail.availableReplicas },
          ].map((fact, index) => ({
            ...fact,
            target: detail.desiredReplicas,
            showTarget: index !== 0,
            tone:
              index === 1
                ? 'primary'
                : index && fact.value < detail.desiredReplicas
                  ? 'warning'
                  : 'success',
          }))}
        />
        <div className="soha-workload-overview-facts-footer">
          <span>
            {zh ? '策略' : 'Strategy'} {detail.strategy || '—'}
          </span>
          <span>Generation {detail.observedGeneration ?? '—'}</span>
          <Button type="link" onClick={() => onTabChange('revisions')}>
            {zh ? '查看版本' : 'View revisions'}
          </Button>
        </div>
      </Card>
      <Card
        className="soha-detail-card"
        title={zh ? '当前模板' : 'Current template'}
        extra={
          <Typography.Text type="secondary">
            {detail.containers
              ? `${detail.containers.length} ${zh ? '个容器' : 'containers'}`
              : zh
                ? '模板未返回'
                : 'Template unavailable'}
          </Typography.Text>
        }
      >
        {detail.containers?.length ? (
          detail.containers.map((container) => (
            <div className="soha-deployment-template" key={container.name}>
              <strong>{container.name}</strong>
              <Typography.Text copyable={{ text: container.image }}>
                {container.image || '—'}
              </Typography.Text>
            </div>
          ))
        ) : (
          <Typography.Text type="secondary">
            {zh ? '未返回容器模板' : 'Container template was not returned'}
          </Typography.Text>
        )}
      </Card>
      <DeploymentDetailPods
        pods={detail.pods || []}
        scope={scope}
        model={model}
        currentRevision={status?.revision}
        compact
        onShowAll={() => onTabChange('pods')}
      />
      <WorkloadConditions
        conditions={status?.conditions?.length ? status.conditions : detail.conditions}
      />
      <WorkloadOverviewMetadata labels={detail.labels} annotations={detail.annotations} />
    </WorkloadOverview>
  )
}
