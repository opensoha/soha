import { getAIWorkbenchPathForMode, getAIWorkbenchPathForSession } from './navigation'
import type { WorkbenchArtifact, WorkbenchMessage, WorkbenchSession } from './types'

type WorkbenchMode = NonNullable<NonNullable<WorkbenchSession['metadata']>['mode']>

export type WorkbenchArtifactEntry = {
  key: string
  artifact: WorkbenchArtifact
  message: WorkbenchMessage
  index: number
}

export type ArtifactLinkKind = 'session' | 'root_cause' | 'inspection' | 'agent'

export type ArtifactContextLink = {
  key: string
  kind: ArtifactLinkKind
  label: string
  value: string
  path: string
}

function artifactModeLabel(mode?: string) {
  switch (mode) {
    case 'root_cause':
      return '根因分析'
    case 'performance':
      return '性能分析'
    case 'trace':
      return '链路分析'
    case 'inspection_review':
      return '巡检复盘'
    default:
      return '通用聊天'
  }
}

function formatArtifactTimestamp(value?: string) {
  if (!value) return '刚刚'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  }).format(date)
}

export function artifactTitle(entry: WorkbenchArtifactEntry) {
  return entry.artifact.title || artifactModeLabel(entry.artifact.kind) || entry.artifact.kind
}

export function artifactMeta(entry: WorkbenchArtifactEntry) {
  return `${staticArtifactLabel(entry.artifact.kind) || entry.artifact.kind} · ${formatArtifactTimestamp(entry.message.createdAt)}`
}

export function staticArtifactLabel(kind: string) {
  return (
    { report: '报告', configuration_preview: '配置预览', resource_table: '资源表' } as Record<
      string,
      string
    >
  )[kind]
}

export function isStaticArtifact(artifact: WorkbenchArtifact) {
  return (
    !!staticArtifactLabel(artifact.kind) && typeof artifact.dataSourceSnapshot?.content === 'string'
  )
}

export function artifactSnapshotText(
  snapshot: Record<string, unknown> | undefined,
  ...keys: string[]
) {
  if (!snapshot) return ''
  for (const key of keys) {
    const value = snapshot[key]
    if (typeof value === 'string' && value.trim()) return value.trim()
    if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  }
  return ''
}

export function artifactContextLinks(
  entry: WorkbenchArtifactEntry,
  session?: WorkbenchSession,
): ArtifactContextLink[] {
  const snapshot = entry.artifact.dataSourceSnapshot
  const sessionId =
    artifactSnapshotText(snapshot, 'sessionId') || entry.message.sessionId || session?.id || ''
  const rootCauseRunId =
    artifactSnapshotText(snapshot, 'rootCauseRunId') ||
    session?.metadata?.analysisRunRefs?.find(
      (item) => item.id === entry.artifact.runId && item.kind === 'root_cause',
    )?.id ||
    (entry.artifact.kind === 'root_cause' ? entry.artifact.runId : '')
  const inspectionRunId =
    artifactSnapshotText(snapshot, 'inspectionRunId') ||
    session?.metadata?.analysisRunRefs?.find(
      (item) => item.id === entry.artifact.runId && item.kind === 'inspection_review',
    )?.id ||
    (entry.artifact.kind === 'inspection_review' && entry.artifact.runId.startsWith('inspection-')
      ? entry.artifact.runId
      : '')
  const agentRunId =
    artifactSnapshotText(snapshot, 'agentRunId', 'agentRuntimeId') ||
    artifactSnapshotText(entry.message.metadata, 'agentRunId') ||
    (entry.artifact.runId.startsWith('agent:') ? entry.artifact.runId : '')
  const links: ArtifactContextLink[] = []

  if (sessionId) {
    links.push({
      key: 'session',
      kind: 'session',
      label: '会话',
      value: sessionId,
      path: getAIWorkbenchPathForSession({
        id: sessionId,
        metadata: { mode: entry.artifact.kind as WorkbenchMode },
      }),
    })
  }
  if (rootCauseRunId) {
    links.push({
      key: 'root-cause',
      kind: 'root_cause',
      label: '根因运行',
      value: rootCauseRunId,
      path: getAIWorkbenchPathForMode(
        'root_cause',
        new URLSearchParams({ session: sessionId, rootCauseRunId }),
      ),
    })
  }
  if (inspectionRunId) {
    links.push({
      key: 'inspection',
      kind: 'inspection',
      label: '巡检运行',
      value: inspectionRunId,
      path: `/ai-workbench/inspection?view=runs&inspectionRunId=${encodeURIComponent(inspectionRunId)}${sessionId ? `&session=${encodeURIComponent(sessionId)}` : ''}`,
    })
  }
  if (agentRunId) {
    links.push({
      key: 'agent',
      kind: 'agent',
      label: 'Agent Run',
      value: agentRunId,
      path: getAIWorkbenchPathForMode(
        entry.artifact.kind,
        new URLSearchParams({ session: sessionId, agentRunId }),
      ),
    })
  }
  return links
}

export function resolveArtifactPublishIds(
  entry: WorkbenchArtifactEntry | undefined,
  personalEntry: WorkbenchArtifactEntry | undefined,
  session?: WorkbenchSession,
): { sessionId: string; artifactId: string } {
  if (!entry) return { sessionId: '', artifactId: '' }
  const snapshot = entry.artifact.dataSourceSnapshot
  const explicitArtifactId = artifactSnapshotText(snapshot, 'artifactId')
  if (personalEntry) {
    return {
      sessionId: artifactSnapshotText(snapshot, 'sessionId') || personalEntry.message.sessionId.trim(),
      artifactId: explicitArtifactId || personalEntry.key.trim() || personalEntry.artifact.runId.trim(),
    }
  }
  const sessionId =
    artifactSnapshotText(snapshot, 'sessionId') ||
    entry.message.sessionId.trim() ||
    session?.id?.trim() ||
    ''
  if (explicitArtifactId) {
    return { sessionId, artifactId: explicitArtifactId }
  }
  const rawList = Array.isArray(entry.message.metadata?.analysisArtifacts)
    ? entry.message.metadata.analysisArtifacts
    : []
  const foundRawIdx = rawList.findIndex(
    (candidate) =>
      candidate === entry.artifact ||
      (typeof candidate === 'object' &&
        candidate !== null &&
        (candidate as WorkbenchArtifact).kind === entry.artifact.kind &&
        (candidate as WorkbenchArtifact).title === entry.artifact.title &&
        (candidate as WorkbenchArtifact).runId === entry.artifact.runId),
  )
  const rawIndex = foundRawIdx >= 0 ? foundRawIdx : entry.index
  const agentRunId = artifactSnapshotText(entry.message.metadata, 'agentRunId')
  if (entry.message.metadata?.source === 'external-agent-run' && agentRunId) {
    return { sessionId, artifactId: `task:${agentRunId}:${rawIndex}` }
  }
  const msgId = entry.message.id.trim()
  if (msgId && !msgId.startsWith('pending:')) {
    return { sessionId, artifactId: `message:${msgId}:${rawIndex}` }
  }
  return { sessionId, artifactId: entry.artifact.runId.trim() }
}

export function graphNodeLabel(kind: string) {
  switch (kind) {
    case 'scope':
      return '范围'
    case 'service':
      return '服务'
    case 'span':
      return 'Span'
    case 'log_signature':
      return '日志'
    case 'metric_signal':
      return '指标'
    case 'hypothesis':
      return '假设'
    case 'missing_source':
      return '缺失源'
    case 'recommendation':
      return '建议'
    default:
      return kind
  }
}
