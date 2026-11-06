import { deliveryStageLabels, targetStageNode } from '../batches/model'
import {
  isDeliveryActiveStatus,
  isDeliveryFailureStatus,
  normalizeDeliveryStatus,
} from '../delivery-status'
import { workflowExecutionSummary } from '../runtime-detail/workflow-execution-model'
import type { DeliveryExecutionHistoryEntry } from '../types'

export type WorkflowCenterStage = {
  key: string
  name: string
  status: string
  summary?: string
  startedAt?: string
  finishedAt?: string
  executionTaskId?: string
  releaseBundleId?: string
  href: string
  evidence: { kind: 'plan' | 'task' | 'docker' | 'build' | 'bundle'; href: string }[]
}

export type WorkflowCenterTarget = {
  id: string
  applicationId: string
  application: string
  service?: string
  environment?: string
  location?: string
  version?: string
  stages: WorkflowCenterStage[]
}

export type WorkflowCenterRun = {
  key: string
  id: string
  createdAt: string
  status: string
  name: string
  href: string
  targets: WorkflowCenterTarget[]
  partialView?: boolean
  summary?: string
}

const runningStatuses = ['pending', 'queued', 'running', 'waiting_execution', 'canceling']
const approvalStatuses = ['waiting_approval', 'pending_approval', 'awaiting_approval']
const englishStageNames: Record<keyof typeof deliveryStageLabels, string> = {
  build: 'Build',
  plan: 'Preflight and approval',
  deploy: 'Deploy',
  health: 'Readiness',
}

export function centerRunPhase(status: string) {
  const value = normalizeDeliveryStatus(status)
  if (approvalStatuses.includes(value)) return 'approval'
  if (runningStatuses.includes(value)) return 'running'
  return 'recent'
}

function metadataText(metadata: Record<string, unknown> | undefined, key: string) {
  const value = metadata?.[key]
  return typeof value === 'string' ? value.trim() : ''
}

function queryPath(path: string, params: Record<string, string>) {
  return `${path}?${new URLSearchParams(params)}`
}

