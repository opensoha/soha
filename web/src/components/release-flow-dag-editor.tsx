import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import {
  Background,
  Controls,
  Handle,
  MarkerType,
  MiniMap,
  Position,
  ReactFlow,
  ReactFlowProvider,
  useEdgesState,
  useNodesState,
  useReactFlow,
  type Connection,
  type Edge,
  type Node,
  type NodeProps,
} from '@xyflow/react'
import dagre from 'dagre'
import { Button, Card, Input, InputNumber, Select, Space, Switch, Tag, Typography } from 'antd'
import { CloseOutlined } from '@ant-design/icons'
import '@xyflow/react/dist/style.css'
import { ManagementState } from '@/components/management-list'
import { WorkflowCanvasSurface } from '@/components/workflow-canvas-surface'
import './release-flow-dag-editor.css'
import { useI18n } from '@/i18n'
import {
  createDagNode,
  createNodeConfig,
  getDefaultReleaseDagNodeLabel,
  type DeliveryDagArtifactOutput,
  type DeliveryDagSelector,
  type ReleaseDagDefinition,
  type ReleaseDagEdgeCondition,
  type ReleaseDagEdgeDefinition,
  type ReleaseDagMode,
  type ReleaseDagNodeDefinition,
  type ReleaseDagNodeType,
} from '@/components/release-flow-dag-definition'

const { Text } = Typography

type DagTagColor = 'grey' | 'blue' | 'cyan' | 'green' | 'yellow' | 'purple' | 'pink' | 'red' | 'orange'
type FlowNode = Node<ReleaseDagNodeData, 'releaseStep'>
type FlowEdge = Edge<{ condition: ReleaseDagEdgeCondition }>
type ReleaseFlowDagEditorLayout = 'default' | 'palette-right-floating-inspector'

const DAG_NODE_TAG_COLORS: Record<DagTagColor, string> = {
  grey: 'default',
  blue: 'blue',
  cyan: 'cyan',
  green: 'green',
  yellow: 'gold',
  purple: 'purple',
  pink: 'magenta',
  red: 'red',
  orange: 'orange',
}

const DAG_NODE_OPTIONS: Array<{ value: ReleaseDagNodeType; color: DagTagColor }> = [
  { value: 'build', color: 'green' },
  { value: 'manual_approval', color: 'orange' },
  { value: 'deploy_update_image', color: 'blue' },
  { value: 'restart_workload', color: 'red' },
  { value: 'scale_workload', color: 'cyan' },
  { value: 'delete_pod', color: 'orange' },
  { value: 'evict_pod', color: 'orange' },
  { value: 'wait_rollout', color: 'cyan' },
  { value: 'check_http', color: 'green' },
  { value: 'check_k8s_event', color: 'yellow' },
  { value: 'verify', color: 'purple' },
  { value: 'smoke_test', color: 'purple' },
  { value: 'external', color: 'pink' },
  { value: 'http_callback', color: 'pink' },
  { value: 'create_silence', color: 'grey' },
  { value: 'notify', color: 'pink' },
  { value: 'rollback_to_previous', color: 'red' },
]

const EDGE_CONDITION_OPTIONS = [
  { value: 'success', label: '成功' },
  { value: 'failure', label: '失败' },
  { value: 'always', label: '总是' },
]

const APPROVAL_MODE_OPTIONS = [
  { value: 'any', labelZh: '任一审批', labelEn: 'Any Approver' },
  { value: 'all', labelZh: '全部会签', labelEn: 'All Approvers' },
  { value: 'quorum', labelZh: '指定人数', labelEn: 'Quorum' },
]

const APPROVAL_TIMEOUT_OPTIONS = [
  { value: 'block', labelZh: '阻断发布', labelEn: 'Block Release' },
  { value: 'reject', labelZh: '自动驳回', labelEn: 'Auto Reject' },
  { value: 'notify', labelZh: '只通知升级', labelEn: 'Notify Only' },
]

const APPROVAL_REJECT_ACTION_OPTIONS = [
  { value: 'stop', labelZh: '终止流程', labelEn: 'Stop Flow' },
  { value: 'rollback', labelZh: '进入回滚', labelEn: 'Rollback' },
  { value: 'previous', labelZh: '回到上一节点', labelEn: 'Back to Previous' },
]

const FAILURE_POLICY_OPTIONS = [
  { value: 'stop', labelZh: '终止流程', labelEn: 'Stop Flow' },
  { value: 'continue', labelZh: '继续执行', labelEn: 'Continue' },
  { value: 'rollback', labelZh: '进入回滚', labelEn: 'Rollback' },
  { value: 'notify', labelZh: '通知后终止', labelEn: 'Notify' },
]

const NODE_WIDTH = 260
const NODE_HEIGHT = 116
const FULL_WIDTH_STYLE = { width: '100%' } as const

interface ReleaseDagNodeData extends ReleaseDagNodeDefinition, Record<string, unknown> {}

function createGraphId(prefix: string) {
  return `${prefix}-${Math.random().toString(36).slice(2, 10)}`
}

function getDagNodeLabel(type: ReleaseDagNodeType, localeCode: 'zh_CN' | 'en_US') {
  if (localeCode === 'en_US') {
    switch (type) {
      case 'build':
        return 'Build'
      case 'manual_approval':
        return 'Approval'
      case 'deploy_update_image':
        return 'Update Image'
      case 'restart_workload':
        return 'Restart Workload'
      case 'scale_workload':
        return 'Scale Workload'
      case 'delete_pod':
        return 'Delete Pod'
      case 'evict_pod':
        return 'Evict Pod'
      case 'wait_rollout':
        return 'Wait Rollout'
      case 'check_http':
        return 'HTTP Check'
      case 'check_k8s_event':
        return 'K8s Event'
      case 'verify':
        return 'Verify'
      case 'smoke_test':
        return 'Smoke Test'
      case 'external':
        return 'External Task'
      case 'http_callback':
        return 'HTTP Callback'
      case 'create_silence':
        return 'Create Silence'
      case 'notify':
        return 'Notify'
      case 'rollback_to_previous':
        return 'Rollback'
    }
  }
  return getDefaultReleaseDagNodeLabel(type)
}

