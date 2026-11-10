import { lazy, Suspense, useRef, useState } from 'react'
import { Alert, Button, Descriptions, Drawer, Popconfirm, Space, Typography } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useParams, useSearchParams } from 'react-router-dom'
import { AdminTable } from '@/components/admin-table'
import { ManagementState } from '@/components/management-list'
import { OperationFeedback, ReadFeedback } from '@/components/request-feedback'
import { readableQueryData, writeOutcomeUnknown } from '@/utils/query-feedback'
import { StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { formatDateTime } from '@/utils/time'
import { TemplateUpdateEntry } from '../template-updates/entry'
import { deliveryQueries } from '../queries'
import { deliveryMutations } from '../mutations'
import type { DeliveryBatch, DeliveryTargetSnapshot } from '../types'
import { DeliveryBatchEditor } from './editor'
import { DeliveryBatchPlanDrawer } from './plan-drawer'
import {
  deliveryTargetActionLabel,
  deliveryModeLabels,
  deliveryStageLabels,
  deliveryStatusLabels,
  isBatchTerminal,
  targetStageNode,
} from './model'

const RuntimeDetailContent = lazy(() =>
  import('../runtime-detail/shared-page').then((module) => ({
    default: module.RuntimeDetailContent,
  })),
)

function StageStatus({ status }: { status?: string }) {
  if (!status) return <Typography.Text type="secondary">—</Typography.Text>
  return (
    <StatusTag
      value={
        ['running', 'canceling', 'waiting_execution'].includes(status)
          ? 'checking'
          : status === 'partially_completed'
            ? 'warning'
            : status
      }
      label={deliveryStatusLabels[status] ?? status}
    />
  )
}

function DeliveryTargetEvidence({
  batch,
  snapshot,
  onPlan,
  onEvidence,
}: {
  batch: DeliveryBatch
  snapshot: DeliveryTargetSnapshot
  onPlan: (id: string) => void
  onEvidence: (kind: 'execution_task' | 'build' | 'release_bundle', id: string) => void
}) {
  const nodes = Object.keys(deliveryStageLabels)
    .map((stage) => targetStageNode(batch, snapshot.target.id, stage))
    .filter((node) => !!node)
  return (
    <Space orientation="vertical" size={12} style={{ width: '100%' }}>
      <Descriptions
        size="small"
        column={2}
        items={[
          {
            key: 'configuration',
            label: '配置摘要',
            children: (
              <Typography.Text code copyable>
                {snapshot.configurationDigest}
              </Typography.Text>
            ),
          },
          {
            key: 'version',
            label: '服务版本',
            children: snapshot.templateUpdate ? '按环境固定模板版本' : snapshot.serviceVersion,
          },
          {
            key: 'template',
            label: '部署模板',
            children: snapshot.templateUpdate
              ? `${snapshot.templateUpdate.templateId} · v${snapshot.templateUpdate.fromVersion} → v${snapshot.templateUpdate.toVersion}`
              : snapshot.deploymentTemplate
                ? `${snapshot.deploymentTemplate.templateId} · v${snapshot.deploymentTemplate.version}`
                : '独立配置',
          },
          {
            key: 'dependencies',
            label: '成功依赖',
            children:
              snapshot.target.dependsOn
                ?.map((id) => {
                  const dependency = batch.targets.find((item) => item.target.id === id)
                  return `${dependency?.serviceName || id} / ${dependency?.environmentName || '—'}`
                })
                .join('、') || '无',
          },
        ]}
      />
      {snapshot.repositoryRefs?.map((ref) => (
        <Typography.Text key={ref.repositoryId} code copyable>
          {ref.repositoryId} · {ref.refName}
        </Typography.Text>
      ))}
      <AdminTable
        rowKey="nodeId"
        dataSource={nodes}
        pagination={false}
        viewportScroll={false}
        enableColumnSelection={false}
        localSorting={false}
        columns={[
          {
            title: '阶段',
            dataIndex: 'stage',
            render: (stage: keyof typeof deliveryStageLabels) =>
              snapshot.templateUpdate && stage === 'deploy'
                ? '采用配置'
                : snapshot.templateUpdate && stage === 'plan'
                  ? '版本校验与审批'
                  : deliveryStageLabels[stage],
          },
          {
            title: '状态',
            dataIndex: 'status',
            render: (status: string) => <StageStatus status={status} />,
          },
          { title: '结果', dataIndex: 'summary' },
          {
            title: '证据',
            key: 'evidence',
            render: (_: unknown, node: DeliveryBatch['nodes'][number]) => (
              <Space wrap>
                {node.executionTaskId ? (
                  <Button
                    type="link"
                    onClick={() => onEvidence('execution_task', node.executionTaskId!)}
                  >
                    任务日志与结果
                  </Button>
                ) : null}
                {node.dockerOperationId ? (
                  <Link
                    to={`/compute/tasks/operations?domain=docker&view=logs&taskId=${encodeURIComponent(node.dockerOperationId)}`}
                  >
                    Docker 日志与结果
                  </Link>
                ) : null}
                {node.buildRecordId ? (
                  <Button type="link" onClick={() => onEvidence('build', node.buildRecordId!)}>
                    构建明细
                  </Button>
                ) : null}
                {node.releaseBundleId ? (
                  <Button
                    type="link"
                    onClick={() => onEvidence('release_bundle', node.releaseBundleId!)}
                  >
                    产物
                  </Button>
                ) : null}
                {node.deliveryPlanId ? (
                  <Button type="link" onClick={() => onPlan(node.deliveryPlanId!)}>
                    计划与审批
                  </Button>
                ) : null}
              </Space>
            ),
          },
        ]}
      />
    </Space>
  )
}

export function DeliveryBatchDetailPage() {
  const { batchId = '' } = useParams()
  const permissions = usePermissionSnapshot()
  const canView = hasPermission(permissions.data?.data, 'delivery.workflows.view')
  const canTrigger = hasPermission(permissions.data?.data, 'delivery.workflows.trigger')
  const query = useQuery(deliveryQueries.batches.detail(batchId, canView))
  const queryClient = useQueryClient()
  const cancel = useMutation(deliveryMutations.batches.cancel(queryClient))
  const [search, setSearch] = useSearchParams()
  const updateSelection = (values: Record<string, string | undefined>) =>
    setSearch(
      (current) => {
        const next = new URLSearchParams(current)
        for (const [key, value] of Object.entries(values)) {
          if (value) next.set(key, value)
          else next.delete(key)
        }
        return next
      },
      { replace: true },
    )
  const planId = search.get('planId') || undefined
  const setPlanId = (id?: string) => updateSelection({ planId: id })
  const evidenceKind = search.get('evidenceKind')
  const evidenceId = search.get('evidenceId') || ''
  const onEvidence = (kind: string, id: string) =>
    updateSelection({ evidenceKind: kind, evidenceId: id })
  const selectedTarget = search.get('targetId') || ''

  const [retry, setRetry] = useState(false)
  const cancelSubmitting = useRef(false)
  const cancelLocked = cancel.isPending || writeOutcomeUnknown(cancel.error)
  const batch = readableQueryData(query)
  if (!canView)
    return (
      <ManagementState
        kind={permissions.isLoading ? 'loading' : 'empty'}
        title="没有交付记录查看权限"
      />
    )
  if (!batch)
    return (
      <ManagementState
        kind={query.isError ? 'error' : 'loading'}
        description={query.error?.message}
        actions={
          query.isError ? <Button onClick={() => void query.refetch()}>重试</Button> : undefined
        }
      />
    )
  const templateBatch =
    batch.targets.length > 0 &&
    batch.targets.every((item) => Boolean(item.target.templateCandidateId))
  const templateReceipt = templateBatch ? batch.targets[0]?.templateUpdate : undefined
  const pendingAdoptionIds = batch.targets
    .filter((item) => targetStageNode(batch, item.target.id, 'deploy')?.status !== 'completed')
    .flatMap((item) => (item.templateUpdate ? [item.templateUpdate.bindingId] : []))
  return (
    <div className="soha-page">
      <Space wrap style={{ justifyContent: 'space-between', width: '100%' }}>
        <Space wrap>
          <Link to="/execution-history">执行记录</Link>
          <Typography.Text strong>{batch.definition.name || '交付详情'}</Typography.Text>
          <StageStatus status={batch.status} />
        </Space>
        <Space>
          <Button onClick={() => void query.refetch()} loading={query.isFetching}>
            刷新
          </Button>
          {canTrigger && !batch.partialView && isBatchTerminal(batch.status) ? (
            templateReceipt ? (
              <TemplateUpdateEntry
                kind={templateReceipt.kind === 'blueprint' ? 'deployment' : templateReceipt.kind}
                templateId={templateReceipt.templateId}
                initialVersion={templateReceipt.toVersion}
                initialTargetIds={pendingAdoptionIds}
                label="重新比较未完成项"
              />
            ) : (
              <Button onClick={() => setRetry(true)}>按参数重跑整个流程</Button>
            )
          ) : null}
          {canTrigger && !batch.partialView && !isBatchTerminal(batch.status) ? (
            <Popconfirm
              title="停止本次交付？"
              description="停止新派发，并等待在途任务确认取消；已完成目标保留结果。"
              disabled={cancelLocked}
              onConfirm={async () => {
                if (cancelSubmitting.current || cancelLocked || query.isError) return
                cancelSubmitting.current = true
                try {
                  await cancel.mutateAsync({
                    id: batch.id,
                    input: { reason: '用户从交付记录停止本次发布' },
                  })
                } catch {
                  /* Mutation feedback keeps the original request visible. */
                } finally {
                  cancelSubmitting.current = false
                }
              }}
            >
              <Button
                danger
                disabled={batch.status === 'canceling' || cancelLocked || query.isError}
                loading={cancel.isPending}
              >
                停止交付
              </Button>
            </Popconfirm>
          ) : null}
        </Space>
      </Space>
      <ReadFeedback query={query} label="交付记录" />
      <OperationFeedback
        pending={cancel.isPending}
        error={cancel.error}
        label="停止交付"
        target={batch.definition.name || batch.id}
        receipt={
          cancel.isSuccess ? { message: '停止请求已受理，以交付记录的最终状态为准。' } : undefined
        }
      />
      {templateBatch ? (
        <Alert
          type="info"
          showIcon
          title="模板配置采用批次"
          description="采用成功表示目标环境配置已更新，运行资源和产物仍保持原部署状态；可后续创建部署 Plan 发布。部分失败后重新比较未完成项，已完成项不会重复采用。"
        />
      ) : null}
      {batch.partialView ? (
        <Alert type="info" showIcon title="当前仅显示可查看的目标，状态和计数对应这些目标。" />
      ) : null}
      {batch.stopSummary ? (
        <Alert
          type={batch.stopReason === 'failure' ? 'error' : 'info'}
          showIcon
          title={batch.stopSummary}
        />
      ) : null}
      <Descriptions
        bordered
        size="small"
        column={3}
        items={[
          {
            key: 'services',
            label: templateBatch ? '配置绑定' : '服务',
            children: templateBatch ? batch.targets.length : batch.serviceCount,
          },
          { key: 'targets', label: '环境目标', children: batch.targetCount },
          { key: 'builds', label: '实际构建', children: batch.buildCount },
          {
            key: 'mode',
            label: '执行方式',
            children: templateBatch
              ? '分批采用配置'
              : deliveryModeLabels[batch.definition.mode ?? 'service_serial'],
          },
          {
            key: 'stop',
            label: '失败停止',
            children:
              batch.definition.stopOnFailure === undefined
                ? '—'
                : batch.definition.stopOnFailure
                  ? '开启'
                  : '关闭',
          },
          { key: 'at', label: '开始时间', children: formatDateTime(batch.createdAt) },
          ...(batch.workflowId
            ? [
                {
                  key: 'workflow',
                  label: '工作流版本',
                  children: (
                    <Link to={`/release-board?workflowId=${encodeURIComponent(batch.workflowId)}`}>
                      v{batch.workflowVersion}
                    </Link>
                  ),
                },
              ]
            : []),
          ...(batch.retryOfBatchId
            ? [
                {
                  key: 'retry',
                  label: '上次交付',
                  children: (
                    <Link to={`/delivery/batches/${encodeURIComponent(batch.retryOfBatchId)}`}>
                      查看原记录
                    </Link>
                  ),
                },
              ]
            : []),
        ]}
      />
      <AdminTable
        rowKey={(item: DeliveryTargetSnapshot) => item.target.id}
        localSorting={false}
        pagination={false}
        dataSource={batch.targets}
        expandable={{
          expandedRowKeys: selectedTarget ? [selectedTarget] : [],
          onExpand: (expanded: boolean, snapshot: DeliveryTargetSnapshot) =>
            updateSelection({ targetId: expanded ? snapshot.target.id : undefined }),
          expandedRowRender: (snapshot: DeliveryTargetSnapshot) => (
            <DeliveryTargetEvidence
              batch={batch}
              snapshot={snapshot}
              onPlan={setPlanId}
              onEvidence={onEvidence}
            />
          ),
        }}
        scroll={{ x: 1060 }}
        columns={[
          {
            title: '顺序',
            key: 'order',
            width: 70,
            render: (_: unknown, _target: DeliveryTargetSnapshot, index: number) => index + 1,
          },
          {
            title: '应用 / 服务',
            key: 'service',
            width: 220,
            render: (_: unknown, snapshot: DeliveryTargetSnapshot) => (
              <Link
                to={`/applications/${encodeURIComponent(snapshot.target.applicationId)}?serviceId=${encodeURIComponent(snapshot.target.serviceId)}&applicationEnvironmentId=${encodeURIComponent(snapshot.target.applicationEnvironmentId ?? '')}`}
              >
                {snapshot.applicationName} / {snapshot.serviceName}
              </Link>
            ),
          },
          { title: '环境', dataIndex: 'environmentName', width: 140 },
          {
            title: '方式',
            key: 'action',
            width: 130,
            render: (_: unknown, snapshot: DeliveryTargetSnapshot) =>
              deliveryTargetActionLabel(snapshot.target),
          },
          ...Object.entries(
            templateBatch ? { plan: '版本校验与审批', deploy: '采用配置' } : deliveryStageLabels,
          ).map(([stage, title]) => ({
            title,
            key: stage,
            width: 140,
            render: (_: unknown, snapshot: DeliveryTargetSnapshot) => {
              const node = targetStageNode(batch, snapshot.target.id, stage)
              return (
                <Space orientation="vertical" size={0}>
                  <StageStatus status={node?.status} />
                  {node?.deliveryPlanId && stage === 'plan' ? (
                    <Button type="link" size="small" onClick={() => setPlanId(node.deliveryPlanId)}>
                      查看计划
                    </Button>
                  ) : null}
                </Space>
              )
            },
          })),
        ]}
      />
      {evidenceId &&
      (evidenceKind === 'execution_task' ||
        evidenceKind === 'build' ||
        evidenceKind === 'release_bundle') ? (
        <Drawer
          open
          title={
            evidenceKind === 'build'
              ? '构建明细'
              : evidenceKind === 'execution_task'
                ? '任务日志与结果'
                : '产物'
          }
          size={880}
          onClose={() => updateSelection({ evidenceKind: undefined, evidenceId: undefined })}
        >
          <Suspense fallback={<ManagementState kind="loading" />}>
            <RuntimeDetailContent kind={evidenceKind} recordId={evidenceId} embedded />
          </Suspense>
        </Drawer>
      ) : null}
      {planId ? <DeliveryBatchPlanDrawer id={planId} onClose={() => setPlanId(undefined)} /> : null}
      {retry ? (
        <DeliveryBatchEditor
          initialDefinition={batch.definition}
          retryOfBatchId={batch.id}
          onClose={() => setRetry(false)}
        />
      ) : null}
    </div>
  )
}
