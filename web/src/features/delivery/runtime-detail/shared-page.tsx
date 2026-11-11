import {
  Alert,
  Button,
  Card,
  Collapse,
  Descriptions,
  Drawer,
  Space,
  Tag,
  Timeline,
  Typography,
  theme,
} from 'antd'
import {
  LinkOutlined,
  ReloadOutlined,
  CheckOutlined,
  ClockCircleOutlined,
  CloseOutlined,
} from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { ManagementDetailHeader, ManagementState } from '@/components/management-list'
import { ReadFeedback, requestErrorMessage, useInlineApiError } from '@/components/request-feedback'
import { useDocumentHidden } from '@/utils/use-document-hidden'
import { readableQueryData } from '@/utils/query-feedback'
import { StatusTag } from '@/components/status-tag'
import { isApiError } from '@/services/api-error'
import { formatDateTime } from '@/utils/time'
import { runtimeDetailQueries } from '../queries'
import { ExecutionRolloutPanel } from './rollout-panel'
import { WorkflowExecutionDetail } from './workflow-execution-detail'
import { workflowExecutionIsLive, workflowExecutionState } from './workflow-execution-model'
import type {
  ApplicationEnvironment,
  BuildRecord,
  DeliveryRuntimeKind,
  DeliveryRuntimeRecord,
  DeliveryEnvironment,
  DeliveryApplicationBindingSummary,
  ExecutionTask,
  ExecutionArtifact,
  ReleaseBundle,
  ReleaseRecord,
  RuntimeObjectDetail,
  RuntimeObjectPermissions,
  WorkflowRun,
  DeliveryApplicationDetail,
} from '../types'

const { Paragraph, Text } = Typography

type RuntimeKind = DeliveryRuntimeKind
type RuntimeRecord = DeliveryRuntimeRecord

type RuntimeEnvironment = DeliveryApplicationBindingSummary | undefined

type ExecutionLogItem = { id: string; logLevel: string; message: string; createdAt: string }

const DEFAULT_RUNTIME_PERMISSIONS: RuntimeObjectPermissions = {
  canViewArtifacts: false,
  canViewAudit: false,
  canViewOperations: false,
}

function runtimeKindLabel(kind: RuntimeKind) {
  switch (kind) {
    case 'build':
      return 'Build'
    case 'workflow':
      return 'Workflow Run'
    case 'release':
      return 'Release'
    case 'release_bundle':
      return 'Release Bundle'
    case 'execution_task':
      return 'Execution Task'
  }
}

function runtimeKindTitle(kind: RuntimeKind) {
  switch (kind) {
    case 'build':
      return '构建详情'
    case 'workflow':
      return '工作流详情'
    case 'release':
      return '发布详情'
    case 'release_bundle':
      return '版本包详情'
    case 'execution_task':
      return '执行任务详情'
  }
}

function runtimeStatus(record: RuntimeRecord | undefined) {
  if (!record) return 'unknown'
  return record.status || 'unknown'
}

function renderMetadata(value?: Record<string, unknown>) {
  const text = value && Object.keys(value).length > 0 ? JSON.stringify(value, null, 2) : ''
  return text ? (
    <pre className="soha-system-json-block">{text}</pre>
  ) : (
    <ManagementState
      bordered={false}
      compact
      title="暂无元数据"
      description="当前对象没有附加元数据。"
    />
  )
}

function renderExternalPipeline(value?: Record<string, unknown>) {
  const source = value?.externalPipeline
  if (!source || typeof source !== 'object' || Array.isArray(source)) return null
  const run = source as Record<string, unknown>
  const url = typeof run.url === 'string' && /^https?:\/\//i.test(run.url) ? run.url : undefined
  return (
    <Space orientation="vertical" size={12}>
      <Text strong>GitLab CI</Text>
      <Descriptions
        column={1}
        size="small"
        items={[
          {
            key: 'run',
            label: '外部运行',
            children: url ? (
              <a href={url} target="_blank" rel="noopener noreferrer">
                {String(run.runId || '查看流水线')}
              </a>
            ) : (
              String(run.runId || '正在对账，运行 ID 尚未确认')
            ),
          },
          { key: 'status', label: '流水线状态', children: String(run.status || 'unknown') },
          {
            key: 'stop',
            label: '停止确认',
            children: run.stopConfirmed === true ? '已确认全部停止' : '尚未确认停止',
          },
          { key: 'commit', label: 'CI 定义提交', children: String(run.pipelineCommit || '-') },
          { key: 'job', label: '产物任务 ID', children: String(run.artifactJobId || '-') },
          { key: 'report', label: '产物报告摘要', children: String(run.artifactDigest || '-') },
          { key: 'image', label: '镜像摘要', children: String(value?.imageDigest || '-') },
        ]}
      />
      {typeof value?.externalPipelineError === 'string' ? (
        <Alert type="warning" showIcon title={value.externalPipelineError} />
      ) : null}
    </Space>
  )
}

