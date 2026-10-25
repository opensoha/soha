import { Button, Typography, theme } from 'antd'
import {
  CheckCircleOutlined,
  ClockCircleOutlined,
  CloseCircleOutlined,
  LoadingOutlined,
} from '@ant-design/icons'
import { analyzeReleaseDagDefinition } from '@/components/release-flow-dag-definition'
import { useDocumentHidden } from '@/utils/use-document-hidden'
import type { ApplicationRuntimeWorkload, DeliveryApplicationBindingSummary } from '../types'
import {
  workflowExecutionStages,
  workflowExecutionState,
} from '../runtime-detail/workflow-execution-model'
import '../runtime-detail/workflow-execution-detail.css'

export function WorkflowProgress({
  binding,
  onSelectStage,
}: {
  binding: Pick<DeliveryApplicationBindingSummary, 'latestWorkflow' | 'workflowTemplate'>
  onSelectStage?: (runId: string, stageKey: string) => void
}) {
  const { token } = theme.useToken()
  const hidden = useDocumentHidden()
  const run = binding.latestWorkflow
  const stages = run ? workflowExecutionStages(run) : []
  const nodes = stages.length
    ? stages
    : binding.workflowTemplate
      ? analyzeReleaseDagDefinition(binding.workflowTemplate.definition).definition.nodes.map(
          (node) => ({
            key: node.id,
            name: node.name,
            summary: '',
            state: { ...workflowExecutionState(), label: run ? '无步骤记录' : '未运行' },
          }),
        )
      : []
  if (!nodes.length) return <Typography.Text type="secondary">暂无执行步骤</Typography.Text>
  return (
    <div
      className="soha-workflow-execution soha-application-workflow-stages"
      data-motion-paused={hidden}
      data-motion-disabled={!token.motion}
    >
      <Typography.Text type="secondary">
        {run ? '按执行记录顺序展示 · 节点可并行' : '模板步骤 · 尚无执行记录'}
      </Typography.Text>
      <ol className="soha-execution-nodes" aria-label="工作流步骤">
        {nodes.map((node, index) => (
          <li key={node.key}>
            <Button
              type="text"
              className={`soha-execution-node is-${node.state.tone}`}
              aria-label={`${node.name || node.key}，${node.state.label}`}
              disabled={!run || !stages.length || !onSelectStage}
              onClick={() => run && onSelectStage?.(run.id, node.key)}
            >
              <span className="soha-execution-node-marker" aria-hidden="true">
                {node.state.tone === 'success' ? (
                  <CheckCircleOutlined />
                ) : node.state.tone === 'error' ? (
                  <CloseCircleOutlined />
                ) : (
                  index + 1
                )}
              </span>
              <span className="soha-execution-node-copy">
                <strong>{node.name || node.key}</strong>
                <small>{node.state.label}</small>
                {node.summary ? <small>{node.summary}</small> : null}
              </span>
              {node.state.tone === 'running' && token.motion ? (
                <span className="soha-execution-live-dot" aria-hidden="true" />
              ) : null}
            </Button>
            {index < nodes.length - 1 ? (
              <span className="soha-workflow-stage-connector" aria-hidden="true">
                →
              </span>
            ) : null}
          </li>
        ))}
      </ol>
    </div>
  )
}

export function WorkloadProgress({ workload }: { workload: ApplicationRuntimeWorkload }) {
  if (!['deployment', 'statefulset', 'daemonset'].includes(workload.workloadKind.toLowerCase()))
    return null
  if (
    workload.healthStatus === 'unknown' ||
    (workload.sourceStatus && !['linked', 'changed'].includes(workload.sourceStatus))
  )
    return <Typography.Text type="secondary">副本观测不完整，更新状态未知</Typography.Text>
  const {
    desiredReplicas: desired,
    updatedReplicas: updated,
    readyReplicas: ready,
    availableReplicas: available,
  } = workload
  if (![desired, updated, ready, available].every((value) => Number.isFinite(value) && value >= 0))
    return null
  const failed = ['failed', 'error', 'critical', 'unavailable'].includes(
    workload.healthStatus || '',
  )
  const updating = desired > 0 && updated < desired
  return (
    <section
      className="soha-delivery-progress soha-rollout-progress"
      aria-label={`${workload.workloadName} 更新状态`}
    >
      <ul className="soha-rollout-stages">
        {[
          {
            title: '更新副本',
            content: `${updated} / ${desired} 已更新`,
            status: desired === 0 ? 'wait' : updating ? (failed ? 'error' : 'process') : 'finish',
          },
          {
            title: '实例就绪',
            content: `${ready} / ${desired} 已就绪`,
            status:
              desired === 0
                ? 'wait'
                : ready >= desired && !updating
                  ? 'finish'
                  : failed
                    ? 'error'
                    : 'process',
          },
          {
            title: '服务可用',
            content: `${available} / ${desired} 可用`,
            status:
              desired === 0
                ? 'wait'
                : available >= desired && !updating
                  ? 'finish'
                  : failed
                    ? 'error'
                    : 'wait',
          },
        ].map((step) => (
          <li key={step.title} data-status={step.status}>
            {step.status === 'finish' ? (
              <CheckCircleOutlined />
            ) : step.status === 'error' ? (
              <CloseCircleOutlined />
            ) : step.status === 'process' ? (
              <LoadingOutlined spin />
            ) : (
              <ClockCircleOutlined />
            )}
            <div>
              <strong>{step.title}</strong>
              <span>{step.content}</span>
            </div>
          </li>
        ))}
      </ul>
    </section>
  )
}
