import { AdminTable } from '@/components/admin-table'
import { BooleanTag } from '@/components/status-tag'
import { useI18n } from '@/i18n'
import type { DeploymentDetail, DeploymentRolloutStatus, ScopeKey } from '@/types'
import { formatDateTime } from '@/utils/time'
import { Alert, Button, Card, Collapse, Typography } from 'antd'
import { DeploymentDetailPods } from './deployment-detail-pods'
import type { buildDeploymentRelationships } from './deployment-detail-model'

export function deploymentDetailState(detail: DeploymentDetail, status?: DeploymentRolloutStatus) {
  if (detail.desiredReplicas === 0) return 'scaled-to-zero'
  return (
    status?.status ||
    (detail.updatedReplicas < detail.desiredReplicas
      ? 'progressing'
      : detail.readyReplicas < detail.desiredReplicas
        ? 'degraded'
        : 'ready')
  )
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
  model: ReturnType<typeof buildDeploymentRelationships>
  onTabChange: (key: string) => void
}) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const zero = detail.desiredReplicas === 0
  const healthy =
    detail.readyReplicas >= detail.desiredReplicas &&
    detail.updatedReplicas >= detail.desiredReplicas
  const current = model.sets.find((set) => set.record?.revision === status?.revision)
  const metadata = (value?: Record<string, string>) => (
    <dl className="soha-deployment-metadata">
      {Object.entries(value || {}).map(([key, entry]) => (
        <div key={key}>
          <dt>{key}</dt>
          <dd>{entry}</dd>
        </div>
      ))}
    </dl>
  )
  return (
    <div className="soha-deployment-overview">
      <div className="soha-deployment-stack">
        <Alert
          showIcon
          type={zero ? 'info' : healthy ? 'success' : 'warning'}
          title={
            zero
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
          <div className="soha-deployment-replica-facts">
            {[
              [zh ? '期望' : 'Desired', detail.desiredReplicas],
              [zh ? '已更新' : 'Updated', detail.updatedReplicas],
              [zh ? '就绪' : 'Ready', detail.readyReplicas],
              [zh ? '可用' : 'Available', detail.availableReplicas],
            ].map(([label, value], index) => (
              <div key={label}>
                <span>{label}</span>
                <strong>
                  {value}
                  <small>{index ? ` / ${detail.desiredReplicas}` : ''}</small>
                </strong>
                <div className="soha-deployment-fact-track">
                  <i
                    style={{
                      width: `${detail.desiredReplicas ? Math.min(100, (Number(value) / detail.desiredReplicas) * 100) : 0}%`,
                      background:
                        index === 1
                          ? 'var(--soha-primary)'
                          : index && Number(value) < detail.desiredReplicas
                            ? 'var(--soha-warning)'
                            : 'var(--soha-success)',
                    }}
                  />
                </div>
              </div>
            ))}
          </div>
          <div className="soha-deployment-facts-footer">
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
              {detail.containers?.length ?? 0} {zh ? '个容器' : 'containers'}
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
        <Card className="soha-detail-card" title={zh ? '运行条件' : 'Conditions'}>
          <details>
            <summary>
              {(status?.conditions || detail.conditions)?.map((condition) => (
                <span key={condition.type}>
                  {condition.type} <BooleanTag value={condition.status === 'True'} />
                </span>
              )) || (zh ? '未返回运行条件' : 'No conditions returned')}
            </summary>
            <AdminTable
              pagination={false}
              enableColumnSelection={false}
              rowKey="type"
              dataSource={status?.conditions || detail.conditions || []}
              columns={[
                { title: zh ? '条件' : 'Condition', dataIndex: 'type', width: 230 },
                { title: zh ? '状态' : 'Status', dataIndex: 'status', width: 100 },
                { title: zh ? '原因' : 'Reason', dataIndex: 'reason', width: 180 },
                { title: zh ? '消息' : 'Message', dataIndex: 'message' },
              ]}
              scroll={{ x: 800 }}
            />
          </details>
        </Card>
        <Collapse
          items={[
            {
              key: 'labels',
              label: `${zh ? '标签' : 'Labels'} · ${Object.keys(detail.labels || {}).length}`,
              children: metadata(detail.labels),
            },
            {
              key: 'annotations',
              label: `${zh ? '注解' : 'Annotations'} · ${Object.keys(detail.annotations || {}).length}`,
              children: metadata(detail.annotations),
            },
          ]}
        />
      </div>
      <aside className="soha-deployment-stack">
        <Card className="soha-detail-card" title={zh ? '运行配置' : 'Configuration'}>
          <dl className="soha-deployment-config">
            <div>
              <dt>{zh ? '命名空间' : 'Namespace'}</dt>
              <dd>{detail.namespace}</dd>
            </div>
            <div>
              <dt>{zh ? '更新策略' : 'Strategy'}</dt>
              <dd>{detail.strategy || '—'}</dd>
            </div>
            <div>
              <dt>{zh ? '创建时间' : 'Created'}</dt>
              <dd>{detail.createdAt ? formatDateTime(detail.createdAt) : '—'}</dd>
            </div>
            <div>
              <dt>{zh ? '选择器' : 'Selector'}</dt>
              <dd>
                {Object.entries(detail.selector || {})
                  .map(([key, value]) => `${key}=${value}`)
                  .join(', ') || '—'}
              </dd>
            </div>
          </dl>
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
      </aside>
    </div>
  )
}
