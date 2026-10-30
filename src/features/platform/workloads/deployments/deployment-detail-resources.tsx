import { AdminTable } from '@/components/admin-table'
import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { resourceInsightQueries } from '@/features/platform/shared/resource-insights/queries'
import { buildKubernetesResourcePath } from '@/features/platform/shared/resource-ref'
import { localizeRelatedRelation } from '@/features/platform/workloads-model'
import { useI18n } from '@/i18n'
import type { ScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { ArrowDownOutlined, CloseOutlined, ReloadOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { Alert, Button, Card, Segmented, Switch, Typography } from 'antd'
import { useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import type {
  buildDeploymentRelationships,
  DeploymentRelationNode,
} from './deployment-detail-model'

export function DeploymentDetailResources({
  scope,
  name,
  model,
  onShowPods,
}: {
  scope: ScopeKey
  name: string
  model: ReturnType<typeof buildDeploymentRelationships>
  onShowPods: () => void
}) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const navigate = useNavigate()
  const [view, setView] = useState('graph')
  const [history, setHistory] = useState(false)
  const [selection, setSelection] = useState<string | null>(null)
  const lastSelection = useRef<HTMLElement | null>(null)
  const control = useRef<HTMLDivElement | null>(null)
  const query = useQuery(resourceInsightQueries.graph(scope, 'Deployment', name))
  const graph = readableQueryData(query)
  const sets = model.sets.filter((set) => history || !set.historical)
  const all: DeploymentRelationNode[] = [
    model.root,
    ...sets.flatMap((set) => [set.node, ...set.pods]),
    ...model.references,
  ]
  const selected = all.find((node) => node.id === selection)
  const permissionKeys: Record<string, string> = {
    Deployment: 'platform.deployment.view',
    ReplicaSet: 'platform.workloads.replica-sets.view',
    Pod: 'platform.pods.view',
    Service: 'platform.network.services.view',
    ConfigMap: 'platform.configuration.config-maps.view',
    Secret: 'platform.configuration.secrets.view',
    ServiceAccount: 'platform.access-control.service-accounts.view',
    PersistentVolumeClaim: 'platform.storage.persistent-volume-claims.view',
    Node: 'platform.nodes.view',
  }
  const route =
    selected &&
    permissionKeys[selected.resource.kind] &&
    hasPermission(permissions, permissionKeys[selected.resource.kind])
      ? buildKubernetesResourcePath(selected.resource)
      : null
  const path = route
    ? (() => {
        const [pathname, raw = ''] = route.split('?')
        const query = new URLSearchParams(raw)
        query.set('clusterId', scope.clusterId || '')
        return `${pathname}?${query}`
      })()
    : null
  const resource = (node: DeploymentRelationNode, meta?: string) => (
    <button
      key={node.id}
      className={`soha-deployment-relation-node ${node.id === model.root.id ? 'is-root' : ''}`}
      onClick={(event) => {
        lastSelection.current = event.currentTarget
        setSelection(node.id)
      }}
    >
      <span>{node.resource.kind}</span>
      <strong>{node.resource.name}</strong>
      {meta && <span>{meta}</span>}
    </button>
  )
  const labels: Record<string, string> = zh
    ? {
        owns: '拥有',
        'uses-config': '引用配置',
        'uses-secret': '引用密钥',
        'uses-service-account': '服务账号',
        mounts: '挂载',
      }
    : {
        owns: 'Owns',
        'uses-config': 'Uses ConfigMap',
        'uses-secret': 'Uses Secret',
        'uses-service-account': 'Service account',
        mounts: 'Mounts',
      }
  const relation = (node: DeploymentRelationNode) =>
    node.id === model.root.id
      ? zh
        ? '当前资源'
        : 'Current resource'
      : labels[node.relation || 'owns'] ||
        localizeRelatedRelation(node.relation || 'owns', localeCode)
  return (
    <div className="soha-deployment-stack">
      <ReadFeedback
        canRead={query.isEnabled}
        query={query}
        label={zh ? '资源关系' : 'Resource relationships'}
      />
      <Card
        className="soha-detail-card"
        title={zh ? '关联资源' : 'Related resources'}
        extra={
          <div ref={control} tabIndex={-1}>
            <Segmented
              aria-label={zh ? '关联资源视图' : 'Related resources view'}
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
        <div className="soha-deployment-toolbar">
          <Typography.Text type="secondary">
            {zh ? 'Deployment 控制关系与直接引用' : 'Deployment ownership and direct references'}
          </Typography.Text>
          <label>
            {zh ? '历史 ReplicaSets' : 'Historical ReplicaSets'}{' '}
            <Switch
              aria-label={zh ? '历史 ReplicaSets' : 'Historical ReplicaSets'}
              checked={history}
              onChange={setHistory}
              size="small"
            />
          </label>
          <Button
            aria-label={zh ? '刷新资源关系' : 'Refresh relationships'}
            type="text"
            icon={<ReloadOutlined />}
            loading={query.isFetching}
            onClick={() => void query.refetch()}
          />
        </div>
        {graph?.warnings?.length ? (
          <Alert showIcon type="warning" title={graph.warnings.join(' · ')} />
        ) : null}
        {view === 'graph' ? (
          <div
            className="soha-deployment-relation-board"
            role="group"
            aria-label={zh ? 'Deployment 资源关系图' : 'Deployment resource graph'}
          >
            {resource(model.root)}
            <div className="soha-deployment-connector">
              {zh ? '拥有' : 'Owns'} <ArrowDownOutlined />
            </div>
            {sets.length ? (
              <div className="soha-deployment-relation-branches">
                {sets.map((set) => (
                  <section key={set.node.id}>
                    {resource(
                      set.node,
                      set.record
                        ? `${zh ? '版本' : 'Revision'} ${set.record.revision} · ${set.record.replicas} ${zh ? '个副本' : 'replicas'}`
                        : zh
                          ? '版本信息未返回'
                          : 'Revision unavailable',
                    )}
                    <div className="soha-deployment-connector">
                      {zh ? '控制' : 'Controls'} <ArrowDownOutlined />
                    </div>
                    <div className="soha-deployment-relation-group">
                      {set.pods.slice(0, 3).map((pod) => resource(pod, pod.status))}
                    </div>
                    {!set.pods.length && (
                      <Typography.Text type="secondary">
                        {set.record?.replicas === 0
                          ? zh
                            ? '当前 0 个副本'
                            : 'Zero replicas'
                          : zh
                            ? '当前关系数据未返回受控 Pod'
                            : 'No owned Pods returned in this graph'}
                      </Typography.Text>
                    )}
                    {set.pods.length > 3 && (
                      <Button type="link" onClick={onShowPods}>
                        {zh
                          ? `查看全部 ${set.pods.length} 个 Pods`
                          : `View all ${set.pods.length} Pods`}
                      </Button>
                    )}
                  </section>
                ))}
              </div>
            ) : (
              <ManagementState
                compact
                title={
                  query.isLoading
                    ? zh
                      ? '正在读取所属关系'
                      : 'Loading ownership'
                    : zh
                      ? '暂无 ReplicaSet 关系数据'
                      : 'No ReplicaSet relationship data'
                }
              />
            )}
            {model.references.length > 0 && (
              <>
                <div className="soha-deployment-connector">
                  {zh ? '关联与引用' : 'Relationships and references'} <ArrowDownOutlined />
                </div>
                <div className="soha-deployment-relation-group">
                  {model.references.map((node) => resource(node, relation(node)))}
                </div>
              </>
            )}
          </div>
        ) : (
          <AdminTable
            pagination={false}
            enableColumnSelection={false}
            rowKey="id"
            dataSource={all}
            columns={[
              {
                title: zh ? '类型' : 'Kind',
                key: 'kind',
                width: 160,
                render: (_: unknown, node: DeploymentRelationNode) => node.resource.kind,
              },
              {
                title: zh ? '名称' : 'Name',
                key: 'name',
                render: (_: unknown, node: DeploymentRelationNode) => (
                  <Button
                    type="link"
                    onClick={(event) => {
                      lastSelection.current = event.currentTarget
                      setSelection(node.id)
                    }}
                  >
                    {node.resource.name}
                  </Button>
                ),
              },
              {
                title: zh ? '关系' : 'Relationship',
                key: 'relation',
                width: 170,
                render: (_: unknown, node: DeploymentRelationNode) => relation(node),
              },
            ]}
            scroll={{ x: 720 }}
          />
        )}
      </Card>
      {selected && (
        <Card
          className="soha-detail-card"
          title={zh ? '选中资源' : 'Selected resource'}
          extra={
            <Button
              type="text"
              icon={<CloseOutlined />}
              aria-label={zh ? '关闭资源信息' : 'Close resource information'}
              onClick={() => {
                if (lastSelection.current?.isConnected) lastSelection.current.focus()
                else control.current?.focus()
                setSelection(null)
              }}
            />
          }
        >
          <div className="soha-deployment-selected">
            <Typography.Text type="secondary">{selected.resource.kind}</Typography.Text>
            <strong>{selected.resource.name}</strong>
            <span>{selected.resource.namespace}</span>
            <span>{relation(selected)}</span>
            <Typography.Text type="secondary">
              {zh ? '来源：' : 'Source: '}
              {selected.evidence === 'history'
                ? zh
                  ? '版本历史'
                  : 'Revision history'
                : selected.evidence === 'detail'
                  ? zh
                    ? 'Deployment 详情'
                    : 'Deployment detail'
                  : zh
                    ? '资源关系 API'
                    : 'Resource graph API'}
            </Typography.Text>
            <Button
              disabled={!path}
              onClick={() => {
                if (path) navigate(path)
              }}
            >
              {zh ? '查看详情' : 'View details'}
            </Button>
          </div>
        </Card>
      )}
    </div>
  )
}
