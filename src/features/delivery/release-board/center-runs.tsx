import { useState } from 'react'
import {
  ArrowRightOutlined,
  CheckOutlined,
  ClockCircleOutlined,
  CloseOutlined,
} from '@ant-design/icons'
import { Link } from 'react-router-dom'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useI18n } from '@/i18n'
import { formatDateTime } from '@/utils/time'
import { isDeliveryFailureStatus, isDeliveryReadyStatus } from '../delivery-status'
import {
  centerRunPhase,
  currentCenterStage,
  type WorkflowCenterRun,
  type WorkflowCenterStage,
  type WorkflowCenterTarget,
} from './center-runs-model'

const evidenceLabels = {
  plan: ['计划与审批', 'Plan and approval'],
  task: ['任务日志与结果', 'Task logs and result'],
  docker: ['Docker 日志与结果', 'Docker logs and result'],
  build: ['构建明细', 'Build details'],
  bundle: ['发布产物', 'Release artifact'],
}

const evidencePermissions = {
  plan: 'delivery.applications.view',
  task: 'delivery.execution-tasks.view',
  docker: 'docker.operations.view',
  build: 'delivery.applications.view',
  bundle: 'delivery.release-bundles.view',
}

function RunStatus({ status, english }: { status: string; english: boolean }) {
  if (centerRunPhase(status) === 'approval')
    return <StatusTag value="approval" label={english ? 'Pending approval' : '待审批'} />
  if (status === 'waiting_execution')
    return <StatusTag value="checking" label={english ? 'Waiting for runner' : '等待执行器'} />
  if (status === 'canceling')
    return <StatusTag value="checking" label={english ? 'Canceling' : '正在取消'} />
  if (status === 'partially_completed')
    return <StatusTag value="warning" label={english ? 'Partially completed' : '部分完成'} />
  return <StatusTag value={status} />
}

function StageMarker({ stage, position }: { stage: WorkflowCenterStage; position: number }) {
  if (isDeliveryReadyStatus(stage.status))
    return (
      <span className="soha-workflow-stage__marker is-complete">
        <CheckOutlined />
      </span>
    )
  if (isDeliveryFailureStatus(stage.status))
    return (
      <span className="soha-workflow-stage__marker is-failed">
        <CloseOutlined />
      </span>
    )
  if (stage.status === 'running' || centerRunPhase(stage.status) === 'approval')
    return (
      <span className="soha-workflow-stage__marker is-current">
        <ClockCircleOutlined />
      </span>
    )
  return <span className="soha-workflow-stage__marker">{position}</span>
}

function TargetStages({ target }: { target: WorkflowCenterTarget }) {
  const english = useI18n().localeCode === 'en_US'
  const permissions = usePermissionSnapshot().data?.data
  const [stageKey, setStageKey] = useState('')
  const selected =
    target.stages.find((stage) => stage.key === stageKey) ?? currentCenterStage(target.stages)
  return (
    <>
      <div className="soha-workflow-center__context">
        <strong>{target.service || target.application}</strong>
        {target.service ? <span>{target.application}</span> : null}
        {target.environment ? <span>{target.environment}</span> : null}
        {target.location ? <span>{target.location}</span> : null}
        {target.version ? <span>{target.version}</span> : null}
      </div>
      {selected ? (
        <>
          <div
            className="soha-workflow-center__stages"
            role="group"
            aria-label={english ? 'Execution stages' : '服务执行阶段'}
          >
            {target.stages.map((stage, index) => (
              <button
                key={stage.key}
                type="button"
                className="soha-workflow-stage"
                aria-pressed={stage.key === selected.key}
                onClick={() => setStageKey(stage.key)}
              >
                <span aria-hidden="true">
                  <StageMarker stage={stage} position={index + 1} />
                </span>
                <strong>{stage.name}</strong>
                <RunStatus status={stage.status} english={english} />
              </button>
            ))}
          </div>
          <div className="soha-workflow-center__evidence">
            <div>
              <strong>{selected.name}</strong>
              <p>
                {selected.summary ||
                  (english ? 'No summary was recorded for this stage.' : '此阶段暂无结果摘要。')}
              </p>
              {selected.startedAt ? (
                <small>
                  {english ? 'Started' : '开始'} {formatDateTime(selected.startedAt)}
                </small>
              ) : null}
              {selected.finishedAt ? (
                <small>
                  {english ? 'Finished' : '结束'} {formatDateTime(selected.finishedAt)}
                </small>
              ) : null}
            </div>
            <div className="soha-workflow-center__links">
              {selected.evidence
                .filter(({ kind }) => hasPermission(permissions, evidencePermissions[kind]))
                .map(({ href, kind }) => (
                  <Link key={kind} to={href}>
                    {evidenceLabels[kind][english ? 1 : 0]}
                  </Link>
                ))}
              <Link to={selected.href}>
                {english ? 'Open stage details' : '查看阶段详情'} <ArrowRightOutlined />
              </Link>
              {selected.releaseBundleId &&
              hasPermission(permissions, 'delivery.release-bundles.view') ? (
                <Link
                  to={`/delivery/testing?${new URLSearchParams({ releaseBundleId: selected.releaseBundleId })}`}
                >
                  {english ? 'Open verification' : '查看测试验证'}
                </Link>
              ) : null}
              {isDeliveryFailureStatus(selected.status) &&
              selected.executionTaskId &&
              hasPermission(permissions, 'delivery.execution-tasks.view') ? (
                <Link
                  to={`/delivery/analysis?${new URLSearchParams({ executionTaskId: selected.executionTaskId })}`}
                >
                  {english ? 'Investigate failed task' : '排查失败任务'}
                </Link>
              ) : null}
            </div>
          </div>
        </>
      ) : (
        <p className="soha-workflow-center__hint">
          {english ? 'No execution stages were recorded.' : '该目标暂无执行阶段记录。'}
        </p>
      )}
    </>
  )
}

