import type { WorkbenchSource } from '@opensoha/contracts/gen/ts/sohaapi'

import {
  canonicalWorkbenchAgentStatus,
  WorkbenchStreamTransportError,
  type WorkbenchStreamState,
} from './stream'
import type { WorkbenchArtifact, WorkbenchToolCall } from './types'
import type { ConversationMessage, WorkbenchBubbleStatus } from './conversation'
import type { WorkbenchArtifactEntry } from './artifacts'

export type ThoughtChainStatus = 'loading' | 'success' | 'error' | 'abort'

export function toolExecutionSummary(tool: Pick<WorkbenchToolCall, 'toolName' | 'summary'>) {
  return tool.summary === `${tool.toolName} executed` ? '' : tool.summary || ''
}

export function toolExecutionLabel(name: string) {
  return (
    (
      {
        'knowledge.search': '检索知识库',
        'k8s.workloads.overview': '读取工作负载概览',
        'k8s.nodes.detail': '读取节点详情',
        'k8s.services.backends': '检查服务后端',
        'artifact.preview': '生成静态草稿',
        'change.request': '提交变更申请',
        'agent.delegate': '专业只读核验',
      } as Record<string, string>
    )[name] || name
  )
}

function recordPreview(value: unknown): Record<string, unknown> | undefined {
  if (!value) return undefined
  if (typeof value === 'object' && !Array.isArray(value)) {
    return value as Record<string, unknown>
  }
  return { preview: value }
}

function streamToolOutputPreview(tool: WorkbenchStreamState['toolCalls'][number]) {
  if (!tool.outputLog) return tool.outputPreview
  if (
    tool.outputPreview &&
    typeof tool.outputPreview === 'object' &&
    !Array.isArray(tool.outputPreview)
  ) {
    return { ...(tool.outputPreview as Record<string, unknown>), log: tool.outputLog }
  }
  if (tool.outputPreview) return { output: tool.outputPreview, log: tool.outputLog }
  return { log: tool.outputLog }
}

function streamToolCallToWorkbenchToolCall(
  tool: WorkbenchStreamState['toolCalls'][number],
): WorkbenchToolCall {
  return {
    id: tool.id,
    adapterId: tool.adapterId,
    toolName: tool.toolName,
    status: tool.status,
    summary: tool.summary,
    input: recordPreview(tool.inputPreview),
    output: recordPreview(streamToolOutputPreview(tool)),
    startedAt: tool.startedAt ?? new Date().toISOString(),
    completedAt: tool.completedAt,
  }
}

function isWorkbenchArtifact(value: unknown): value is WorkbenchArtifact {
  if (!value || typeof value !== 'object') return false
  const item = value as Partial<WorkbenchArtifact>
  return (
    typeof item.kind === 'string' &&
    typeof item.runId === 'string' &&
    typeof item.summary === 'string'
  )
}

function streamStateArtifacts(state: WorkbenchStreamState): WorkbenchArtifact[] {
  return state.artifacts
    .filter(isWorkbenchArtifact)
    .filter((artifact) => artifact.kind !== 'stream')
}

export function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value && typeof value === 'object' && !Array.isArray(value))
}

function normalizeWorkbenchToolCall(value: unknown): WorkbenchToolCall | undefined {
  if (!isRecord(value)) return undefined
  if (
    typeof value.id !== 'string' ||
    typeof value.adapterId !== 'string' ||
    typeof value.toolName !== 'string' ||
    typeof value.status !== 'string'
  )
    return undefined
  return {
    id: value.id,
    adapterId: value.adapterId,
    toolName: value.toolName,
    status: value.status,
    summary: typeof value.summary === 'string' ? value.summary : undefined,
    input: isRecord(value.input) ? value.input : undefined,
    output: isRecord(value.output) ? value.output : undefined,
    startedAt: typeof value.startedAt === 'string' ? value.startedAt : '',
    completedAt: typeof value.completedAt === 'string' ? value.completedAt : undefined,
  }
}