function runtimeListPath(kind: RuntimeKind) {
  switch (kind) {
    case 'build':
      return '/execution-history'
    case 'workflow':
      return '/execution-history'
    case 'release':
      return '/releases'
    case 'release_bundle':
      return '/delivery/release-bundles'
    case 'execution_task':
      return '/delivery/execution-tasks'
  }
}

function isNotFoundError(error: unknown) {
  return isApiError(error) && error.status === 404
}

function compactApplicationName(detail?: DeliveryApplicationDetail) {
  return detail?.application.name || detail?.application.key || detail?.application.id || '-'
}

function bindingEnvironmentName(environment?: DeliveryEnvironment) {
  return environment?.name || environment?.key || environment?.id || ''
}

function buildRuntimeBindingSummary(
  kind: RuntimeKind,
  record: RuntimeRecord,
  binding?: ApplicationEnvironment,
  environment?: DeliveryEnvironment,
  runtimeDetail?: RuntimeObjectDetail<RuntimeRecord>,
): DeliveryApplicationBindingSummary | undefined {
  if (!binding) return undefined
  const summary: DeliveryApplicationBindingSummary = {
    applicationEnvironmentId: binding.id,
    environmentId: binding.environmentId,
    environmentName: bindingEnvironmentName(environment),
    environmentKey: environment?.key || binding.environmentKey,
    actionKind: binding.releasePolicy?.actionKind,
    requiresApproval: Boolean(
      environment?.requiresApproval || binding.releasePolicy?.requiresApproval,
    ),
    workflowTemplateId: runtimeDetail?.workflowTemplate?.id || binding.workflowTemplateId,
    workflowTemplateName: runtimeDetail?.workflowTemplate?.name,
    workflowTemplate: runtimeDetail?.workflowTemplate || binding.workflowTemplate,
    targetCount: binding.targets?.length ?? 0,
    targets: binding.targets,
    buildSourceId: runtimeDetail?.buildSource?.id || binding.buildPolicy?.sourceId,
    buildSource: runtimeDetail?.buildSource,
    buildPolicy: binding.buildPolicy,
  }
  if (kind === 'build') summary.latestBuild = record as BuildRecord
  if (kind === 'workflow') summary.latestWorkflow = record as WorkflowRun
  if (kind === 'release') summary.latestRelease = record as ReleaseRecord
  if (kind === 'release_bundle') summary.latestBundle = record as ReleaseBundle
  if (kind === 'execution_task') summary.latestExecutionTask = record as ExecutionTask
  return summary
}

function buildApplicationDetailFromRuntime(
  kind: RuntimeKind,
  runtimeDetail?: RuntimeObjectDetail<RuntimeRecord>,
): DeliveryApplicationDetail | undefined {
  const record = runtimeDetail?.object
  const application = runtimeDetail?.application
  if (!record || !application) return undefined
  const binding = buildRuntimeBindingSummary(
    kind,
    record,
    runtimeDetail.binding,
    runtimeDetail.environment,
    runtimeDetail,
  )
  const detail: DeliveryApplicationDetail = {
    application,
    bindings: binding ? [binding] : [],
  }
  if (kind === 'build') detail.latestBuild = record as BuildRecord
  if (kind === 'workflow') detail.latestWorkflow = record as WorkflowRun
  if (kind === 'release') detail.latestRelease = record as ReleaseRecord
  if (kind === 'release_bundle') detail.latestBundle = record as ReleaseBundle
  if (kind === 'execution_task') detail.latestExecutionTask = record as ExecutionTask
  return detail
}