function RunDetail({ run }: { run: WorkflowCenterRun }) {
  const english = useI18n().localeCode === 'en_US'
  const [targetId, setTargetId] = useState('')
  const groups = new Map<string, WorkflowCenterTarget[]>()
  for (const target of run.targets) {
    const group = groups.get(target.applicationId)
    if (group) group.push(target)
    else groups.set(target.applicationId, [target])
  }
  const target =
    run.targets.find((item) => item.id === targetId) ??
    run.targets.find(
      (item) => centerRunPhase(currentCenterStage(item.stages)?.status ?? '') !== 'recent',
    ) ??
    run.targets[0]
  return (
    <section
      className="soha-workflow-center__panel"
      aria-label={english ? 'Selected execution' : '所选执行'}
    >
      <header className="soha-workflow-center__panel-head">
        <div>
          <h3>{run.name}</h3>
          <p>
            {run.id} · {formatDateTime(run.createdAt)}
          </p>
        </div>
        <RunStatus status={run.status} english={english} />
      </header>
      <div className="soha-workflow-center__panel-body">
        {run.partialView ? (
          <MetadataTag label={english ? 'Accessible targets only' : '仅显示可见目标'} />
        ) : null}
        {run.summary ? <p className="soha-workflow-center__hint">{run.summary}</p> : null}
        {run.targets.length > 1 ? (
          <div
            className="soha-workflow-center__targets"
            role="group"
            aria-label={english ? 'Service targets' : '服务执行目标'}
          >
            {[...groups].map(([applicationId, items]) => (
              <div key={applicationId} className="soha-workflow-center__target-group">
                <strong>{items[0].application}</strong>
                {items.map((item) => {
                  const stage = currentCenterStage(item.stages)
                  return (
                    <button
                      key={item.id}
                      type="button"
                      aria-pressed={item.id === target?.id}
                      onClick={() => setTargetId(item.id)}
                    >
                      <span>
                        {item.service || item.application}
                        {item.environment ? ` · ${item.environment}` : ''}
                      </span>
                      {stage ? <RunStatus status={stage.status} english={english} /> : null}
                    </button>
                  )
                })}
              </div>
            ))}
          </div>
        ) : null}
        {target ? <TargetStages key={target.id} target={target} /> : null}
        <div className="soha-workflow-center__links">
          <Link to={run.href}>
            {english ? 'Open execution details' : '查看执行详情'} <ArrowRightOutlined />
          </Link>
        </div>
      </div>
    </section>
  )
}

export function WorkflowCenterRuns({
  rows,
  selectedKey,
  onSelect,
  active,
}: {
  rows: WorkflowCenterRun[]
  selectedKey: string | null
  onSelect: (key: string) => void
  active: boolean
}) {
  const english = useI18n().localeCode === 'en_US'
  const selected = rows.find((row) => row.key === selectedKey) ?? rows[0]
  const title = active
    ? english
      ? 'Active executions'
      : '进行中的执行'
    : english
      ? 'Recent executions'
      : '最近执行'
  if (!selected) return null
  return (
    <div className="soha-workflow-center__workbench">
      <section className="soha-workflow-center__panel" aria-label={title}>
        <header className="soha-workflow-center__panel-head">
          <h3>{title}</h3>
        </header>
        <div role="group" aria-label={english ? 'Choose execution' : '选择执行记录'}>
          {rows.map((run) => {
            const applications = [...new Set(run.targets.map((target) => target.application))]
            const first = run.targets[0]
            const context =
              run.targets.length > 1
                ? [
                    applications.slice(0, 2).join(' / '),
                    english ? `${run.targets.length} targets` : `${run.targets.length} 个目标`,
                  ]
                : [first?.application, first?.service, first?.environment]
            return (
              <button
                key={run.key}
                type="button"
                className="soha-workflow-center__run"
                aria-pressed={run.key === selected.key}
                onClick={() => onSelect(run.key)}
              >
                <span className="soha-workflow-center__run-identity">
                  <strong>{run.name}</strong>
                  <small>{context.filter(Boolean).join(' · ')}</small>
                  <small>{run.id}</small>
                </span>
                <span className="soha-workflow-center__run-facts">
                  <RunStatus status={run.status} english={english} />
                  <small>{formatDateTime(run.createdAt)}</small>
                </span>
              </button>
            )
          })}
        </div>
      </section>
      <RunDetail key={selected.key} run={selected} />
    </div>
  )
}
