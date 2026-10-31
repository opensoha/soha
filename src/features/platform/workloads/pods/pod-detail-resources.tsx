import { AdminTable } from '@/components/admin-table'
import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { buildKubernetesResourcePath } from '@/features/platform/shared/resource-ref'
import { resourceInsightQueries } from '@/features/platform/shared/resource-insights/queries'
import { localizeRelatedRelation } from '@/features/platform/workloads-model'
import { useI18n } from '@/i18n'
import type { PodDetail, ScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import {
  ArrowDownOutlined,
  ArrowLeftOutlined,
  ArrowRightOutlined,
  CloseOutlined,
  ReloadOutlined,
} from '@ant-design/icons'
import type { KubernetesResourceGraph } from '@opensoha/contracts/gen/ts/sohaapi'
import { useQuery } from '@tanstack/react-query'
import { Alert, Button, Card, Segmented, Space, Spin, Switch, Tooltip, Typography } from 'antd'
import { Fragment, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'

type GraphNode = KubernetesResourceGraph['nodes'][number]
export type PodRelationNode = GraphNode & {
  relation: string
  evidence: string
  details?: string[]
  relations?: string[]
}
const controllers = new Set([
  'Deployment',
  'ReplicaSet',
  'StatefulSet',
  'DaemonSet',
  'Job',
  'CronJob',
  'ReplicationController',
])
const validName = (name?: string) => Boolean(name?.trim() && !['-', '—'].includes(name.trim()))
const identity = (node: GraphNode) =>
  `${node.resource.kind}:${node.resource.namespace || ''}:${node.resource.name}`

export function buildPodRelationships(
  pod: PodDetail,
  clusterId: string,
  graph?: KubernetesResourceGraph,
) {
  const make = (
    kind: string,
    name: string,
    namespace: string | undefined,
    relation: string,
    evidence: string,
  ): PodRelationNode => ({
    id: `pod-detail:${kind}:${namespace || ''}:${name}`,
    resource: {
      kind,
      name,
      namespace,
      clusterId,
      apiVersion: 'unknown',
      scopeMode: namespace ? 'namespace' : 'cluster',
    },
    relation,
    evidence,
  })
  const graphNodes = graph?.nodes ?? []
  const graphRoot = graphNodes.find(
    (node) =>
      node.id === graph?.rootId &&
      node.resource.kind === 'Pod' &&
      node.resource.name === pod.name &&
      node.resource.namespace === pod.namespace,
  )
  const root: PodRelationNode = {
    ...(graphRoot || make('Pod', pod.name, pod.namespace, 'current', 'Pod')),
    status: pod.phase,
    relation: 'current',
    evidence: 'Pod',
  }
  const ancestors: PodRelationNode[] = []
  const seen = new Set([root.id])
  let child = graphRoot
  while (child) {
    const parentEdge = graph?.edges.find(
      (edge) => edge.targetId === child!.id && edge.relation === 'owns' && !seen.has(edge.sourceId),
    )
    const parent = graphNodes.find((node) => node.id === parentEdge?.sourceId)
    if (!parent) break
    seen.add(parent.id)
    ancestors.unshift({ ...parent, relation: 'owns', evidence: 'resource-graph' })
    child = parent
  }
  if (!ancestors.length) {
    const owners = (pod.relatedResources ?? []).filter(
      (item) =>
        controllers.has(item.kind) &&
        validName(item.name) &&
        (!item.namespace || item.namespace === pod.namespace) &&
        item.relations?.includes('owner'),
    )
    // The detail API supplies actual OwnerReferences for controllers omitted by the graph.
    if (owners.length === 1) {
      const owner = owners[0]
      ancestors.push({
        ...make(
          owner.kind,
          owner.name,
          owner.namespace || pod.namespace,
          'owns',
          'pod-related-resources',
        ),
        details: owner.details,
        relations: owner.relations,
      })
    }
  }
  const references = new Map<string, PodRelationNode>()
  for (const edge of graph?.edges ?? []) {
    if (!graphRoot || ![edge.sourceId, edge.targetId].includes(graphRoot.id)) continue
    const node = graphNodes.find(
      (item) => item.id === (edge.sourceId === graphRoot.id ? edge.targetId : edge.sourceId),
    )
    if (
      !node ||
      seen.has(node.id) ||
      controllers.has(node.resource.kind) ||
      node.resource.kind === 'Node'
    )
      continue
    references.set(identity(node), { ...node, relation: edge.relation, evidence: 'resource-graph' })
  }
  for (const item of pod.relatedResources ?? []) {
    if (controllers.has(item.kind) || item.kind === 'Node' || !validName(item.name)) continue
    const node = make(
      item.kind,
      item.name,
      item.namespace || pod.namespace,
      item.relations?.[0] || 'references',
      'pod-related-resources',
    )
    const existing = references.get(identity(node))
    references.set(identity(node), {
      ...(existing || node),
      details: item.details,
      relations: item.relations,
    })
  }
  // These relationships come from the requested Pod's actual spec, even when graph collection omits them.
  if (validName(pod.serviceAccountName)) {
    const account = make(
      'ServiceAccount',
      pod.serviceAccountName!,
      pod.namespace,
      'service-account',
      'pod.serviceAccountName',
    )
    if (!references.has(identity(account))) references.set(identity(account), account)
  }
  const graphNode = graphNodes.find(
    (node) => node.resource.kind === 'Node' && node.resource.name === pod.nodeName,
  )
  const node = validName(pod.nodeName)
    ? {
        ...(graphNode || make('Node', pod.nodeName!, undefined, 'scheduled-on', 'pod.nodeName')),
        relation: 'scheduled-on',
        evidence: 'pod.nodeName',
      }
    : undefined
  const deployment = ancestors.find((item) => item.resource.kind === 'Deployment')
  const otherReplicaSets = deployment
    ? graphNodes
        .filter(
          (item) =>
            item.resource.kind === 'ReplicaSet' &&
            !seen.has(item.id) &&
            graph?.edges.some(
              (edge) =>
                edge.relation === 'owns' &&
                edge.sourceId === deployment.id &&
                edge.targetId === item.id,
            ),
        )
        .map((item) => ({ ...item, relation: 'owns', evidence: 'resource-graph' }))
    : []
  const order = ['ConfigMap', 'Secret', 'ServiceAccount', 'PersistentVolumeClaim', 'Service']
  const refs = [...references.values()].sort((a, b) => {
    const rank = (kind: string) => (order.includes(kind) ? order.indexOf(kind) : order.length)
    return (
      rank(a.resource.kind) - rank(b.resource.kind) ||
      a.resource.name.localeCompare(b.resource.name)
    )
  })
  return { root, ancestors, node, references: refs, otherReplicaSets }
}

export function PodDetailResources({
  pod,
  scope,
  active,
}: {
  pod: PodDetail
  scope: ScopeKey
  active: boolean
}) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const navigate = useNavigate()
  const lastSelection = useRef<HTMLElement | null>(null)
  const viewControl = useRef<HTMLDivElement | null>(null)
  const [view, setView] = useState('graph')
  const [includeOther, setIncludeOther] = useState(false)
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const options = resourceInsightQueries.graph(scope, 'Pod', pod.name)
  const query = useQuery({ ...options, enabled: Boolean(options.enabled && active) })
  const graph = readableQueryData(query)
  const model = useMemo(
    () => buildPodRelationships(pod, scope.clusterId || '', graph),
    [pod, scope.clusterId, graph],
  )
  const all: PodRelationNode[] = [
    ...model.ancestors,
    ...(model.node ? [model.node] : []),
    ...model.references,
    ...(includeOther ? model.otherReplicaSets : []),
  ]
  const selected = [model.root, ...all].find((item) => item.id === selectedId)
  useEffect(() => {
    setSelectedId(null)
    setIncludeOther(false)
  }, [scope.clusterId, scope.namespace, pod.name])
  const relationLabel = (item: PodRelationNode) =>
    item.relation === 'current'
      ? zh
        ? '当前 Pod'
        : 'Current pod'
      : item.relation === 'scheduled-on'
        ? zh
          ? '调度到'
          : 'Scheduled on'
        : item.relation === 'service-account'
          ? zh
            ? '服务账号'
            : 'Service account'
          : localizeRelatedRelation(item.relation, localeCode)
  const sourceLabel = (item: PodRelationNode) =>
    item.evidence === 'resource-graph'
      ? zh
        ? '资源关系图'
        : 'Resource graph'
      : item.evidence === 'pod-related-resources'
        ? zh
          ? 'Pod 关联信息'
          : 'Pod related resources'
        : item.evidence === 'pod.nodeName'
          ? zh
            ? 'Pod 节点分配'
            : 'Pod node assignment'
          : item.evidence === 'pod.serviceAccountName'
            ? zh
              ? 'Pod 服务账号'
              : 'Pod service account'
            : zh
              ? 'Pod 详情'
              : 'Pod detail'
  const detailPath = selected ? buildKubernetesResourcePath(selected.resource) : null
  const scopedPath = detailPath
    ? (() => {
        const [path, search] = detailPath.split('?')
        const params = new URLSearchParams(search)
        params.set('clusterId', scope.clusterId || '')
        return `${path}?${params}`
      })()
    : null
  const resource = (item: PodRelationNode, current = false) => (
    <button
      key={item.id}
      type="button"
      aria-label={`${item.resource.kind} ${item.resource.name}`}
      aria-pressed={selectedId === item.id}
      onClick={(event) => {
        lastSelection.current = event.currentTarget
        setSelectedId(item.id)
      }}
      className={`soha-pod-relation-resource${current ? ' is-current' : ''}${selectedId === item.id ? ' is-selected' : ''}`}
    >
      <span>{item.resource.kind}</span>
      <strong>{item.resource.name}</strong>
      {current && <StatusTag value={pod.phase} />}
    </button>
  )
  return (
    <div className="soha-pod-relations-workspace">
      <Card
        className="soha-pod-detail-card soha-pod-relations-card"
        title={zh ? '关联资源' : 'Related resources'}
        extra={
          <div
            ref={viewControl}
            tabIndex={-1}
            role="group"
            aria-label={zh ? '关联资源视图切换' : 'Resource view switch'}
          >
            <Segmented
              aria-label={zh ? '关联资源视图' : 'Related resource view'}
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
        <ReadFeedback query={query} label={zh ? '资源关系' : 'Resource relationships'} />
        <div className="soha-pod-relations-toolbar">
          <Typography.Text type="secondary">
            {zh
              ? '当前 Pod 的直接关联与所属工作负载'
              : 'Direct pod relationships and owning workload'}
          </Typography.Text>
          <Space size={12}>
            <Tooltip
              title={
                zh
                  ? '同一 Deployment 下除当前所属之外的 ReplicaSets，不根据名称推断版本新旧'
                  : 'Other ReplicaSets owned by the same Deployment; revision age is not inferred from names'
              }
            >
              <label className="soha-pod-relations-toggle">
                {zh ? '其他 ReplicaSets' : 'Other ReplicaSets'}{' '}
                <Switch
                  aria-label={zh ? '其他 ReplicaSets' : 'Other ReplicaSets'}
                  size="small"
                  checked={includeOther}
                  onChange={setIncludeOther}
                />
              </label>
            </Tooltip>
            <Button
              type="text"
              size="small"
              icon={<ReloadOutlined />}
              loading={query.isFetching}
              aria-label={zh ? '刷新资源关系' : 'Refresh resource relationships'}
              onClick={() => void query.refetch()}
            />
          </Space>
        </div>
        {graph?.warnings?.length ? (
          <Alert type="warning" showIcon title={graph.warnings.join(' · ')} />
        ) : null}
        {view === 'graph' ? (
          <div
            className="soha-pod-relations-board"
            role="group"
            aria-label={zh ? '当前 Pod 资源关系图' : 'Current pod resource relationships'}
          >
            {query.isLoading ? <Spin /> : null}
            {model.ancestors.length ? (
              <>
                <div className="soha-pod-relation-owners">
                  {model.ancestors.map((item, index) => (
                    <Fragment key={item.id}>
                      {index > 0 && (
                        <span className="soha-pod-relation-connector">
                          {zh ? '拥有' : 'Owns'} <ArrowRightOutlined />
                        </span>
                      )}
                      {resource(item)}
                    </Fragment>
                  ))}
                </div>
                <div className="soha-pod-relation-vertical">
                  {zh ? '控制' : 'Controls'} <ArrowDownOutlined />
                </div>
              </>
            ) : (
              <Typography.Text type="secondary" className="soha-pod-relation-owner-empty">
                {query.isLoading
                  ? zh
                    ? '正在读取所属关系…'
                    : 'Loading ownership…'
                  : query.isError
                    ? zh
                      ? '所属关系暂不可用'
                      : 'Ownership is unavailable'
                    : zh
                      ? '暂无所属工作负载关系数据'
                      : 'No owning workload relationship returned'}
              </Typography.Text>
            )}
            <div className="soha-pod-relation-middle">
              <div className="soha-pod-relation-scheduling">
                {model.node && (
                  <>
                    {resource(model.node)}
                    <span className="soha-pod-relation-connector">
                      {zh ? '调度到' : 'Scheduled on'} <ArrowLeftOutlined />
                    </span>
                  </>
                )}
              </div>
              {resource(model.root, true)}
              <span />
            </div>
            {model.references.length ? (
              <>
                <div className="soha-pod-relation-vertical">
                  {zh ? '关联' : 'Related'} <ArrowDownOutlined />
                </div>
                <div className="soha-pod-relation-references">
                  {model.references.map((item) => resource(item))}
                </div>
              </>
            ) : null}
            {includeOther && (
              <div className="soha-pod-relation-other">
                <Typography.Text type="secondary">
                  {zh ? '其他 ReplicaSets' : 'Other ReplicaSets'}
                </Typography.Text>
                {model.otherReplicaSets.length ? (
                  <div className="soha-pod-relation-references">
                    {model.otherReplicaSets.map((item) => resource(item))}
                  </div>
                ) : (
                  <Typography.Text type="secondary">
                    {zh
                      ? '当前图数据未返回其他 ReplicaSets'
                      : 'No other ReplicaSets returned in this graph'}
                  </Typography.Text>
                )}
              </div>
            )}
          </div>
        ) : (
          <AdminTable
            dataSource={all}
            rowKey={(item) => item.id}
            pagination={false}
            enableColumnSelection={false}
            columns={[
              {
                title: zh ? '资源类型' : 'Kind',
                width: 160,
                render: (_: unknown, item: PodRelationNode) => item.resource.kind,
              },
              {
                title: zh ? '名称' : 'Name',
                render: (_: unknown, item: PodRelationNode) => (
                  <Button
                    type="link"
                    className="soha-pod-relation-list-name"
                    onClick={(event) => {
                      lastSelection.current = event.currentTarget
                      setSelectedId(item.id)
                    }}
                  >
                    {item.resource.name}
                  </Button>
                ),
              },
              {
                title: zh ? '关系' : 'Relationship',
                width: 170,
                render: (_: unknown, item: PodRelationNode) => relationLabel(item),
              },
              {
                title: zh ? '依据' : 'Source',
                width: 180,
                render: (_: unknown, item: PodRelationNode) => sourceLabel(item),
              },
            ]}
            empty={
              <ManagementState
                compact
                title={zh ? '暂无关联资源' : 'No related resources'}
                description={
                  zh
                    ? '该 Pod 尚未返回关联资源。'
                    : 'No related resources have been returned for this pod.'
                }
              />
            }
          />
        )}
      </Card>
      {selected && (
        <Card
          className="soha-pod-detail-card soha-pod-relation-inspector"
          title={zh ? '选中资源' : 'Selected resource'}
          extra={
            <Button
              type="text"
              icon={<CloseOutlined />}
              aria-label={zh ? '关闭资源信息' : 'Close resource information'}
              onClick={() => {
                if (lastSelection.current?.isConnected) lastSelection.current.focus()
                else viewControl.current?.focus()
                setSelectedId(null)
              }}
            />
          }
        >
          <div>
            <Typography.Text type="secondary">{selected.resource.kind}</Typography.Text>
            <strong>{selected.resource.name}</strong>
            <Typography.Text type="secondary">
              {selected.resource.namespace || (zh ? '集群级资源' : 'Cluster scoped')} ·{' '}
              {selected.relations?.length
                ? selected.relations
                    .map((relation) => localizeRelatedRelation(relation, localeCode))
                    .join(' · ')
                : relationLabel(selected)}
            </Typography.Text>
            <Typography.Text type="secondary">{sourceLabel(selected)}</Typography.Text>
            {selected.details?.map((detail, index) => (
              <Typography.Text key={index}>{detail}</Typography.Text>
            ))}
          </div>
          <Button
            disabled={!scopedPath}
            onClick={() => {
              if (scopedPath) navigate(scopedPath)
            }}
          >
            {zh ? '查看详情' : 'View details'}
          </Button>
        </Card>
      )}
    </div>
  )
}