function findMatchedBinding(
  detail?: DeliveryApplicationDetail,
  record?: RuntimeRecord,
  runtimeKind?: RuntimeKind,
) {
  const bindings = detail?.bindings ?? []
  if (!bindings.length) return undefined
  const directEnvironmentId =
    (record as ReleaseBundle | ExecutionTask | undefined)?.applicationEnvironmentId ||
    (record as ReleaseRecord | undefined)?.metadata?.applicationEnvironmentId ||
    ''
  if (directEnvironmentId) {
    const directMatch = bindings.find(
      (binding) => binding.applicationEnvironmentId === directEnvironmentId,
    )
    if (directMatch) return directMatch
  }
  const recordId = record?.id
  if (!recordId) return undefined
  return bindings.find((binding) => {
    if (binding.latestBuild?.id === recordId) return true
    if (binding.latestWorkflow?.id === recordId) return true
    if (binding.latestRelease?.id === recordId) return true
    if (binding.latestBundle?.id === recordId) return true
    if (binding.latestExecutionTask?.id === recordId) return true
    if (
      runtimeKind === 'workflow' &&
      binding.workflowTemplate?.name &&
      binding.latestWorkflow?.workflowName === (record as WorkflowRun)?.workflowName
    )
      return true
    return false
  })
}

function runtimeTemplateLabel(
  detail?: DeliveryApplicationDetail,
  record?: RuntimeRecord,
  runtimeKind?: RuntimeKind,
) {
  const binding = findMatchedBinding(detail, record, runtimeKind)
  if (binding?.workflowTemplate?.name) return binding.workflowTemplate.name
  if (binding?.workflowTemplateName) return binding.workflowTemplateName
  if (binding?.buildSource?.name) return binding.buildSource.name
  if (binding?.buildSourceId) return binding.buildSourceId
  return '-'
}

function runtimeEnvironmentLabel(
  detail?: DeliveryApplicationDetail,
  record?: RuntimeRecord,
  runtimeKind?: RuntimeKind,
) {
  const binding = findMatchedBinding(detail, record, runtimeKind)
  if (binding?.environmentName) return binding.environmentName
  if (binding?.environmentKey) return binding.environmentKey
  if (binding?.environmentId) return binding.environmentId
  return (record as ReleaseBundle | ExecutionTask | undefined)?.applicationEnvironmentId || '-'
}

function runtimeEnvironment(
  detail?: DeliveryApplicationDetail,
  record?: RuntimeRecord,
  runtimeKind?: RuntimeKind,
): RuntimeEnvironment {
  const binding = findMatchedBinding(detail, record, runtimeKind)
  return binding
}

function runtimeAuditPath(kind: RuntimeKind, id: string) {
  const metadataKey = `runtime.${kind}.id`
  return `/system/audit?metadataKey=${encodeURIComponent(metadataKey)}&metadataValue=${encodeURIComponent(id)}`
}

function runtimeOperationPath(kind: RuntimeKind, id: string) {
  const metadataKey = `runtime.${kind}.id`
  return `/system/operations?metadataKey=${encodeURIComponent(metadataKey)}&metadataValue=${encodeURIComponent(id)}`
}

function runtimeApplicationPath(applicationId: string, focus?: Record<string, string>) {
  const params = new URLSearchParams({ tab: 'delivery', ...focus })
  return `/applications/${encodeURIComponent(applicationId)}?${params.toString()}`
}

function runtimeSummaryItems(
  kind: RuntimeKind,
  record: RuntimeRecord | undefined,
  detail?: DeliveryApplicationDetail,
  runtimeDetail?: RuntimeObjectDetail<RuntimeRecord>,
) {
  const binding = runtimeEnvironment(detail, record, kind)
  return [
    { key: 'application', label: '应用', children: compactApplicationName(detail) },
    { key: 'environment', label: '环境', children: runtimeEnvironmentLabel(detail, record, kind) },
    { key: 'template', label: '模板 / 来源', children: runtimeTemplateLabel(detail, record, kind) },
    { key: 'binding', label: '绑定 ID', children: binding?.applicationEnvironmentId || '-' },
    {
      key: 'templateId',
      label: '模板 ID',
      children: binding?.workflowTemplateId || binding?.buildSourceId || '-',
    },
    {
      key: 'artifactCount',
      label: 'Artifacts',
      children:
        runtimeDetail?.artifacts?.length ?? String(runtimeDetail?.evidence?.artifactCount ?? '-'),
    },
  ]
}

