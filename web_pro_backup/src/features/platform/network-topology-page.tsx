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
import { Button, Card, Empty, Input, Space, Tag, Typography } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import type { TableColumnsType } from 'antd'
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

type TopologyDataState = 'live' | 'pending' | 'demo'
type LocaleCode = 'zh_CN' | 'en_US'
type TopologySourceType = 'ingress' | 'httproute' | 'gateway' | 'demo'
type TopologyNodeKind =
  | 'entry'
  | 'ingress-route'
  | 'http-route'
  | 'pending-route'
  | 'service'
  | 'missing-service'
  | 'backend-group'
  | 'empty-backend'
  | 'pod'

interface ServiceView {
  name: string
  namespace: string
  selector?: Record<string, string>
}

interface IngressView {
  name: string
  namespace: string
  className?: string
  hosts?: string[]
  address?: string
  backendServices?: string[]
}

interface HTTPRouteView {
  name: string
  namespace: string
  hostnames?: string[]
  parentRefs?: string[]
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

interface GatewayIndexItem {
  id: string
  name: string
  namespace: string
  addressSummary: string
  gatewayClass: string
  listenerCount: number
}

interface TopologyNode {
  id: string
  name: string
  kind: TopologyNodeKind
  state: TopologyDataState
  namespace?: string
  resourceName?: string
  subtitle?: string
  badge?: string
}

interface TopologyTrace {
  id: string
  entry: TopologyNode
  route: TopologyNode
  service?: TopologyNode
  terminals: TopologyNode[]
  sourceType: TopologySourceType
  state: TopologyDataState
  notes: string
}

interface TableRow {
  id: string
  entry: TopologyNode
  route: TopologyNode
  service?: TopologyNode
  terminals: TopologyNode[]
  sourceType: TopologySourceType
  state: TopologyDataState
  notes: string
}

interface TopologySelectionDetail {
  notes: string[]
  relatedEntries: TopologyNode[]
  relatedRoutes: TopologyNode[]
  relatedServices: TopologyNode[]
  summary: string
  terminalNodes: TopologyNode[]
}

interface TopologyGraphNodeData extends Record<string, unknown> {
  name: string
  kind: TopologyNodeKind
  state: TopologyDataState
  subtitle?: string
  badge?: string
  namespace?: string
  resourceName?: string
  serviceId?: string
  terminalNodes?: TopologyNode[]
}

interface TopologyGraphEdgeData extends Record<string, unknown> {
  sourceKind: TopologyNodeKind
  state: TopologyDataState
}

interface TopologyGraphData {
  edges: TopologyFlowEdge[]
  entryCount: number
  fitKey: string
  nodeMap: Map<string, TopologyGraphNodeData>
  nodes: TopologyFlowNode[]
  podCount: number
  routeCount: number
  serviceCount: number
}

type TopologyFlowNode = Node<TopologyGraphNodeData, 'topologyNode'>
type TopologyFlowEdge = Edge<TopologyGraphEdgeData>

const NODE_WIDTH = 256
const NODE_HEIGHT = 118

const NODE_COLORS: Record<TopologyNodeKind, string> = {
  entry: '#3f67f6',
  'ingress-route': '#0f766e',
  'http-route': '#d97706',
  'pending-route': '#b08900',
  service: '#1d92c5',
  'missing-service': '#d84c45',
  'backend-group': '#22a36a',
  'empty-backend': '#94a3b8',
  pod: '#22a36a',
}

const LEGEND_TAG_COLORS: Record<Exclude<TopologyNodeKind, 'pod'>, string> = {
  entry: 'blue',
  'ingress-route': 'cyan',
  'http-route': 'orange',
  'pending-route': 'gold',
  service: 'blue',
  'missing-service': 'red',
  'backend-group': 'green',
  'empty-backend': 'default',
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

function resolveNodeColor(kind: TopologyNodeKind) {
  return NODE_COLORS[kind] ?? NODE_COLORS.entry
}

function getTopologyKindLabel(kind: TopologyNodeKind, localeCode: LocaleCode) {
  if (localeCode === 'en_US') {
    switch (kind) {
      case 'entry':
        return 'Entry'
      case 'ingress-route':
        return 'Ingress Route'
      case 'http-route':
        return 'HTTPRoute'
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
    case 'ingress-route':
      return 'Ingress 路由'
    case 'http-route':
      return 'HTTPRoute'
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

function formatBackendSubtitle(pods: TopologyNode[], localeCode: LocaleCode) {
  if (pods.length === 0) {
    return localeCode === 'zh_CN'
      ? 'Service 已解析，但 selector 暂未命中 Pod。'
      : 'Service resolved, but the selector does not match any pods yet.'
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

function inferControllerFamily(value?: string) {
  const normalized = value?.trim().toLowerCase() ?? ''
  if (!normalized) {
    return 'generic'
  }
  if (normalized.includes('kong')) {
    return 'kong'
  }
  if (normalized.includes('apisix')) {
    return 'apisix'
  }
  if (normalized.includes('traefik')) {
    return 'traefik'
  }
  return 'generic'
}

function formatIngressControllerLabel(className: string | undefined, localeCode: LocaleCode) {
  const family = inferControllerFamily(className)
  switch (family) {
    case 'kong':
      return 'Kong Ingress'
    case 'apisix':
      return 'APISIX Ingress'
    case 'traefik':
      return 'Traefik Ingress'
    default:
      if (className && className.trim() !== '') {
        return localeCode === 'zh_CN' ? `IngressClass: ${className}` : `IngressClass: ${className}`
      }
      return 'Ingress'
  }
}

function formatGatewayClassLabel(gatewayClass: string | undefined, localeCode: LocaleCode) {
  const family = inferControllerFamily(gatewayClass)
  switch (family) {
    case 'kong':
      return 'Kong Gateway'
    case 'apisix':
      return 'APISIX Gateway'
    case 'traefik':
      return 'Traefik Gateway'
    default:
      if (gatewayClass && gatewayClass.trim() !== '') {
        return localeCode === 'zh_CN' ? `GatewayClass: ${gatewayClass}` : `GatewayClass: ${gatewayClass}`
      }
      return localeCode === 'zh_CN' ? 'Gateway API' : 'Gateway API'
  }
}

function makeNode(
  id: string,
  name: string,
  kind: TopologyNodeKind,
  state: TopologyDataState,
  namespace?: string,
  resourceName?: string,
  subtitle?: string,
  badge?: string,
): TopologyNode {
  return {
    id,
    name,
    kind,
    state,
    namespace,
    resourceName,
    subtitle,
    badge,
  }
}

function buildGatewayIndexItems(gateways: GatewayView[]): GatewayIndexItem[] {
  return gateways.map((gateway) => ({
    id: `${gateway.namespace}/${gateway.name}`,
    name: gateway.name,
    namespace: gateway.namespace,
    addressSummary: gateway.addresses?.join(', ') || '-',
    gatewayClass: gateway.gatewayClass || '-',
    listenerCount: gateway.listenerCount ?? 0,
  }))
}

function buildServiceLookup(services: ServiceView[]) {
  return new Map(services.map((service) => [`${service.namespace}/${service.name}`, service]))
}

function buildPodsByNamespace(pods: PodView[]) {
  const podsByNamespace = new Map<string, PodView[]>()
  pods.forEach((pod) => {
    const items = podsByNamespace.get(pod.namespace) ?? []
    items.push(pod)
    podsByNamespace.set(pod.namespace, items)
  })
  return podsByNamespace
}

function resolveServiceNode(
  routeNamespace: string,
  serviceName: string,
  services: Map<string, ServiceView>,
  podsByNamespace: Map<string, PodView[]>,
  localeCode: LocaleCode,
  missingNote: string,
  noPodNote: string,
) {
  const serviceKey = `${routeNamespace}/${serviceName}`
  const service = services.get(serviceKey)
  const serviceNode = service
    ? makeNode(`service:${serviceKey}`, service.name, 'service', 'live', service.namespace, service.name, service.namespace, 'Service')
    : makeNode(`missing-service:${serviceKey}`, serviceName, 'missing-service', 'pending', routeNamespace, serviceName, routeNamespace, localeCode === 'zh_CN' ? '当前 scope 不可见' : 'Out of current scope')

  const matchedPods = service
    ? (podsByNamespace.get(service.namespace) ?? []).filter((pod) => selectorMatchesLabels(service.selector, pod.labels))
    : []
  const terminalPods = matchedPods.map((pod) =>
    makeNode(`pod:${pod.namespace}/${pod.name}`, pod.name, 'pod', 'live', pod.namespace, pod.name),
  )

  const note = service
    ? terminalPods.length > 0
      ? localeCode === 'zh_CN'
        ? `已解析 ${terminalPods.length} 个 Pod 后端。`
        : `${terminalPods.length} backend pods resolved.`
      : noPodNote
    : missingNote

  return {
    note,
    serviceNode,
    state: service ? 'live' as const : 'pending' as const,
    terminals: terminalPods,
  }
}

function buildIngressTraces(
  ingresses: IngressView[],
  services: Map<string, ServiceView>,
  podsByNamespace: Map<string, PodView[]>,
  localeCode: LocaleCode,
) {
  return ingresses.flatMap((ingress) => {
    const routeNode = makeNode(
      `ingress-route:${ingress.namespace}/${ingress.name}`,
      ingress.name,
      'ingress-route',
      'live',
      ingress.namespace,
      ingress.name,
      ingress.namespace,
      formatIngressControllerLabel(ingress.className, localeCode),
    )
    const entries = ingress.hosts && ingress.hosts.length > 0
      ? ingress.hosts
      : ingress.address && ingress.address.trim() !== ''
        ? ingress.address.split(',').map((item) => item.trim()).filter(Boolean)
        : [ingress.name]
    const backendServices = Array.from(new Set(ingress.backendServices?.filter(Boolean) ?? []))

    return entries.flatMap((entryLabel) => {
      const entryNode = makeNode(
        `entry:ingress:${ingress.namespace}/${ingress.name}/${entryLabel}`,
        entryLabel,
        'entry',
        'live',
        ingress.namespace,
        ingress.name,
        ingress.namespace,
        formatIngressControllerLabel(ingress.className, localeCode),
      )

      if (backendServices.length === 0) {
        return [{
          id: `${entryNode.id}:no-backend`,
          entry: entryNode,
          route: routeNode,
          service: makeNode(
            `missing-service:${ingress.namespace}/${ingress.name}:no-backend`,
            localeCode === 'zh_CN' ? '未声明后端 Service' : 'No backend service',
            'missing-service',
            'pending',
            ingress.namespace,
            undefined,
            ingress.namespace,
            localeCode === 'zh_CN' ? '后端待补齐' : 'Backend pending',
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
        const resolved = resolveServiceNode(
          ingress.namespace,
          serviceName,
          services,
          podsByNamespace,
          localeCode,
          localeCode === 'zh_CN'
            ? 'Ingress 指向的 Service 当前在 scope 内不可见。'
            : 'The service referenced by this ingress is not visible in the current scope.',
          localeCode === 'zh_CN'
            ? 'Service 已解析，但当前没有匹配到 Pod。'
            : 'Service resolved, but no matching pods were found.',
        )

        return {
          id: `${entryNode.id}:${routeNode.id}:${serviceName}`,
          entry: entryNode,
          route: routeNode,
          service: resolved.serviceNode,
          terminals: resolved.terminals,
          sourceType: 'ingress' as const,
          state: resolved.state,
          notes: resolved.note,
        }
      })
    })
  })
}

function buildHTTPRouteTraces(
  httpRoutes: HTTPRouteView[],
  gateways: Map<string, GatewayIndexItem>,
  services: Map<string, ServiceView>,
  podsByNamespace: Map<string, PodView[]>,
  localeCode: LocaleCode,
) {
  return httpRoutes.flatMap((route) => {
    const parentRefs = route.parentRefs?.filter(Boolean) ?? []
    const resolvedParents = parentRefs.length > 0
      ? parentRefs.map((ref) => gateways.get(ref) ?? {
        id: ref,
        name: ref.split('/').pop() || ref,
        namespace: ref.split('/')[0] || route.namespace,
        addressSummary: '-',
        gatewayClass: '-',
        listenerCount: 0,
        source: 'live' as const,
      })
      : [{
        id: `unbound:${route.namespace}/${route.name}`,
        name: localeCode === 'zh_CN' ? '未绑定 Gateway' : 'Unbound gateway',
        namespace: route.namespace,
        addressSummary: '-',
        gatewayClass: '-',
        listenerCount: 0,
        source: 'live' as const,
      }]

    const gatewayLabels = uniqueStrings(resolvedParents.map((item) => item.name))
    const routeBadge = uniqueStrings(resolvedParents.map((item) => formatGatewayClassLabel(item.gatewayClass, localeCode))).join(' · ') || 'HTTPRoute'
    const routeSubtitle = route.hostnames && route.hostnames.length > 0
      ? route.hostnames.join(' · ')
      : gatewayLabels.join(' · ') || route.namespace
    const routeNode = makeNode(
      `http-route:${route.namespace}/${route.name}`,
      route.name,
      'http-route',
      'live',
      route.namespace,
      route.name,
      routeSubtitle || route.namespace,
      `HTTPRoute · ${routeBadge}`,
    )
    const backendServices = Array.from(new Set(route.backendServices?.filter(Boolean) ?? []))

    return resolvedParents.flatMap((parent) => {
      const entries = route.hostnames && route.hostnames.length > 0
        ? route.hostnames
        : parent.addressSummary !== '-' && parent.addressSummary.trim() !== ''
          ? parent.addressSummary.split(',').map((item) => item.trim()).filter(Boolean)
          : [parent.name]

      return entries.flatMap((entryLabel) => {
        const entryNode = makeNode(
          `entry:gateway:${parent.id}:${entryLabel}`,
          entryLabel,
          'entry',
          parent.name.startsWith('未绑定') || parent.name.startsWith('Unbound') ? 'pending' : 'live',
          parent.namespace,
          parent.name,
          parent.namespace,
          formatGatewayClassLabel(parent.gatewayClass, localeCode),
        )

        if (backendServices.length === 0) {
          return [{
            id: `${entryNode.id}:${routeNode.id}:no-backend`,
            entry: entryNode,
            route: routeNode,
            service: makeNode(
              `missing-service:${route.namespace}/${route.name}:no-backend`,
              localeCode === 'zh_CN' ? '未声明后端 Service' : 'No backend service',
              'missing-service',
              'pending',
              route.namespace,
              undefined,
              route.namespace,
              localeCode === 'zh_CN' ? '后端待补齐' : 'Backend pending',
            ),
            terminals: [],
            sourceType: 'httproute' as const,
            state: 'pending' as const,
            notes: localeCode === 'zh_CN'
              ? 'HTTPRoute 当前没有可解析的 backendRefs。'
              : 'The HTTPRoute does not expose resolved backendRefs yet.',
          }]
        }

        return backendServices.map((serviceName) => {
          const resolved = resolveServiceNode(
            route.namespace,
            serviceName,
            services,
            podsByNamespace,
            localeCode,
            localeCode === 'zh_CN'
              ? 'HTTPRoute 指向的 Service 当前在 scope 内不可见。'
              : 'The service referenced by this HTTPRoute is not visible in the current scope.',
            localeCode === 'zh_CN'
              ? 'Service 已解析，但当前没有匹配到 Pod。'
              : 'Service resolved, but no matching pods were found.',
          )

          return {
            id: `${entryNode.id}:${routeNode.id}:${serviceName}`,
            entry: entryNode,
            route: routeNode,
            service: resolved.serviceNode,
            terminals: resolved.terminals,
            sourceType: 'httproute' as const,
            state: resolved.state,
            notes: parent.name.startsWith('未绑定') || parent.name.startsWith('Unbound')
              ? localeCode === 'zh_CN'
                ? `${resolved.note} 当前 HTTPRoute 没有关联到可见的 Gateway。`
                : `${resolved.note} The HTTPRoute is not bound to a visible gateway yet.`
              : resolved.note,
          }
        })
      })
    })
  })
}

function buildPendingGatewayTraces(
  gateways: GatewayIndexItem[],
  referencedParents: Set<string>,
  localeCode: LocaleCode,
) {
  return gateways
    .filter((gateway) => !referencedParents.has(gateway.id))
    .map((gateway) => {
      const entryName = gateway.addressSummary !== '-' ? gateway.addressSummary : gateway.name
      return {
        id: `pending-gateway:${gateway.id}`,
        entry: makeNode(
          `entry:pending-gateway:${gateway.id}`,
          entryName,
          'entry',
          'pending',
          gateway.namespace,
          gateway.name,
          gateway.namespace,
          formatGatewayClassLabel(gateway.gatewayClass, localeCode),
        ),
        route: makeNode(
          `pending-route:${gateway.id}`,
          localeCode === 'zh_CN' ? 'HTTPRoute 待接入' : 'HTTPRoute pending',
          'pending-route',
          'pending',
          gateway.namespace,
          gateway.name,
          gateway.name,
          formatGatewayClassLabel(gateway.gatewayClass, localeCode),
        ),
        service: undefined,
        terminals: [],
        sourceType: 'gateway' as const,
        state: 'pending' as const,
        notes: localeCode === 'zh_CN'
          ? '当前 Gateway 还没有关联到可见的 HTTPRoute。'
          : 'This gateway is not associated with any visible HTTPRoute yet.',
      }
    })
}

function filterTraces(traces: TopologyTrace[], keyword: string) {
  if (!keyword) {
    return traces
  }

  return traces.filter((trace) => {
    const fields = [
      trace.entry.name,
      trace.route.name,
      trace.route.badge ?? '',
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

function buildTopologyGraph(traces: TopologyTrace[]): TopologyGraphData {
  const nodeMap = new Map<string, TopologyGraphNodeData>()
  const edgeMap = new Map<string, TopologyFlowEdge>()
  const serviceMap = new Map<string, TopologyNode>()
  const backendPodsByService = new Map<string, Map<string, TopologyNode>>()

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

  const addEdge = (source: string, target: string, sourceKind: TopologyNodeKind, state: TopologyDataState) => {
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
      subtitle: trace.entry.subtitle || trace.entry.namespace || '-',
      badge: trace.entry.badge,
      namespace: trace.entry.namespace,
      resourceName: trace.entry.resourceName,
    })

    addNode(trace.route.id, {
      name: trace.route.name,
      kind: trace.route.kind,
      state: trace.route.state,
      subtitle: trace.route.subtitle || trace.route.namespace || '-',
      badge: trace.route.badge,
      namespace: trace.route.namespace,
      resourceName: trace.route.resourceName,
    })

    addEdge(trace.entry.id, trace.route.id, trace.entry.kind, trace.state)

    if (!trace.service) {
      return
    }

    addNode(trace.service.id, {
      name: trace.service.name,
      kind: trace.service.kind,
      state: trace.service.state,
      subtitle: trace.service.subtitle || trace.service.namespace || '-',
      badge: trace.service.badge,
      namespace: trace.service.namespace,
      resourceName: trace.service.resourceName,
    })
    addEdge(trace.route.id, trace.service.id, trace.route.kind, trace.state)

    if (trace.service.kind !== 'service') {
      return
    }

    serviceMap.set(trace.service.id, trace.service)
    if (trace.terminals.length === 0) {
      return
    }

    const pods = backendPodsByService.get(trace.service.id) ?? new Map<string, TopologyNode>()
    trace.terminals.forEach((terminal) => {
      pods.set(terminal.id, terminal)
    })
    backendPodsByService.set(trace.service.id, pods)
  })

  serviceMap.forEach((serviceNode, serviceID) => {
    const pods = Array.from(backendPodsByService.get(serviceID)?.values() ?? [])
    if (pods.length > 0) {
      const backendID = `backend:${serviceID}`
      addNode(backendID, {
        name: `${pods.length} backend pods`,
        kind: 'backend-group',
        state: 'live',
        subtitle: formatBackendSubtitle(pods, 'en_US'),
        badge: 'Backend group',
        namespace: serviceNode.namespace,
        resourceName: serviceNode.resourceName,
        serviceId: serviceID,
        terminalNodes: pods,
      })
      addEdge(serviceID, backendID, serviceNode.kind, 'live')
      return
    }

    const backendID = `backend-empty:${serviceID}`
    addNode(backendID, {
      name: 'No matching backend pods',
      kind: 'empty-backend',
      state: 'pending',
      subtitle: 'Service resolved, but selector has not matched any pod yet.',
      badge: 'Selector not matched',
      namespace: serviceNode.namespace,
      resourceName: serviceNode.resourceName,
      serviceId: serviceID,
      terminalNodes: [],
    })
    addEdge(serviceID, backendID, serviceNode.kind, 'pending')
  })

  const flowNodes = Array.from(nodeMap.entries()).map(([id, data]) => ({
    id,
    type: 'topologyNode' as const,
    position: { x: 0, y: 0 },
    data,
  }))
  const flowEdges = Array.from(edgeMap.values())
  const nodes = layoutTopologyGraph(flowNodes, flowEdges)
  const podIDs = new Set<string>()
  backendPodsByService.forEach((pods) => {
    pods.forEach((pod) => podIDs.add(pod.id))
  })

  return {
    nodes,
    edges: flowEdges,
    nodeMap,
    fitKey: `${nodes.map((node) => node.id).join(',')}::${flowEdges.map((edge) => edge.id).join(',')}`,
    entryCount: Array.from(nodeMap.values()).filter((node) => node.kind === 'entry').length,
    routeCount: Array.from(nodeMap.values()).filter((node) => ['ingress-route', 'http-route', 'pending-route'].includes(node.kind)).length,
    serviceCount: Array.from(nodeMap.values()).filter((node) => ['service', 'missing-service'].includes(node.kind)).length,
    podCount: podIDs.size,
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

function buildSelectionDetail(
  nodeID: string | null,
  nodeData: TopologyGraphNodeData | null,
  traces: TopologyTrace[],
  localeCode: LocaleCode,
): TopologySelectionDetail | null {
  if (!nodeID || !nodeData) {
    return null
  }

  switch (nodeData.kind) {
    case 'entry': {
      const relatedTraces = traces.filter((trace) => trace.entry.id === nodeID)
      const relatedRoutes = uniqueTopologyNodes(relatedTraces.map((trace) => trace.route))
      const relatedServices = uniqueTopologyNodes(relatedTraces.map((trace) => trace.service))
      const terminalNodes = uniqueTopologyNodes(relatedTraces.flatMap((trace) => trace.terminals))
      return {
        relatedEntries: [],
        relatedRoutes,
        relatedServices,
        terminalNodes,
        notes: uniqueStrings(relatedTraces.map((trace) => trace.notes)),
        summary: localeCode === 'zh_CN'
          ? `该入口当前连接 ${relatedRoutes.length} 条路由、${relatedServices.length} 个 Service，并覆盖 ${terminalNodes.length} 个后端 Pod。`
          : `This entry currently connects to ${relatedRoutes.length} routes, ${relatedServices.length} services, and ${terminalNodes.length} backend pods.`,
      }
    }
    case 'ingress-route':
    case 'http-route':
    case 'pending-route': {
      const relatedTraces = traces.filter((trace) => trace.route.id === nodeID)
      const relatedEntries = uniqueTopologyNodes(relatedTraces.map((trace) => trace.entry))
      const relatedServices = uniqueTopologyNodes(relatedTraces.map((trace) => trace.service))
      const terminalNodes = uniqueTopologyNodes(relatedTraces.flatMap((trace) => trace.terminals))
      return {
        relatedEntries,
        relatedRoutes: [],
        relatedServices,
        terminalNodes,
        notes: uniqueStrings(relatedTraces.map((trace) => trace.notes)),
        summary: nodeData.kind === 'pending-route'
          ? localeCode === 'zh_CN'
            ? `当前待接路由节点关联 ${relatedEntries.length} 个入口，但还没有接到可见的 Service。`
            : `This pending-route node is attached to ${relatedEntries.length} entries but is not connected to any visible service yet.`
          : localeCode === 'zh_CN'
            ? `该路由节点当前承接 ${relatedEntries.length} 个入口，并连接 ${relatedServices.length} 个 Service。`
            : `This route node currently receives ${relatedEntries.length} entries and connects to ${relatedServices.length} services.`,
      }
    }
    case 'service':
    case 'missing-service': {
      const relatedTraces = traces.filter((trace) => trace.service?.id === nodeID)
      const relatedEntries = uniqueTopologyNodes(relatedTraces.map((trace) => trace.entry))
      const relatedRoutes = uniqueTopologyNodes(relatedTraces.map((trace) => trace.route))
      const terminalNodes = uniqueTopologyNodes(relatedTraces.flatMap((trace) => trace.terminals))
      return {
        relatedEntries,
        relatedRoutes,
        relatedServices: [],
        terminalNodes,
        notes: uniqueStrings(relatedTraces.map((trace) => trace.notes)),
        summary: nodeData.kind === 'service'
          ? localeCode === 'zh_CN'
            ? `该 Service 当前被 ${relatedRoutes.length} 条路由引用，并汇总 ${terminalNodes.length} 个后端 Pod。`
            : `This service is referenced by ${relatedRoutes.length} routes and aggregates ${terminalNodes.length} backend pods.`
          : localeCode === 'zh_CN'
            ? `当前有 ${relatedRoutes.length} 条路由指向一个在 scope 内不可见的 Service。`
            : `${relatedRoutes.length} routes currently point to a service that is not visible in the current scope.`,
      }
    }
    case 'backend-group':
    case 'empty-backend': {
      const serviceID = String(nodeData.serviceId ?? '')
      const relatedTraces = traces.filter((trace) => trace.service?.id === serviceID)
      const relatedEntries = uniqueTopologyNodes(relatedTraces.map((trace) => trace.entry))
      const relatedRoutes = uniqueTopologyNodes(relatedTraces.map((trace) => trace.route))
      const terminalNodes = uniqueTopologyNodes(nodeData.terminalNodes ?? [])
      return {
        relatedEntries,
        relatedRoutes,
        relatedServices: uniqueTopologyNodes(relatedTraces.map((trace) => trace.service)),
        terminalNodes,
        notes: uniqueStrings(relatedTraces.map((trace) => trace.notes)),
        summary: nodeData.kind === 'backend-group'
          ? localeCode === 'zh_CN'
            ? `总览图把后端收敛成一个集合节点，当前包含 ${terminalNodes.length} 个 Pod。`
            : `The overview graph collapses the backend into one aggregate node that currently contains ${terminalNodes.length} pods.`
          : localeCode === 'zh_CN'
            ? 'Service 已解析，但 selector 还没有命中任何后端 Pod。'
            : 'The service is resolved, but its selector has not matched any backend pod yet.',
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
  onSelectNode: (nodeID: string | null) => void
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
  onSelectNode: (nodeID: string | null) => void
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
  const [selectedNodeID, setSelectedNodeID] = useState<string | null>(null)
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

  const httpRoutesQuery = useQuery({
    queryKey: ['network-topology-httproutes', clusterId, namespace],
    queryFn: () => api.get<ApiResponse<HTTPRouteView[]>>(buildClusterScopedPath(clusterId!, 'network/httproutes', namespace)),
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

  const serviceLookup = useMemo(
    () => buildServiceLookup(servicesQuery.data?.data ?? []),
    [servicesQuery.data],
  )
  const podsByNamespace = useMemo(
    () => buildPodsByNamespace(podsQuery.data?.data ?? []),
    [podsQuery.data],
  )
  const gatewayItems = useMemo(
    () => buildGatewayIndexItems(gatewaysQuery.data?.data ?? []),
    [gatewaysQuery.data],
  )
  const gatewayMap = useMemo(
    () => new Map(gatewayItems.map((gateway) => [gateway.id, gateway])),
    [gatewayItems],
  )

  const ingressTraces = useMemo(
    () => buildIngressTraces(
      ingressesQuery.data?.data ?? [],
      serviceLookup,
      podsByNamespace,
      localeCode,
    ),
    [ingressesQuery.data, localeCode, podsByNamespace, serviceLookup],
  )

  const httpRouteTraces = useMemo(
    () => buildHTTPRouteTraces(
      httpRoutesQuery.data?.data ?? [],
      gatewayMap,
      serviceLookup,
      podsByNamespace,
      localeCode,
    ),
    [gatewayMap, httpRoutesQuery.data, localeCode, podsByNamespace, serviceLookup],
  )

  const referencedGatewayParents = useMemo(
    () => new Set((httpRoutesQuery.data?.data ?? []).flatMap((route) => route.parentRefs?.filter(Boolean) ?? [])),
    [httpRoutesQuery.data],
  )

  const pendingGatewayTraces = useMemo(
    () => buildPendingGatewayTraces(gatewayItems, referencedGatewayParents, localeCode),
    [gatewayItems, localeCode, referencedGatewayParents],
  )

  const liveTraces = useMemo(
    () => [...ingressTraces, ...httpRouteTraces, ...pendingGatewayTraces],
    [httpRouteTraces, ingressTraces, pendingGatewayTraces],
  )

  const hasLiveTopology = liveTraces.length > 0

  const filteredTraces = useMemo(
    () => filterTraces(liveTraces, deferredSearchKeyword),
    [deferredSearchKeyword, liveTraces],
  )

  const topologyGraph = useMemo(
    () => buildTopologyGraph(filteredTraces),
    [filteredTraces],
  )

  const flowNodes = useMemo(
    () => topologyGraph.nodes.map((node) => ({
      ...node,
      selected: node.id === selectedNodeID,
      data: {
        ...node.data,
        subtitle: node.data.kind === 'backend-group'
          ? formatBackendSubtitle(node.data.terminalNodes as TopologyNode[] ?? [], localeCode)
          : node.data.kind === 'empty-backend'
            ? (localeCode === 'zh_CN' ? 'Service 已解析，但 selector 暂未命中 Pod。' : 'Service resolved, but selector has not matched any pod yet.')
            : node.data.subtitle,
        badge: node.data.kind === 'backend-group'
          ? (localeCode === 'zh_CN' ? '后端集合' : 'Backend group')
          : node.data.kind === 'empty-backend'
            ? (localeCode === 'zh_CN' ? '选择器未命中' : 'Selector not matched')
            : node.data.badge,
        name: node.data.kind === 'backend-group'
          ? (localeCode === 'zh_CN'
            ? `${(node.data.terminalNodes as TopologyNode[] ?? []).length} 个 Backend Pods`
            : `${(node.data.terminalNodes as TopologyNode[] ?? []).length} backend pods`)
          : node.data.kind === 'empty-backend'
            ? (localeCode === 'zh_CN' ? '未匹配到后端 Pod' : 'No matching backend pods')
            : node.data.name,
      },
    })),
    [localeCode, selectedNodeID, topologyGraph.nodes],
  )

  const tableRows = useMemo<TableRow[]>(
    () => filteredTraces.map((trace) => ({
      id: trace.id,
      entry: trace.entry,
      route: trace.route,
      service: trace.service,
      terminals: trace.terminals,
      sourceType: trace.sourceType,
      state: trace.state,
      notes: trace.notes,
    })),
    [filteredTraces],
  )

  useEffect(() => {
    if (selectedNodeID && !topologyGraph.nodeMap.has(selectedNodeID)) {
      setSelectedNodeID(null)
    }
  }, [selectedNodeID, topologyGraph.nodeMap])

  const liveErrors = [
    getErrorMessage(servicesQuery.error),
    getErrorMessage(ingressesQuery.error),
    getErrorMessage(httpRoutesQuery.error),
    getErrorMessage(gatewaysQuery.error),
    getErrorMessage(podsQuery.error),
  ].filter(Boolean)
  const liveLoading = clusterId && (servicesQuery.isLoading || ingressesQuery.isLoading || httpRoutesQuery.isLoading || gatewaysQuery.isLoading || podsQuery.isLoading)
  const topologyViewState = !clusterId
    ? 'cluster-required'
    : liveLoading
      ? 'loading'
      : hasLiveTopology
        ? 'live'
        : 'unavailable'
  const viewTag = topologyViewState === 'live'
    ? (localeCode === 'zh_CN' ? '实时拓扑' : 'Live topology')
    : topologyViewState === 'loading'
      ? (localeCode === 'zh_CN' ? '加载中' : 'Loading')
      : topologyViewState === 'cluster-required'
        ? (localeCode === 'zh_CN' ? '待选集群' : 'Select cluster')
        : (localeCode === 'zh_CN' ? '实时不可用' : 'Live unavailable')
  const viewTagColor = topologyViewState === 'live'
    ? 'green'
    : topologyViewState === 'loading'
      ? 'blue'
      : topologyViewState === 'cluster-required'
        ? 'default'
        : 'orange'

  const viewDescription = topologyViewState === 'live'
    ? (localeCode === 'zh_CN'
      ? 'Ingress(controller)、Gateway / HTTPRoute 和 Service 后端已经统一进入同一张入口网络拓扑；未挂接 HTTPRoute 的 Gateway 会以待接路由节点保留在图里。'
      : 'Ingress controllers, Gateway / HTTPRoute, and service backends now share one topology; gateways without HTTPRoute bindings remain visible as pending-route nodes.')
    : topologyViewState === 'loading'
      ? (localeCode === 'zh_CN'
        ? '正在从 Ingress、Gateway、HTTPRoute、Service 和 Pod 资源装载实时入口拓扑。'
        : 'Loading the live entry topology from ingress, gateway, HTTPRoute, service, and pod resources.')
      : topologyViewState === 'cluster-required'
        ? (localeCode === 'zh_CN'
          ? '先选择一个集群，再加载当前 scope 下的实时入口拓扑。'
          : 'Select a cluster first to load the live entry topology for the current scope.')
        : (localeCode === 'zh_CN'
          ? '当前 scope 下没有检测到实时入口拓扑，因此页面不会回退到演示或预览链路。请检查 Ingress、Gateway、HTTPRoute 和 Service 关系是否在当前集群或命名空间可见。'
          : 'No live entry topology was detected in the current scope, so the page does not fall back to demo or preview traces. Verify that ingress, gateway, HTTPRoute, and service relations are visible in the selected cluster or namespace.')

  const emptyStateTitle = topologyViewState === 'loading'
    ? (localeCode === 'zh_CN' ? '实时拓扑加载中' : 'Loading live topology')
    : topologyViewState === 'cluster-required'
      ? (localeCode === 'zh_CN' ? '选择集群后查看拓扑' : 'Select a cluster to view topology')
      : hasLiveTopology
        ? (localeCode === 'zh_CN' ? '当前筛选条件下没有可展示的链路' : 'No visible trace matches the current filter')
        : (localeCode === 'zh_CN' ? '当前 scope 下没有实时入口拓扑' : 'No live entry topology in the current scope')
  const emptyStateDescription = topologyViewState === 'loading'
    ? (localeCode === 'zh_CN'
      ? '页面只展示实时链路，正在等待网络资源返回。'
      : 'The page renders live traces only and is waiting for network resources to return.')
    : topologyViewState === 'cluster-required'
      ? (localeCode === 'zh_CN'
        ? '选择集群后，这里会展示 Kong、APISIX、Traefik、原生 Ingress 和 Gateway 入口路径。'
        : 'After a cluster is selected, this view will render Kong, APISIX, Traefik, native Ingress, and Gateway entry paths.')
      : hasLiveTopology
        ? (localeCode === 'zh_CN'
          ? '调整搜索关键词，或清空筛选后重试。'
          : 'Adjust the search keyword or clear the filter and try again.')
        : (localeCode === 'zh_CN'
          ? '当前没有检测到任何可见的 Ingress 或 Gateway 入口链路，页面不会再渲染演示数据。'
          : 'No visible ingress or gateway entry path was detected, and the page no longer renders demo data in this state.')

  const selectedTopologyNode = selectedNodeID ? topologyGraph.nodeMap.get(selectedNodeID) ?? null : null
  const selectionDetail = useMemo(
    () => buildSelectionDetail(selectedNodeID, selectedTopologyNode, filteredTraces, localeCode),
    [filteredTraces, localeCode, selectedNodeID, selectedTopologyNode],
  )

  const selectedServicePath = selectedTopologyNode?.resourceName && selectedTopologyNode.namespace
    && ['service', 'backend-group', 'empty-backend'].includes(selectedTopologyNode.kind)
    ? buildServiceDetailPath(selectedTopologyNode.resourceName, namespace, selectedTopologyNode.namespace)
    : null

  const columns: TableColumnsType<TableRow> = [
    {
      title: localeCode === 'zh_CN' ? '入口' : 'Entry',
      dataIndex: 'entry',
      render: (_: TopologyNode, record: TableRow) => (
        <div className="flex flex-col gap-1">
          <Text strong>{record.entry.name}</Text>
          <Text type="secondary" className="text-xs">{record.entry.badge || record.entry.namespace || '-'}</Text>
        </div>
      ),
    },
    {
      title: localeCode === 'zh_CN' ? '路由' : 'Route',
      dataIndex: 'route',
      render: (_: TopologyNode, record: TableRow) => (
        <div className="flex flex-col gap-1">
          <Text strong>{record.route.name}</Text>
          <Space wrap>
            {record.route.badge ? <Tag color={LEGEND_TAG_COLORS[record.route.kind as Exclude<TopologyNodeKind, 'pod'>]}>{record.route.badge}</Tag> : null}
            {record.route.subtitle ? <Text type="secondary" className="text-xs">{record.route.subtitle}</Text> : null}
          </Space>
        </div>
      ),
    },
    {
      title: 'Service',
      dataIndex: 'service',
      render: (_: TopologyNode | undefined, record: TableRow) => {
        if (!record.service) {
          return <Text type="secondary">-</Text>
        }
        const canNavigate = record.service.kind === 'service' && record.service.resourceName && record.service.namespace
        if (!canNavigate) {
          return (
            <div className="flex flex-col gap-1">
              <Text>{record.service.name}</Text>
              {record.service.badge ? <Text type="secondary" className="text-xs">{record.service.badge}</Text> : null}
            </div>
          )
        }
        return (
          <Button
            type="text"
            onClick={() => navigate(buildServiceDetailPath(record.service!.resourceName!, namespace, record.service!.namespace!))}
          >
            {record.service.name}
          </Button>
        )
      },
    },
    {
      title: localeCode === 'zh_CN' ? 'Backend' : 'Backend',
      dataIndex: 'terminals',
      render: (_: TopologyNode[], record: TableRow) => {
        if (record.terminals.length === 0) {
          return <Text type="secondary">-</Text>
        }

        const visibleItems = record.terminals.slice(0, 3)
        const remainCount = record.terminals.length - visibleItems.length

        return (
          <Space wrap>
            {visibleItems.map((item) => item.resourceName && item.namespace ? (
              <Button
                key={item.id}
                variant="outlined"
                size="small"
                onClick={() => navigate(buildPodDetailPath(item.resourceName!, namespace, item.namespace!))}
              >
                {item.name}
              </Button>
            ) : (
              <Tag key={item.id}>{item.name}</Tag>
            ))}
            {remainCount > 0 ? <Tag color="default">+{remainCount}</Tag> : null}
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
      render: (value: string) => <Text type="secondary">{value}</Text>,
    },
  ]

  return (
    <div className="kc-page">
      <PlatformScopeToolbar />

      <Card className="kc-detail-card">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex min-w-[280px] flex-1 flex-wrap items-center gap-3">
            <Input
              value={searchKeyword}
              onChange={(event) => setSearchKeyword(event.target.value)}
              placeholder={localeCode === 'zh_CN' ? '搜索入口 / 路由 / Service / Pod / 说明' : 'Search entry / route / service / pod / note'}
              style={{ width: 340 }}
              className="kc-platform-compact-field"
            />
            <Text type="secondary" className="text-xs">
              {viewDescription}
            </Text>
          </div>
          <Space wrap>
            <Tag color={viewTagColor}>{viewTag}</Tag>
            {liveErrors.length > 0 ? <Tag color="red">{localeCode === 'zh_CN' ? '实时数据部分失败' : 'Partial live data failure'}</Tag> : null}
            <Tag color={LEGEND_TAG_COLORS.entry}>{localeCode === 'zh_CN' ? '入口' : 'Entry'}</Tag>
            <Tag color={LEGEND_TAG_COLORS['ingress-route']}>Ingress</Tag>
            <Tag color={LEGEND_TAG_COLORS['http-route']}>HTTPRoute</Tag>
            <Tag color={LEGEND_TAG_COLORS['pending-route']}>{localeCode === 'zh_CN' ? '待接路由' : 'Pending route'}</Tag>
            <Tag color={LEGEND_TAG_COLORS.service}>Service</Tag>
            <Tag color={LEGEND_TAG_COLORS['backend-group']}>{localeCode === 'zh_CN' ? '后端集合' : 'Backend group'}</Tag>
            <Tag color={LEGEND_TAG_COLORS['empty-backend']}>{localeCode === 'zh_CN' ? '无匹配 Pod' : 'No matching pod'}</Tag>
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
          { label: localeCode === 'zh_CN' ? '路由节点' : 'Route nodes', value: topologyGraph.routeCount },
          { label: localeCode === 'zh_CN' ? 'Service 节点' : 'Service nodes', value: topologyGraph.serviceCount },
          { label: localeCode === 'zh_CN' ? '后端 Pods' : 'Backend pods', value: topologyGraph.podCount },
        ]}
      />

      <Card
        className="kc-detail-card"
        title={localeCode === 'zh_CN' ? '入口 -> 路由 -> Service -> Backend 拓扑' : 'Entry -> Route -> Service -> Backend topology'}
        extra={(
          <Space wrap>
            <Text type="secondary" className="text-xs">
              {localeCode === 'zh_CN'
                ? `${filteredTraces.length} 条链路`
                : `${filteredTraces.length} traces`}
            </Text>
            {selectedTopologyNode ? (
              <Tag color={LEGEND_TAG_COLORS[selectedTopologyNode.kind as Exclude<TopologyNodeKind, 'pod'>] ?? 'blue'}>
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
              onSelectNode={setSelectedNodeID}
            />
            <div className="kc-topology-selection">
              {selectedTopologyNode && selectionDetail ? (
                <>
                  <div className="flex flex-wrap items-start justify-between gap-3">
                    <div className="flex min-w-[240px] flex-1 flex-col gap-1">
                      <Text strong>{selectedTopologyNode.name}</Text>
                      <Text type="secondary" className="text-xs">{selectedTopologyNode.subtitle || '-'}</Text>
                    </div>
                    <Space wrap>
                      <Tag color={LEGEND_TAG_COLORS[selectedTopologyNode.kind as Exclude<TopologyNodeKind, 'pod'>] ?? 'blue'}>
                        {getTopologyKindLabel(selectedTopologyNode.kind, localeCode)}
                      </Tag>
                      {renderTraceState(selectedTopologyNode.state, localeCode)}
                    </Space>
                  </div>

                  <Text type="secondary">{selectionDetail.summary}</Text>

                  {selectedServicePath ? (
                    <Space wrap>
                      <Button
                        type="primary"
                        variant="outlined"
                        size="small"
                        onClick={() => navigate(selectedServicePath)}
                      >
                        {localeCode === 'zh_CN' ? '查看 Service 详情' : 'Open Service detail'}
                      </Button>
                    </Space>
                  ) : null}

                  {selectionDetail.relatedEntries.length > 0 ? (
                    <div className="flex flex-col gap-2">
                      <Text strong className="text-xs">{localeCode === 'zh_CN' ? '相关入口' : 'Related entries'}</Text>
                      <Space wrap>
                        {selectionDetail.relatedEntries.map((item) => (
                          <Tag key={item.id}>{item.name}</Tag>
                        ))}
                      </Space>
                    </div>
                  ) : null}

                  {selectionDetail.relatedRoutes.length > 0 ? (
                    <div className="flex flex-col gap-2">
                      <Text strong className="text-xs">{localeCode === 'zh_CN' ? '相关路由' : 'Related routes'}</Text>
                      <Space wrap>
                        {selectionDetail.relatedRoutes.map((item) => (
                          <Tag key={item.id} color={LEGEND_TAG_COLORS[item.kind as Exclude<TopologyNodeKind, 'pod'>] ?? 'blue'}>
                            {item.name}
                          </Tag>
                        ))}
                      </Space>
                    </div>
                  ) : null}

                  {selectionDetail.relatedServices.length > 0 ? (
                    <div className="flex flex-col gap-2">
                      <Text strong className="text-xs">{localeCode === 'zh_CN' ? '相关 Service' : 'Related services'}</Text>
                      <Space wrap>
                        {selectionDetail.relatedServices.map((item) => item.kind === 'service' && item.resourceName && item.namespace ? (
                          <Button
                            key={item.id}
                            variant="outlined"
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
                      <Text strong className="text-xs">{localeCode === 'zh_CN' ? '后端 Pods' : 'Backend pods'}</Text>
                      <Space wrap>
                        {selectionDetail.terminalNodes.slice(0, 6).map((item) => item.resourceName && item.namespace ? (
                          <Button
                            key={item.id}
                            variant="outlined"
                            size="small"
                            onClick={() => navigate(buildPodDetailPath(item.resourceName!, namespace, item.namespace!))}
                          >
                            {item.name}
                          </Button>
                        ) : (
                          <Tag key={item.id}>{item.name}</Tag>
                        ))}
                        {selectionDetail.terminalNodes.length > 6 ? (
                          <Tag color="default">{`+${selectionDetail.terminalNodes.length - 6}`}</Tag>
                        ) : null}
                      </Space>
                    </div>
                  ) : null}

                  {selectionDetail.notes.length > 0 ? (
                    <div className="kc-topology-note-list">
                      {selectionDetail.notes.slice(0, 3).map((item) => (
                        <Text key={item} type="secondary" className="text-xs">{item}</Text>
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
            <Empty
              description={(
                <div className="flex flex-col items-center gap-1">
                  <Text strong>{emptyStateTitle}</Text>
                  <Text type="secondary" className="text-xs">{emptyStateDescription}</Text>
                </div>
              )}
            />
          </div>
        )}
      </Card>

      <Card
        className="kc-detail-card"
        title={localeCode === 'zh_CN' ? '网络拓扑明细' : 'Network topology detail'}
        extra={(
          <Text type="secondary" className="text-xs">
            {hasLiveTopology
              ? (localeCode === 'zh_CN' ? '图上做总览收敛，路由和 Pod 明细继续在这里展开' : 'The graph stays collapsed for overview, while route and pod details remain here')
              : topologyViewState === 'loading'
                ? (localeCode === 'zh_CN' ? '等待实时资源返回后再展示明细' : 'Details appear after the live resources finish loading')
                : topologyViewState === 'cluster-required'
                  ? (localeCode === 'zh_CN' ? '选择集群后再查看实时入口链路明细' : 'Select a cluster to inspect live entry-path details')
                  : (localeCode === 'zh_CN' ? '当前没有可展示的实时入口链路明细' : 'There are no live entry-path details to show in the current scope')}
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
