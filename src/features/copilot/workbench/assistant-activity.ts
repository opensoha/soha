import { isLegacyAgentRun } from './agent-run-replay'
import { artifactSnapshotText, type WorkbenchArtifactEntry } from './artifacts'
import { modeLabel } from './mode'
import type { ConversationMessage } from './conversation'
import {
  metadataToolExecutions,
  toolExecutionLabel,
  toolExecutionSummary,
} from './stream-presentation'
import type {
  PersonalArtifact,
  WorkbenchAgentRun,
  WorkbenchRunRef,
  WorkbenchSession,
  WorkbenchToolCall,
} from './types'

export type ActivityArtifact = PersonalArtifact & { current?: WorkbenchArtifactEntry }

export interface AssistantActivity {
  id: string
  kind: 'task' | 'tool' | 'artifact'
  title: string
  timestamp: string
  status: string
  sessionId?: string
  run?: WorkbenchAgentRun
  tools: WorkbenchToolCall[]
  children: WorkbenchAgentRun[]
  artifacts: ActivityArtifact[]
  analysisRuns?: WorkbenchRunRef[]
}

export function conciseActivityText(value: string, limit = 36) {
  const chars = Array.from(value.replace(/\s+/g, ' ').trim())
  return chars.length > limit ? chars.slice(0, limit).join('') + '…' : chars.join('')
}

function timeValue(value?: string) {
  const time = Date.parse(value || '')
  return Number.isFinite(time) ? time : 0
}

function latestTime(values: (string | undefined)[]) {
  return (
    values
      .filter((value): value is string => timeValue(value) > 0)
      .sort((a, b) => timeValue(b) - timeValue(a))[0] || ''
  )
}

export function activitySummary(item: AssistantActivity) {
  const run = item.run
  if (run && isLegacyAgentRun(run)) return '旧版任务记录，仅供查看'
  if (run?.stopReason === 'unknown_outcome') return '执行结果未知，请核对原任务回执'
  if (run?.phase === 'paused' && run.stopReason === 'waiting_input') return '等待你补充信息后继续'
  if (run?.phase === 'waiting_approval' || item.status === 'waiting_approval') return '等待变更审批'
  if (run?.phase === 'waiting_task') return '等待领域执行与验收'
  if (run?.phase === 'cancelling') return '取消处理中，等待领域终态确认'
  if (['failed', 'error'].includes(item.status))
    return /timed?\s*out|timeout/i.test(run?.errorMessage || '')
      ? '执行超时，请核对执行端状态'
      : `执行失败${run?.errorMessage ? `：${conciseActivityText(run.errorMessage, 40)}` : ''}`
  if (['cancelled', 'canceled'].includes(item.status)) return '本次任务已取消'
  const summary =
    (typeof run?.output?.summary === 'string' ? run.output.summary : '') ||
    item.artifacts[0]?.artifact.summary ||
    (item.tools[0] ? toolExecutionSummary(item.tools[0]) : '')
  if (['queued', 'pending'].includes(item.status)) return '等待执行'
  if (item.status === 'running')
    return summary ? `进行中 · ${conciseActivityText(summary, 40)}` : '正在执行'
  return conciseActivityText(
    summary ||
      (['completed', 'success', 'succeeded'].includes(item.status) ? '已完成执行' : '查看执行记录'),
    46,
  )
}

export function activityDateLabel(timestamp: string, now = new Date()) {
  if (!timeValue(timestamp)) return '时间待同步'
  const date = new Date(timestamp)
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  const yesterday = new Date(today)
  yesterday.setDate(yesterday.getDate() - 1)
  const day = new Date(date.getFullYear(), date.getMonth(), date.getDate()).getTime()
  if (day === today.getTime()) return '今天'
  if (day === yesterday.getTime()) return '昨天'
  return new Intl.DateTimeFormat('zh-CN', {
    ...(date.getFullYear() !== now.getFullYear() ? { year: 'numeric' as const } : {}),
    month: 'long',
    day: 'numeric',
  }).format(date)
}

