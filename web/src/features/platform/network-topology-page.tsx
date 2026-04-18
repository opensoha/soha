import { useDeferredValue, useEffect, useMemo, useState } from 'react'
import {
  Background,
  Controls,
  Handle,
  MarkerType,
  Position,
  ReactFlow,
  ReactFlowProvider,
  useReactFlow,
  type Edge,
  type Node,
  type NodeProps,
} from '@xyflow/react'
import dagre from 'dagre'
import { Button, Card, Empty, Input, Space, Tag, Typography } from '@douyinfe/semi-ui'
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import type { ColumnProps } from '@douyinfe/semi-ui/lib/es/table'
import '@xyflow/react/dist/style.css'
import { AdminTable } from '@/components/admin-table'
import { PageHeader } from '@/components/page-header'
import { PlatformScopeToolbar } from '@/components/platform-scope-toolbar'
import { StatGrid } from '@/components/stat-grid'
import { useI18n } from '@/i18n'
import { buildClusterScopedPath } from '@/features/platform/platform-scope-query'
import { api } from '@/services/api-client'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import type { ApiResponse } from '@/types'

const { Text } = Typography

type SemiTagColor = 'grey' | 'blue' | 'cyan' | 'green' | 'yellow' | 'red' | 'orange'

interface ServiceView {
  name: string
  namespace: string
  selector?: Record<string, string>
}

interface IngressView {
  name: string
  namespace: string
  hosts?: string[]
  backendServices?: string[]
}

interface GatewayView {
  name: string
  namespace: string
  gatewayClass?: string
  addresses?: string[]
  listenerCount?: number
}

interface PodView {
  name: string
  namespace: string
  labels?: Record<string, string>
}

type TopologyNodeKind = 'entry' | 'gateway' | 'pending-route' | 'service' | 'missing-service' | 'pod'
type TopologyGraphNodeKind = TopologyNodeKind | 'backend-group' | 'empty-backend'
type TopologyDataState = 'live' | 'pending' | 'demo'
type LocaleCode = 'zh_CN' | 'en_US'

interface TopologyNode {
  id: string
  name: string
  kind: TopologyNodeKind
  state: TopologyDataState
  namespace?: string
  resourceName?: string
}

interface TopologyTrace {
  id: string
  entry: TopologyNode
  service?: TopologyNode
  terminals: TopologyNode[]
  sourceType: 'ingress' | 'gateway' | 'demo'
  state: TopologyDataState
  notes: string
}

interface GatewayCoverageItem {
  id: string
  name: string
  namespace: string
  addressSummary: string
  gatewayClass: string
  listenerCount: number
  source: 'live' | 'demo'
}

interface TableRow {
  id: string
  entry: TopologyNode
  service?: TopologyNode
  terminals: TopologyNode[]
  sourceType: 'ingress' | 'gateway' | 'demo'
  state: TopologyDataState
  notes: string
}

interface TopologyGraphNodeData extends Record<string, unknown> {
  name: string
  kind: TopologyGraphNodeKind
  state: TopologyDataState
  subtitle?: string
  badge?: string
  namespace?: string
  resourceName?: string
  serviceId?: string
  terminalNodes?: TopologyNode[]
}

interface TopologyGraphEdgeData extends Record<string, unknown> {
  sourceKind: TopologyGraphNodeKind
  state: TopologyDataState
}

interface TopologySelectionDetail {
  notes: string[]
  relatedEntries: TopologyNode[]
  relatedServices: TopologyNode[]
  serviceNode?: TopologyNode
  summary: string
  terminalNodes: TopologyNode[]
}

interface TopologyGraphData {
  edges: TopologyFlowEdge[]
  entryCount: number
  fitKey: string
  nodeMap: Map<string, TopologyGraphNodeData>
  nodes: TopologyFlowNode[]
  podCount: number
  serviceCount: number
}

type TopologyFlowNode = Node<TopologyGraphNodeData, 'topologyNode'>
type TopologyFlowEdge = Edge<TopologyGraphEdgeData>

const NODE_WIDTH = 252
const NODE_HEIGHT = 118

const NODE_COLORS: Record<TopologyGraphNodeKind, string> = {
  entry: '#3f67f6',
  gateway: '#f38a1d',
  'pending-route': '#d4a72c',
  service: '#1d92c5',
  'missing-service': '#d84c45',
  pod: '#22a36a',
  'backend-group': '#22a36a',
  'empty-backend': '#94a3b8',
}

const LEGEND_TAG_COLORS: Partial<Record<TopologyGraphNodeKind, SemiTagColor>> = {
  entry: 'blue',
  gateway: 'orange',
  'pending-route': 'yellow',
  service: 'cyan',
  'missing-service': 'red',
  'backend-group': 'green',
  'empty-backend': 'grey',
}

function hexToRgba(hex: string, alpha: number) {
  const normalized = hex.replace('#', '')
  const value = normalized.length === 3
    ? normalized.split('').map((part) => `${part}${part}`).join('')
    : normalized

  if (value.length !== 6) {
    return hex
  }

  const red = Number.parseInt(value.slice(0, 2), 16)
  const green = Number.parseInt(value.slice(2, 4), 16)
  const blue = Number.parseInt(value.slice(4, 6), 16)

  if ([red, green, blue].some((item) => Number.isNaN(item))) {
    return hex
  }

  return `rgba(${red}, ${green}, ${blue}, ${alpha})`
}

function mergeTopologyState(current: TopologyDataState, next: TopologyDataState) {
  if (current === next) {
    return current
  }
  if (current === 'live' || next === 'live') {
    return 'live'
  }
  if (current === 'pending' || next === 'pending') {
    return 'pending'
  }
  return 'demo'
}

function resolveNodeColor(kind: TopologyGraphNodeKind) {
  return NODE_COLORS[kind] ?? NODE_COLORS.entry
}

function getTopologyKindLabel(kind: TopologyGraphNodeKind, localeCode: LocaleCode) {
  if (localeCode === 'en_US') {
    switch (kind) {
      case 'entry':
        return 'Entry'
      case 'gateway':
        return 'Gateway'
      case 'pending-route':
        return 'Pending Route'
      case 'service':
        return 'Service'
      case 'missing-service':
        return 'Missing Service'
      case 'backend-group':
        return 'Backend Pods'
      case 'empty-backend':
        return 'No Backend'
      case 'pod':
        return 'Pod'
    }
  }

  switch (kind) {
    case 'entry':
      return '入口'
    case 'gateway':
      return '网关'
    case 'pending-route':
      return '待接路由'
    case 'service':
      return 'Service'
    case 'missing-service':
      return '缺失 Service'
    case 'backend-group':
      return '后端 Pods'
    case 'empty-backend':
      return '无后端'
    case 'pod':
      return 'Pod'
  }
}