function buildTimelineItems(
  items: Array<{
    id: unknown
    title: unknown
    status: unknown
    summary?: unknown
    createdAt?: unknown
  }>,
) {
  return items.map((item, index) => {
    const id = String(item.id || index)
    const title = String(item.title || id)
    const status = String(item.status || 'unknown')
    const summary = item.summary === undefined || item.summary === null ? '' : String(item.summary)
    const createdAt =
      item.createdAt === undefined || item.createdAt === null ? '' : String(item.createdAt)
    return {
      key: id,
      content: (
        <Space orientation="vertical" size={0}>
          <Text strong>{title}</Text>
          <Text type="secondary">
            {status}
            {summary ? ` · ${summary}` : ''}
          </Text>
          {createdAt ? <Text type="secondary">{formatDateTime(createdAt)}</Text> : null}
        </Space>
      ),
      color:
        workflowExecutionState(status).tone === 'error'
          ? 'red'
          : workflowExecutionState(status).tone === 'running'
            ? 'blue'
            : workflowExecutionState(status).tone === 'success'
              ? 'green'
              : 'gray',
    }
  })
}

function RuntimeHeader({
  kind,
  record,
  detail,
}: {
  kind: RuntimeKind
  record?: RuntimeRecord
  detail?: DeliveryApplicationDetail
}) {
  return (
    <Space wrap size={[8, 8]}>
      <StatusTag
        value={
          kind === 'release_bundle'
            ? runtimeStatus(record)
            : workflowExecutionState(runtimeStatus(record)).tag
        }
        label={runtimeStatus(record)}
      />
      <Tag>{compactApplicationName(detail)}</Tag>
      <Tag>{runtimeEnvironmentLabel(detail, record, kind)}</Tag>
      <Tag>{runtimeTemplateLabel(detail, record, kind)}</Tag>
    </Space>
  )
}

function renderArtifactTimeline(
  artifacts?: ExecutionArtifact[],
  emptyDescription = '当前对象没有返回可展示的制品。',
) {
  return artifacts?.length ? (
    <Timeline
      items={artifacts.map((artifact, index) => ({
        key: artifact.id || `${artifact.kind}:${index}`,
        color: artifact.status === 'failed' ? 'red' : 'gray',
        content: (
          <Space orientation="vertical" size={6} style={{ maxWidth: '100%' }}>
            <Space wrap>
              <Text strong>{artifact.name || artifact.kind}</Text>
              <StatusTag value={artifact.status || 'available'} />
            </Space>
            {artifact.ref ? (
              <Text copyable style={{ overflowWrap: 'anywhere' }}>
                {artifact.ref}
              </Text>
            ) : null}
            {artifact.digest ? (
              <Text code copyable style={{ overflowWrap: 'anywhere' }}>
                {artifact.digest}
              </Text>
            ) : null}
            {artifact.path ? (
              <Text type="secondary" style={{ overflowWrap: 'anywhere' }}>
                {artifact.path}
              </Text>
            ) : null}
            <Space wrap>
              {artifact.workflowRunId ? (
                <Link to={`/workflows/${encodeURIComponent(artifact.workflowRunId)}`}>
                  来源工作流
                </Link>
              ) : null}
              {artifact.executionTaskId ? (
                <Link
                  to={`/delivery/execution-tasks/${encodeURIComponent(artifact.executionTaskId)}`}
                >
                  来源执行任务
                </Link>
              ) : null}
              {artifact.releaseBundleId ? (
                <Link
                  to={`/delivery/release-bundles/${encodeURIComponent(artifact.releaseBundleId)}`}
                >
                  版本包
                </Link>
              ) : null}
              {artifact.workflowNodeId ? (
                <Text type="secondary">节点 {artifact.workflowNodeId}</Text>
              ) : null}
            </Space>
            <Text type="secondary">
              {formatDateTime(artifact.createdAt || artifact.updatedAt || artifact.modifiedAt)}
            </Text>
          </Space>
        ),
      }))}
    />
  ) : (
    <ManagementState bordered={false} compact title="暂无制品" description={emptyDescription} />
  )
}

