import { useState, type ReactNode } from 'react'
import { Button, Segmented, Typography } from 'antd'
import { AdminTable } from '@/components/admin-table'
import { ManagementState } from '@/components/management-list'
import { MetadataTag } from '@/components/status-tag'
import { formatDateTime } from '@/utils/time'
import type { WorkloadRelation } from '@/types'
import type { ResourceRelationNode, ResourceRelationsView } from '../domain-types'
import './resource-relation-workbench.css'

export const relationRoleLabels = {
  root: '配置根',
  owned: '拥有',
  manual: '人工关联',
  reference: '共享引用',
  candidate: '匹配候选',
}
const edgeLabels = {
  owns: '拥有',
  manual: '人工关联',
  references: '引用',
  selector_candidate: '匹配候选',
}
const entryKinds = new Set(['Service', 'Ingress', 'Gateway', 'HTTPRoute', 'GRPCRoute'])
const configKinds = new Set(['ConfigMap', 'Secret', 'PersistentVolumeClaim'])

export type RelationWorkbenchData = Pick<ResourceRelationsView, 'root' | 'nodes' | 'edges'> & {
  observedAt?: string
}

export type RelationDisplayNode = {
  key: string
  resource: ResourceRelationNode['resource']
  role: ResourceRelationNode['role']
  depth: number
  basis?: string
}

export function relationDisplayNodes(
  data: RelationWorkbenchData,
  references: WorkloadRelation[] = [],
): RelationDisplayNode[] {
  const nodes = data.nodes.map((node) => ({
    ...node,
    key:
      node.resource.uid ||
      JSON.stringify([
        node.resource.clusterId,
        node.resource.namespace,
        node.resource.kind,
        node.resource.name,
      ]),
  }))
  const result: RelationDisplayNode[] = [...nodes]
  for (const ref of references) {
    const namespace = ref.namespace || data.root.namespace
    if (
      result.some(
        (node) =>
          node.resource.kind === ref.kind &&
          node.resource.name === ref.name &&
          node.resource.namespace === namespace,
      )
    )
      continue
    result.push({
      key: JSON.stringify([data.root.clusterId, namespace, ref.kind, ref.name]),
      resource: {
        clusterId: data.root.clusterId,
        namespace,
        kind: ref.kind,
        name: ref.name,
        apiVersion: '',
        scopeMode: 'namespace',
      },
      role: 'reference',
      depth: 0,
      basis: ref.relation || '工作负载运行快照中的引用；未提供引用对象 UID',
    })
  }
  return result
}

