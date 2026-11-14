import { localeText, type LocaleCode } from '@/i18n'
import type { TopologyNode, TopologyEdge } from './types'
import { relationshipLabel } from '../../shared/resource-insights/relationship-model'

export const resolutionLabels: Record<string, string> = {
  resolved: '已解析',
  unresolved: '引用未解析',
  not_found: '资源不存在',
  not_visible: '引用不可见',
  no_ready_endpoints: '没有就绪端点',
  external: '外部后端',
  unsupported_fragment: '存在未支持配置',
  unknown: '尚未确认',
}
export const coverageLabels: Record<string, string> = {
  available: '可读取',
  partial: '部分数据',
  denied: '无读取权限',
  not_installed: 'API 未安装',
  unsupported: '版本不支持',
  error: '读取失败',
}

export function topologyEntrySummary(
  node: TopologyNode,
  nodes: TopologyNode[],
  edges: TopologyEdge[],
  localeCode: LocaleCode = 'zh_CN',
) {
  if (node.type !== 'entry' || node.resolutionState === 'not_visible') return ''
  const matches = edges.filter((edge) => edge.relation === 'matches' && edge.source === node.id)
  const ruleIDs = new Set(matches.map((edge) => edge.target))
  const hosts =
    node.protocol === 'TCP' || node.protocol === 'UDP'
      ? []
      : [
          ...(node.properties
            ?.filter((property) => ['Hostnames', 'SNI'].includes(property.name))
            .flatMap((property) => property.value.split(',').map((value) => value.trim())) ?? []),
          ...nodes
            .filter(
              (rule) =>
                rule.type === 'rule' &&
                rule.resolutionState !== 'not_visible' &&
                ruleIDs.has(rule.id),
            )
            .flatMap(
              (rule) =>
                rule.rule?.matchGroups?.flatMap((group) =>
                  group
                    .filter((match) => ['host', 'sni'].includes(match.type))
                    .map((match) => match.value),
                ) ?? [],
            ),
        ].filter((value): value is string => Boolean(value))
  if (hosts.length)
    return `${node.protocol === 'TLS' ? 'SNI' : localeText(localeCode, '域名', 'Hostnames')} · ${[...new Set(hosts)].join(', ')}`
  const ports = edges
    .filter(
      (edge) => edge.relation === 'matches' && (edge.source === node.id || edge.target === node.id),
    )
    .map((edge) => edge.port)
    .filter((port): port is string => Boolean(port))
  return ports.length
    ? `${localeText(localeCode, '监听端口', 'Listener ports')} · ${[...new Set(ports)].map((port) => `:${port}`).join(', ')}`
    : ''
}

function walk(seed: Set<string>, edges: TopologyEdge[], direction: 'up' | 'down') {
  const visited = new Set(seed)
  const queue = [...seed]
  for (let index = 0; index < queue.length; index++) {
    const current = queue[index]
    for (const edge of edges) {
      if ((direction === 'up' ? edge.target : edge.source) !== current) continue
      const next = direction === 'up' ? edge.source : edge.target
      if (!visited.has(next)) {
        visited.add(next)
        queue.push(next)
      }
    }
  }
  return visited
}

export function connectedPath(seed: Set<string>, edges: TopologyEdge[]) {
  return new Set([...walk(seed, edges, 'up'), ...walk(seed, edges, 'down')])
}

export function filterTopology(
  nodes: TopologyNode[],
  edges: TopologyEdge[],
  filter: { provider: string; protocol: string; keyword: string; issues: boolean; focus: string },
) {
  const keyword = filter.keyword.trim().toLowerCase()
  const seeds = new Set(
    nodes
      .filter(
        (node) =>
          (!filter.provider || node.provider === filter.provider) &&
          (!filter.protocol || node.protocol === filter.protocol) &&
          (!filter.issues || !['resolved', 'external'].includes(node.resolutionState)) &&
          (!keyword ||
            [
              node.label,
              node.resource?.namespace,
              node.resource?.kind,
              node.rule?.matchSummary,
              ...(node.rule?.matchGroups?.flatMap((group) =>
                group.flatMap((match) => [match.type, match.name, match.operator, match.value]),
              ) ?? []),
              ...(node.properties?.map((property) => property.value) ?? []),
            ].some((value) => value?.toLowerCase().includes(keyword))),
      )
      .map((node) => node.id),
  )
  let visible = connectedPath(seeds, edges)
  if (filter.focus && nodes.some((node) => node.id === filter.focus)) {
    const focus = connectedPath(new Set([filter.focus]), edges)
    visible = new Set([...visible].filter((id) => focus.has(id)))
  }
  return {
    nodes: nodes.filter((node) => visible.has(node.id)),
    edges: edges.filter((edge) => visible.has(edge.source) && visible.has(edge.target)),
  }
}

export function edgeLabel(edge: TopologyEdge, localeCode: LocaleCode = 'zh_CN', outgoing = true) {
  return [
    relationshipLabel(edge.relation, localeCode, outgoing),
    edge.protocol,
    edge.port && `:${edge.port}`,
    edge.sectionName && `SectionName ${edge.sectionName}`,
    edge.weight !== undefined &&
      `${localeText(localeCode, '权重', 'Weight')} ${edge.weight}${edge.totalWeight !== undefined ? `/${edge.totalWeight}` : ''}`,
    edge.mirrorPercent !== undefined &&
      `${localeText(localeCode, '镜像', 'Mirror')} ${edge.mirrorPercent}%`,
  ]
    .filter((value) => value !== undefined && value !== false && value !== '')
    .join(' · ')
}

export function topologyResourcePath(node: TopologyNode, clusterId: string) {
  const resource = node.resource
  if (
    !resource?.name ||
    resource.clusterId !== clusterId ||
    !['resolved', 'external', 'no_ready_endpoints', 'unsupported_fragment', 'unresolved'].includes(
      node.resolutionState,
    )
  )
    return undefined
  const paths: Record<string, string> = {
    Service: '/network/services',
    Pod: '/workloads/pods',
    Ingress: '/network/ingresses',
    HTTPRoute: '/network/gateway-api/httproutes',
    GRPCRoute: '/network/gateway-api/grpcroutes',
    Gateway: '/network/gateway-api/gateways',
    EndpointSlice: '/network/endpointslices',
  }
  const prefix = paths[resource.kind]
  if (!prefix) return undefined
  const params = new URLSearchParams({ clusterId })
  if (resource.namespace) params.set('namespace', resource.namespace)
  return `${prefix}/${encodeURIComponent(resource.name)}?${params}`
}

export function topologyResourceEditPath(node: TopologyNode, clusterId: string) {
  if (
    !node.resource?.namespace ||
    node.resource.clusterId !== clusterId ||
    !['Service', 'Ingress', 'Gateway', 'HTTPRoute', 'GRPCRoute', 'EndpointSlice'].includes(
      node.resource.kind,
    )
  )
    return undefined
  const path = topologyResourcePath(node, clusterId)
  return path ? `${path}&tab=yaml` : undefined
}