function RuntimeEvidenceCard({
  kind,
  record,
  bundleArtifacts,
  executionArtifacts,
  executionLogs,
  onEvidence,
  navigate,
}: {
  kind: RuntimeKind
  record?: RuntimeRecord
  bundleArtifacts?: ExecutionArtifact[]
  executionArtifacts?: ExecutionArtifact[]
  executionLogs?: ExecutionLogItem[]
  onEvidence?: (kind: RuntimeKind, id: string) => void
  navigate?: (path: string) => void
}) {
  if (!record) return null
  if (kind === 'workflow') {
    const workflow = record as WorkflowRun
    const timelineItems = buildTimelineItems(
      (workflow.nodeRuns?.length ? workflow.nodeRuns : workflow.steps).map((item, index) => ({
        id: ('nodeId' in item ? item.nodeId : '') || `${item.name || 'step'}:${index}`,
        title: item.name || ('nodeId' in item ? item.nodeId : `step-${index + 1}`),
        status: item.status,
        summary: item.summary,
      })),
    )
    return (
      <Card title="关键证据" className="soha-detail-card" size="small">
        <Timeline
          items={timelineItems.map((item, index) => {
            const node = workflow.nodeRuns?.[index]
            return {
              ...item,
              content: (
                <>
                  {item.content}
                  {node ? (
                    <Space wrap>
                      {node.executionTaskId ? (
                        <Button
                          size="small"
                          type="link"
                          onClick={() => onEvidence?.('execution_task', node.executionTaskId!)}
                        >
                          任务日志与结果
                        </Button>
                      ) : null}
                      {node.buildRecordId ? (
                        <Button
                          size="small"
                          type="link"
                          onClick={() => onEvidence?.('build', node.buildRecordId!)}
                        >
                          构建明细
                        </Button>
                      ) : null}
                      {node.releaseBundleId ? (
                        <Button
                          size="small"
                          type="link"
                          onClick={() => onEvidence?.('release_bundle', node.releaseBundleId!)}
                        >
                          产物
                        </Button>
                      ) : null}
                    </Space>
                  ) : null}
                </>
              ),
            }
          })}
        />
        <Paragraph className="soha-detail-card__paragraph" type="secondary">
          {workflow.clusterId || workflow.namespace || workflow.deploymentName
            ? `${workflow.clusterId || '-'} / ${workflow.namespace || '-'} / ${workflow.deploymentName || '-'}`
            : '没有额外运行证据。'}
        </Paragraph>
        <Text strong>制品</Text>
        {renderArtifactTimeline(executionArtifacts, '该工作流没有返回 artifact store 明细。')}
      </Card>
    )
  }
  if (kind === 'release_bundle') {
    const bundle = record as ReleaseBundle
    return (
      <Card title="关键证据" className="soha-detail-card" size="small">
        <Space orientation="vertical" size={6}>
          <Text strong>Artifact</Text>
          <Text>{bundle.artifactRef || '-'}</Text>
          <Text type="secondary">Digest: {bundle.artifactDigest || '-'}</Text>
          {renderArtifactTimeline(bundleArtifacts, '该版本包没有单独的制品明细。')}
        </Space>
      </Card>
    )
  }
  if (kind === 'execution_task') {
    const task = record as ExecutionTask
    return (
      <Card title="关键证据" className="soha-detail-card" size="small">
        <Space orientation="vertical" size={8}>
          {renderExternalPipeline(task.result)}
          <Text strong>日志</Text>
          {executionLogs?.length ? (
            <Timeline
              items={buildTimelineItems(
                executionLogs.map((item) => ({
                  id: item.id,
                  title: item.logLevel,
                  status: item.logLevel,
                  summary: item.message,
                  createdAt: item.createdAt,
                })),
              )}
            />
          ) : (
            <ManagementState
              bordered={false}
              compact
              title="暂无日志"
              description="该执行任务没有返回可展示的日志。"
            />
          )}
          <Text strong>制品</Text>
          {renderArtifactTimeline(executionArtifacts, '该执行任务没有返回可展示的制品。')}
          {task.releaseBundleId ? (
            <Button
              type="link"
              size="small"
              onClick={() => navigate?.(`/delivery/release-bundles/${task.releaseBundleId}`)}
            >
              {`版本包 ${task.releaseBundleId}`}
            </Button>
          ) : (
            <Text type="secondary">没有关联版本包</Text>
          )}
        </Space>
      </Card>
    )
  }
  return (
    <Card title="关键证据" className="soha-detail-card" size="small">
      {executionArtifacts ? (
        <>
          <Text strong>制品与来源</Text>
          {renderArtifactTimeline(executionArtifacts)}
        </>
      ) : null}
      {renderExternalPipeline((record as BuildRecord | ReleaseRecord).metadata)}
      {(record as BuildRecord | ReleaseRecord).metadata?.externalPipeline ? (
        <Collapse
          items={[
            {
              key: 'metadata',
              label: '完整构建证据',
              children: renderMetadata((record as BuildRecord | ReleaseRecord).metadata),
            },
          ]}
        />
      ) : (
        renderMetadata((record as BuildRecord | ReleaseRecord).metadata)
      )}
    </Card>
  )
}

