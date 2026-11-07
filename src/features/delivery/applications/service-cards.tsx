import { useState, type ReactNode } from 'react'
import { Button, Card, Segmented, Typography } from 'antd'
import './service-cards.css'
import { DeploymentUnitOutlined } from '@ant-design/icons'
import { AdminTable } from '@/components/admin-table'
import { ManagementState } from '@/components/management-list'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import type {
  ApplicationRuntimeWorkload,
  ApplicationServiceComponent,
  DeliveryApplicationBindingSummary,
  WorkflowRun,
} from '../types'
import { WorkflowProgress } from './delivery-progress'
import { serviceReadiness, serviceWorkflow } from './service-workflow'

export type ServiceCard = {
  id: string
  name: string
  kind: string
  service?: ApplicationServiceComponent
  workloads: ApplicationRuntimeWorkload[]
}

export function ServiceWorkflowCard({
  service,
  environmentId,
  binding,
  workloads,
  runs,
  canView,
  state = 'ready',
  onSelectStage,
}: {
  service?: ApplicationServiceComponent
  environmentId: string
  binding?: DeliveryApplicationBindingSummary
  workloads: ApplicationRuntimeWorkload[]
  runs?: WorkflowRun[]
  canView: boolean
  state?: 'ready' | 'loading' | 'error'
  onSelectStage: (runId: string, stageKey: string) => void
}) {
  const run: WorkflowRun | undefined = serviceWorkflow(
    service,
    environmentId,
    binding,
    workloads,
    runs,
  )
  return (
    <section className="soha-service-workflow" aria-label="当前服务工作流">
      <div className="soha-service-workflow__heading">
        <Typography.Text strong>当前工作流</Typography.Text>
        {canView && state === 'ready' && (run || binding?.workflowTemplate) ? (
          <StatusTag value={run?.status || 'default'} label={run ? undefined : '暂无可归属记录'} />
        ) : null}
      </div>
      {!canView ? (
        <Typography.Text type="secondary">无工作流查看权限</Typography.Text>
      ) : state !== 'ready' ? (
        <Typography.Text type="secondary">
          {state === 'loading' ? '正在读取工作流' : '工作流读取失败，请刷新重试'}
        </Typography.Text>
      ) : binding?.workflowTemplate || run ? (
        <>
          <Typography.Text type="secondary">
            {run?.workflowName || binding?.workflowTemplate?.name}
          </Typography.Text>
          <WorkflowProgress
            binding={{ latestWorkflow: run, workflowTemplate: binding?.workflowTemplate }}
            onSelectStage={onSelectStage}
          />
        </>
      ) : (
        <Typography.Text type="secondary">未配置工作流</Typography.Text>
      )}
    </section>
  )
}

export function ServiceCards({
  cards,
  statusFor,
  runtimeReady,
  canConfigure,
  onSelect,
  onConfigure,
  refreshAction,
}: {
  refreshAction?: ReactNode
  cards: ServiceCard[]
  statusFor: (
    items: ApplicationRuntimeWorkload[],
    serviceId?: string,
  ) => { tone: string; label: string }
  runtimeReady: boolean
  canConfigure: boolean
  onSelect: (id: string) => void
  onConfigure: (id: string) => void
}) {
  const [layout, setLayout] = useState<string>('cards')
  const items = cards.map((card) => ({
    card,
    status: statusFor(card.workloads, card.service?.id),
    readiness: runtimeReady ? serviceReadiness(card.workloads) : undefined,
  }))
  const identity = (card: ServiceCard) => (
    <button
      type="button"
      className="soha-service-card__identity"
      title={card.name}
      onClick={() => onSelect(card.id)}
    >
      <DeploymentUnitOutlined aria-hidden="true" />
      <strong>{card.name}</strong>
    </button>
  )
  const buildAction = (card: ServiceCard) =>
    card.service && canConfigure ? (
      <Button onClick={() => onConfigure(card.id)}>构建与更新</Button>
    ) : null
  type Item = (typeof items)[number]
  const columns = [
    { key: 'service', title: '服务', render: (_: unknown, { card }: Item) => identity(card) },
    {
      key: 'type',
      title: '类型',
      render: (_: unknown, { card }: Item) => <MetadataTag label={card.kind} />,
    },
    {
      key: 'status',
      title: '运行状态',
      render: (_: unknown, { status }: Item) => (
        <StatusTag value={status.tone} label={status.label} />
      ),
    },
    {
      key: 'ready',
      title: '实例就绪',
      render: (_: unknown, { readiness }: Item) =>
        readiness ? `${readiness.ready} / ${readiness.desired}` : '待观测',
    },
    { key: 'actions', title: '操作', render: (_: unknown, { card }: Item) => buildAction(card) },
  ]
  return (
    <section className="soha-service-overview-grid" aria-label="服务总览">
      <div className="soha-service-catalog__toolbar">
        <strong id="service-catalog-heading">
          服务清单 <Typography.Text type="secondary">{cards.length} 个服务</Typography.Text>
        </strong>
        <div className="soha-service-catalog__actions">
          <Segmented
            aria-label="服务显示方式"
            value={layout}
            onChange={setLayout}
            options={[
              { label: '卡片', value: 'cards' },
              { label: '列表', value: 'list' },
            ]}
          />
          {refreshAction}
        </div>
      </div>
      <div
        className="soha-service-catalog__scroll"
        role="region"
        aria-labelledby="service-catalog-heading"
        tabIndex={cards.length ? 0 : undefined}
      >
        {!cards.length ? (
          <ManagementState
            compact
            kind="empty"
            title="没有匹配的服务"
            description="调整名称或服务类型筛选。"
          />
        ) : layout === 'cards' ? (
          <div className="soha-service-card-grid" role="list" aria-label="服务卡片">
            {items.map(({ card, status, readiness }) => (
              <Card
                key={card.id}
                className="soha-service-card soha-service-card--compact"
                classNames={{ body: 'soha-service-card__body' }}
                role="listitem"
                aria-label={`服务 ${card.name}`}
              >
                <div className="soha-service-card__heading">
                  {identity(card)}
                  <StatusTag value={status.tone} label={status.label} />
                </div>
                <MetadataTag label={card.kind} />
                <div className="soha-service-catalog__footer">
                  <span>
                    <small>实例就绪</small>
                    <strong>
                      {readiness ? `${readiness.ready} / ${readiness.desired}` : '待观测'}
                    </strong>
                  </span>
                  {buildAction(card)}
                </div>
              </Card>
            ))}
          </div>
        ) : (
          <AdminTable
            columns={columns}
            dataSource={items}
            rowKey={(item: Item) => item.card.id}
            pagination={false}
            enableColumnSelection={false}
            scroll={{ x: 620 }}
          />
        )}
      </div>
    </section>
  )
}