function getEdgeConditionOptions(localeCode: 'zh_CN' | 'en_US') {
  if (localeCode === 'en_US') {
    return [
      { value: 'success', label: 'Success' },
      { value: 'failure', label: 'Failure' },
      { value: 'always', label: 'Always' },
    ]
  }
  return EDGE_CONDITION_OPTIONS
}

function localizedOptions(
  options: Array<{ value: string; labelZh: string; labelEn: string }>,
  localeCode: 'zh_CN' | 'en_US',
) {
  return options.map((item) => ({
    value: item.value,
    label: localeCode === 'zh_CN' ? item.labelZh : item.labelEn,
  }))
}

function toConfigObject(value: unknown) {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {}
}

function toConfigStringList(value: unknown) {
  if (!Array.isArray(value)) return []
  return value.map((item) => String(item).trim()).filter(Boolean)
}

function splitConfigStringList(value: string) {
  return value.split(',').map((item) => item.trim()).filter(Boolean)
}

function selectorSummary(selector?: DeliveryDagSelector) {
  if (!selector) return ''
  const labels = Object.entries(selector.matchLabels ?? {}).map(([key, value]) => `${key}=${value}`)
  return [selector.id, selector.key, selector.keys?.join(','), ...labels].filter(Boolean).join(' / ')
}

function artifactOutputSummary(items?: DeliveryDagArtifactOutput[]) {
  if (!items?.length) return ''
  return items.map((item) => `${item.kind}:${item.name}`).join(' / ')
}

function parseJsonObject(value: string) {
  const text = value.trim()
  if (!text) return undefined
  const parsed = JSON.parse(text)
  return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed as Record<string, unknown> : undefined
}

function parseJsonArray(value: string) {
  const text = value.trim()
  if (!text) return undefined
  const parsed = JSON.parse(text)
  return Array.isArray(parsed) ? parsed : undefined
}

function stringifyJsonValue(value: unknown) {
  if (value === undefined || value === null) return ''
  return JSON.stringify(value, null, 2)
}

function patchNodeDeliveryField<K extends keyof ReleaseDagNodeDefinition>(
  node: ReleaseDagNodeDefinition,
  key: K,
  value: ReleaseDagNodeDefinition[K] | undefined,
) {
  const next = { ...node }
  if (value === undefined || (Array.isArray(value) && value.length === 0)) {
    delete next[key]
  } else {
    next[key] = value
  }
  return next
}

function approvalModeLabel(value: unknown, localeCode: 'zh_CN' | 'en_US') {
  const option = APPROVAL_MODE_OPTIONS.find((item) => item.value === String(value || 'any'))
  if (!option) return String(value || 'any')
  return localeCode === 'zh_CN' ? option.labelZh : option.labelEn
}

function approvalNodeSummary(node: ReleaseDagNodeDefinition, localeCode: 'zh_CN' | 'en_US') {
  if (node.type !== 'manual_approval') return ''
  const config = node.config ?? {}
  const requiredApprovals = Math.max(1, Number(config.requiredApprovals ?? 1))
  const roles = toConfigStringList(config.approverRoles)
  const teams = toConfigStringList(config.approverTeams)
  const users = toConfigStringList(config.approverUsers)
  const actor = roles[0] ?? teams[0] ?? users[0] ?? (localeCode === 'zh_CN' ? '未配置审批人' : 'No approver')
  if (localeCode === 'zh_CN') {
    return `${approvalModeLabel(config.approvalMode, localeCode)} / ${requiredApprovals} 人 / ${actor}`
  }
  return `${approvalModeLabel(config.approvalMode, localeCode)} / ${requiredApprovals} / ${actor}`
}

function toFlowNode(node: ReleaseDagNodeDefinition): FlowNode {
  return {
    id: node.id,
    type: 'releaseStep',
    position: node.position,
    data: { ...node },
  }
}

function toFlowEdge(edge: ReleaseDagEdgeDefinition): FlowEdge {
  const condition = edge.condition || 'success'
  return {
    id: edge.id,
    source: edge.source,
    target: edge.target,
    type: 'smoothstep',
    label: condition,
    markerEnd: { type: MarkerType.ArrowClosed },
    data: { condition },
  }
}

function serializeNodes(nodes: FlowNode[]): ReleaseDagNodeDefinition[] {
  return nodes.map((node) => ({
    id: node.id,
    type: node.data.type,
    name: node.data.name,
    ...(node.data.executorKind ? { executorKind: node.data.executorKind } : {}),
    ...(node.data.targetKind ? { targetKind: node.data.targetKind } : {}),
    ...(node.data.capabilityRef ? { capabilityRef: node.data.capabilityRef } : {}),
    ...(node.data.providerRef ? { providerRef: node.data.providerRef } : {}),
    timeoutSeconds: node.data.timeoutSeconds ?? 300,
    continueOnFailure: Boolean(node.data.continueOnFailure),
    ...(node.data.inputs?.length ? { inputs: node.data.inputs } : {}),
    ...(node.data.outputs?.length ? { outputs: node.data.outputs } : {}),
    ...(node.data.serviceSelector ? { serviceSelector: node.data.serviceSelector } : {}),
    ...(node.data.environmentSelector ? { environmentSelector: node.data.environmentSelector } : {}),
    ...(node.data.targetSelector ? { targetSelector: node.data.targetSelector } : {}),
    ...(node.data.inputMapping ? { inputMapping: node.data.inputMapping } : {}),
    ...(node.data.artifactOutputs?.length ? { artifactOutputs: node.data.artifactOutputs } : {}),
    ...(node.data.artifactKinds?.length ? { artifactKinds: node.data.artifactKinds } : {}),
    ...(node.data.runCondition ? { runCondition: node.data.runCondition } : {}),
    ...(node.data.failurePolicy ? { failurePolicy: node.data.failurePolicy } : {}),
    ...(node.data.observability ? { observability: node.data.observability } : {}),
    config: node.data.config ?? {},
    position: node.position,
  }))
}