export function RuntimeDetailPage({ kind }: { kind: RuntimeKind }) {
  const params = useParams()
  const [search, setSearch] = useSearchParams()
  const evidenceKind = search.get('evidenceKind')
  const evidenceId = search.get('evidenceId') || ''
  const selectedKind =
    evidenceKind === 'build' ||
    evidenceKind === 'execution_task' ||
    evidenceKind === 'release_bundle'
      ? evidenceKind
      : undefined
  const openEvidence = (kind?: RuntimeKind, id?: string) =>
    setSearch(
      (current) => {
        const next = new URLSearchParams(current)
        if (kind && id) {
          next.set('evidenceKind', kind)
          next.set('evidenceId', id)
        } else {
          next.delete('evidenceKind')
          next.delete('evidenceId')
        }
        return next
      },
      { replace: true },
    )
  const recordId = String(
    params[
      kind === 'build'
        ? 'buildId'
        : kind === 'workflow'
          ? 'workflowId'
          : kind === 'release'
            ? 'releaseId'
            : kind === 'release_bundle'
              ? 'releaseBundleId'
              : 'executionTaskId'
    ] || '',
  ).trim()

  return (
    <>
      <RuntimeDetailContent
        kind={kind}
        recordId={recordId}
        onEvidence={openEvidence}
        initialWorkflowStageKey={search.get('stage') || undefined}
      />
      <Drawer
        title="执行证据"
        open={kind === 'workflow' && !!selectedKind && !!evidenceId}
        size={880}
        onClose={() => openEvidence()}
        destroyOnHidden
      >
        {selectedKind && evidenceId ? (
          <RuntimeDetailContent kind={selectedKind} recordId={evidenceId} embedded />
        ) : null}
      </Drawer>
    </>
  )
}