function workflowCenterRun(
  entry: DeliveryExecutionHistoryEntry,
  english: boolean,
): WorkflowCenterRun | undefined {
  const key = `${entry.kind}/${entry.id}`
  const id = encodeURIComponent(entry.id)
  const base = { key, id: entry.id, createdAt: entry.createdAt }
  if (entry.kind === 'batch' && entry.batch) {
    const batch = entry.batch
    const href = `/delivery/batches/${id}`
    const targets = batch.targets.map((snapshot): WorkflowCenterTarget => {
      const targetId = snapshot.target.id
      const targetHref = queryPath(href, { targetId })
      const stages = Object.entries(deliveryStageLabels).flatMap(([stage, label]) => {
        const node = targetStageNode(batch, targetId, stage)
        if (!node) return []
        const name =
          snapshot.templateUpdate && stage === 'deploy'
            ? english
              ? 'Adopt configuration'
              : '采用配置'
            : snapshot.templateUpdate && stage === 'plan'
              ? english
                ? 'Version check and approval'
                : '版本校验与审批'
              : english
                ? englishStageNames[stage as keyof typeof englishStageNames]
                : label
        const evidence: WorkflowCenterStage['evidence'] = []
        if (node.deliveryPlanId)
          evidence.push({
            kind: 'plan',
            href: queryPath(href, { targetId, planId: node.deliveryPlanId }),
          })
        if (node.executionTaskId)
          evidence.push({
            kind: 'task',
            href: queryPath(href, {
              targetId,
              evidenceKind: 'execution_task',
              evidenceId: node.executionTaskId,
            }),
          })
        if (node.dockerOperationId)
          evidence.push({
            kind: 'docker',
            href: queryPath('/compute/tasks/operations', {
              domain: 'docker',
              view: 'logs',
              taskId: node.dockerOperationId,
            }),
          })
        if (node.buildRecordId)
          evidence.push({
            kind: 'build',
            href: queryPath(href, {
              targetId,
              evidenceKind: 'build',
              evidenceId: node.buildRecordId,
            }),
          })
        if (node.releaseBundleId)
          evidence.push({
            kind: 'bundle',
            href: queryPath(href, {
              targetId,
              evidenceKind: 'release_bundle',
              evidenceId: node.releaseBundleId,
            }),
          })
        return [
          {
            key: node.nodeId,
            name,
            status: node.status,
            summary: node.summary,
            startedAt: node.startedAt,
            finishedAt: node.finishedAt,
            executionTaskId: node.executionTaskId,
            releaseBundleId: node.releaseBundleId,
            href: targetHref,
            evidence,
          },
        ]
      })
      return {
        id: targetId,
        applicationId: snapshot.target.applicationId,
        application: snapshot.applicationName || snapshot.target.applicationId,
        service: snapshot.serviceName || snapshot.target.serviceId,
        environment: snapshot.environmentName || snapshot.target.applicationEnvironmentId,
        version: snapshot.serviceVersion ? `v${snapshot.serviceVersion}` : undefined,
        stages,
      }
    })
    return {
      ...base,
      href,
      targets,
      status: batch.status,
      name: batch.definition.name,
      partialView: batch.partialView,
      summary: batch.stopSummary,
    }
  }
  if (entry.kind === 'application' && entry.application) {
    const run = entry.application
    const href = `/workflows/${id}`
    const summary = workflowExecutionSummary(run)
    return {
      ...base,
      href,
      status: run.status,
      name: run.workflowName,
      targets: [
        {
          id: run.id,
          applicationId: run.applicationId,
          application: metadataText(run.metadata, 'applicationName') || run.applicationId,
          service:
            metadataText(run.metadata, 'serviceName') || metadataText(run.metadata, 'serviceId'),
          environment:
            metadataText(run.metadata, 'environmentName') ||
            metadataText(run.metadata, 'bindingId'),
          location: [run.clusterId, run.namespace, run.deploymentName].filter(Boolean).join(' / '),
          stages: summary.stages.map(
            (stage): WorkflowCenterStage => ({
              ...stage,
              href: queryPath(href, { stage: stage.key }),
              evidence: [],
            }),
          ),
        },
      ] satisfies WorkflowCenterTarget[],
    }
  }
  if (entry.kind === 'build' && entry.build) {
    const build = entry.build
    const href = `/builds/${id}`
    const name =
      metadataText(build.metadata, 'buildSourceName') || (english ? 'Build execution' : '构建执行')
    return {
      ...base,
      href,
      status: build.status,
      name,
      targets: [
        {
          id: build.id,
          applicationId: build.applicationId,
          application: metadataText(build.metadata, 'applicationName') || build.applicationId,
          service:
            metadataText(build.metadata, 'serviceName') ||
            metadataText(build.metadata, 'serviceId'),
          environment:
            metadataText(build.metadata, 'environmentName') ||
            metadataText(build.metadata, 'applicationEnvironmentId'),
          stages: [
            {
              key: build.id,
              name: english ? 'Build' : '构建',
              status: build.status,
              href,
              evidence: [],
            },
          ],
        },
      ] satisfies WorkflowCenterTarget[],
    }
  }
}

export function currentCenterStage(stages: WorkflowCenterStage[]) {
  return (
    stages.find(({ status }) => ['running', 'waiting_execution', 'canceling'].includes(status)) ??
    stages.find(({ status }) => centerRunPhase(status) === 'approval') ??
    stages.find(({ status }) => isDeliveryFailureStatus(status)) ??
    stages.find(({ status }) => isDeliveryActiveStatus(status)) ??
    stages.at(-1)
  )
}

export function mergeCenterRuns(entries: DeliveryExecutionHistoryEntry[], english: boolean) {
  const rows = new Map<string, WorkflowCenterRun>()
  for (const entry of entries) {
    const run = workflowCenterRun(entry, english)
    if (run && !rows.has(run.key)) rows.set(run.key, run)
  }
  return [...rows.values()].sort(
    (left, right) =>
      Date.parse(right.createdAt) - Date.parse(left.createdAt) || left.key.localeCompare(right.key),
  )
}