function serializeDefinition(mode: ReleaseDagMode, nodes: FlowNode[], edges: FlowEdge[]): ReleaseDagDefinition {
  return {
    schemaVersion: 2,
    mode,
    nodes: serializeNodes(nodes),
    edges: serializeEdges(edges),
  }
}

function serializeEdges(edges: FlowEdge[]): ReleaseDagEdgeDefinition[] {
  return edges.map((edge) => ({
    id: edge.id,
    source: edge.source,
    target: edge.target,
    condition: edge.data?.condition || 'success',
  }))
}

function layoutGraph(
  nodes: FlowNode[],
  edges: FlowEdge[],
  direction: 'TB' | 'LR' = 'TB',
) {
  const graph = new dagre.graphlib.Graph()
  graph.setDefaultEdgeLabel(() => ({}))
  graph.setGraph({ rankdir: direction, ranksep: 80, nodesep: 60 })

  nodes.forEach((node) => {
    graph.setNode(node.id, { width: NODE_WIDTH, height: NODE_HEIGHT })
  })
  edges.forEach((edge) => {
    graph.setEdge(edge.source, edge.target)
  })
  dagre.layout(graph)

  return nodes.map((node) => {
    const position = graph.node(node.id)
    return {
      ...node,
      position: {
        x: position.x - NODE_WIDTH / 2,
        y: position.y - NODE_HEIGHT / 2,
      },
    }
  })
}

function ReleaseStepNode({ data, selected }: NodeProps<FlowNode>) {
  const { localeCode } = useI18n()
  const option = DAG_NODE_OPTIONS.find((item) => item.value === data.type)
  return (
    <div className={`soha-dag-node ${selected ? 'is-selected' : ''}`}>
      <Handle type="target" position={Position.Top} />
      <div className="soha-dag-node-card">
        <div className="soha-dag-node-head">
          <Text strong>{data.name}</Text>
          <Tag color={option ? DAG_NODE_TAG_COLORS[option.color] : 'blue'}>{getDagNodeLabel(data.type, localeCode)}</Tag>
        </div>
        <Text type="secondary" className="text-xs">{localeCode === 'zh_CN' ? `超时 ${data.timeoutSeconds ?? 300}s` : `timeout ${data.timeoutSeconds ?? 300}s`}</Text>
        {data.type === 'manual_approval' ? <Text type="secondary" className="text-xs">{approvalNodeSummary(data, localeCode)}</Text> : null}
        {data.executorKind ? <Text type="secondary" className="text-xs">{`executor ${data.executorKind}`}</Text> : null}
        {data.capabilityRef ? <Text type="secondary" className="text-xs">{`capability ${data.capabilityRef}`}</Text> : null}
        {data.inputs?.length ? <Text type="secondary" className="text-xs">{`inputs ${data.inputs.join(', ')}`}</Text> : null}
        {data.outputs?.length ? <Text type="secondary" className="text-xs">{`outputs ${data.outputs.join(', ')}`}</Text> : null}
        {artifactOutputSummary(data.artifactOutputs) ? <Text type="secondary" className="text-xs">{artifactOutputSummary(data.artifactOutputs)}</Text> : null}
        {selectorSummary(data.serviceSelector) || selectorSummary(data.environmentSelector) || selectorSummary(data.targetSelector) ? (
          <Text type="secondary" className="text-xs">{selectorSummary(data.serviceSelector) || selectorSummary(data.environmentSelector) || selectorSummary(data.targetSelector)}</Text>
        ) : null}
        {data.continueOnFailure ? <Text type="warning" className="text-xs">{localeCode === 'zh_CN' ? '失败继续' : 'Continue on failure'}</Text> : null}
      </div>
      <Handle type="source" position={Position.Bottom} />
    </div>
  )
}