function getTopologyStateLabel(state: TopologyDataState, localeCode: LocaleCode) {
  if (state === 'live') {
    return localeCode === 'zh_CN' ? '已验证' : 'Verified'
  }
  if (state === 'pending') {
    return localeCode === 'zh_CN' ? '待接入' : 'Pending'
  }
  return localeCode === 'zh_CN' ? '演示' : 'Preview'
}

function buildServiceDetailPath(name: string, selectedNamespace: string | null, rowNamespace: string) {
  const params = new URLSearchParams()
  const namespace = selectedNamespace && selectedNamespace !== '' ? selectedNamespace : rowNamespace
  if (namespace) {
    params.set('namespace', namespace)
  }
  const query = params.toString()
  return query ? `/network/services/${name}?${query}` : `/network/services/${name}`
}

function buildPodDetailPath(name: string, selectedNamespace: string | null, rowNamespace: string) {
  const params = new URLSearchParams()
  const namespace = selectedNamespace && selectedNamespace !== '' ? selectedNamespace : rowNamespace
  if (namespace) {
    params.set('namespace', namespace)
  }
  const query = params.toString()
  return query ? `/workloads/pods/${name}?${query}` : `/workloads/pods/${name}`
}

function selectorMatchesLabels(selector?: Record<string, string>, labels?: Record<string, string>) {
  const entries = Object.entries(selector ?? {})
  if (entries.length === 0) return false
  return entries.every(([key, value]) => (labels ?? {})[key] === value)
}

function uniqueTopologyNodes(items: Array<TopologyNode | undefined>) {
  const map = new Map<string, TopologyNode>()
  items.forEach((item) => {
    if (item) {
      map.set(item.id, item)
    }
  })
  return Array.from(map.values())
}

function uniqueStrings(items: string[]) {
  return Array.from(new Set(items.filter(Boolean)))
}

function formatBackendSubtitle(pods: TopologyNode[], localeCode: LocaleCode) {
  if (pods.length === 0) {
    return localeCode === 'zh_CN' ? 'Service 已解析，但 selector 暂未命中 Pod。' : 'Service resolved, but the selector does not match any pods yet.'
  }

  const previewNames = pods.slice(0, 2).map((pod) => pod.name)
  const remain = pods.length - previewNames.length

  if (remain > 0) {
    return localeCode === 'zh_CN'
      ? `${previewNames.join(' · ')} · +${remain} 个`
      : `${previewNames.join(' · ')} · +${remain}`
  }

  return previewNames.join(' · ')
}

function makeNode(
  id: string,
  name: string,
  kind: TopologyNodeKind,
  state: TopologyDataState,
  namespace?: string,
  resourceName?: string,
): TopologyNode {
  return {
    id,
    name,
    kind,
    state,
    namespace,
    resourceName,
  }
}

function buildIngressTraces(
  ingresses: IngressView[],
  services: ServiceView[],
  pods: PodView[],
  localeCode: LocaleCode,
) {
  const serviceMap = new Map(services.map((service) => [`${service.namespace}/${service.name}`, service]))
  const podsByNamespace = new Map<string, PodView[]>()

  pods.forEach((pod) => {
    const items = podsByNamespace.get(pod.namespace) ?? []
    items.push(pod)
    podsByNamespace.set(pod.namespace, items)
  })

  return ingresses.flatMap((ingress) => {
    const hosts = ingress.hosts && ingress.hosts.length > 0 ? ingress.hosts : [ingress.name]
    const backendServices = Array.from(new Set(ingress.backendServices?.filter(Boolean) ?? []))

    return hosts.flatMap((host) => {
      const entryNode = makeNode(
        `entry:${ingress.namespace}/${ingress.name}/${host}`,
        host,
        'entry',
        'live',
        ingress.namespace,
        ingress.name,
      )

      if (backendServices.length === 0) {
        return [{
          id: `${entryNode.id}:no-backend`,
          entry: entryNode,
          service: makeNode(
            `pending-service:${ingress.namespace}/${ingress.name}`,
            localeCode === 'zh_CN' ? '未声明后端 Service' : 'No backend service',
            'missing-service',
            'pending',
            ingress.namespace,
          ),
          terminals: [],
          sourceType: 'ingress' as const,
          state: 'pending' as const,
          notes: localeCode === 'zh_CN'
            ? 'Ingress 当前没有可解析的 backendServices 字段。'
            : 'The ingress does not expose a resolved backendServices list yet.',
        }]
      }

      return backendServices.map((serviceName) => {
        const serviceKey = `${ingress.namespace}/${serviceName}`
        const service = serviceMap.get(serviceKey)
        const serviceNode = service
          ? makeNode(`service:${serviceKey}`, service.name, 'service', 'live', service.namespace, service.name)
          : makeNode(`missing-service:${serviceKey}`, serviceName, 'missing-service', 'pending', ingress.namespace, serviceName)
        const matchedPods = service
          ? (podsByNamespace.get(service.namespace) ?? []).filter((pod) => selectorMatchesLabels(service.selector, pod.labels))
          : []
        const terminalPods = matchedPods.map((pod) =>
          makeNode(`pod:${pod.namespace}/${pod.name}`, pod.name, 'pod', 'live', pod.namespace, pod.name),
        )

        return {
          id: `${entryNode.id}:${serviceName}`,
          entry: entryNode,
          service: serviceNode,
          terminals: terminalPods,
          sourceType: 'ingress' as const,
          state: service ? 'live' as const : 'pending' as const,
          notes: service
            ? terminalPods.length > 0
              ? localeCode === 'zh_CN'
                ? `已解析 ${terminalPods.length} 个 Pod 后端。`
                : `${terminalPods.length} backend pods resolved.`
              : localeCode === 'zh_CN'
                ? 'Service 已解析，但当前没有匹配到 Pod。'
                : 'Service resolved, but no matching pods were found.'
            : localeCode === 'zh_CN'
              ? 'Ingress 指向的 Service 当前在 scope 内不可见。'
              : 'The service referenced by this ingress is not visible in the current scope.',
        }
      })
    })
  })
}

function buildGatewayCoverageItems(gateways: GatewayView[]): GatewayCoverageItem[] {
  return gateways.map((gateway) => ({
    id: `${gateway.namespace}/${gateway.name}`,
    name: gateway.name,
    namespace: gateway.namespace,
    addressSummary: gateway.addresses?.join(', ') || '-',
    gatewayClass: gateway.gatewayClass || '-',
    listenerCount: gateway.listenerCount ?? 0,
    source: 'live',
  }))
}

function buildDemoGatewayCoverageItems(): GatewayCoverageItem[] {
  return [{
    id: 'demo-gateway',
    name: 'edge-gateway',
    namespace: 'infra-system',
    addressSummary: 'gw.kubecrux.local',
    gatewayClass: 'kong',
    listenerCount: 3,
    source: 'demo',
  }]
}

