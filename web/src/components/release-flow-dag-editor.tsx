import { useCallback, useEffect, useMemo, useState } from 'react'
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
import '@xyflow/react/dist/style.css'
import { ManagementState } from '@/components/management-list'
import './release-flow-dag-editor.css'
import { useI18n } from '@/i18n'
import {
  createDagNode,
  createNodeConfig,
  getDefaultReleaseDagNodeLabel,
  type ReleaseDagDefinition,
  type ReleaseDagEdgeCondition,
  type ReleaseDagEdgeDefinition,
  type ReleaseDagNodeDefinition,
  type ReleaseDagNodeType,
} from '@/components/release-flow-dag-definition'

const { Text } = Typography

type DagTagColor = 'grey' | 'blue' | 'cyan' | 'green' | 'yellow' | 'purple' | 'pink' | 'red' | 'orange'
type FlowNode = Node<ReleaseDagNodeData, 'releaseStep'>
type FlowEdge = Edge<{ condition: ReleaseDagEdgeCondition }>

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

const DAG_NODE_OPTIONS: Array<{ value: ReleaseDagNodeType; label: string; color: DagTagColor }> = [
  { value: 'build', label: '构建', color: 'green' },
  { value: 'manual_approval', label: '审批', color: 'orange' },
  { value: 'deploy_update_image', label: '更新镜像', color: 'blue' },
  { value: 'restart_workload', label: '重启工作负载', color: 'red' },
  { value: 'scale_workload', label: '扩缩容', color: 'cyan' },
  { value: 'delete_pod', label: '删除 Pod', color: 'orange' },
  { value: 'evict_pod', label: '驱逐 Pod', color: 'orange' },
  { value: 'wait_rollout', label: '等待 Rollout', color: 'cyan' },
  { value: 'check_http', label: 'HTTP 检查', color: 'green' },
  { value: 'check_k8s_event', label: 'K8s 事件检查', color: 'yellow' },
  { value: 'smoke_test', label: 'Smoke Test', color: 'purple' },
  { value: 'http_callback', label: 'HTTP 回调', color: 'pink' },
  { value: 'create_silence', label: '创建静默', color: 'grey' },
  { value: 'notify', label: '通知', color: 'pink' },
  { value: 'rollback_to_previous', label: '失败回滚', color: 'red' },
]

const EDGE_CONDITION_OPTIONS = [
  { value: 'success', label: '成功' },
  { value: 'failure', label: '失败' },
  { value: 'always', label: '总是' },
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
      case 'smoke_test':
        return 'Smoke Test'
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
    timeoutSeconds: node.data.timeoutSeconds ?? 300,
    continueOnFailure: Boolean(node.data.continueOnFailure),
    config: node.data.config ?? {},
    position: node.position,
  }))
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
    case 'manual_approval':
      return (
        <>
          <Input
            value={Array.isArray(config.approverRoles) ? config.approverRoles.join(', ') : ''}
            placeholder="release-manager, ops-lead"
            onChange={(event) => patch('approverRoles', event.target.value.split(',').map((item) => item.trim()).filter(Boolean))}
          />
          <div className="soha-step-inline">
            <Text type="secondary" className="text-xs">{localeCode === 'zh_CN' ? '必须审批' : 'Required approval'}</Text>
            <Switch checked={config.required !== false} onChange={(checked) => patch('required', checked)} />
          </div>
        </>
      )
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