function StepConfigInspector({
  node,
  onChange,
}: {
  node: ReleaseDagNodeDefinition
  onChange: (config: Record<string, unknown>) => void
}) {
  const { localeCode } = useI18n()
  const config = node.config ?? {}
  const patch = (key: string, value: unknown) => onChange({ ...config, [key]: value })

  switch (node.type) {
    case 'build':
      return (
        <>
          <Input value={String(config.sourceRef ?? 'binding')} placeholder="binding source" onChange={(event) => patch('sourceRef', event.target.value)} />
          <Select
            style={FULL_WIDTH_STYLE}
            value={String(config.refType ?? 'branch')}
            options={[
              { value: 'branch', label: localeCode === 'zh_CN' ? '分支' : 'Branch' },
              { value: 'tag', label: localeCode === 'zh_CN' ? '标签' : 'Tag' },
            ]}
            onChange={(value) => patch('refType', String(value))}
          />
          <Input value={String(config.refValue ?? '')} placeholder="main / v1.0.0" onChange={(event) => patch('refValue', event.target.value)} />
        </>
      )
    case 'manual_approval': {
      const changeWindow = toConfigObject(config.changeWindow)
      const patchChangeWindow = (key: string, value: unknown) => patch('changeWindow', { ...changeWindow, [key]: value })
      return (
        <div className="soha-dag-approval-config">
          <div className="soha-dag-inspector-field">
            <Text type="secondary" className="text-xs">{localeCode === 'zh_CN' ? '审批模式' : 'Approval Mode'}</Text>
            <Select
              style={FULL_WIDTH_STYLE}
              value={String(config.approvalMode ?? 'any')}
              options={localizedOptions(APPROVAL_MODE_OPTIONS, localeCode)}
              onChange={(value) => patch('approvalMode', String(value))}
            />
          </div>
          <div className="soha-dag-inspector-field">
            <Text type="secondary" className="text-xs">{localeCode === 'zh_CN' ? '最少审批人数' : 'Required Approvals'}</Text>
            <InputNumber
              style={FULL_WIDTH_STYLE}
              value={Number(config.requiredApprovals ?? 1)}
              min={1}
              step={1}
              onChange={(value) => patch('requiredApprovals', Math.max(1, Number(value || 1)))}
            />
          </div>
          <div className="soha-dag-inspector-field">
            <Text type="secondary" className="text-xs">{localeCode === 'zh_CN' ? '候选用户' : 'Approver Users'}</Text>
            <Input
              value={toConfigStringList(config.approverUsers).join(', ')}
              placeholder="release-owner, qa-owner"
              onChange={(event) => patch('approverUsers', splitConfigStringList(event.target.value))}
            />
          </div>
          <div className="soha-dag-inspector-field">
            <Text type="secondary" className="text-xs">{localeCode === 'zh_CN' ? '候选角色' : 'Approver Roles'}</Text>
            <Input
              value={toConfigStringList(config.approverRoles).join(', ')}
              placeholder="release-manager, ops-lead"
              onChange={(event) => patch('approverRoles', splitConfigStringList(event.target.value))}
            />
          </div>
          <div className="soha-dag-inspector-field">
            <Text type="secondary" className="text-xs">{localeCode === 'zh_CN' ? '候选团队' : 'Approver Teams'}</Text>
            <Input
              value={toConfigStringList(config.approverTeams).join(', ')}
              placeholder="qa, sre"
              onChange={(event) => patch('approverTeams', splitConfigStringList(event.target.value))}
            />
          </div>
          <div className="soha-dag-inspector-field">
            <Text type="secondary" className="text-xs">{localeCode === 'zh_CN' ? '审批 SLA(分钟)' : 'Approval SLA(min)'}</Text>
            <InputNumber
              style={FULL_WIDTH_STYLE}
              value={Number(config.slaMinutes ?? 60)}
              min={1}
              step={5}
              onChange={(value) => patch('slaMinutes', Math.max(1, Number(value || 60)))}
            />
          </div>
          <div className="soha-dag-inspector-field">
            <Text type="secondary" className="text-xs">{localeCode === 'zh_CN' ? '超时动作' : 'Timeout Action'}</Text>
            <Select
              style={FULL_WIDTH_STYLE}
              value={String(config.onTimeout ?? 'block')}
              options={localizedOptions(APPROVAL_TIMEOUT_OPTIONS, localeCode)}
              onChange={(value) => patch('onTimeout', String(value))}
            />
          </div>
          <div className="soha-dag-inspector-field">
            <Text type="secondary" className="text-xs">{localeCode === 'zh_CN' ? '驳回后动作' : 'Reject Action'}</Text>
            <Select
              style={FULL_WIDTH_STYLE}
              value={String(config.rejectAction ?? 'stop')}
              options={localizedOptions(APPROVAL_REJECT_ACTION_OPTIONS, localeCode)}
              onChange={(value) => patch('rejectAction', String(value))}
            />
          </div>
          <div className="soha-step-inline">
            <Text type="secondary" className="text-xs">{localeCode === 'zh_CN' ? '必须审批' : 'Required approval'}</Text>
            <Switch checked={config.required !== false} onChange={(checked) => patch('required', checked)} />
          </div>
          <div className="soha-dag-approval-window">
            <div className="soha-step-inline">
              <Text type="secondary" className="text-xs">{localeCode === 'zh_CN' ? '启用变更窗口' : 'Change Window'}</Text>
              <Switch checked={Boolean(changeWindow.enabled)} onChange={(checked) => patchChangeWindow('enabled', checked)} />
            </div>
            <Input
              value={String(changeWindow.startsAt ?? '')}
              placeholder={localeCode === 'zh_CN' ? '窗口开始，例如 09:00' : 'Start, e.g. 09:00'}
              onChange={(event) => patchChangeWindow('startsAt', event.target.value)}
            />
            <Input
              value={String(changeWindow.endsAt ?? '')}
              placeholder={localeCode === 'zh_CN' ? '窗口结束，例如 18:00' : 'End, e.g. 18:00'}
              onChange={(event) => patchChangeWindow('endsAt', event.target.value)}
            />
            <Input
              value={String(changeWindow.timezone ?? 'Asia/Shanghai')}
              placeholder="Asia/Shanghai"
              onChange={(event) => patchChangeWindow('timezone', event.target.value)}
            />
          </div>
        </div>
      )
    }
    case 'deploy_update_image':
      return (
        <>
          <Input value={String(config.targetRef ?? 'primary')} placeholder="target ref" onChange={(event) => patch('targetRef', event.target.value)} />
          <Select
            style={FULL_WIDTH_STYLE}
            value={String(config.imageTagSource ?? 'workflow_input')}
            options={[
              { value: 'workflow_input', label: localeCode === 'zh_CN' ? '执行时输入' : 'Runtime input' },
              { value: 'application_default', label: localeCode === 'zh_CN' ? '应用默认 Tag' : 'Application default tag' },
              { value: 'build_artifact', label: localeCode === 'zh_CN' ? '构建产物' : 'Build artifact' },
            ]}
            onChange={(value) => patch('imageTagSource', String(value))}
          />
        </>
      )
    case 'restart_workload':
      return <Input value={String(config.deploymentName ?? '')} placeholder="deployment name" onChange={(event) => patch('deploymentName', event.target.value)} />
    case 'scale_workload':
      return (
        <>
          <Input value={String(config.deploymentName ?? '')} placeholder="deployment name" onChange={(event) => patch('deploymentName', event.target.value)} />
          <InputNumber style={FULL_WIDTH_STYLE} value={Number(config.replicas ?? 1)} min={0} step={1} onChange={(value) => patch('replicas', Number(value || 1))} />
        </>
      )
    case 'delete_pod':
    case 'evict_pod':
      return <Input value={String(config.podName ?? '')} placeholder="pod name" onChange={(event) => patch('podName', event.target.value)} />
    case 'wait_rollout':
      return (
        <InputNumber style={FULL_WIDTH_STYLE} value={Number(config.timeoutSeconds ?? 300)} min={30} step={30} onChange={(value) => patch('timeoutSeconds', Number(value || 300))} />
      )
    case 'check_http':
      return (
        <>
          <Input value={String(config.url ?? '')} placeholder="https://service/healthz" onChange={(event) => patch('url', event.target.value)} />
          <InputNumber style={FULL_WIDTH_STYLE} value={Number(config.expectedStatus ?? 200)} min={100} max={599} onChange={(value) => patch('expectedStatus', Number(value || 200))} />
        </>
      )
    case 'check_k8s_event':
      return (
        <>
          <Select
            style={FULL_WIDTH_STYLE}
            value={String(config.eventType ?? 'Warning')}
            options={[
              { value: 'Warning', label: 'Warning' },
              { value: 'Normal', label: 'Normal' },
            ]}
            onChange={(value) => patch('eventType', String(value))}
          />
          <Input value={String(config.reasonContains ?? '')} placeholder="BackOff / Failed / Unhealthy" onChange={(event) => patch('reasonContains', event.target.value)} />
        </>
      )
    case 'smoke_test':
    case 'verify':
    case 'external':
      return (
        <>
          <Input value={String(config.endpoint ?? '')} placeholder="https://service/smoke" onChange={(event) => patch('endpoint', event.target.value)} />
          <InputNumber style={FULL_WIDTH_STYLE} value={Number(config.expectedStatus ?? 200)} min={100} max={599} onChange={(value) => patch('expectedStatus', Number(value || 200))} />
        </>
      )
    case 'http_callback':
      return (
        <>
          <Input value={String(config.url ?? '')} placeholder="https://hooks/service" onChange={(event) => patch('url', event.target.value)} />
          <Input value={String(config.method ?? 'POST')} placeholder="POST" onChange={(event) => patch('method', event.target.value)} />
          <InputNumber style={FULL_WIDTH_STYLE} value={Number(config.expectedStatus ?? 200)} min={100} max={599} onChange={(value) => patch('expectedStatus', Number(value || 200))} />
          <Input.TextArea value={String(config.body ?? '{}')} rows={4} onChange={(event) => patch('body', event.target.value)} />
        </>
      )
    case 'create_silence':
      return (
        <>
          <Input value={String(config.name ?? '')} placeholder="silence name" onChange={(event) => patch('name', event.target.value)} />
          <Input value={String(config.reason ?? '')} placeholder="reason" onChange={(event) => patch('reason', event.target.value)} />
          <InputNumber style={FULL_WIDTH_STYLE} value={Number(config.durationMinutes ?? 60)} min={1} step={5} onChange={(value) => patch('durationMinutes', Number(value || 60))} />
          <Input.TextArea value={JSON.stringify(config.matchers ?? {}, null, 2)} rows={4} onChange={(event) => {
            try {
              patch('matchers', JSON.parse(event.target.value))
            } catch {
              patch('matchers', config.matchers ?? {})
            }
          }} />
        </>
      )
    case 'notify':
      return (
        <>
          <Input value={String(config.channel ?? '')} placeholder="wecom-release / ding-group" onChange={(event) => patch('channel', event.target.value)} />
          <Input value={String(config.template ?? 'release-result')} placeholder="template" onChange={(event) => patch('template', event.target.value)} />
        </>
      )
    case 'rollback_to_previous':
      return <Text type="secondary" className="text-xs">{localeCode === 'zh_CN' ? '回滚到最近成功的上一版 revision。' : 'Roll back to the previous successful revision.'}</Text>
    default:
      return null
  }
}

