import { useRef } from 'react'
import { Alert, Button, Card, Descriptions, Drawer, Space, Typography } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { OperationFeedback, ReadFeedback } from '@/components/request-feedback'
import { readableQueryData, writeOutcomeUnknown } from '@/utils/query-feedback'
import { StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { templateUpdateQueries } from '../template-updates/queries'
import { TemplateAdoptionDiff } from '../template-updates/diff'
import { deliveryQueries } from '../queries'
import { deliveryMutations } from '../mutations'

const approvalLabels: Record<string, string> = {
  requested: '已申请审批',
  approved: '已批准',
  rejected: '已拒绝',
}

export function DeliveryBatchPlanDrawer({ id, onClose }: { id: string; onClose: () => void }) {
  const queryClient = useQueryClient()
  const query = useQuery(deliveryQueries.plans.detail(id))
  const decision = useMutation(deliveryMutations.plans.approval(queryClient))
  const permissions = usePermissionSnapshot()
  const canApprove = hasPermission(
    permissions.data?.data,
    'delivery.application-environments.approve',
  )
  const submitting = useRef(false)
  const locked = decision.isPending || writeOutcomeUnknown(decision.error)
  const plan = readableQueryData(query)
  const configurationOnly = Boolean(plan?.templateCandidateId)
  const previewId =
    configurationOnly && typeof plan?.impact?.templatePreviewId === 'string'
      ? plan.impact.templatePreviewId
      : ''
  const previewQuery = useQuery(templateUpdateQueries.preview(previewId))
  const templateCandidate = readableQueryData(previewQuery)?.candidates.find(
    (item) => item.id === plan?.templateCandidateId,
  )
  const comparisonMissing =
    configurationOnly && (!templateCandidate || previewQuery.isError || previewQuery.isLoading)
  const decide = async (action: 'approve' | 'reject') => {
    if (
      submitting.current ||
      locked ||
      query.isError ||
      comparisonMissing ||
      !canApprove ||
      plan?.status !== 'waiting_approval'
    )
      return
    submitting.current = true
    try {
      await decision.mutateAsync({ id, action })
    } catch {
      /* The persistent receipt owns this failure. */
    } finally {
      submitting.current = false
    }
  }
  const history = Array.isArray(plan?.impact?.approval)
    ? (plan.impact.approval as Record<string, unknown>[])
    : []
  return (
    <Drawer
      open
      title={configurationOnly ? '模板配置采用计划' : '最终部署计划'}
      size={880}
      onClose={onClose}
      footer={
        <Space>
          <Button onClick={onClose}>关闭</Button>
          {plan?.status === 'waiting_approval' && canApprove ? (
            <>
              <Button
                danger
                disabled={locked || query.isError || comparisonMissing}
                onClick={() => void decide('reject')}
              >
                拒绝
              </Button>
              <Button
                type="primary"
                loading={decision.isPending}
                disabled={locked || query.isError || comparisonMissing}
                onClick={() => void decide('approve')}
              >
                批准此计划
              </Button>
            </>
          ) : null}
        </Space>
      }
    >
      <Space orientation="vertical" size={16} style={{ width: '100%' }}>
        <ReadFeedback query={query} label={configurationOnly ? '配置采用计划' : '部署计划'} />
        {configurationOnly ? <ReadFeedback query={previewQuery} label="冻结模板差异" /> : null}
        <Button loading={query.isFetching} onClick={() => void query.refetch()}>
          刷新计划
        </Button>
        <OperationFeedback
          pending={decision.isPending}
          error={decision.error}
          label={decision.variables?.action === 'reject' ? '拒绝计划' : '批准计划'}
          target={id}
          receipt={
            decision.isSuccess
              ? { message: '审批请求已受理，以计划状态和审批记录为准。' }
              : undefined
          }
        />
        {query.isLoading ? <Typography.Text>正在读取计划…</Typography.Text> : null}
        {plan ? (
          <>
            <Alert
              type={plan.status === 'waiting_approval' ? 'warning' : 'info'}
              showIcon
              title={
                configurationOnly
                  ? '采用模板配置；不会直接发布或改变运行资源。请核对冻结的累计与逐版本差异。'
                  : plan.status === 'waiting_approval'
                    ? '待审批：核对目标与资源清单后批准'
                    : '此计划固定了本次交付的产物和目标资源，执行结果见批次详情。'
              }
            />
            <Descriptions
              bordered
              size="small"
              column={1}
              items={[
                {
                  key: 'application',
                  label: '应用',
                  children: plan.applicationName || plan.applicationId,
                },
                {
                  key: 'environment',
                  label: '环境',
                  children: plan.environmentKey || plan.applicationEnvironmentId,
                },
                { key: 'target', label: '目标', children: plan.targetSummary || '—' },
                { key: 'status', label: '计划状态', children: <StatusTag value={plan.status} /> },
                {
                  key: 'bundle',
                  label: '产物',
                  children: plan.releaseBundleId ? (
                    <Link
                      to={`/delivery/release-bundles/${encodeURIComponent(plan.releaseBundleId)}`}
                    >
                      查看已冻结产物
                    </Link>
                  ) : (
                    '—'
                  ),
                },
                { key: 'risk', label: '风险', children: <StatusTag value={plan.riskLevel} /> },
                { key: 'rollback', label: '回滚策略', children: plan.rollbackStrategy || '—' },
              ]}
            />
            {configurationOnly && templateCandidate ? (
              <TemplateAdoptionDiff candidate={templateCandidate} />
            ) : null}
            {plan.dockerSnapshots?.map((snapshot) => (
              <Card key={snapshot.targetId} size="small" title="Docker / Compose 部署">
                <Descriptions
                  size="small"
                  column={1}
                  items={[
                    { key: 'host', label: '主机', children: snapshot.hostId },
                    {
                      key: 'project',
                      label: '项目',
                      children: (
                        <Link
                          to={`/compute/runtimes/projects/${encodeURIComponent(snapshot.projectId)}`}
                        >
                          {snapshot.projectId}
                        </Link>
                      ),
                    },
                    {
                      key: 'config',
                      label: '原项目配置摘要',
                      children: (
                        <Typography.Text code copyable>
                          {snapshot.projectDigest}
                        </Typography.Text>
                      ),
                    },
                    {
                      key: 'rendered',
                      label: '本次部署配置摘要',
                      children: (
                        <Typography.Text code copyable>
                          {snapshot.renderedDigest}
                        </Typography.Text>
                      ),
                    },
                    {
                      key: 'preflight',
                      label: '预检',
                      children: (
                        <Link
                          to={`/compute/tasks/operations?domain=docker&view=logs&taskId=${encodeURIComponent(snapshot.preflightOperationId)}`}
                        >
                          预检日志与结果
                        </Link>
                      ),
                    },
                    {
                      key: 'deploy',
                      label: '部署操作标识',
                      children: (
                        <Typography.Text code copyable>
                          {snapshot.deployOperationId}
                        </Typography.Text>
                      ),
                    },
                  ]}
                />
                <details>
                  <summary>固定镜像（{Object.keys(snapshot.images).length}）</summary>
                  <ul>
                    {Object.entries(snapshot.images).map(([name, image]) => (
                      <li key={name}>
                        {name} ·{' '}
                        <Typography.Text code copyable>
                          {image}
                        </Typography.Text>
                      </li>
                    ))}
                  </ul>
                </details>
              </Card>
            ))}
            {plan.helmSnapshots?.map((snapshot) => (
              <Card
                key={snapshot.targetId}
                size="small"
                title={`${snapshot.releaseName} · ${snapshot.clusterId} / ${snapshot.namespace}`}
              >
                <Descriptions
                  size="small"
                  column={1}
                  items={[
                    {
                      key: 'operation',
                      label: '操作',
                      children: { install: '首次安装', upgrade: '升级', rollback: '回滚' }[
                        snapshot.operation
                      ],
                    },
                    {
                      key: 'chart',
                      label: 'Chart',
                      children: `${snapshot.chart} ${snapshot.chartVersion}`,
                    },
                    {
                      key: 'revision',
                      label: 'Release 版本',
                      children: `${snapshot.expectedRevision} → ${snapshot.expectedRevision + 1}${snapshot.rollbackRevision ? `（恢复 revision ${snapshot.rollbackRevision}）` : ''}`,
                    },
                    {
                      key: 'digest',
                      label: 'Chart 摘要',
                      children: (
                        <Typography.Text code copyable>
                          {snapshot.chartDigest}
                        </Typography.Text>
                      ),
                    },
                    {
                      key: 'render',
                      label: '资源清单摘要',
                      children: (
                        <Typography.Text code copyable>
                          {snapshot.renderedDigest}
                        </Typography.Text>
                      ),
                    },
                    {
                      key: 'preflight',
                      label: '预检',
                      children: (
                        <Link
                          to={`/delivery/execution-tasks/${encodeURIComponent(snapshot.preflightTaskId)}`}
                        >
                          预检日志与结果
                        </Link>
                      ),
                    },
                  ]}
                />
                <details>
                  <summary>待应用资源（{snapshot.resources.length}）</summary>
                  <ul>
                    {snapshot.resources.map((resource) => (
                      <li
                        key={`${resource.apiVersion}/${resource.kind}/${resource.namespace}/${resource.name}`}
                      >
                        {resource.kind} · {resource.namespace}/{resource.name}
                        {resource.hook ? ` · ${resource.hook}` : ''}
                      </li>
                    ))}
                  </ul>
                </details>
              </Card>
            ))}
            {plan.manifestSnapshots?.map((snapshot) => (
              <Card
                key={snapshot.bindingId}
                size="small"
                title={`${snapshot.clusterId} / ${snapshot.namespace}`}
              >
                <Descriptions
                  size="small"
                  column={1}
                  items={[
                    {
                      key: 'version',
                      label: '配置版本',
                      children: `${snapshot.packageId} · v${snapshot.revision}`,
                    },
                    {
                      key: 'commit',
                      label: '清单来源 Commit',
                      children: snapshot.sourceCommit || 'Soha 托管版本',
                    },
                    {
                      key: 'digest',
                      label: '资源清单摘要',
                      children: (
                        <Typography.Text code copyable>
                          {snapshot.renderedDigest}
                        </Typography.Text>
                      ),
                    },
                    {
                      key: 'preflight',
                      label: '预检',
                      children: (
                        <Link
                          to={`/delivery/execution-tasks/${encodeURIComponent(snapshot.preflightTaskId)}`}
                        >
                          预检日志与结果
                        </Link>
                      ),
                    },
                  ]}
                />
                <details>
                  <summary>
                    查看待应用资源（
                    {snapshot.documents.length + (snapshot.gitOpsDocuments?.length ?? 0)}）
                  </summary>
                  {[...snapshot.documents, ...(snapshot.gitOpsDocuments ?? [])].map((document) => (
                    <pre
                      className="soha-json-block"
                      key={`${document.kind}/${document.namespace}/${document.name}`}
                    >
                      {document.content}
                    </pre>
                  ))}
                </details>
              </Card>
            ))}
            {history.length ? (
              <Card size="small" title="审批记录">
                <Space orientation="vertical">
                  {history.map((item, index) => (
                    <Typography.Text key={index}>
                      {String(item.actorName || item.actorId || '')} ·{' '}
                      {approvalLabels[String(item.status)] || '未知状态'} · {String(item.at || '')}
                      {item.comment ? ` · ${String(item.comment)}` : ''}
                    </Typography.Text>
                  ))}
                </Space>
              </Card>
            ) : null}
          </>
        ) : null}
      </Space>
    </Drawer>
  )
}