function buildGatewayTraces(gateways: GatewayCoverageItem[], localeCode: LocaleCode): TopologyTrace[] {
  return gateways.map((gateway) => ({
    id: `gateway:${gateway.id}`,
    entry: makeNode(
      `gateway:${gateway.id}`,
      gateway.addressSummary !== '-' ? gateway.addressSummary : gateway.name,
      'gateway',
      gateway.source === 'live' ? 'pending' : 'demo',
      gateway.namespace,
      gateway.name,
    ),
    service: makeNode(
      `httproute:${gateway.id}`,
      localeCode === 'zh_CN' ? 'HTTPRoute 待接入' : 'HTTPRoute pending',
      'pending-route',
      'pending',
      gateway.namespace,
    ),
    terminals: [],
    sourceType: 'gateway',
    state: 'pending',
    notes: localeCode === 'zh_CN'
      ? '当前后端还没有 Gateway -> HTTPRoute -> Service 的聚合关系。'
      : 'Gateway -> HTTPRoute -> Service aggregation is not available in the backend yet.',
  }))
}

function buildDemoTraces(localeCode: LocaleCode): TopologyTrace[] {
  const frontendService = makeNode('demo-service:web', 'web-frontend', 'service', 'demo', 'demo-app', 'web-frontend')
  const apiService = makeNode('demo-service:api', 'billing-api', 'service', 'demo', 'demo-app', 'billing-api')

  return [
    {
      id: 'demo-ingress-web',
      entry: makeNode('demo-entry:web', 'app.kubecrux.local', 'entry', 'demo', 'demo-app', 'edge-web'),
      service: frontendService,
      terminals: [
        makeNode('demo-pod:web-0', 'web-frontend-85d7c7d7c6-v91k2', 'pod', 'demo', 'demo-app', 'web-frontend-85d7c7d7c6-v91k2'),
        makeNode('demo-pod:web-1', 'web-frontend-85d7c7d7c6-j8t6w', 'pod', 'demo', 'demo-app', 'web-frontend-85d7c7d7c6-j8t6w'),
      ],
      sourceType: 'demo',
      state: 'demo',
      notes: localeCode === 'zh_CN' ? '演示链路，用于预览入口到 Pod 的拓扑形态。' : 'Preview flow used when no live topology is available.',
    },
    {
      id: 'demo-ingress-api',
      entry: makeNode('demo-entry:api', 'api.kubecrux.local', 'entry', 'demo', 'demo-app', 'edge-api'),
      service: apiService,
      terminals: [
        makeNode('demo-pod:api-0', 'billing-api-6cb687f57d-k9nj2', 'pod', 'demo', 'demo-app', 'billing-api-6cb687f57d-k9nj2'),
        makeNode('demo-pod:api-1', 'billing-api-6cb687f57d-mr4qb', 'pod', 'demo', 'demo-app', 'billing-api-6cb687f57d-mr4qb'),
        makeNode('demo-pod:api-2', 'billing-api-6cb687f57d-x7lph', 'pod', 'demo', 'demo-app', 'billing-api-6cb687f57d-x7lph'),
      ],
      sourceType: 'demo',
      state: 'demo',
      notes: localeCode === 'zh_CN' ? '演示链路，用于展示一对多后端 Pod 扇出。' : 'Preview flow that shows fan-out from one service to multiple pods.',
    },
    {
      id: 'demo-gateway',
      entry: makeNode('demo-gateway:edge', 'gw.kubecrux.local', 'gateway', 'demo', 'infra-system', 'edge-gateway'),
      service: makeNode('demo-httproute:edge', localeCode === 'zh_CN' ? 'HTTPRoute 待接入' : 'HTTPRoute pending', 'pending-route', 'pending', 'infra-system'),
      terminals: [],
      sourceType: 'gateway',
      state: 'pending',
      notes: localeCode === 'zh_CN'
        ? '演示 Gateway 通道，真实 HTTPRoute 聚合待后端补齐。'
        : 'Preview gateway lane. Real HTTPRoute aggregation is pending in the backend.',
    },
  ]
}

function filterTraces(traces: TopologyTrace[], keyword: string) {
  if (!keyword) {
    return traces
  }

  return traces.filter((trace) => {
    const fields = [
      trace.entry.name,
      trace.service?.name ?? '',
      trace.notes,
      ...trace.terminals.map((item) => item.name),
    ]
    return fields.some((field) => field.toLowerCase().includes(keyword))
  })
}

function layoutTopologyGraph(nodes: TopologyFlowNode[], edges: TopologyFlowEdge[]) {
  const graph = new dagre.graphlib.Graph()
  graph.setDefaultEdgeLabel(() => ({}))
  graph.setGraph({ rankdir: 'LR', ranksep: 120, nodesep: 42 })

  nodes.forEach((node) => {
    graph.setNode(node.id, { width: NODE_WIDTH, height: NODE_HEIGHT })
  })

  edges.forEach((edge) => {
    graph.setEdge(edge.source, edge.target)
  })

  dagre.layout(graph)

  return nodes.map((node) => {
    const position = graph.node(node.id) ?? { x: NODE_WIDTH / 2, y: NODE_HEIGHT / 2 }
    return {
      ...node,
      position: {
        x: position.x - NODE_WIDTH / 2,
        y: position.y - NODE_HEIGHT / 2,
      },
      sourcePosition: Position.Right,
      targetPosition: Position.Left,
    }
  })
}