function DeliveryDagInspector({
  node,
  onChange,
}: {
  node: ReleaseDagNodeDefinition
  onChange: (node: ReleaseDagNodeDefinition) => void
}) {
  const { localeCode } = useI18n()
  const patch = <K extends keyof ReleaseDagNodeDefinition>(key: K, value: ReleaseDagNodeDefinition[K] | undefined) => {
    onChange(patchNodeDeliveryField(node, key, value))
  }
  const patchJSON = <K extends keyof ReleaseDagNodeDefinition>(key: K, value: string, parser: (input: string) => unknown) => {
    try {
      patch(key, parser(value) as ReleaseDagNodeDefinition[K])
    } catch {
      // Keep the previous valid structured field while the user is editing invalid JSON.
    }
  }

  return (
    <div className="soha-dag-approval-config">
      <div className="soha-dag-inspector-field">
        <Text type="secondary" className="text-xs">{localeCode === 'zh_CN' ? '执行器' : 'Executor'}</Text>
        <Input
          value={node.executorKind ?? ''}
          placeholder="runner / mcp / webhook_callback"
          onChange={(event) => patch('executorKind', event.target.value.trim() || undefined)}
        />
      </div>
      <div className="soha-dag-inspector-field">
        <Text type="secondary" className="text-xs">{localeCode === 'zh_CN' ? '目标类型' : 'Target Kind'}</Text>
        <Input
          value={node.targetKind ?? ''}
          placeholder="k8s_workload / ai_test"
          onChange={(event) => patch('targetKind', event.target.value.trim() || undefined)}
        />
      </div>
      <div className="soha-dag-inspector-field">
        <Text type="secondary" className="text-xs">{localeCode === 'zh_CN' ? '能力引用' : 'Capability Ref'}</Text>
        <Input
          value={node.capabilityRef ?? ''}
          placeholder="testing.ui.run"
          onChange={(event) => patch('capabilityRef', event.target.value.trim() || undefined)}
        />
      </div>
      <div className="soha-dag-inspector-field">
        <Text type="secondary" className="text-xs">{localeCode === 'zh_CN' ? 'Provider 引用' : 'Provider Ref'}</Text>
        <Input
          value={node.providerRef ?? ''}
          placeholder="external-test-platform"
          onChange={(event) => patch('providerRef', event.target.value.trim() || undefined)}
        />
      </div>
      <div className="soha-dag-inspector-field">
        <Text type="secondary" className="text-xs">{localeCode === 'zh_CN' ? '输入产物' : 'Inputs'}</Text>
        <Input
          value={node.inputs?.join(', ') ?? ''}
          placeholder="source.image, scan.sbom"
          onChange={(event) => patch('inputs', splitConfigStringList(event.target.value))}
        />
      </div>
      <div className="soha-dag-inspector-field">
        <Text type="secondary" className="text-xs">{localeCode === 'zh_CN' ? '输出产物' : 'Outputs'}</Text>
        <Input
          value={node.outputs?.join(', ') ?? ''}
          placeholder="image, test_report"
          onChange={(event) => patch('outputs', splitConfigStringList(event.target.value))}
        />
      </div>
      <div className="soha-dag-inspector-field">
        <Text type="secondary" className="text-xs">{localeCode === 'zh_CN' ? '运行条件' : 'Run Condition'}</Text>
        <Input
          value={node.runCondition ?? ''}
          placeholder="branch == main"
          onChange={(event) => patch('runCondition', event.target.value.trim() || undefined)}
        />
      </div>
      <div className="soha-dag-inspector-field">
        <Text type="secondary" className="text-xs">{localeCode === 'zh_CN' ? '失败策略' : 'Failure Policy'}</Text>
        <Select
          allowClear
          style={FULL_WIDTH_STYLE}
          value={node.failurePolicy}
          options={localizedOptions(FAILURE_POLICY_OPTIONS, localeCode)}
          onChange={(value) => patch('failurePolicy', value ? String(value) : undefined)}
        />
      </div>
      <div className="soha-dag-inspector-field">
        <Text type="secondary" className="text-xs">{localeCode === 'zh_CN' ? '服务选择器(JSON)' : 'Service Selector JSON'}</Text>
        <Input.TextArea
          rows={3}
          value={stringifyJsonValue(node.serviceSelector)}
          placeholder='{"matchLabels":{"service":"api"}}'
          onChange={(event) => patchJSON('serviceSelector', event.target.value, parseJsonObject)}
        />
      </div>
      <div className="soha-dag-inspector-field">
        <Text type="secondary" className="text-xs">{localeCode === 'zh_CN' ? '环境选择器(JSON)' : 'Environment Selector JSON'}</Text>
        <Input.TextArea
          rows={3}
          value={stringifyJsonValue(node.environmentSelector)}
          placeholder='{"key":"prod"}'
          onChange={(event) => patchJSON('environmentSelector', event.target.value, parseJsonObject)}
        />
      </div>
      <div className="soha-dag-inspector-field">
        <Text type="secondary" className="text-xs">{localeCode === 'zh_CN' ? '目标选择器(JSON)' : 'Target Selector JSON'}</Text>
        <Input.TextArea
          rows={3}
          value={stringifyJsonValue(node.targetSelector)}
          placeholder='{"matchLabels":{"tier":"backend"}}'
          onChange={(event) => patchJSON('targetSelector', event.target.value, parseJsonObject)}
        />
      </div>
      <div className="soha-dag-inspector-field">
        <Text type="secondary" className="text-xs">{localeCode === 'zh_CN' ? '输入映射(JSON)' : 'Input Mapping JSON'}</Text>
        <Input.TextArea
          rows={3}
          value={stringifyJsonValue(node.inputMapping)}
          placeholder='{"baseUrl":"${environment.url}"}'
          onChange={(event) => patchJSON('inputMapping', event.target.value, parseJsonObject)}
        />
      </div>
      <div className="soha-dag-inspector-field">
        <Text type="secondary" className="text-xs">{localeCode === 'zh_CN' ? '产物类型' : 'Artifact Kinds'}</Text>
        <Input
          value={node.artifactKinds?.join(', ') ?? ''}
          placeholder="test_report, screenshot, video, junit"
          onChange={(event) => patch('artifactKinds', splitConfigStringList(event.target.value))}
        />
      </div>
      <div className="soha-dag-inspector-field">
        <Text type="secondary" className="text-xs">{localeCode === 'zh_CN' ? '产物输出(JSON)' : 'Artifact Outputs JSON'}</Text>
        <Input.TextArea
          rows={4}
          value={stringifyJsonValue(node.artifactOutputs)}
          placeholder='[{"name":"image","kind":"image","required":true}]'
          onChange={(event) => patchJSON('artifactOutputs', event.target.value, parseJsonArray)}
        />
      </div>
      <div className="soha-dag-inspector-field">
        <Text type="secondary" className="text-xs">{localeCode === 'zh_CN' ? '可观测事件(JSON)' : 'Observability JSON'}</Text>
        <Input.TextArea
          rows={3}
          value={stringifyJsonValue(node.observability)}
          placeholder='{"events":["started","completed"]}'
          onChange={(event) => patchJSON('observability', event.target.value, parseJsonObject)}
        />
      </div>
    </div>
  )
}

