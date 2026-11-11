import type { WorkbenchStreamEvent } from '@opensoha/contracts/gen/ts/sohaapi'

import {
  canonicalWorkbenchAgentStatus,
  isRunningWorkbenchAgentStatus,
  type WorkbenchStreamState,
} from './stream'
import type { WorkbenchAgentRun, WorkbenchMessage } from './types'
import { artifactSnapshotText } from './artifacts'
import type { ConversationMessage } from './conversation'
import {
  isRecord,
  metadataAgentStatus,
  metadataArtifacts,
  streamBubbleStatus,
  streamMessageMetadata,
} from './stream-presentation'

const EXTERNAL_AGENT_REPLAY_SOURCE = 'agent-run-replay'

export function isExternalAgentRun(run: WorkbenchAgentRun) {
  const providerKind = run.providerKind.trim().toLowerCase()
  const providerId = run.providerId.trim().toLowerCase()
  return !usesRunEventJournal(run) && providerKind !== 'internal' && providerId !== 'internal'
}

export function usesRunEventJournal(run: WorkbenchAgentRun) {
  // Both current dsh runs and historical Native records expose durable event pages.
  return (
    ['native', 'dsh'].includes(run.providerKind.trim().toLowerCase()) ||
    run.executionOwner === 'native'
  )
}

export function isDurableAgentRun(run: WorkbenchAgentRun) {
  return usesRunEventJournal(run) || isExternalAgentRun(run)
}

export function isAwaitingAgentCancellation(run: WorkbenchAgentRun) {
  return run.status === 'canceled' && run.output?.cancellationPending === true
}

export function isRunningExternalAgentRun(run: WorkbenchAgentRun) {
  return (
    isExternalAgentRun(run) &&
    (isRunningWorkbenchAgentStatus(run.status) || isAwaitingAgentCancellation(run))
  )
}

export function isRunningAgentRun(run: WorkbenchAgentRun) {
  return (
    !isReadOnlyAgentRun(run) &&
    isDurableAgentRun(run) &&
    (isRunningWorkbenchAgentStatus(run.status) || isAwaitingAgentCancellation(run))
  )
}

export function isFinalAgentRunMessage(item: WorkbenchMessage | ConversationMessage) {
  if (item.role === 'user') return false
  const status = metadataAgentStatus(item.metadata)
  return !status || !isRunningWorkbenchAgentStatus(status.status)
}

export function isExternalAgentRunReplayMessage(item: ConversationMessage) {
  return (
    item.id.startsWith('local:agent-run:') || item.metadata?.source === EXTERNAL_AGENT_REPLAY_SOURCE
  )
}

function externalAgentRunReplayMessageId(run: WorkbenchAgentRun) {
  return `local:agent-run:${run.sessionId || 'session'}:${run.id}`
}

export function agentRunTimestamp(run: WorkbenchAgentRun) {
  const candidates = [
    run.updatedAt,
    run.lastHeartbeatAt,
    run.completedAt,
    run.startedAt,
    run.queuedAt,
    run.createdAt,
  ]
  return candidates.reduce((latest, value) => {
    if (!value) return latest
    const timestamp = new Date(value).getTime()
    return Number.isFinite(timestamp) ? Math.max(latest, timestamp) : latest
  }, 0)
}

function agentRunMessageCreatedAt(run: WorkbenchAgentRun) {
  const createdAt =
    run.startedAt || run.queuedAt || run.createdAt || run.updatedAt || new Date().toISOString()
  const queued = new Date(run.queuedAt ?? '').getTime()
  if (Number.isFinite(queued) && new Date(createdAt).getTime() <= queued) {
    return new Date(queued + 1).toISOString()
  }
  return createdAt
}

export function messageAgentRunRefs(item: WorkbenchMessage | ConversationMessage) {
  const ids = new Set<string>()
  const metadata = item.metadata
  const topLevelId = artifactSnapshotText(metadata, 'agentRunId', 'agentRuntimeId')
  if (topLevelId) ids.add(topLevelId)
  if (isRecord(metadata?.agentStatus)) {
    const statusId = artifactSnapshotText(metadata.agentStatus, 'agentRunId', 'agentRuntimeId')
    if (statusId) ids.add(statusId)
  }
  for (const artifact of metadataArtifacts(metadata)) {
    const artifactRunId = artifactSnapshotText(
      artifact.dataSourceSnapshot,
      'agentRunId',
      'agentRuntimeId',
    )
    if (artifactRunId) ids.add(artifactRunId)
  }
  return [...ids]
}

function isWorkbenchStreamEvent(value: unknown): value is WorkbenchStreamEvent {
  if (!isRecord(value)) return false
  return (
    typeof value.id === 'string' &&
    typeof value.type === 'string' &&
    typeof value.sessionId === 'string' &&
    typeof value.sequence === 'number' &&
    typeof value.createdAt === 'string'
  )
}

function agentRunWorkbenchEvents(run: WorkbenchAgentRun) {
  const raw = run.output?.workbenchEvents
  return Array.isArray(raw) ? raw.filter(isWorkbenchStreamEvent) : []
}

