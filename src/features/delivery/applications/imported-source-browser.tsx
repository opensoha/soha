import { useState } from 'react'
import { Button, Typography } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { ReadFeedback } from '@/components/request-feedback'
import { ManagementState } from '@/components/management-list'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { deliveryQueries } from '../queries'
import { ResourceRelationsBrowser } from '../runtime/resource-relations-browser'
import type { ApplicationEnvironment, ApplicationRuntimeWorkload } from '../types'

const sourceLabels = {
  linked: '已关联',
  changed: '配置已变化',
  replaced: '同名资源已重建',
  missing: '源资源已删除',
  unavailable: '身份暂不可验证',
  forbidden: '无资源读取权限',
}

export function ImportedSourceBrowser({
  workloads,
  binding,
  view = 'resources',
}: {
  workloads: ApplicationRuntimeWorkload[]
  binding?: ApplicationEnvironment
  view?: 'resources' | 'metrics'
}) {
  const [selectedKey, setSelectedKey] = useState('')
  const sources = workloads.filter(
    (item) =>
      item.targetId &&
      item.source &&
      (view !== 'metrics' || item.source.crdName || item.workloadKind !== 'Deployment'),
  )
  const keyOf = (item: ApplicationRuntimeWorkload) =>
    `${item.applicationEnvironmentId}/${item.targetId}/${item.source?.resource.uid}`
  const selected = sources.find((item) => keyOf(item) === selectedKey) ?? sources[0]
  if (!selected)
    return view === 'metrics' ? null : (
      <ManagementState compact kind="not-configured" title="源身份尚未绑定" />
    )
  return (
    <div className="soha-resource-browser">
      <aside aria-label="集群源配置列表" className="soha-resource-browser-list">
        {sources.map((item) => (
          <button
            type="button"
            key={keyOf(item)}
            aria-pressed={keyOf(selected) === keyOf(item)}
            onClick={() => setSelectedKey(keyOf(item))}
          >
            <strong>{item.workloadName}</strong>
            <span>{item.source?.resource.kind || item.workloadKind}</span>
            <small>
              {item.clusterId} / {item.namespace}
            </small>
          </button>
        ))}
      </aside>
      <section className="soha-resource-browser-preview" aria-label="集群源配置">
        <SourcePreview
          key={keyOf(selected) + view}
          workload={selected}
          binding={binding}
          view={view}
        />
      </section>
    </div>
  )
}

function SourcePreview({
  workload,
  binding,
  view,
}: {
  workload: ApplicationRuntimeWorkload
  binding?: ApplicationEnvironment
  view: 'resources' | 'metrics'
}) {
  const [showYAML, setShowYAML] = useState(false)
  const permissions = usePermissionSnapshot().data?.data
  const source = workload.source!
  const readable = ['linked', 'changed'].includes(workload.sourceStatus ?? '')
  const sensitive = Boolean(source.crdName)
  const canRead =
    hasPermission(permissions, 'delivery.application-environments.view') &&
    (!sensitive || hasPermission(permissions, 'platform.configuration.secret-data.view'))
  if (view === 'metrics')
    return readable ? (
      <ResourceRelationsBrowser workload={workload} binding={binding} view={view} />
    ) : (
      <ManagementState compact kind="error" title="根资源身份不可验证，无法读取指标关系" />
    )
  return (
    <>
      <div className="soha-resource-browser-title">
        {source.resource.kind} / {source.resource.name} · 集群当前配置
      </div>
      <div>
        <MetadataTag label={source.resource.apiVersion} />{' '}
        <StatusTag
          value={readable ? 'info' : 'warning'}
          label={sourceLabels[workload.sourceStatus ?? 'unavailable']}
        />
      </div>
      <p>
        <Typography.Text type="secondary">
          UID {source.resource.uid}
          <br />
          关联时摘要 {source.specDigest.slice(0, 12)} · 健康状态待观测
        </Typography.Text>
      </p>
      {source.owners?.length ? (
        <p>
          拥有者：
          {source.owners.map((owner) => `${owner.kind}/${owner.name} (${owner.uid})`).join('、')}
        </p>
      ) : null}
      {!readable ? (
        <ManagementState
          compact
          kind="error"
          title={sourceLabels[workload.sourceStatus ?? 'unavailable']}
          description="恢复源身份或人工核对后重新关联；不会自动绑定同名新资源。"
        />
      ) : !canRead ? (
        <ManagementState
          compact
          kind="no-permission"
          title={sensitive ? '查看 CR 全文需要敏感数据读取权限' : '无环境读取权限'}
        />
      ) : (
        <>
          <Button onClick={() => setShowYAML((value) => !value)}>
            {showYAML ? '关闭 YAML' : '查看源 YAML'}
          </Button>
          {showYAML ? <SourceYAML workload={workload} /> : null}
        </>
      )}
      {readable && (source.crdName || workload.workloadKind !== 'Deployment') ? (
        <ResourceRelationsBrowser workload={workload} binding={binding} view="resources" />
      ) : null}
    </>
  )
}

function SourceYAML({ workload }: { workload: ApplicationRuntimeWorkload }) {
  const query = useQuery(
    deliveryQueries.environments.targetSource(
      workload.applicationEnvironmentId,
      workload.targetId!,
      workload.source!.resource.uid,
      true,
    ),
  )
  if (query.isPending) return <ManagementState compact kind="loading" />
  if (query.isError) return <ReadFeedback query={query} label="源配置" />
  return (
    <pre className="soha-resource-yaml-content" tabIndex={0} aria-label="源配置 YAML">
      {query.data.content}
    </pre>
  )
}