function ReleaseFlowDagEditorInner({
  initialDefinition,
  onChange,
  variant = 'panel',
  layout = 'default',
  height,
  className,
}: {
  initialDefinition: ReleaseDagDefinition
  onChange: (definition: ReleaseDagDefinition) => void
  variant?: 'panel' | 'embedded'
  layout?: ReleaseFlowDagEditorLayout
  height?: number | string
  className?: string
}) {
  const { localeCode } = useI18n()
  const edgeConditionOptions = useMemo(() => getEdgeConditionOptions(localeCode), [localeCode])
  const [nodes, setNodes, onNodesChange] = useNodesState<FlowNode>(initialDefinition.nodes.map(toFlowNode))
  const [edges, setEdges, onEdgesChange] = useEdgesState<FlowEdge>(initialDefinition.edges.map(toFlowEdge))
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null)
  const [selectedEdgeId, setSelectedEdgeId] = useState<string | null>(null)
  const { fitView } = useReactFlow()

  useEffect(() => {
    onChange(serializeDefinition(initialDefinition.mode, nodes, edges))
  }, [edges, initialDefinition.mode, nodes, onChange])

  useEffect(() => {
    setNodes(initialDefinition.nodes.map(toFlowNode))
    setEdges(initialDefinition.edges.map(toFlowEdge))
    setSelectedNodeId(null)
    setSelectedEdgeId(null)
  }, [initialDefinition.edges, initialDefinition.nodes, setEdges, setNodes])

  const selectedNode = useMemo(
    () => nodes.find((node) => node.id === selectedNodeId),
    [nodes, selectedNodeId],
  )
  const selectedEdge = useMemo(
    () => edges.find((edge) => edge.id === selectedEdgeId),
    [edges, selectedEdgeId],
  )
  const useFloatingInspector = layout === 'palette-right-floating-inspector'

  const addNode = useCallback((type: ReleaseDagNodeType) => {
    setNodes((current) => [...current, toFlowNode(createDagNode(type, { x: 120 + current.length * 24, y: 120 + current.length * 24 }))])
  }, [setNodes])

  const onConnect = useCallback((connection: Connection) => {
    if (!connection.source || !connection.target) {
      return
    }
    setEdges((current) => current.concat({
      id: createGraphId('edge'),
      source: connection.source,
      target: connection.target,
      type: 'smoothstep',
      label: 'success',
      markerEnd: { type: MarkerType.ArrowClosed },
      data: { condition: 'success' },
    }))
  }, [setEdges])

  const applyAutoLayout = useCallback(() => {
    setNodes((current) => layoutGraph(current, edges))
    requestAnimationFrame(() => fitView({ padding: 0.2 }))
  }, [edges, fitView, setNodes])

  const removeSelected = useCallback(() => {
    if (selectedNodeId) {
      setNodes((current) => current.filter((node) => node.id !== selectedNodeId))
      setEdges((current) => current.filter((edge) => edge.source !== selectedNodeId && edge.target !== selectedNodeId))
      setSelectedNodeId(null)
      return
    }
    if (selectedEdgeId) {
      setEdges((current) => current.filter((edge) => edge.id !== selectedEdgeId))
      setSelectedEdgeId(null)
    }
  }, [selectedEdgeId, selectedNodeId, setEdges, setNodes])

  const closeFloatingInspector = useCallback(() => {
    setSelectedNodeId(null)
    setSelectedEdgeId(null)
  }, [])

  const panel = (title: ReactNode, content: ReactNode, panelClassName = 'soha-dag-panel') => {
    if (variant === 'embedded') {
      return (
        <section className={`${panelClassName} soha-dag-embedded-panel`}>
          <div className="soha-dag-embedded-panel__head">{title}</div>
          <div className="soha-dag-embedded-panel__body">{content}</div>
        </section>
      )
    }
    return <Card className={panelClassName} title={title}>{content}</Card>
  }

  const inspectorTitle = selectedNode
    ? (localeCode === 'zh_CN' ? '节点属性' : 'Node Properties')
    : selectedEdge
      ? (localeCode === 'zh_CN' ? '连线属性' : 'Edge Properties')
      : (localeCode === 'zh_CN' ? '属性面板' : 'Inspector')
  const selectedNodeOption = selectedNode ? DAG_NODE_OPTIONS.find((item) => item.value === selectedNode.data.type) : undefined
  const nodeTypeEditor = selectedNode ? (
    useFloatingInspector ? (
      <div className="soha-dag-inspector-type">
        <Text type="secondary" className="text-xs">{localeCode === 'zh_CN' ? '节点功能' : 'Node Function'}</Text>
        <Tag color={selectedNodeOption ? DAG_NODE_TAG_COLORS[selectedNodeOption.color] : 'blue'}>
          {getDagNodeLabel(selectedNode.data.type, localeCode)}
        </Tag>
      </div>
    ) : (
      <Select
        style={FULL_WIDTH_STYLE}
        value={selectedNode.data.type}
        options={DAG_NODE_OPTIONS.map((item) => ({ value: item.value, label: getDagNodeLabel(item.value, localeCode) }))}
        onChange={(value) => {
          const nextType = String(value) as ReleaseDagNodeType
          setNodes((current) => current.map((node) => node.id === selectedNode.id ? {
            ...node,
            data: {
              ...node.data,
              type: nextType,
              config: createNodeConfig(nextType),
            },
          } : node))
        }}
      />
    )
  ) : null

  const nodeInspectorFields = selectedNode ? (
    <>
      <Input
        value={selectedNode.data.name}
        onChange={(event) => setNodes((current) => current.map((node) => node.id === selectedNode.id ? { ...node, data: { ...node.data, name: event.target.value } } : node))}
      />
      {nodeTypeEditor}
      <InputNumber
        style={FULL_WIDTH_STYLE}
        value={selectedNode.data.timeoutSeconds ?? 300}
        min={30}
        step={30}
        onChange={(value) => setNodes((current) => current.map((node) => node.id === selectedNode.id ? { ...node, data: { ...node.data, timeoutSeconds: Number(value || 300) } } : node))}
      />
      <div className="soha-step-inline">
        <Text type="secondary" className="text-xs">{localeCode === 'zh_CN' ? '失败继续' : 'Continue on Failure'}</Text>
        <Switch
          checked={Boolean(selectedNode.data.continueOnFailure)}
          onChange={(checked) => setNodes((current) => current.map((node) => node.id === selectedNode.id ? { ...node, data: { ...node.data, continueOnFailure: checked } } : node))}
        />
      </div>
      <StepConfigInspector
        node={selectedNode.data}
        onChange={(config) => setNodes((current) => current.map((node) => node.id === selectedNode.id ? { ...node, data: { ...node.data, config } } : node))}
      />
      <DeliveryDagInspector
        node={selectedNode.data}
        onChange={(nextNode) => setNodes((current) => current.map((node) => node.id === selectedNode.id ? { ...node, data: { ...node.data, ...nextNode } } : node))}
      />
    </>
  ) : null
  const edgeInspectorFields = selectedEdge ? (
    <>
      <Select
        style={FULL_WIDTH_STYLE}
        value={selectedEdge.data?.condition || 'success'}
        options={edgeConditionOptions}
        onChange={(value) => {
          const condition = String(value) as ReleaseDagEdgeCondition
          setEdges((current) => current.map((edge) => edge.id === selectedEdge.id ? { ...edge, label: condition, data: { condition } } : edge))
        }}
      />
      <Text type="secondary" className="text-xs">{`${selectedEdge.source} -> ${selectedEdge.target}`}</Text>
    </>
  ) : null
  const activeInspectorFields = selectedNode ? nodeInspectorFields : edgeInspectorFields
  const inspectorContent = activeInspectorFields ? (
    <div className="soha-dag-inspector">
      <Text strong>{inspectorTitle}</Text>
      {activeInspectorFields}
    </div>
  ) : (
    <ManagementState
      bordered={false}
      compact
      kind="select-scope"
      title={localeCode === 'zh_CN' ? '请选择节点或连线' : 'Select a node or edge'}
      description={localeCode === 'zh_CN' ? '选择节点或连线后，可在这里编辑属性' : 'Select a node or edge to edit its properties here'}
    />
  )
  const floatingInspector = useFloatingInspector && activeInspectorFields ? (
    <div className="soha-dag-floating-inspector">
      <div className="soha-dag-floating-inspector__head">
        <Text strong>{inspectorTitle}</Text>
        <Button
          aria-label={localeCode === 'zh_CN' ? '关闭属性面板' : 'Close inspector'}
          icon={<CloseOutlined />}
          size="small"
          type="text"
          onClick={closeFloatingInspector}
        />
      </div>
      <div className="soha-dag-inspector">
        {activeInspectorFields}
      </div>
    </div>
  ) : null
  const palettePanel = panel(localeCode === 'zh_CN' ? '节点面板' : 'Node Palette', (
    <>
    <div className="soha-dag-palette">
      {DAG_NODE_OPTIONS.map((item) => (
        <Button key={item.value} onClick={() => addNode(item.value)}>
          {getDagNodeLabel(item.value, localeCode)}
        </Button>
      ))}
    </div>
    <Space wrap>
      <Button type="primary" onClick={applyAutoLayout}>{localeCode === 'zh_CN' ? '自动布局' : 'Auto Layout'}</Button>
      <Button onClick={() => fitView({ padding: 0.2 })}>{localeCode === 'zh_CN' ? '适配视图' : 'Fit View'}</Button>
      <Button type="text" danger disabled={!selectedNodeId && !selectedEdgeId} onClick={removeSelected}>
        {localeCode === 'zh_CN' ? '删除选中' : 'Delete Selected'}
      </Button>
    </Space>
    <Text type="secondary" className="text-xs">
      {useFloatingInspector
        ? (localeCode === 'zh_CN'
          ? '节点可直接在画布中拖动。选择节点或连线后，在画布内编辑属性。'
          : 'Drag nodes on the canvas. Select a node or edge to edit it inside the canvas.')
        : (localeCode === 'zh_CN'
          ? '节点可直接在画布中拖动。连线默认表示 `success`，右侧属性面板可以改成 `failure` 或 `always`。'
          : 'Drag nodes directly on the canvas. Edges default to `success`, and the inspector can switch them to `failure` or `always`.')}
    </Text>
    </>
  ))
  const canvasPanel = panel(localeCode === 'zh_CN' ? 'DAG 画布' : 'DAG Canvas', (
    <WorkflowCanvasSurface className="soha-dag-canvas" height={height}>
      <ReactFlow<FlowNode, FlowEdge>
        nodes={nodes}
        edges={edges}
        nodeTypes={{ releaseStep: ReleaseStepNode } as const}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onConnect={onConnect}
        onSelectionChange={({ nodes: activeNodes, edges: activeEdges }) => {
          const activeNodeId = activeNodes[0]?.id ?? null
          setSelectedNodeId(activeNodeId)
          setSelectedEdgeId(activeNodeId ? null : activeEdges[0]?.id ?? null)
        }}
        fitView
      >
        <Background gap={20} size={1} />
        <Controls />
        <MiniMap />
      </ReactFlow>
      {floatingInspector}
    </WorkflowCanvasSurface>
  ), 'soha-dag-canvas-card')
  const inspectorPanel = panel(localeCode === 'zh_CN' ? '属性面板' : 'Inspector', inspectorContent)

  return (
    <div className={[
      'soha-dag-editor-shell',
      variant === 'embedded' ? 'soha-dag-editor-shell--embedded' : '',
      layout === 'palette-right-floating-inspector' ? 'soha-dag-editor-shell--palette-right' : '',
      className ?? '',
    ].filter(Boolean).join(' ')}>
      {layout === 'palette-right-floating-inspector' ? (
        <>
          {canvasPanel}
          {palettePanel}
        </>
      ) : (
        <>
          {palettePanel}
          {canvasPanel}
          {inspectorPanel}
        </>
      )}
    </div>
  )
}

export function ReleaseFlowDagEditor({
  initialDefinition,
  onChange,
  variant,
  layout,
  height,
  className,
}: {
  initialDefinition: ReleaseDagDefinition
  onChange: (definition: ReleaseDagDefinition) => void
  variant?: 'panel' | 'embedded'
  layout?: ReleaseFlowDagEditorLayout
  height?: number | string
  className?: string
}) {
  return (
    <ReactFlowProvider>
      <ReleaseFlowDagEditorInner
        className={className}
        height={height}
        initialDefinition={initialDefinition}
        layout={layout}
        onChange={onChange}
        variant={variant}
      />
    </ReactFlowProvider>
  )
}