function ReleaseFlowDagEditorInner({
  initialDefinition,
  onChange,
}: {
  initialDefinition: ReleaseDagDefinition
  onChange: (definition: ReleaseDagDefinition) => void
}) {
  const { localeCode } = useI18n()
  const edgeConditionOptions = useMemo(() => getEdgeConditionOptions(localeCode), [localeCode])
  const [nodes, setNodes, onNodesChange] = useNodesState<FlowNode>(initialDefinition.nodes.map(toFlowNode))
  const [edges, setEdges, onEdgesChange] = useEdgesState<FlowEdge>(initialDefinition.edges.map(toFlowEdge))
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null)
  const [selectedEdgeId, setSelectedEdgeId] = useState<string | null>(null)
  const { fitView } = useReactFlow()

  useEffect(() => {
    onChange({
      schemaVersion: 2,
      mode: 'release_dag',
      nodes: serializeNodes(nodes),
      edges: serializeEdges(edges),
    })
  }, [edges, nodes, onChange])

  const selectedNode = useMemo(
    () => nodes.find((node) => node.id === selectedNodeId),
    [nodes, selectedNodeId],
  )
  const selectedEdge = useMemo(
    () => edges.find((edge) => edge.id === selectedEdgeId),
    [edges, selectedEdgeId],
  )

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

  return (
    <div className="soha-dag-editor-shell">
      <Card className="soha-dag-panel" title={localeCode === 'zh_CN' ? '节点面板' : 'Node Palette'}>
        <div className="soha-dag-palette">
          {DAG_NODE_OPTIONS.map((item) => (
            <Button key={item.value} onClick={() => addNode(item.value)}>
              {getDagNodeLabel(item.value, localeCode)}
            </Button>
          ))}
        </div>
        <Space>
          <Button type="primary" onClick={applyAutoLayout}>{localeCode === 'zh_CN' ? '自动布局' : 'Auto Layout'}</Button>
          <Button onClick={() => fitView({ padding: 0.2 })}>{localeCode === 'zh_CN' ? '适配视图' : 'Fit View'}</Button>
          <Button type="text" danger disabled={!selectedNodeId && !selectedEdgeId} onClick={removeSelected}>
            {localeCode === 'zh_CN' ? '删除选中' : 'Delete Selected'}
          </Button>
        </Space>
        <Text type="secondary" className="text-xs">
          {localeCode === 'zh_CN'
            ? '节点可直接在画布中拖动。连线默认表示 `success`，右侧属性面板可以改成 `failure` 或 `always`。'
            : 'Drag nodes directly on the canvas. Edges default to `success`, and the inspector can switch them to `failure` or `always`.'}
        </Text>
      </Card>

      <Card className="soha-dag-canvas-card" title={localeCode === 'zh_CN' ? 'DAG 画布' : 'DAG Canvas'}>
        <div className="soha-dag-canvas">
          <ReactFlow<FlowNode, FlowEdge>
            nodes={nodes}
            edges={edges}
            nodeTypes={{ releaseStep: ReleaseStepNode } as const}
            onNodesChange={onNodesChange}
            onEdgesChange={onEdgesChange}
            onConnect={onConnect}
            onSelectionChange={({ nodes: activeNodes, edges: activeEdges }) => {
              setSelectedNodeId(activeNodes[0]?.id ?? null)
              setSelectedEdgeId(activeEdges[0]?.id ?? null)
            }}
            fitView
          >
            <Background gap={20} size={1} />
            <Controls />
            <MiniMap />
          </ReactFlow>
        </div>
      </Card>

      <Card className="soha-dag-panel" title={localeCode === 'zh_CN' ? '属性面板' : 'Inspector'}>
        {selectedNode ? (
          <div className="soha-dag-inspector">
            <Text strong>{localeCode === 'zh_CN' ? '节点属性' : 'Node Properties'}</Text>
            <Input
              value={selectedNode.data.name}
              onChange={(event) => setNodes((current) => current.map((node) => node.id === selectedNode.id ? { ...node, data: { ...node.data, name: event.target.value } } : node))}
            />
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
          </div>
        ) : selectedEdge ? (
          <div className="soha-dag-inspector">
            <Text strong>{localeCode === 'zh_CN' ? '连线属性' : 'Edge Properties'}</Text>
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
          </div>
        ) : (
          <ManagementState
            bordered={false}
            compact
            kind="select-scope"
            title={localeCode === 'zh_CN' ? '请选择节点或连线' : 'Select a node or edge'}
            description={localeCode === 'zh_CN' ? '选择节点或连线后，可在这里编辑属性' : 'Select a node or edge to edit its properties here'}
          />
        )}
      </Card>
    </div>
  )
}

export function ReleaseFlowDagEditor({
  initialDefinition,
  onChange,
}: {
  initialDefinition: ReleaseDagDefinition
  onChange: (definition: ReleaseDagDefinition) => void
}) {
  return (
    <ReactFlowProvider>
      <ReleaseFlowDagEditorInner initialDefinition={initialDefinition} onChange={onChange} />
    </ReactFlowProvider>
  )
}