function isWorkbenchSource(value: unknown): value is WorkbenchSource {
  if (!isRecord(value)) return false
  return (
    typeof value.id === 'string' &&
    typeof value.kind === 'string' &&
    typeof value.title === 'string'
  )
}

function isAgentStatusSnapshot(
  value: unknown,
): value is NonNullable<WorkbenchStreamState['agentStatus']> {
  if (!isRecord(value)) return false
  return (
    typeof value.providerId === 'string' &&
    typeof value.providerKind === 'string' &&
    typeof value.status === 'string'
  )
}

function metadataArray<T>(
  metadata: Record<string, unknown> | undefined,
  key: string,
  guard: (value: unknown) => value is T,
) {
  const raw = metadata?.[key]
  return Array.isArray(raw) ? raw.filter(guard) : []
}

export function metadataToolExecutions(metadata: Record<string, unknown> | undefined) {
  const raw = metadata?.toolExecutions
  return Array.isArray(raw)
    ? raw.map(normalizeWorkbenchToolCall).filter((item): item is WorkbenchToolCall => Boolean(item))
    : []
}

export function metadataSources(metadata: Record<string, unknown> | undefined) {
  const sources = metadataArray(metadata, 'sources', isWorkbenchSource)
  if (sources.length) return sources
  const snapshot = metadata?.contextSnapshot
  if (!isRecord(snapshot) || !Array.isArray(snapshot.citations)) return sources
  return snapshot.citations
    .filter(isRecord)
    .flatMap((citation): WorkbenchSource[] =>
      typeof citation.id === 'string' && typeof citation.documentTitle === 'string'
        ? [{ id: citation.id, kind: 'document', title: citation.documentTitle }]
        : [],
    )
}

export function metadataThinkingSummary(metadata: Record<string, unknown> | undefined) {
  const raw = metadata?.thinkingSummary
  return typeof raw === 'string' ? raw : ''
}

export function metadataAgentStatus(metadata: Record<string, unknown> | undefined) {
  const raw = metadata?.agentStatus
  return isAgentStatusSnapshot(raw)
    ? { ...raw, status: canonicalWorkbenchAgentStatus(raw.status) }
    : undefined
}

export function metadataArtifacts(metadata: Record<string, unknown> | undefined) {
  return metadataArray(metadata, 'analysisArtifacts', isWorkbenchArtifact)
}

export function replayArtifactsForMessage(message: ConversationMessage): WorkbenchArtifact[] {
  const artifacts = metadataArtifacts(message.metadata).filter(
    (artifact) =>
      artifact.kind !== 'stream' &&
      !(message.metadata?.mode === 'general' && artifact.kind === 'general'),
  )
  const tools = metadataToolExecutions(message.metadata)
  return artifacts.map((artifact, index) =>
    index === 0 && tools.length ? { ...artifact, toolExecutions: tools } : artifact,
  )
}

export function streamMessageMetadata(
  current: Record<string, unknown> | undefined,
  state: WorkbenchStreamState,
) {
  const serverMetadata = state.message.metadata ?? {}
  const toolExecutions = metadataToolExecutions(serverMetadata)
  const sources = metadataSources(serverMetadata)
  const artifacts = metadataArtifacts(serverMetadata)
  const agentStatus = metadataAgentStatus(serverMetadata)
  return {
    ...(current ?? {}),
    ...serverMetadata,
    source:
      typeof serverMetadata.source === 'string'
        ? serverMetadata.source
        : (current?.source ?? 'workbench-stream'),
    streamMessageId: state.message.id,
    thinkingSummary: metadataThinkingSummary(serverMetadata) || state.thinking?.summary,
    toolExecutions:
      toolExecutions.length > 0
        ? toolExecutions
        : state.toolCalls.map(streamToolCallToWorkbenchToolCall),
    sources: sources.length > 0 ? sources : state.sources,
    analysisArtifacts: artifacts.length > 0 ? artifacts : streamStateArtifacts(state),
    agentStatus: agentStatus ?? state.agentStatus,
    runtimeAssembly: serverMetadata.runtimeAssembly ?? state.agentStatus?.metadata?.runtimeAssembly ?? current?.runtimeAssembly,
    runtimeHooks: serverMetadata.runtimeHooks ?? state.agentStatus?.metadata?.runtimeHooks ?? current?.runtimeHooks,
    ...(state.error
      ? {
          error: state.error.message,
          errorCode: state.error.code,
          errorRetryable: state.error.retryable,
        }
      : {}),
  }
}