function buildTopologyGraph(traces: TopologyTrace[], localeCode: LocaleCode): TopologyGraphData {
  const nodeMap = new Map<string, TopologyGraphNodeData>()
  const edgeMap = new Map<string, TopologyFlowEdge>()
  const serviceMap = new Map<string, TopologyNode>()
  const backendPodsByService = new Map<string, Map<string, TopologyNode>>()
  const emptyBackendNotes = new Map<string, string[]>()

  const addNode = (id: string, data: TopologyGraphNodeData) => {
    const current = nodeMap.get(id)
    if (!current) {
      nodeMap.set(id, data)
      return
    }

    nodeMap.set(id, {
      ...current,
      ...data,
      state: mergeTopologyState(current.state, data.state),
      subtitle: current.subtitle || data.subtitle,
      badge: current.badge || data.badge,
      terminalNodes: uniqueTopologyNodes([...(current.terminalNodes ?? []), ...(data.terminalNodes ?? [])]),
    })
  }

  const addEdge = (source: string, target: string, sourceKind: TopologyGraphNodeKind, state: TopologyDataState) => {
    const key = `${source}->${target}`
    const color = resolveNodeColor(sourceKind)
    const current = edgeMap.get(key)
    const nextState = current ? mergeTopologyState(current.data?.state ?? 'demo', state) : state

    edgeMap.set(key, {
      id: key,
      source,
      target,
      type: 'smoothstep',
      markerEnd: {
        type: MarkerType.ArrowClosed,
        color,
      },
      style: {
        stroke: color,
        strokeWidth: nextState === 'live' ? 1.9 : 1.5,
        strokeDasharray: nextState === 'pending' ? '8 6' : undefined,
        opacity: nextState === 'demo' ? 0.72 : 0.96,
      },
      data: {
        sourceKind,
        state: nextState,
      },
    })
  }

  traces.forEach((trace) => {
    addNode(trace.entry.id, {
      name: trace.entry.name,
      kind: trace.entry.kind,
      state: trace.entry.state,
      subtitle: trace.entry.namespace || '-',
      badge: trace.sourceType === 'gateway'
        ? (localeCode === 'zh_CN' ? 'Gateway 入口' : 'Gateway entry')
        : trace.sourceType === 'demo'
          ? (localeCode === 'zh_CN' ? '演示入口' : 'Preview entry')
          : 'Ingress',
      namespace: trace.entry.namespace,
      resourceName: trace.entry.resourceName,
    })

    if (!trace.service) {
      return
    }

    addNode(trace.service.id, {
      name: trace.service.name,
      kind: trace.service.kind,
      state: trace.service.state,
      subtitle: trace.service.namespace || '-',
      badge: trace.service.kind === 'pending-route'
        ? (localeCode === 'zh_CN' ? '路由占位' : 'Route placeholder')
        : trace.service.kind === 'missing-service'
          ? (localeCode === 'zh_CN' ? '当前 scope 不可见' : 'Out of current scope')
          : 'Service',
      namespace: trace.service.namespace,
      resourceName: trace.service.resourceName,
    })

    addEdge(trace.entry.id, trace.service.id, trace.entry.kind, trace.state)

    if (trace.service.kind !== 'service') {
      return
    }

    serviceMap.set(trace.service.id, trace.service)

    if (trace.terminals.length > 0) {
      const pods = backendPodsByService.get(trace.service.id) ?? new Map<string, TopologyNode>()
      trace.terminals.forEach((terminal) => {
        pods.set(terminal.id, terminal)
      })
      backendPodsByService.set(trace.service.id, pods)
      return
    }

    const notes = emptyBackendNotes.get(trace.service.id) ?? []
    notes.push(trace.notes)
    emptyBackendNotes.set(trace.service.id, notes)
  })

  serviceMap.forEach((serviceNode, serviceId) => {
    const pods = Array.from(backendPodsByService.get(serviceId)?.values() ?? [])

    if (pods.length > 0) {
      const backendId = `backend:${serviceId}`
      addNode(backendId, {
        name: localeCode === 'zh_CN' ? `${pods.length} 个 Backend Pods` : `${pods.length} backend pods`,
        kind: 'backend-group',
        state: 'live',
        subtitle: formatBackendSubtitle(pods, localeCode),
        badge: localeCode === 'zh_CN' ? '后端集合' : 'Backend set',
        namespace: serviceNode.namespace,
        resourceName: serviceNode.resourceName,
        serviceId,
        terminalNodes: pods,
      })
      addEdge(serviceId, backendId, 'service', 'live')
      return
    }

    const backendId = `backend-empty:${serviceId}`
    addNode(backendId, {
      name: localeCode === 'zh_CN' ? '未匹配到后端 Pod' : 'No matching backend pods',
      kind: 'empty-backend',
      state: 'pending',
      subtitle: localeCode === 'zh_CN'
        ? 'Service 已解析，但 selector 暂未命中 Pod。'
        : 'Service resolved, but the selector does not match any pods yet.',
      badge: localeCode === 'zh_CN' ? '选择器未命中' : 'Selector not matched',
      namespace: serviceNode.namespace,
      resourceName: serviceNode.resourceName,
      serviceId,
      terminalNodes: [],
    })
    addEdge(serviceId, backendId, 'service', 'pending')
  })

  const flowNodes = Array.from(nodeMap.entries()).map(([id, data]) => ({
    id,
    type: 'topologyNode' as const,
    position: { x: 0, y: 0 },
    data,
  }))

  const flowEdges = Array.from(edgeMap.values())
  const nodes = layoutTopologyGraph(flowNodes, flowEdges)
  const podIds = new Set<string>()

  backendPodsByService.forEach((pods) => {
    pods.forEach((pod) => {
      podIds.add(pod.id)
    })
  })

  return {
    nodes,
    edges: flowEdges,
    nodeMap,
    fitKey: `${nodes.map((node) => node.id).join(',')}::${flowEdges.map((edge) => edge.id).join(',')}`,
    entryCount: Array.from(nodeMap.values()).filter((node) => node.kind === 'entry' || node.kind === 'gateway').length,
    serviceCount: Array.from(nodeMap.values()).filter((node) => ['service', 'missing-service', 'pending-route'].includes(node.kind)).length,
    podCount: podIds.size,
  }
}

function getErrorMessage(error: unknown) {
  return error instanceof Error ? error.message : ''
}

function renderTraceState(state: TopologyDataState, localeCode: LocaleCode) {
  if (state === 'live') {
    return <Tag color="green">{localeCode === 'zh_CN' ? '已验证' : 'Verified'}</Tag>
  }
  if (state === 'pending') {
    return <Tag color="orange">{localeCode === 'zh_CN' ? '待接入' : 'Pending'}</Tag>
  }
  return <Tag color="blue">{localeCode === 'zh_CN' ? '演示' : 'Preview'}</Tag>
}

function renderSourceType(sourceType: TopologyTrace['sourceType'], localeCode: LocaleCode) {
  if (sourceType === 'gateway') {
    return <Tag color="orange">Gateway</Tag>
  }
  if (sourceType === 'ingress') {
    return <Tag color="blue">Ingress</Tag>
  }
  return <Tag color="blue">{localeCode === 'zh_CN' ? '演示' : 'Demo'}</Tag>
}

