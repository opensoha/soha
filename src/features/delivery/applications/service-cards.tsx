import { Button, Card, Typography } from 'antd'
import { DeploymentUnitOutlined } from '@ant-design/icons'
import { ManagementState } from '@/components/management-list'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import type {
  ApplicationRuntimeWorkload,
  ApplicationServiceComponent,
  BuildSource,
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
  buildSources,
  environmentId,
  binding,
  runs,
  statusFor,
  runtimeReady,
  workflowState,
  canViewWorkflows,
  canConfigure,
  onSelect,
  onConfigure,
  onSelectStage,
}: {
  cards: ServiceCard[]
  buildSources: BuildSource[]
  environmentId: string
  binding?: DeliveryApplicationBindingSummary
  runs?: WorkflowRun[]
  statusFor: (
    items: ApplicationRuntimeWorkload[],
    serviceId?: string,
  ) => { tone: string; label: string }
  runtimeReady: boolean
  workflowState: 'ready' | 'loading' | 'error'
  canViewWorkflows: boolean
  canConfigure: boolean
  onSelect: (id: string) => void
  onConfigure: (id: string) => void
  onSelectStage: (runId: string, stageKey: string) => void
}) {
  return (
    <section className="soha-service-overview-grid" aria-label="服务总览">
      {!cards.length ? (
        <ManagementState
          compact
          kind="empty"
          title="没有匹配的服务"
          description="调整名称或服务类型筛选。"
        />
      ) : (
        <div className="soha-service-card-grid" role="list" aria-label="服务卡片">
          {cards.map((card) => {
            const status = statusFor(card.workloads, card.service?.id)
            const readiness = runtimeReady ? serviceReadiness(card.workloads) : undefined
            const source = buildSources.find((item) => item.id === card.service?.buildSourceId)
            return (
              <Card
                key={card.id}
                className="soha-service-card"
                classNames={{ body: 'soha-service-card__body' }}
                role="listitem"
                aria-label={`服务 ${card.name}`}
              >
                <div className="soha-service-card__heading">
                  <button
                    type="button"
                    className="soha-service-card__identity"
                    onClick={() => onSelect(card.id)}
                  >
                    <DeploymentUnitOutlined aria-hidden="true" />
                    <strong>{card.name}</strong>
                  </button>
                  <StatusTag value={status.tone} label={status.label} />
                </div>
                <MetadataTag label={card.kind} />
                <dl className="soha-service-card__facts">
                  <div>
                    <dt>实例就绪</dt>
                    <dd>{readiness ? `${readiness.ready} / ${readiness.desired}` : '待观测'}</dd>
                  </div>
                  <div>
                    <dt>构建绑定</dt>
                    <dd>
                      {source?.name ||
                        (card.service?.buildSourceId ? '构建定义不可读取' : '未绑定构建')}
                    </dd>
                  </div>
                </dl>
                <ServiceWorkflowCard
                  service={card.service}
                  environmentId={environmentId}
                  binding={binding}
                  runs={runs}
                  workloads={card.workloads}
                  canView={canViewWorkflows}
                  state={workflowState}
                  onSelectStage={onSelectStage}
                />
                <div className="soha-service-card__actions">
                  <Button onClick={() => onSelect(card.id)}>查看服务</Button>
                  {card.service && canConfigure ? (
                    <Button onClick={() => onConfigure(card.id)}>构建与更新</Button>
                  ) : null}
                </div>
              </Card>
            )
          })}
        </div>
      )}
    </section>
  )
}