export function RuntimeDetailContent({
  kind,
  recordId,
  embedded = false,
  onEvidence,
  initialWorkflowStageKey,
}: {
  kind: RuntimeKind
  recordId: string
  embedded?: boolean
  onEvidence?: (kind: RuntimeKind, id: string) => void
  initialWorkflowStageKey?: string
}) {
  const navigate = useNavigate()
  const { token } = theme.useToken()
  const hidden = useDocumentHidden()
  const runtimeDetailQuery = useQuery({
    ...runtimeDetailQueries.detail(kind, recordId),
    refetchInterval:
      kind !== 'release_bundle'
        ? (query) =>
            query.state.status !== 'error' &&
            workflowExecutionIsLive(query.state.data?.object.status)
              ? 4000
              : false
        : false,
  })
  useInlineApiError(runtimeDetailQuery.isError ? runtimeDetailQuery.error : null)
  const runtimeDetail = readableQueryData(runtimeDetailQuery)
  const retainEvidence = !!runtimeDetail

  if (!recordId) {
    return (
      <div className="soha-page">
        <ManagementState
          kind="not-found"
          description={`${runtimeKindLabel(kind)} unknown 未找到`}
        />
      </div>
    )
  }
  if (runtimeDetailQuery.isLoading) {
    return (
      <div className="soha-page">
        <ManagementState kind="loading" />
      </div>
    )
  }
  if (runtimeDetailQuery.isError && isNotFoundError(runtimeDetailQuery.error)) {
    return (
      <div className="soha-page">
        <ManagementState
          kind="not-found"
          description={`${runtimeKindLabel(kind)} ${recordId || 'unknown'} 未找到`}
        />
      </div>
    )
  }
  if (runtimeDetailQuery.isError && !retainEvidence) {
    return (
      <div className="soha-page">
        <ManagementState
          kind="error"
          actions={
            <Button aria-label="重试执行证据" onClick={() => void runtimeDetailQuery.refetch()}>
              重试
            </Button>
          }
          description={
            runtimeDetailQuery.error instanceof Error
              ? requestErrorMessage(runtimeDetailQuery.error, 'zh_CN')
              : '加载运行态详情失败'
          }
        />
      </div>
    )
  }

  const record = runtimeDetail?.object
  if (!record) {
    return (
      <div className="soha-page">
        <ManagementState
          kind="not-found"
          description={`${runtimeKindLabel(kind)} ${recordId || 'unknown'} 未找到`}
        />
      </div>
    )
  }

  const applicationDetail = buildApplicationDetailFromRuntime(kind, runtimeDetail)
  const runtimeArtifacts = runtimeDetail?.artifacts ?? []
  const bundleArtifacts = kind === 'release_bundle' ? runtimeArtifacts : undefined
  const executionArtifacts = runtimeArtifacts
  const executionLogs = Array.isArray(runtimeDetail?.evidence?.logs)
    ? (runtimeDetail.evidence.logs as ExecutionLogItem[])
    : undefined
  const links = runtimeDetail?.links
  const permissions = runtimeDetail?.permissions ?? DEFAULT_RUNTIME_PERMISSIONS
  const relatedInfo = (
    <>
      <Descriptions
        column={{ xs: 1, sm: 2 }}
        items={runtimeSummaryItems(kind, record, applicationDetail, runtimeDetail)}
      />
      <Space wrap size={[8, 8]} style={{ marginTop: 16 }}>
        <Button
          icon={<LinkOutlined />}
          onClick={() =>
            navigate(
              links?.application ||
                runtimeApplicationPath(
                  record.applicationId,
                  kind === 'build'
                    ? { buildId: record.id }
                    : kind === 'workflow'
                      ? { workflowRunId: record.id }
                      : kind === 'release'
                        ? { releaseId: record.id }
                        : kind === 'release_bundle'
                          ? { releaseBundleId: record.id }
                          : { executionTaskId: record.id },
                ),
            )
          }
        >
          打开应用运行态
        </Button>
        <Button
          disabled={!permissions.canViewAudit}
          icon={<LinkOutlined />}
          onClick={() => navigate(links?.audit || runtimeAuditPath(kind, record.id))}
        >
          审计日志
        </Button>
        <Button
          disabled={!permissions.canViewOperations}
          icon={<LinkOutlined />}
          onClick={() => navigate(links?.operations || runtimeOperationPath(kind, record.id))}
        >
          操作日志
        </Button>
        <Button
          disabled={!permissions.canViewArtifacts || !links?.artifacts}
          icon={<LinkOutlined />}
          onClick={() => links?.artifacts && navigate(links.artifacts)}
        >
          Artifacts
        </Button>
      </Space>
    </>
  )

  if (kind === 'workflow' && !embedded) {
    return (
      <WorkflowExecutionDetail
        key={record.id}
        record={record as WorkflowRun}
        applicationName={compactApplicationName(applicationDetail)}
        environmentName={runtimeEnvironmentLabel(applicationDetail, record, kind)}
        sourceName={runtimeTemplateLabel(applicationDetail, record, kind)}
        refreshing={runtimeDetailQuery.isFetching}
        refreshError={runtimeDetailQuery.isError ? runtimeDetailQuery.error : undefined}
        onRefresh={async () => {
          const result = await runtimeDetailQuery.refetch()
          if (result.error) throw result.error
          return result.data?.object as WorkflowRun | undefined
        }}
        onBack={() => navigate(runtimeListPath(kind))}
        onEvidence={onEvidence}
        initialStageKey={initialWorkflowStageKey}
        relatedInfo={relatedInfo}
        artifacts={renderArtifactTimeline(runtimeArtifacts, '该工作流没有返回制品明细。')}
        artifactCount={runtimeDetail?.artifacts?.length}
      />
    )
  }

  return (
    <div
      className="soha-page soha-runtime-evidence soha-workflow-execution"
      data-motion-disabled={!token.motion}
      data-motion-paused={hidden}
    >
      <ReadFeedback query={runtimeDetailQuery} label="执行证据" />
      {!embedded ? (
        <ManagementDetailHeader
          title={runtimeKindTitle(kind)}
          description={record.id}
          meta={<RuntimeHeader kind={kind} record={record} detail={applicationDetail} />}
          actions={<Button onClick={() => navigate(runtimeListPath(kind))}>返回列表</Button>}
        />
      ) : null}
      <section
        className={`soha-execution-feedback is-${kind === 'release_bundle' ? 'idle' : workflowExecutionState(record.status).tone}`}
        aria-label="运行记录状态"
      >
        <div className="soha-execution-feedback-main" role="status" aria-live="polite">
          <span className="soha-execution-state-mark" aria-hidden>
            {workflowExecutionState(record.status).tone === 'success' ? (
              <CheckOutlined />
            ) : workflowExecutionState(record.status).tone === 'error' ? (
              <CloseOutlined />
            ) : (
              <ClockCircleOutlined />
            )}
          </span>
          <div>
            <strong>
              {kind === 'release_bundle'
                ? (record as ReleaseBundle).version || '版本包'
                : workflowExecutionState(record.status).label}
            </strong>
            <p>
              {kind === 'release_bundle'
                ? '核对制品引用与摘要，再查看来源记录和审计。'
                : workflowExecutionIsLive(record.status)
                  ? '状态自动更新，日志与制品以当前读取的证据为准。'
                  : '运行记录已保留，可继续核对制品、来源和审计。'}
            </p>
          </div>
        </div>
        <div className="soha-execution-counts">
          <span>
            <b>{runtimeArtifacts.length}</b> 项制品
          </span>
          <small>
            {runtimeDetailQuery.isError
              ? '证据更新已暂停'
              : `读取于 ${formatDateTime(new Date(runtimeDetailQuery.dataUpdatedAt).toISOString())}`}
          </small>
        </div>
      </section>
      <Card
        className="soha-detail-card"
        title="基础信息"
        size="small"
        extra={
          <Button
            size="small"
            icon={<ReloadOutlined />}
            loading={runtimeDetailQuery.isFetching}
            onClick={() => void runtimeDetailQuery.refetch()}
          >
            刷新证据
          </Button>
        }
      >
        <Descriptions
          column={{ xs: 1, sm: 2 }}
          items={[
            { key: 'id', label: '对象 ID', children: record.id },
            { key: 'kind', label: '类型', children: runtimeKindLabel(kind) },
            { key: 'status', label: '状态', children: <StatusTag value={record.status} /> },
            {
              key: 'application',
              label: '关联应用',
              children: compactApplicationName(applicationDetail),
            },
            {
              key: 'environment',
              label: '关联环境',
              children: runtimeEnvironmentLabel(applicationDetail, record, kind),
            },
            {
              key: 'template',
              label: '关联模板/来源',
              children: runtimeTemplateLabel(applicationDetail, record, kind),
            },
            {
              key: 'createdAt',
              label: '创建时间',
              children: formatDateTime(
                (
                  record as
                    | BuildRecord
                    | ReleaseRecord
                    | ReleaseBundle
                    | ExecutionTask
                    | WorkflowRun
                ).createdAt,
              ),
            },
            {
              key: 'updatedAt',
              label: '更新时间',
              children: formatDateTime(
                (record as WorkflowRun | ReleaseBundle | ExecutionTask).updatedAt ||
                  (record as ReleaseRecord).deployedAt ||
                  (record as BuildRecord).finishedAt ||
                  (record as BuildRecord).startedAt ||
                  (record as ReleaseRecord).createdAt,
              ),
            },
          ]}
        />
      </Card>
      <Card className="soha-detail-card" title="关联信息" size="small">
        {relatedInfo}
      </Card>
      {kind === 'execution_task' ? <ExecutionRolloutPanel task={record as ExecutionTask} /> : null}
      <RuntimeEvidenceCard
        kind={kind}
        record={record}
        bundleArtifacts={bundleArtifacts}
        executionArtifacts={executionArtifacts}
        executionLogs={executionLogs}
        onEvidence={onEvidence}
        navigate={navigate}
      />
    </div>
  )
}