function sourceItemsFromWorkbenchSources(sources: WorkbenchSource[]) {
  return sources.map((item) => ({
    key: item.id,
    title: item.title,
    url: item.url,
    description: item.summary || item.kind,
  }))
}

function evidenceSourceItems(artifact?: WorkbenchArtifact) {
  return (artifact?.evidence ?? []).map((item) => ({
    key: item.id,
    title: item.title,
    description: item.summary,
  }))
}

export function sourceItemsForArtifactEntry(entry?: WorkbenchArtifactEntry) {
  const metadataSourceItems = sourceItemsFromWorkbenchSources(
    metadataSources(entry?.message.metadata),
  )
  return metadataSourceItems.length > 0 ? metadataSourceItems : evidenceSourceItems(entry?.artifact)
}

export class WorkbenchStreamEventError extends Error {
  code?: string
  retryable?: boolean

  constructor(error: NonNullable<WorkbenchStreamState['error']>) {
    super(error.message)
    this.name = 'WorkbenchStreamEventError'
    this.code = error.code
    this.retryable = error.retryable
  }
}

export function workbenchStreamErrorMessage(err: Error) {
  if (
    (err instanceof WorkbenchStreamEventError || err instanceof WorkbenchStreamTransportError) &&
    err.code
  )
    return `${err.message} (${err.code})`
  return err.message
}

export function isRetryableWorkbenchStreamError(err: Error) {
  return (
    (err instanceof WorkbenchStreamEventError || err instanceof WorkbenchStreamTransportError) &&
    err.retryable === true
  )
}

export function streamBubbleStatus(state: WorkbenchStreamState): WorkbenchBubbleStatus {
  if (state.error || state.agentStatus?.status === 'failed') return 'error'
  if (state.agentStatus?.status === 'cancelled') return 'abort'
  if (state.done || state.message.done) return 'success'
  return 'loading'
}

export function streamFallbackContent(state: WorkbenchStreamState, currentContent: string) {
  if (state.message.content) return state.message.content
  if (state.error) return state.error.message
  if (state.agentStatus?.status === 'failed') return 'Agent 执行失败。'
  if (state.agentStatus?.status === 'cancelled') return '已取消本次回复。'
  if (state.agentStatus?.status === 'succeeded' && !currentContent)
    return '分析已完成，正在刷新会话。'
  return currentContent
}

export function thoughtChainStatus(status: string) {
  switch (status) {
    case 'pending':
    case 'running':
      return 'loading' as const
    case 'success':
    case 'completed':
    case 'succeeded':
      return 'success' as const
    case 'error':
    case 'failed':
      return 'error' as const
    default:
      return 'abort' as const
  }
}

export function agentStatusLabel(status?: NonNullable<WorkbenchStreamState['agentStatus']>) {
  if (!status) return ''
  return `${status.providerId} / ${canonicalWorkbenchAgentStatus(status.status)}`
}

export function toolCallSummaryText(toolCalls: WorkbenchToolCall[]) {
  const successCount = toolCalls.filter(
    (item) => thoughtChainStatus(item.status) === 'success',
  ).length
  const failedCount = toolCalls.filter((item) => thoughtChainStatus(item.status) === 'error').length
  return `${toolCalls.length} 个工具调用，${successCount} 成功，${failedCount} 失败`
}
export function toolApprovalId(tool: { toolName: string; output?: Record<string, unknown> }) {
  const related = tool.output?.relatedIds as Record<string, unknown> | undefined
  const id = related?.approvalRequestId ?? related?.confirmationRequestId
  return tool.toolName === 'change.request' && typeof id === 'string' ? id : undefined
}