function agentRunStatusReplayEvent(
  run: WorkbenchAgentRun,
  sessionId: string,
): WorkbenchStreamEvent {
  const status = isAwaitingAgentCancellation(run)
    ? 'running'
    : canonicalWorkbenchAgentStatus(run.status)
  return {
    id: `evt:${sessionId}:${run.id}:status:${status}`,
    type: 'agent.status',
    sessionId,
    runId: run.rootCauseRunId || run.id,
    sequence: 0,
    createdAt:
      run.updatedAt ||
      run.lastHeartbeatAt ||
      run.startedAt ||
      run.queuedAt ||
      run.createdAt ||
      new Date().toISOString(),
    providerId: run.providerId,
    providerKind: run.providerKind,
    status,
  } as WorkbenchStreamEvent
}

export function sortedAgentRunReplayEvents(run: WorkbenchAgentRun, sessionId: string) {
  return [
    agentRunStatusReplayEvent(run, sessionId),
    ...(usesRunEventJournal(run) ? [] : agentRunWorkbenchEvents(run)),
  ].sort(
    (left, right) =>
      left.sequence - right.sequence ||
      left.createdAt.localeCompare(right.createdAt) ||
      left.id.localeCompare(right.id),
  )
}

function agentRunStatusSnapshot(run: WorkbenchAgentRun, state: WorkbenchStreamState) {
  return {
    providerId: state.agentStatus?.providerId || run.providerId,
    providerKind: state.agentStatus?.providerKind || run.providerKind,
    status: isAwaitingAgentCancellation(run)
      ? 'running'
      : canonicalWorkbenchAgentStatus(state.agentStatus?.status || run.status),
    runId: run.rootCauseRunId || run.id,
    agentRunId: run.id,
    externalRunId: run.externalRunId,
  }
}

function agentRunReplayArtifacts(metadata: Record<string, unknown>, run: WorkbenchAgentRun) {
  const artifacts = metadataArtifacts(metadata)
  if (artifacts.length === 0 && (run.analysisArtifacts ?? []).length > 0)
    return run.analysisArtifacts
  const fallbackRunId = run.rootCauseRunId || run.id
  return artifacts.map((artifact) =>
    artifact.runId === 'stream' ? { ...artifact, runId: fallbackRunId } : artifact,
  )
}

function agentRunReplayMetadata(run: WorkbenchAgentRun, state: WorkbenchStreamState) {
  const metadata = streamMessageMetadata(
    {
      source: EXTERNAL_AGENT_REPLAY_SOURCE,
      agentRunId: run.id,
      externalRunId: run.externalRunId,
      agentProviderId: run.providerId,
    },
    state,
  )
  return {
    ...metadata,
    ...(run.input?.contextSnapshot ? { contextSnapshot: run.input.contextSnapshot } : {}),
    ...(run.output?.sessionPatch ? { sessionPatch: run.output.sessionPatch } : {}),
    source: EXTERNAL_AGENT_REPLAY_SOURCE,
    agentRunId: run.id,
    externalRunId: run.externalRunId,
    agentProviderId: run.providerId,
    ...(isReadOnlyAgentRun(run) ? { executionReadOnly: true } : {}),
    agentPhase: isAwaitingAgentCancellation(run) ? 'cancelling' : run.phase,
    agentStopReason:
      (typeof run.output?.stopReason === 'string' ? run.output.stopReason : undefined) ||
      run.stopReason,
    agentStatus: isAwaitingAgentCancellation(run)
      ? agentRunStatusSnapshot(run, state)
      : (metadataAgentStatus(metadata) ?? agentRunStatusSnapshot(run, state)),
    analysisArtifacts: agentRunReplayArtifacts(metadata, run),
  }
}

function agentRunReplayContent(run: WorkbenchAgentRun, state: WorkbenchStreamState) {
  if (state.message.content) return state.message.content
  if (state.error) return state.error.message
  return isRunningAgentRun(run) ? '' : state.thinking?.summary || ''
}

export function agentRunReplayMessage(
  run: WorkbenchAgentRun,
  state: WorkbenchStreamState,
): ConversationMessage {
  const sessionId = run.sessionId || state.message.sessionId || ''
  return {
    id: externalAgentRunReplayMessageId(run),
    sessionId,
    role: 'assistant',
    content: agentRunReplayContent(run, state),
    metadata: agentRunReplayMetadata(run, state),
    createdAt: agentRunMessageCreatedAt(run),
    deliveryStatus: isReadOnlyAgentRun(run) ? 'success' : streamBubbleStatus(state),
  }
}

export function isLegacyAgentRun(run: WorkbenchAgentRun) {
  return (
    run.providerKind.trim().toLowerCase() === 'native' ||
    run.executionOwner === 'native' ||
    run.output?.executionReadOnly === true ||
    run.output?.stopReason === 'legacy_reconciliation_required'
  )
}

export function isReadOnlyAgentRun(run: WorkbenchAgentRun) {
  // Core retains every Native identity as historical read-only data, including
  // records created before explicit reconciliation output markers were added.
  return isLegacyAgentRun(run)
}

export function isReadOnlyAgentRunMessage(
  message: WorkbenchMessage | ConversationMessage,
  readOnlyRunIds?: ReadonlySet<string>,
) {
  return (
    message.metadata?.executionReadOnly === true ||
    message.metadata?.agentStopReason === 'legacy_reconciliation_required' ||
    message.metadata?.agentProviderId === 'native' ||
    metadataAgentStatus(message.metadata)?.providerKind.trim().toLowerCase() === 'native' ||
    (readOnlyRunIds?.size ? messageAgentRunRefs(message).some((id) => readOnlyRunIds.has(id)) : false)
  )
}