function buildSelectionDetail(
  nodeId: string | null,
  nodeData: TopologyGraphNodeData | null,
  traces: TopologyTrace[],
  localeCode: LocaleCode,
): TopologySelectionDetail | null {
  if (!nodeId || !nodeData) {
    return null
  }

  switch (nodeData.kind) {
    case 'entry':
    case 'gateway': {
      const relatedTraces = traces.filter((trace) => trace.entry.id === nodeId)
      const relatedServices = uniqueTopologyNodes(relatedTraces.map((trace) => trace.service))
      const terminalNodes = uniqueTopologyNodes(relatedTraces.flatMap((trace) => trace.terminals))
      return {
        relatedEntries: [],
        relatedServices,
        serviceNode: undefined,
        terminalNodes,
        notes: uniqueStrings(relatedTraces.map((trace) => trace.notes)),
        summary: localeCode === 'zh_CN'
          ? `当前节点向下游连接 ${relatedServices.length} 个 Service / Route，并覆盖 ${terminalNodes.length} 个后端 Pod。`
          : `This node connects to ${relatedServices.length} downstream services or routes and covers ${terminalNodes.length} backend pods.`,
      }
    }
    case 'service':
    case 'missing-service':
    case 'pending-route': {
      const relatedTraces = traces.filter((trace) => trace.service?.id === nodeId)
      const relatedEntries = uniqueTopologyNodes(relatedTraces.map((trace) => trace.entry))
      const terminalNodes = uniqueTopologyNodes(relatedTraces.flatMap((trace) => trace.terminals))
      return {
        relatedEntries,
        relatedServices: [],
        serviceNode: relatedTraces[0]?.service,
        terminalNodes,
        notes: uniqueStrings(relatedTraces.map((trace) => trace.notes)),
        summary: nodeData.kind === 'service'
          ? localeCode === 'zh_CN'
            ? `该 Service 当前承接 ${relatedEntries.length} 个上游入口，并汇总 ${terminalNodes.length} 个后端 Pod。`
            : `This service receives ${relatedEntries.length} upstream entries and aggregates ${terminalNodes.length} backend pods.`
          : nodeData.kind === 'missing-service'
            ? localeCode === 'zh_CN'
              ? `有 ${relatedEntries.length} 个入口指向当前缺失的 Service，用于提示 scope 内不可见或配置缺失。`
              : `${relatedEntries.length} entries currently point to this missing service, which highlights an out-of-scope or missing backend.`
            : localeCode === 'zh_CN'
              ? '当前只展示 Gateway 到 HTTPRoute 的占位关系，等待后端聚合继续拉通到 Service。'
              : 'The graph currently stops at the pending HTTPRoute placeholder until backend aggregation reaches Service.',
      }
    }
    case 'backend-group':
    case 'empty-backend': {
      const serviceId = nodeData.serviceId
      const relatedTraces = traces.filter((trace) => trace.service?.id === serviceId)
      const relatedEntries = uniqueTopologyNodes(relatedTraces.map((trace) => trace.entry))
      const terminalNodes = uniqueTopologyNodes(nodeData.terminalNodes ?? [])
      return {
        relatedEntries,
        relatedServices: [],
        serviceNode: relatedTraces[0]?.service,
        terminalNodes,
        notes: uniqueStrings(relatedTraces.map((trace) => trace.notes)),
        summary: nodeData.kind === 'backend-group'
          ? localeCode === 'zh_CN'
            ? `这是聚合后的后端集合节点，归属于一个 Service，并默认收起为 ${terminalNodes.length} 个 Pod。`
            : `This aggregated backend node belongs to one service and is collapsed into ${terminalNodes.length} pods by default.`
          : localeCode === 'zh_CN'
            ? 'Service 已解析，但 selector 还没有匹配到后端 Pod，因此总览图先停在这里。'
            : 'The service is resolved, but the selector does not match backend pods yet, so the topology stops here.',
      }
    }
    case 'pod':
    default:
      return null
  }
}

function TopologyCanvasNode({ data, selected }: NodeProps<TopologyFlowNode>) {
  const { localeCode } = useI18n()
  const accent = resolveNodeColor(data.kind)

  return (
    <div className={`kc-topology-node ${selected ? 'is-selected' : ''} is-${data.state}`}>
      <Handle
        type="target"
        position={Position.Left}
        isConnectable={false}
        style={{ opacity: 0, background: 'transparent', border: 0 }}
      />
      <div
        className="kc-topology-node-card"
        style={{
          borderColor: hexToRgba(accent, selected ? 0.9 : 0.28),
          background: `linear-gradient(180deg, ${hexToRgba(accent, 0.14)} 0%, rgba(255, 255, 255, 0.96) 100%)`,
        }}
      >
        <div className="kc-topology-node-head">
          <span className="kc-topology-node-kind" style={{ color: accent, background: hexToRgba(accent, 0.12) }}>
            {getTopologyKindLabel(data.kind, localeCode)}
          </span>
          <span className={`kc-topology-node-state is-${data.state}`}>
            {getTopologyStateLabel(data.state, localeCode)}
          </span>
        </div>
        <div className="kc-topology-node-title">{data.name}</div>
        {data.subtitle ? <div className="kc-topology-node-subtitle">{data.subtitle}</div> : null}
        {data.badge ? <div className="kc-topology-node-badge">{data.badge}</div> : null}
      </div>
      <Handle
        type="source"
        position={Position.Right}
        isConnectable={false}
        style={{ opacity: 0, background: 'transparent', border: 0 }}
      />
    </div>
  )
}

const TOPOLOGY_NODE_TYPES = {
  topologyNode: TopologyCanvasNode,
} as const

function TopologyCanvasInner({
  edges,
  fitKey,
  nodes,
  onSelectNode,
}: {
  edges: TopologyFlowEdge[]
  fitKey: string
  nodes: TopologyFlowNode[]
  onSelectNode: (nodeId: string | null) => void
}) {
  const { fitView } = useReactFlow()

  useEffect(() => {
    if (nodes.length === 0) {
      return
    }

    const frame = requestAnimationFrame(() => {
      fitView({ padding: 0.16, duration: 240 })
    })

    return () => cancelAnimationFrame(frame)
  }, [fitKey, fitView, nodes.length])

  return (
    <div className="kc-topology-canvas">
      <ReactFlow<TopologyFlowNode, TopologyFlowEdge>
        nodes={nodes}
        edges={edges}
        nodeTypes={TOPOLOGY_NODE_TYPES}
        fitView
        elementsSelectable
        nodesDraggable={false}
        nodesConnectable={false}
        edgesFocusable={false}
        proOptions={{ hideAttribution: true }}
        onPaneClick={() => onSelectNode(null)}
        onNodeClick={(_, node) => onSelectNode(node.id)}
      >
        <Background gap={20} size={1} />
        <Controls showInteractive={false} />
      </ReactFlow>
    </div>
  )
}

function TopologyCanvas({
  edges,
  fitKey,
  nodes,
  onSelectNode,
}: {
  edges: TopologyFlowEdge[]
  fitKey: string
  nodes: TopologyFlowNode[]
  onSelectNode: (nodeId: string | null) => void
}) {
  return (
    <ReactFlowProvider>
      <TopologyCanvasInner edges={edges} fitKey={fitKey} nodes={nodes} onSelectNode={onSelectNode} />
    </ReactFlowProvider>
  )
}

