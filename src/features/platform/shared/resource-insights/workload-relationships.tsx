import { AdminTable } from '@/components/admin-table'
import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { buildKubernetesResourcePath } from '@/features/platform/shared/resource-ref'
import { useI18n } from '@/i18n'
import { useAuthStore } from '@/stores/auth-store'
import type { ScopeKey } from '@/types'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { CloseOutlined, ReloadOutlined } from '@ant-design/icons'
import type { KubernetesResourceGraph } from '@opensoha/contracts/gen/ts/sohaapi'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, Button, Card, Segmented, Spin, Switch, Typography } from 'antd'
import {
  lazy,
  Suspense,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import { useNavigate } from 'react-router-dom'
import type { ResourceGraphRootAPI } from './api'
import { resourceInsightQueries } from './queries'
import {
  accessControlRelationshipKinds,
  graphMatches,
  relationshipFacts,
  type RelationshipEdge,
  networkRelationshipKinds,
  podRelationshipGraph,
  relationshipLabel,
  relationshipResourceDescriptors,
  relationshipResourceDescriptor,
  storageRelationshipKinds,
  type RelationshipNode,
  type RelationshipRootKind,
} from './relationship-model'
import './workload-relationships.css'

const Canvas = lazy(() => import('./workload-relationship-canvas'))
export function WorkloadRelationships({
  kind,
  name,
  scope,
  active = true,
  controls,
  filterGraph,
  rootAPI,
  resolveCustomDetailPath,
}: {
  kind: string
  name: string
  scope: ScopeKey
  active?: boolean
  controls?: ReactNode
  filterGraph?: (graph: KubernetesResourceGraph) => KubernetesResourceGraph
  rootAPI?: ResourceGraphRootAPI
  resolveCustomDetailPath?: (resource: RelationshipNode['resource']) => string | null
}) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const networkRoot = !rootAPI && networkRelationshipKinds.some((networkKind) => networkKind === kind)
  const storageRoot = !rootAPI && storageRelationshipKinds.some((storageKind) => storageKind === kind)
  const accessControlRoot = !rootAPI && accessControlRelationshipKinds.some((value) => value === kind)
  const showNamespaces = storageRoot || accessControlRoot || Boolean(rootAPI)
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const permission = rootAPI
    ? 'platform.extensions.view'
    : relationshipResourceDescriptors[kind as RelationshipRootKind]?.permission
  const canRead = Boolean(permission && hasPermission(permissions, permission))
  const sessionGeneration = useAuthStore((state) => state.sessionGeneration)
  const options = resourceInsightQueries.graph(scope, kind, name, sessionGeneration, rootAPI)
  const queryIdentity = JSON.stringify(options.queryKey)
  // Keep a rejection isolated to its root until an explicit read succeeds.
  const [deniedReads, setDeniedReads] = useState<Record<string, { error: Error | null }>>({})
  const denial = deniedReads[queryIdentity]
  const canRequest = Boolean(options.enabled && active && canRead)
  const query = useQuery({
    ...options,
    enabled: canRequest && !denial,
    staleTime: 30_000,
    retry: (failureCount, error) => !queryAccessDenied({ error }) && failureCount < 3,
  })
  const client = useQueryClient()
  const navigate = useNavigate()
  const [selection, setSelection] = useState<{ identity: string; id: string } | null>(null)
  const [view, setView] = useState('graph')
  const [includeOther, setIncludeOther] = useState(false)
  const workspace = useRef<HTMLDivElement>(null)
  const inspector = useRef<HTMLElement>(null)
  const lastSelection = useRef<HTMLElement | null>(null)
  const restoreFocus = useRef<HTMLElement | null>(null)
  const control = useRef<HTMLDivElement>(null)
  const denied = queryAccessDenied(query)
  const purgedDenials = useRef(new WeakSet<object>())
  const [retryingIdentity, setRetryingIdentity] = useState<string | null>(null)
  const retrying = retryingIdentity === queryIdentity
  const raw = canRead && !denial?.error ? readableQueryData(query) : undefined
  const feedbackQuery = denial?.error
    ? { ...query, error: denial.error, isError: true, refetch: () => refresh() }
    : { ...query, refetch: () => refresh() }
  const refresh = async () => {
    if (!canRequest || retrying) return
    setRetryingIdentity(queryIdentity)
    try {
      const result = await query.refetch()
      if (queryAccessDenied(result)) {
        setDeniedReads((previous) => ({ ...previous, [queryIdentity]: { error: result.error } }))
      } else if (result.isSuccess && denial) {
        setDeniedReads((previous) => {
          const next = { ...previous }
          delete next[queryIdentity]
          return next
        })
      } else if (denial) {
        setDeniedReads((previous) => ({ ...previous, [queryIdentity]: { error: null } }))
      }
    } finally {
      setRetryingIdentity((current) => (current === queryIdentity ? null : current))
    }
  }
  const graph = useMemo(() => {
    if (!raw || !graphMatches(raw, kind, name, scope.clusterId, scope.namespace, rootAPI?.apiVersion))
      return undefined
    const filtered = kind === 'Pod' && !rootAPI ? podRelationshipGraph(raw, includeOther) : raw
    return filterGraph ? filterGraph(filtered) : filtered
  }, [raw, kind, name, scope.clusterId, scope.namespace, rootAPI, includeOther, filterGraph])
  const identity = JSON.stringify([
    queryIdentity,
    graph?.nodes.find((node) => node.id === graph.rootId)?.resource.uid || '',
  ])
  const selected =
    selection?.identity === identity
      ? graph?.nodes.find((node) => node.id === selection.id)
      : undefined
  const select = (id: string | null, element?: HTMLElement) => {
    lastSelection.current = element || (document.activeElement as HTMLElement)
    setSelection(id ? { identity, id } : null)
  }
  const close = () => {
    if (lastSelection.current?.isConnected && !inspector.current?.contains(lastSelection.current))
      restoreFocus.current = lastSelection.current
    else if (selected) {
      const node = [
        ...(workspace.current?.querySelectorAll<HTMLElement>('[data-resource-id], [data-id]') ||
          []),
      ].find((el) => el.dataset.resourceId === selected.id || el.dataset.id === selected.id)
      restoreFocus.current = node || control.current
    } else restoreFocus.current = control.current
    setSelection(null)
  }
  // Restore after the inspector has unmounted so its pending focus effect cannot
  // leave focus on an element that the close commit removes.
  useLayoutEffect(() => {
    if (!selected && restoreFocus.current) {
      const target = restoreFocus.current.isConnected ? restoreFocus.current : control.current
      restoreFocus.current = null
      target?.focus({ preventScroll: true })
    }
  }, [selected])
  useEffect(() => {
    if (selected) inspector.current?.focus({ preventScroll: true })
  }, [selected?.id])
  useEffect(() => {
    if (
      selection &&
      (selection.identity !== identity || !graph?.nodes.some((node) => node.id === selection.id))
    )
      setSelection(null)
  }, [identity, graph, selection])
  useEffect(() => {
    setIncludeOther(false)
  }, [identity])
  useEffect(() => {
    if (denied && !denial && !query.isFetching)
      setDeniedReads((previous) => ({ ...previous, [queryIdentity]: { error: query.error } }))
  }, [denied, denial, query.isFetching, query.error, queryIdentity])
  useEffect(() => {
    // Run only after enabled=false has reached the observer, and never remove an
    // in-flight explicit retry or process the same cached rejection twice.
    if (denial?.error && !query.isFetching && !retrying && !purgedDenials.current.has(denial)) {
      purgedDenials.current.add(denial)
      client.removeQueries({ queryKey: options.queryKey, exact: true })
    }
  }, [denial, client, options.queryKey, query.isFetching, retrying])
  const edgesFor = (id: string) =>
    graph?.edges.filter((edge) => edge.sourceId === id || edge.targetId === id) || []
  const pathFor = (resource: RelationshipNode['resource']) => {
    if (resource.clusterId !== scope.clusterId) return null
    if (rootAPI && canRead && resolveCustomDetailPath) {
      const customPath = resolveCustomDetailPath(resource)
      if (customPath) return customPath
    }
    const descriptor = relationshipResourceDescriptor(resource)
    if (
      !descriptor ||
      !hasPermission(permissions, descriptor.permission)
    )
      return null
    const route = buildKubernetesResourcePath(resource)
    if (!route) return null
    const [pathname, search] = route.split('?')
    const params = new URLSearchParams(search)
    params.set('clusterId', resource.clusterId)
    return `${pathname}?${params}`
  }
  const facts = (edge: RelationshipEdge) => {
    const sourcePath = edge.sourceRef ? pathFor(edge.sourceRef) : null
    const values = relationshipFacts(edge, localeCode)
    if (!values.length && !edge.sourceRef) return null
    return (
      <dl className="soha-workload-relationships-edge-facts">
        {values.map(([label, value]) => (
          <div key={label}>
            <dt>{label}</dt>
            <dd>{value}</dd>
          </div>
        ))}
        {edge.sourceRef && (
          <div>
            <dt>{zh ? '声明来源' : 'Declaration source'}</dt>
            <dd>
              {sourcePath ? (
                <button type="button" onClick={() => navigate(sourcePath)}>
                  {edge.sourceRef.kind}{' '}
                  {edge.sourceRef.namespace ? `${edge.sourceRef.namespace} / ` : ''}
                  {edge.sourceRef.name}
                </button>
              ) : zh ? (
                '无可用详情入口或查看权限'
              ) : (
                'No detail route or view permission'
              )}
            </dd>
          </div>
        )}
      </dl>
    )
  }
  const neighborLabel = ({ resource }: RelationshipNode) => {
    const namespace = showNamespaces && resource.namespace ? `${resource.namespace}/` : ''
    return `${resource.kind} ${namespace}${resource.name}`
  }
  const relationText = (node: RelationshipNode) =>
    edgesFor(node.id).map((edge) => {
      const outgoing = edge.sourceId === node.id
      const other = graph?.nodes.find(
        (item) => item.id === (outgoing ? edge.targetId : edge.sourceId),
      )
      return (
        <div key={edge.id}>
          <span>
            {relationshipLabel(
              edge.relation,
              localeCode,
              outgoing,
              edge.resolutionState,
              edge.fieldPath,
            )}{' '}
            {other && neighborLabel(other)}
          </span>
          {facts(edge)}
        </div>
      )
    })
  const selectedEdges = selected ? edgesFor(selected.id) : []
  const path =
    selected && !['unresolved', 'unsupported'].includes(selected.status || '')
      ? pathFor(selected.resource)
      : null
  return (
    <Card
      className="soha-workload-relationships soha-detail-card"
      title={zh ? '关联资源' : 'Related resources'}
      extra={
        <div ref={control} tabIndex={-1}>
          <Segmented
            aria-label={zh ? '关联资源视图' : 'Resource view'}
            value={view}
            onChange={(value) => setView(String(value))}
            options={[
              { value: 'graph', label: zh ? '关系图' : 'Graph' },
              { value: 'list', label: zh ? '列表' : 'List' },
            ]}
          />
        </div>
      }
    >
      <ReadFeedback
        canRead={canRead && active}
        query={feedbackQuery}
        label={zh ? '资源关系' : 'Resource relationships'}
      />
      {!canRead ? (
        <ManagementState
          compact
          kind="no-permission"
          title={zh ? '无资源查看权限' : 'Resource view permission required'}
        />
      ) : (
        <>
          {networkRoot && (
            <Typography.Text type="secondary">
              {zh
                ? '配置引用、选择器匹配与端点观测分别展示；不代表实际流量、可达性或完整有效权限。'
                : 'Configuration references, selector matches and endpoint observations are distinct; they do not establish traffic, reachability or effective permissions.'}
            </Typography.Text>
          )}
          {accessControlRoot && (
            <Typography.Text type="secondary">
              {zh
                ? '显示已观测的绑定声明、身份引用和聚合选择器匹配。权限判断请查看「有效权限」。'
                : 'Shows observed bindings, identity references and aggregation matches. Use Effective access for permission checks.'}
            </Typography.Text>
          )}
          <div className="soha-workload-relationships-toolbar">
            {controls}
            {kind === 'Pod' && !rootAPI && (
              <label>
                <Switch
                  size="small"
                  aria-label={zh ? '包含其他 ReplicaSets' : 'Include other ReplicaSets'}
                  checked={includeOther}
                  onChange={setIncludeOther}
                />{' '}
                {zh ? '包含其他 ReplicaSets' : 'Include other ReplicaSets'}
              </label>
            )}
            <Button
              aria-label={zh ? '刷新资源关系' : 'Refresh relationships'}
              type="text"
              icon={<ReloadOutlined />}
              disabled={!canRequest}
              loading={query.isFetching || retrying}
              onClick={() => void refresh()}
            />
            {graph?.generatedAt && (
              <Typography.Text type="secondary">
                <time dateTime={graph.generatedAt}>
                  {new Date(graph.generatedAt).toLocaleString(
                    localeCode === 'zh_CN' ? 'zh-CN' : 'en-US',
                  )}
                </time>
              </Typography.Text>
            )}
          </div>
          {graph?.warnings.length ? (
            <Alert showIcon type="warning" title={graph.warnings.join(' · ')} />
          ) : null}
          {!graph ? (
            query.isLoading ? (
              <Spin />
            ) : (
              !query.isError && (
                <ManagementState
                  compact
                  title={zh ? '资源关系数据未返回' : 'Resource relationship data unavailable'}
                />
              )
            )
          ) : (
            <div
              className="soha-workload-relationships-workspace"
              ref={workspace}
              onKeyDown={(event) => {
                if (event.key === 'Escape' && selected) {
                  event.stopPropagation()
                  close()
                }
              }}
            >
              <div className="soha-workload-relationships-view">
                {view === 'graph' ? (
                  <Suspense fallback={<Spin />}>
                    <Canvas
                      graph={graph}
                      selectedId={selected?.id || null}
                      onSelect={(id) =>
                        select(
                          id,
                          [
                            ...(workspace.current?.querySelectorAll<HTMLElement>('[data-id]') ||
                              []),
                          ].find((el) => el.dataset.id === id),
                        )
                      }
                    />
                  </Suspense>
                ) : (
                  <AdminTable
                    rowKey="id"
                    dataSource={graph.nodes}
                    pagination={false}
                    enableColumnSelection={false}
                    scroll={{ x: networkRoot ? 1000 : showNamespaces ? 880 : 720, y: 560 }}
                    columns={[
                      {
                        title: zh ? '类型' : 'Kind',
                        key: 'kind',
                        width: 160,
                        render: (_: unknown, node: RelationshipNode) => node.resource.kind,
                      },
                      {
                        title: zh ? '资源' : 'Resource',
                        key: 'resource',
                        width: 230,
                        render: (_: unknown, node: RelationshipNode) => (
                          <Button
                            type="link"
                            data-resource-id={node.id}
                            aria-pressed={selected?.id === node.id}
                            onClick={(event) => select(node.id, event.currentTarget)}
                          >
                            {node.resource.name}
                            {node.id === graph.rootId ? (zh ? '（当前）' : ' (current)') : ''}
                          </Button>
                        ),
                      },
                      ...(networkRoot || showNamespaces
                        ? [
                            {
                              title: zh ? '命名空间' : 'Namespace',
                              key: 'namespace',
                              width: 160,
                              render: (_: unknown, node: RelationshipNode) =>
                                node.resource.namespace || (zh ? '集群级' : 'Cluster scoped'),
                            },
                          ]
                        : []),
                      {
                        title: zh ? '直接关系' : 'Direct relationships',
                        key: 'relations',
                        render: (_: unknown, node: RelationshipNode) =>
                          edgesFor(node.id).length ? relationText(node) : '—',
                      },
                      {
                        title: zh ? '状态' : 'Status',
                        key: 'status',
                        width: 130,
                        render: (_: unknown, node: RelationshipNode) =>
                          node.status === 'unresolved'
                            ? zh
                              ? '未解析'
                              : 'Unresolved'
                            : (networkRoot ? node.status : node.health || node.status) || '—',
                      },
                    ]}
                  />
                )}
              </div>
              {selected && (
                <aside
                  className="soha-workload-relationships-inspector"
                  aria-label={zh ? '选中资源信息' : 'Selected resource information'}
                  tabIndex={-1}
                  ref={inspector}
                >
                  <div className="soha-workload-relationships-heading">
                    <Typography.Text type="secondary">
                      {zh ? '选中资源' : 'Selected resource'}
                    </Typography.Text>
                    <Button
                      type="text"
                      icon={<CloseOutlined />}
                      aria-label={zh ? '关闭资源信息' : 'Close resource information'}
                      onClick={close}
                    />
                  </div>
                  <Typography.Text>{selected.resource.kind}</Typography.Text>
                  <strong>{selected.resource.name}</strong>
                  <dl>
                    <dt>{zh ? '命名空间' : 'Namespace'}</dt>
                    <dd>{selected.resource.namespace || (zh ? '集群级' : 'Cluster scoped')}</dd>
                    <dt>API</dt>
                    <dd>{selected.resource.apiVersion}</dd>
                    <dt>{zh ? '状态' : 'Status'}</dt>
                    <dd>
                      {selected.status === 'unresolved'
                        ? zh
                          ? '未解析：仅有引用声明'
                          : 'Unresolved: reference only'
                        : (networkRoot ? selected.status : selected.health || selected.status) ||
                          '—'}
                    </dd>
                    {selected.resource.uid && (
                      <>
                        <dt>UID</dt>
                        <dd>{selected.resource.uid}</dd>
                      </>
                    )}
                    {networkRoot && selected.health && (
                      <>
                        <dt>{zh ? '资源观测状态' : 'Observed resource state'}</dt>
                        <dd>{selected.health}</dd>
                      </>
                    )}
                  </dl>
                  <Typography.Text strong>
                    {zh ? '直接关系' : 'Direct relationships'}
                  </Typography.Text>
                  <ul>
                    {selectedEdges.map((edge) => {
                      const outgoing = edge.sourceId === selected.id
                      const other = graph.nodes.find(
                        (node) => node.id === (outgoing ? edge.targetId : edge.sourceId),
                      )
                      return (
                        <li key={edge.id}>
                          {relationshipLabel(
                            edge.relation,
                            localeCode,
                            outgoing,
                            edge.resolutionState,
                            edge.fieldPath,
                          )}{' '}
                          {other && (
                            <button
                              type="button"
                              onClick={(event) => select(other.id, event.currentTarget)}
                            >
                              {neighborLabel(other)}
                            </button>
                          )}
                          {facts(edge)}
                        </li>
                      )
                    })}
                  </ul>
                  <Button
                    disabled={!path}
                    onClick={() => {
                      if (path) navigate(path)
                    }}
                  >
                    {zh ? '查看详情' : 'View details'}
                  </Button>
                  {!path && (
                    <Typography.Text type="secondary">
                      {zh
                        ? '引用未解析、无查看权限或暂无详情入口'
                        : 'Unresolved reference, no view permission, or no detail page'}
                    </Typography.Text>
                  )}
                </aside>
              )}
            </div>
          )}
        </>
      )}
    </Card>
  )
}