export function buildAssistantActivity({
  personalRuns,
  runs,
  messages,
  artifacts,
  personalArtifacts,
  session,
}: {
  personalRuns: WorkbenchAgentRun[]
  runs: WorkbenchAgentRun[]
  messages: ConversationMessage[]
  artifacts: WorkbenchArtifactEntry[]
  personalArtifacts: PersonalArtifact[]
  session?: WorkbenchSession
}): AssistantActivity[] {
  const runMap = new Map<string, WorkbenchAgentRun>()
  for (const run of [...personalRuns, ...runs]) {
    const previous = runMap.get(run.id)
    if (!previous || timeValue(run.updatedAt) >= timeValue(previous.updatedAt))
      runMap.set(run.id, run)
  }
  const rootId = (id: string) => runMap.get(id)?.parentRunId || id
  const entries = new Map<string, AssistantActivity>()
  const entry = (id: string, kind: AssistantActivity['kind'], sessionId?: string) => {
    if (!entries.has(id))
      entries.set(id, {
        id,
        kind,
        sessionId,
        title: '',
        timestamp: '',
        status: '',
        tools: [],
        children: [],
        artifacts: [],
      })
    return entries.get(id)!
  }
  const addTools = (item: AssistantActivity, tools: WorkbenchToolCall[]) => {
    for (const tool of tools) {
      const index = item.tools.findIndex((existing) => existing.id === tool.id)
      if (index < 0) item.tools.push(tool)
      else if (timeValue(tool.completedAt) >= timeValue(item.tools[index].completedAt))
        item.tools[index] = tool
      item.timestamp = latestTime([item.timestamp, tool.completedAt, tool.startedAt])
    }
  }
  for (const run of runMap.values()) {
    const item = entry(`task:${rootId(run.id)}`, 'task', run.sessionId)
    if (run.parentRunId) {
      item.children.push(run)
      item.timestamp = latestTime([item.timestamp, run.updatedAt, run.completedAt])
    } else {
      item.run = run
      item.status = run.status
      item.timestamp = latestTime([
        item.timestamp,
        run.updatedAt,
        run.completedAt,
        run.startedAt,
        run.queuedAt,
        run.createdAt,
      ])
      const question = typeof run.input?.question === 'string' ? run.input.question : ''
      // Only remove the delegation boilerplate; the goal remains an excerpt of the real request.
      item.title =
        conciseActivityText(
          question.replace(
            /^(?:请)?(?:调用\s+agent\.delegate\s+)?(?:重新)?委派[^，,]+[，,]\s*/u,
            '',
          ),
          20,
        ) || '对话任务'
    }
  }
  for (const run of [...personalRuns, ...runs])
    addTools(entry(`task:${rootId(run.id)}`, 'task', run.sessionId), run.toolExecutions ?? [])

  for (const message of messages) {
    const runId =
      typeof message.metadata?.agentRunId === 'string' ? message.metadata.agentRunId : ''
    for (const tool of metadataToolExecutions(message.metadata)) {
      const item = entry(
        runId ? `task:${rootId(runId)}` : `tool:${tool.id}`,
        runId ? 'task' : 'tool',
        message.sessionId,
      )
      addTools(item, [tool])
      item.timestamp = latestTime([item.timestamp, tool.completedAt, tool.startedAt])
    }
  }

  const artifactMap = new Map<string, ActivityArtifact>()
  for (const run of runMap.values())
    (run.analysisArtifacts ?? []).forEach((artifact, index) => {
      if (run.parentRunId) return
      const id = `task:${run.id}:${index}`
      artifactMap.set(id, {
        id,
        taskId: run.id,
        sessionId: run.sessionId,
        artifact,
        createdAt: run.completedAt || run.updatedAt || run.createdAt || '',
      })
    })
  for (const item of personalArtifacts) artifactMap.set(item.id, item)
  for (const current of artifacts) {
    const taskId =
      typeof current.message.metadata?.agentRunId === 'string'
        ? current.message.metadata.agentRunId
        : undefined
    const id = taskId
      ? `task:${taskId}:${current.index}`
      : `message:${current.message.id}:${current.index}`
    artifactMap.set(id, {
      id,
      taskId,
      messageId: current.message.id,
      sessionId: current.message.sessionId,
      createdAt: current.message.createdAt,
      ...artifactMap.get(id),
      artifact: current.artifact,
      current,
    })
  }
  for (const artifact of artifactMap.values()) {
    const item = entry(
      artifact.taskId ? `task:${rootId(artifact.taskId)}` : `artifact:${artifact.id}`,
      artifact.taskId ? 'task' : 'artifact',
      artifact.sessionId,
    )
    item.artifacts.push(artifact)
    item.timestamp = latestTime([item.timestamp, artifact.createdAt])
    if (!item.title) item.title = conciseActivityText(artifact.artifact.title || '成果可查看')
  }
  const analysisOwners = new Map<string, AssistantActivity>()
  for (const item of entries.values()) {
    if (!session || item.sessionId !== session.id) continue
    for (const run of [item.run, ...item.children])
      if (run?.rootCauseRunId) analysisOwners.set(`root_cause:${run.rootCauseRunId}`, item)
    for (const { artifact } of item.artifacts) {
      analysisOwners.set(`${artifact.kind}:${artifact.runId}`, item)
      for (const [kind, key] of [
        ['root_cause', 'rootCauseRunId'],
        ['inspection_review', 'inspectionRunId'],
      ]) {
        const id = artifactSnapshotText(artifact.dataSourceSnapshot, key)
        if (id) analysisOwners.set(`${kind}:${id}`, item)
      }
    }
  }
  if (session) {
    for (const ref of session.metadata?.analysisRunRefs ?? []) {
      const owner = analysisOwners.get(`${ref.kind}:${ref.id}`)
      const item =
        owner || entry(`analysis:${session.id}:${ref.kind}:${ref.id}`, 'task', session.id)
      if (
        item.analysisRuns?.some((existing) => existing.kind === ref.kind && existing.id === ref.id)
      )
        continue
      item.analysisRuns = [...(item.analysisRuns ?? []), ref]
      if (!owner) {
        item.title = modeLabel(ref.kind)
        item.status = ref.status || ''
        item.timestamp = ref.createdAt || ''
      }
    }
  }
  for (const item of entries.values()) {
    item.tools.sort((a, b) => timeValue(b.startedAt) - timeValue(a.startedAt))
    const latestTool = item.tools[0]
    const toolTitle = latestTool
      ? toolExecutionSummary(latestTool) || toolExecutionLabel(latestTool.toolName)
      : '助手执行记录'
    item.title ||= conciseActivityText(toolTitle)
    if (item.kind === 'tool' && item.tools[0]) {
      item.title = conciseActivityText(toolTitle)
      item.status = item.tools[0].status
    }
  }
  return [...entries.values()].sort(
    (a, b) => timeValue(b.timestamp) - timeValue(a.timestamp) || a.id.localeCompare(b.id),
  )
}