export function NetworkTopologyPage() {
  const { localeCode } = useI18n()
  const navigate = useNavigate()
  const { clusterId, namespace } = usePlatformScopeStore()
  const [searchKeyword, setSearchKeyword] = useState('')
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null)
  const deferredSearchKeyword = useDeferredValue(searchKeyword.trim().toLowerCase())

  const servicesQuery = useQuery({
    queryKey: ['network-topology-services', clusterId, namespace],
    queryFn: () => api.get<ApiResponse<ServiceView[]>>(buildClusterScopedPath(clusterId!, 'network/services', namespace)),
    enabled: !!clusterId,
  })

  const ingressesQuery = useQuery({
    queryKey: ['network-topology-ingresses', clusterId, namespace],
    queryFn: () => api.get<ApiResponse<IngressView[]>>(buildClusterScopedPath(clusterId!, 'network/ingresses', namespace)),
    enabled: !!clusterId,
  })

  const gatewaysQuery = useQuery({
    queryKey: ['network-topology-gateways', clusterId, namespace],
    queryFn: () => api.get<ApiResponse<GatewayView[]>>(buildClusterScopedPath(clusterId!, 'network/gateways', namespace)),
    enabled: !!clusterId,
  })

  const podsQuery = useQuery({
    queryKey: ['network-topology-pods', clusterId, namespace],
    queryFn: () => api.get<ApiResponse<PodView[]>>(buildClusterScopedPath(clusterId!, 'workloads/pods', namespace)),
    enabled: !!clusterId,
  })

  const liveTraces = useMemo(
    () => buildIngressTraces(
      ingressesQuery.data?.data ?? [],
      servicesQuery.data?.data ?? [],
      podsQuery.data?.data ?? [],
      localeCode,
    ),
    [ingressesQuery.data, localeCode, podsQuery.data, servicesQuery.data],
  )

  const liveGatewayCoverageItems = useMemo(
    () => buildGatewayCoverageItems(gatewaysQuery.data?.data ?? []),
    [gatewaysQuery.data],
  )

  const hasLiveTopology = liveTraces.length > 0
  const hasLiveGatewayCoverage = liveGatewayCoverageItems.length > 0
  const chartMode = hasLiveTopology || hasLiveGatewayCoverage ? 'live' : 'demo'
  const gatewayCoverageItems = useMemo(
    () => liveGatewayCoverageItems.length > 0
      ? liveGatewayCoverageItems
      : chartMode === 'live'
        ? []
        : buildDemoGatewayCoverageItems(),
    [chartMode, liveGatewayCoverageItems],
  )
  const chartTraces = useMemo(
    () => chartMode === 'live'
      ? [...liveTraces, ...buildGatewayTraces(liveGatewayCoverageItems, localeCode)]
      : buildDemoTraces(localeCode),
    [chartMode, liveGatewayCoverageItems, liveTraces, localeCode],
  )

  const filteredTraces = useMemo(
    () => filterTraces(chartTraces, deferredSearchKeyword),
    [chartTraces, deferredSearchKeyword],
  )

  const topologyGraph = useMemo(
    () => buildTopologyGraph(filteredTraces, localeCode),
    [filteredTraces, localeCode],
  )

  const flowNodes = useMemo(
    () => topologyGraph.nodes.map((node) => ({
      ...node,
      selected: node.id === selectedNodeId,
    })),
    [selectedNodeId, topologyGraph.nodes],
  )

  const tableRows = useMemo<TableRow[]>(
    () => filteredTraces.map((trace) => ({
      id: trace.id,
      entry: trace.entry,
      service: trace.service,
      terminals: trace.terminals,
      sourceType: trace.sourceType,
      state: trace.state,
      notes: trace.notes,
    })),
    [filteredTraces],
  )

  const hasLivePendingGateway = liveGatewayCoverageItems.length > 0
  const liveErrors = [
    getErrorMessage(servicesQuery.error),
    getErrorMessage(ingressesQuery.error),
    getErrorMessage(podsQuery.error),
    getErrorMessage(gatewaysQuery.error),
  ].filter(Boolean)
  const liveLoading = clusterId && (servicesQuery.isLoading || ingressesQuery.isLoading || podsQuery.isLoading || gatewaysQuery.isLoading)

  const viewTag = chartMode === 'live'
    ? hasLiveTopology
      ? hasLivePendingGateway
        ? (localeCode === 'zh_CN' ? '混合视图' : 'Hybrid view')
        : (localeCode === 'zh_CN' ? '实时链路' : 'Live flow')
      : (localeCode === 'zh_CN' ? 'Gateway 视图' : 'Gateway view')
    : (localeCode === 'zh_CN' ? '演示视图' : 'Preview flow')

  const viewDescription = chartMode === 'live'
    ? hasLiveTopology
      ? hasLivePendingGateway
        ? (localeCode === 'zh_CN'
          ? '主图改为分层拓扑：Ingress -> Service -> Backend Pods 使用实时聚合，Gateway / HTTPRoute 继续以待接占位展示。'
          : 'The main graph now uses a layered topology: Ingress -> Service -> Backend Pods is live, while Gateway / HTTPRoute stays as a pending placeholder.')
        : (localeCode === 'zh_CN'
          ? '当前 scope 内已经解析出实时入口链路，并按分层拓扑收敛后端节点。'
          : 'Live entry paths are available in the current scope and rendered as a layered topology with collapsed backends.')
      : (localeCode === 'zh_CN'
        ? '当前 scope 先只发现 Gateway 覆盖信息，HTTPRoute -> Service 聚合仍待后端补齐。'
        : 'The current scope only exposes Gateway coverage for now, while HTTPRoute -> Service aggregation is still pending in the backend.')
    : clusterId
      ? liveLoading
        ? (localeCode === 'zh_CN'
          ? '正在加载实时链路，当前先展示演示拓扑。'
          : 'Live topology is still loading, so the preview graph is shown first.')
        : (localeCode === 'zh_CN'
          ? '当前 scope 下没有可展示的实时入口链路，先切换到演示拓扑看布局和交互。'
          : 'No live entry path is available in the current scope, so the preview graph is shown.')
      : (localeCode === 'zh_CN'
        ? '还没有选定集群，先展示一版演示拓扑。'
        : 'No cluster is selected yet, so the preview graph is shown.')

  useEffect(() => {
    if (selectedNodeId && !topologyGraph.nodeMap.has(selectedNodeId)) {
      setSelectedNodeId(null)
    }
  }, [selectedNodeId, topologyGraph.nodeMap])

  const selectedTopologyNode = selectedNodeId ? topologyGraph.nodeMap.get(selectedNodeId) ?? null : null
  const selectionDetail = useMemo(
    () => buildSelectionDetail(selectedNodeId, selectedTopologyNode, filteredTraces, localeCode),
    [filteredTraces, localeCode, selectedNodeId, selectedTopologyNode],
  )

  const columns: ColumnProps<TableRow>[] = [
    {
      title: localeCode === 'zh_CN' ? '入口' : 'Entry',
      dataIndex: 'entry',
      render: (_: TopologyNode, record: TableRow) => (
        <div className="flex flex-col gap-1">
          <Text strong>{record.entry.name}</Text>
          <Text type="tertiary" size="small">{record.entry.namespace || '-'}</Text>
        </div>
      ),
    },
    {
      title: localeCode === 'zh_CN' ? '类型' : 'Type',
      dataIndex: 'sourceType',
      width: 110,
      render: (_: TableRow['sourceType'], record: TableRow) => renderSourceType(record.sourceType, localeCode),
    },
    {
      title: 'Service / Route',
      dataIndex: 'service',
      render: (_: TopologyNode | undefined, record: TableRow) => {
        if (!record.service) {
          return '-'
        }

        const canNavigate = record.service.kind === 'service' && record.service.resourceName && record.service.namespace
        if (!canNavigate) {
          return <Text>{record.service.name}</Text>
        }

        return (
          <Button
            theme="borderless"
            type="primary"
            onClick={() => navigate(buildServiceDetailPath(record.service!.resourceName!, namespace, record.service!.namespace!))}
          >
            {record.service.name}
          </Button>
        )
      },
    },
    {
      title: localeCode === 'zh_CN' ? 'Pods / 后续节点' : 'Pods / Next hops',
      dataIndex: 'terminals',
      render: (_: TopologyNode[], record: TableRow) => {
        if (record.terminals.length === 0) {
          return <Text type="tertiary">-</Text>
        }

        const visibleItems = record.terminals.slice(0, 3)
        const remainCount = record.terminals.length - visibleItems.length

        return (
          <Space wrap>
            {visibleItems.map((item) => item.kind === 'pod' && item.resourceName && item.namespace ? (
              <Button
                key={item.id}
                theme="light"
                type="tertiary"
                size="small"
                onClick={() => navigate(buildPodDetailPath(item.resourceName!, namespace, item.namespace!))}
              >
                {item.name}
              </Button>
            ) : (
              <Tag key={item.id}>{item.name}</Tag>
            ))}
            {remainCount > 0 ? <Tag color="grey">+{remainCount}</Tag> : null}
          </Space>
        )
      },
    },
    {
      title: localeCode === 'zh_CN' ? '状态' : 'State',
      dataIndex: 'state',
      width: 110,
      render: (_: TopologyDataState, record: TableRow) => renderTraceState(record.state, localeCode),
    },
    {
      title: localeCode === 'zh_CN' ? '说明' : 'Notes',
      dataIndex: 'notes',
      render: (value: string) => <Text type="tertiary">{value}</Text>,
    },
  ]

  const selectedNodeServicePath = selectedTopologyNode?.resourceName && selectedTopologyNode.namespace
    && ['service', 'backend-group', 'empty-backend'].includes(selectedTopologyNode.kind)
    ? buildServiceDetailPath(selectedTopologyNode.resourceName, namespace, selectedTopologyNode.namespace)
    : null

  return (
    <div className="kc-page">
      <PageHeader
        title={localeCode === 'zh_CN' ? '网络链路' : 'Network Topology'}
        description={localeCode === 'zh_CN'
          ? '把入口、路由、Service 与后端聚合进一张分层拓扑图里，总览保持清晰，Pod 明细留给下方表格和节点详情继续钻取。'
          : 'Aggregate entries, routes, services, and backends into one layered topology so the overview stays readable while pod-level detail remains available below.'}
        actions={(
          <Space wrap>
            <Tag color={chartMode === 'live' ? 'green' : 'blue'}>{viewTag}</Tag>
            {liveErrors.length > 0 ? <Tag color="red">{localeCode === 'zh_CN' ? '实时数据部分失败' : 'Partial live data failure'}</Tag> : null}
          </Space>
        )}
      />
      <PlatformScopeToolbar />

      <Card className="kc-detail-card">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex min-w-[280px] flex-1 flex-wrap items-center gap-3">
            <Input
              value={searchKeyword}
              onChange={setSearchKeyword}
              placeholder={localeCode === 'zh_CN' ? '搜索域名 / Service / Pod / 说明' : 'Search host / service / pod / note'}
              style={{ width: 320 }}
              className="kc-platform-compact-field"
            />
            <Text type="tertiary" size="small">
              {viewDescription}
            </Text>
          </div>
          <Space wrap>
            <Tag color={LEGEND_TAG_COLORS.entry}>Ingress</Tag>
            <Tag color={LEGEND_TAG_COLORS.gateway}>Gateway</Tag>
            <Tag color={LEGEND_TAG_COLORS['pending-route']}>{localeCode === 'zh_CN' ? '待接 HTTPRoute' : 'HTTPRoute pending'}</Tag>
            <Tag color={LEGEND_TAG_COLORS.service}>Service</Tag>
            <Tag color={LEGEND_TAG_COLORS['backend-group']}>{localeCode === 'zh_CN' ? 'Backend Pods' : 'Backend Pods'}</Tag>
            <Tag color={LEGEND_TAG_COLORS['empty-backend']}>{localeCode === 'zh_CN' ? '无匹配 Pod' : 'No matching pods'}</Tag>
            <Tag color={LEGEND_TAG_COLORS['missing-service']}>{localeCode === 'zh_CN' ? '缺失 Service' : 'Missing service'}</Tag>
          </Space>
        </div>
        {liveErrors.length > 0 ? (
          <div style={{ marginTop: 12 }}>
            <Text style={{ color: 'var(--semi-color-danger)' }}>
              {localeCode === 'zh_CN' ? '实时数据错误：' : 'Live data error: '}
              {liveErrors.join(' / ')}
            </Text>
          </div>
        ) : null}
      </Card>

      <StatGrid
        items={[
          { label: localeCode === 'zh_CN' ? '入口节点' : 'Entry nodes', value: topologyGraph.entryCount },
          { label: localeCode === 'zh_CN' ? 'Service / Route' : 'Service / Route', value: topologyGraph.serviceCount },
          { label: localeCode === 'zh_CN' ? '后端 Pods' : 'Backend pods', value: topologyGraph.podCount },
          { label: localeCode === 'zh_CN' ? '待接入 Gateway' : 'Pending gateways', value: gatewayCoverageItems.length },
        ]}
      />

      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.65fr)_minmax(280px,360px)]">
        <Card
          className="kc-detail-card"
          title={localeCode === 'zh_CN' ? '入口 -> 路由 -> Service -> Backend 拓扑' : 'Entry -> Route -> Service -> Backend topology'}
          headerExtraContent={(
            <Space wrap>
              <Text type="tertiary" size="small">
                {localeCode === 'zh_CN'
                  ? `${filteredTraces.length} 条链路`
                  : `${filteredTraces.length} traces`}
              </Text>
              {selectedTopologyNode ? (
                <Tag color={LEGEND_TAG_COLORS[selectedTopologyNode.kind] ?? 'blue'}>
                  {selectedTopologyNode.name}
                </Tag>
              ) : null}
            </Space>
          )}
          bodyStyle={{ padding: 12 }}
        >
          {flowNodes.length > 0 ? (
            <>
              <TopologyCanvas
                nodes={flowNodes}
                edges={topologyGraph.edges}
                fitKey={topologyGraph.fitKey}
                onSelectNode={setSelectedNodeId}
              />
              <div className="kc-topology-selection">
                {selectedTopologyNode && selectionDetail ? (
                  <>
                    <div className="flex flex-wrap items-start justify-between gap-3">
                      <div className="flex min-w-[240px] flex-1 flex-col gap-1">
                        <Text strong>{selectedTopologyNode.name}</Text>
                        <Text type="tertiary" size="small">{selectedTopologyNode.subtitle || '-'}</Text>
                      </div>
                      <Space wrap>
                        <Tag color={LEGEND_TAG_COLORS[selectedTopologyNode.kind] ?? 'blue'}>
                          {getTopologyKindLabel(selectedTopologyNode.kind, localeCode)}
                        </Tag>
                        {renderTraceState(selectedTopologyNode.state, localeCode)}
                      </Space>
                    </div>

                    <Text type="tertiary">{selectionDetail.summary}</Text>

                    {selectedNodeServicePath ? (
                      <Space wrap>
                        <Button
                          theme="light"
                          type="primary"
                          size="small"
                          onClick={() => navigate(selectedNodeServicePath)}
                        >
                          {localeCode === 'zh_CN' ? '查看 Service 详情' : 'Open Service detail'}
                        </Button>
                      </Space>
                    ) : null}

                    {selectionDetail.relatedEntries.length > 0 ? (
                      <div className="flex flex-col gap-2">
                        <Text strong size="small">{localeCode === 'zh_CN' ? '上游入口' : 'Upstream entries'}</Text>
                        <Space wrap>
                          {selectionDetail.relatedEntries.map((item) => (
                            <Tag key={item.id}>{item.name}</Tag>
                          ))}
                        </Space>
                      </div>
                    ) : null}

                    {selectionDetail.relatedServices.length > 0 ? (
                      <div className="flex flex-col gap-2">
                        <Text strong size="small">{localeCode === 'zh_CN' ? '下游 Service / Route' : 'Downstream services / routes'}</Text>
                        <Space wrap>
                          {selectionDetail.relatedServices.map((item) => item.kind === 'service' && item.resourceName && item.namespace ? (
                            <Button
                              key={item.id}
                              theme="light"
                              type="tertiary"
                              size="small"
                              onClick={() => navigate(buildServiceDetailPath(item.resourceName!, namespace, item.namespace!))}
                            >
                              {item.name}
                            </Button>
                          ) : (
                            <Tag key={item.id}>{item.name}</Tag>
                          ))}
                        </Space>
                      </div>
                    ) : null}

                    {selectionDetail.terminalNodes.length > 0 ? (
                      <div className="flex flex-col gap-2">
                        <Text strong size="small">{localeCode === 'zh_CN' ? '后端 Pods' : 'Backend pods'}</Text>
                        <Space wrap>
                          {selectionDetail.terminalNodes.slice(0, 6).map((item) => item.resourceName && item.namespace ? (
                            <Button
                              key={item.id}
                              theme="light"
                              type="tertiary"
                              size="small"
                              onClick={() => navigate(buildPodDetailPath(item.resourceName!, namespace, item.namespace!))}
                            >
                              {item.name}
                            </Button>
                          ) : (
                            <Tag key={item.id}>{item.name}</Tag>
                          ))}
                          {selectionDetail.terminalNodes.length > 6 ? (
                            <Tag color="grey">{`+${selectionDetail.terminalNodes.length - 6}`}</Tag>
                          ) : null}
                        </Space>
                      </div>
                    ) : null}

                    {selectionDetail.notes.length > 0 ? (
                      <div className="kc-topology-note-list">
                        {selectionDetail.notes.slice(0, 3).map((item) => (
                          <Text key={item} type="tertiary" size="small">{item}</Text>
                        ))}
                      </div>
                    ) : null}
                  </>
                ) : (
                  <Empty description={localeCode === 'zh_CN' ? '点击上方拓扑节点，查看它的上下游关系和跳转动作' : 'Click a topology node above to inspect its upstream and downstream relations'} />
                )}
              </div>
            </>
          ) : (
            <div className="flex min-h-[320px] items-center justify-center">
              <Empty description={localeCode === 'zh_CN' ? '当前筛选条件下没有可展示的链路' : 'No visible trace matches the current filter'} />
            </div>
          )}
        </Card>

        <Card
          className="kc-detail-card"
          title={localeCode === 'zh_CN' ? 'Gateway / HTTPRoute 覆盖状态' : 'Gateway / HTTPRoute coverage'}
          headerExtraContent={hasLivePendingGateway ? <Tag color="orange">{localeCode === 'zh_CN' ? '待接真实路由' : 'Pending live routing'}</Tag> : null}
        >
          <div className="flex flex-col gap-3">
            <Text type="tertiary">
              {localeCode === 'zh_CN'
                ? 'Gateway 独立保留在侧栏里，避免主拓扑被未完成的 HTTPRoute 聚合关系干扰。等后端补齐后，这些节点会直接进入主图。'
                : 'Gateway stays in a separate card so unfinished HTTPRoute aggregation does not clutter the main topology. These nodes will move into the main graph once backend aggregation is ready.'}
            </Text>
            {gatewayCoverageItems.length === 0 ? (
              <Empty description={localeCode === 'zh_CN' ? '当前 scope 下未发现 Gateway' : 'No gateways found in the current scope'} />
            ) : gatewayCoverageItems.map((item) => (
              <div
                key={item.id}
                style={{
                  padding: 12,
                  borderRadius: 10,
                  border: '1px solid var(--semi-color-border)',
                  background: item.source === 'live' ? 'var(--semi-color-bg-1)' : 'rgba(248, 250, 252, 0.92)',
                }}
              >
                <div className="flex items-start justify-between gap-3">
                  <div className="flex flex-col gap-1">
                    <Text strong>{item.addressSummary !== '-' ? item.addressSummary : item.name}</Text>
                    <Text type="tertiary" size="small">{item.namespace}</Text>
                  </div>
                  <Tag color={item.source === 'live' ? 'orange' : 'blue'}>
                    {item.source === 'live'
                      ? (localeCode === 'zh_CN' ? '待接 HTTPRoute' : 'HTTPRoute pending')
                      : (localeCode === 'zh_CN' ? '演示' : 'Demo')}
                  </Tag>
                </div>
                <div className="mt-3 flex flex-wrap gap-2">
                  <Tag>{`Class: ${item.gatewayClass}`}</Tag>
                  <Tag>{localeCode === 'zh_CN' ? `${item.listenerCount} 个监听器` : `${item.listenerCount} listeners`}</Tag>
                </div>
              </div>
            ))}
          </div>
        </Card>
      </div>

      <Card
        className="kc-detail-card"
        title={localeCode === 'zh_CN' ? '入口链路明细' : 'Trace detail'}
        headerExtraContent={(
          <Text type="tertiary" size="small">
            {chartMode === 'live'
              ? (localeCode === 'zh_CN' ? '图上做总览收敛，Service / Pod 明细继续在这里展开' : 'The graph stays collapsed for overview, while detailed services and pods remain here')
              : (localeCode === 'zh_CN' ? '演示数据用于确认页面布局和交互' : 'Preview data is used to validate layout and interaction')}
          </Text>
        )}
      >
        <AdminTable
          columns={columns}
          dataSource={tableRows}
          rowKey="id"
          pageSize={8}
          enableColumnSelection={false}
        />
      </Card>
    </div>
  )
}