export function ResourceRelationWorkbench({
  data,
  references,
  renderActions,
  runtimeSnapshot = false,
}: {
  runtimeSnapshot?: boolean
  data: RelationWorkbenchData
  references?: WorkloadRelation[]
  renderActions: (node: RelationDisplayNode) => ReactNode
}) {
  const [mode, setMode] = useState('graph')
  const [selectedKey, setSelectedKey] = useState('')
  const nodes = relationDisplayNodes(data, references)
  const selected = selectedKey
    ? nodes.find((node) => node.key === selectedKey)
    : nodes.find((node) => node.role === 'root') || nodes[0]
  const entries = nodes.filter((node) => entryKinds.has(node.resource.kind) && node.role !== 'root')
  const configuration = nodes.filter(
    (node) => configKinds.has(node.resource.kind) && node.role !== 'root',
  )
  const runtime = nodes.filter((node) => !entries.includes(node) && !configuration.includes(node))
  const levels = [...new Set(runtime.map((node) => node.depth))].sort((a, b) => a - b)
  const roleLabel = (node: RelationDisplayNode) =>
    runtimeSnapshot && node.role === 'reference' && node.resource.kind === 'Pod'
      ? '运行快照关联'
      : relationRoleLabels[node.role]
  const evidence = (node: RelationDisplayNode) => {
    const incoming = node.resource.uid
      ? data.edges.filter((edge) => edge.toUid === node.resource.uid)
      : []
    return (
      node.basis ||
      incoming
        .map((edge) => {
          const from = nodes.find((item) => item.resource.uid === edge.fromUid)
          return `${from?.resource.kind || ''} / ${from?.resource.name || edge.fromUid} → ${edgeLabels[edge.type]}${edge.ruleId ? ` · ${edge.ruleId}` : ''}`
        })
        .join('；') ||
      (node.role === 'root'
        ? node.resource.uid
          ? '已登记的配置根，按 UID 核验'
          : '当前工作负载运行快照；未提供配置根 UID'
        : roleLabel(node))
    )
  }
  const nodeButton = (node: RelationDisplayNode) => (
    <button
      type="button"
      key={node.key}
      className="soha-relation-node"
      data-role={node.role}
      aria-label={`选择 ${node.resource.kind} ${node.resource.name}`}
      aria-pressed={selected?.key === node.key}
      onClick={() => setSelectedKey(node.key)}
    >
      <span>{node.resource.kind}</span>
      <strong>{node.resource.name}</strong>
      <small>{roleLabel(node)}</small>
      {node.role !== 'root' ? <small>{evidence(node)}</small> : null}
    </button>
  )
  const group = (title: string, items: RelationDisplayNode[], description: string) => (
    <section className="soha-relation-group" aria-label={title}>
      <div className="soha-relation-group__heading">
        <strong>{title}</strong>
        <span>{description}</span>
      </div>
      {items.length ? (
        <div className="soha-relation-reference-grid">{items.map(nodeButton)}</div>
      ) : (
        <Typography.Text type="secondary">本次读取未发现这类关联；覆盖情况见上方。</Typography.Text>
      )}
    </section>
  )
  return (
    <div className="soha-relation-workbench">
      <div className="soha-relation-canvas">
        <header className="soha-relation-canvas__header">
          <div>
            <strong>运行资源关系</strong>
            <small>选择资源查看身份、关联依据与排障入口</small>
          </div>
          <Segmented
            aria-label="资源关系视图"
            value={mode}
            onChange={(value) => setMode(String(value))}
            options={[
              { value: 'graph', label: '关系图' },
              { value: 'list', label: '资源列表' },
            ]}
          />
        </header>
        {mode === 'graph' ? (
          <>
            {group('流量入口', entries, '路由、Service 与工作负载的已知关联')}
            <section className="soha-relation-group" aria-label="运行资源主链">
              <div className="soha-relation-group__heading">
                <strong>配置根与运行资源</strong>
                <span>拥有 / 人工关联 / 候选按实际证据区分</span>
              </div>
              <div className="soha-relation-levels">
                {levels.map((level) => (
                  <div className="soha-relation-level" key={level}>
                    {runtime.filter((node) => node.depth === level).map(nodeButton)}
                  </div>
                ))}
              </div>
            </section>
            {group('配置与存储', configuration, 'ConfigMap / Secret / PVC · 引用不代表拥有')}
          </>
        ) : (
          <AdminTable
            rowKey="key"
            enableColumnSelection={false}
            dataSource={nodes}
            pageSize={10}
            columns={[
              {
                title: '资源',
                key: 'name',
                render: (_: unknown, node: RelationDisplayNode) => (
                  <Button type="link" onClick={() => setSelectedKey(node.key)}>
                    {node.resource.name}
                  </Button>
                ),
              },
              {
                title: '类型',
                key: 'kind',
                render: (_: unknown, node: RelationDisplayNode) => node.resource.kind,
              },
              {
                title: '关系',
                key: 'role',
                render: (_: unknown, node: RelationDisplayNode) => relationRoleLabels[node.role],
              },
              {
                title: '关联依据',
                key: 'evidence',
                render: (_: unknown, node: RelationDisplayNode) => evidence(node),
              },
            ]}
          />
        )}
        <footer className="soha-relation-canvas__footer">
          <span>实线节点：配置根 / 拥有 · 虚线节点：引用 / 人工关联 / 候选</span>
          <span>
            {data.observedAt
              ? `${formatDateTime(data.observedAt)} 观察`
              : '运行快照未提供独立观察时间'}
          </span>
        </footer>
      </div>
      <aside className="soha-relation-detail" aria-label="所选资源详情">
        {selected ? (
          <>
            <header>
              <strong>资源详情</strong>
              <MetadataTag label={roleLabel(selected)} />
            </header>
            <span className="soha-relation-detail__kind">{selected.resource.kind}</span>
            <h3>{selected.resource.name}</h3>
            <dl>
              <dt>集群</dt>
              <dd>{selected.resource.clusterId}</dd>
              <dt>Namespace</dt>
              <dd>{selected.resource.namespace || '—'}</dd>
              <dt>API 版本</dt>
              <dd>{selected.resource.apiVersion || '引用快照未提供'}</dd>
              <dt>UID</dt>
              <dd>{selected.resource.uid || '按名称引用，未固定 UID'}</dd>
            </dl>
            <section>
              <strong>关联依据</strong>
              <p>{evidence(selected)}</p>
            </section>
            <div className="soha-relation-detail__actions">{renderActions(selected)}</div>
          </>
        ) : (
          <ManagementState
            compact
            kind="not-found"
            title="所选资源不在本次关系范围"
            description="可能已删除或权限已变化，请重新选择；不会自动切换到同名新资源。"
          />
        )}
      </aside>
    </div>
  )
}
