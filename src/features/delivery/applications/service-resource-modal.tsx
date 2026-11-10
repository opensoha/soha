import { ImportedSourceBrowser } from './imported-source-browser'
import { useState } from 'react'
import { Button, Modal, Pagination, Tabs } from 'antd'
import { useQueries, useQuery } from '@tanstack/react-query'
import { ManagementState } from '@/components/management-list'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { RelatedResourceYaml } from '@/features/platform'
import { deliveryQueries } from '../queries'
import { manifestQueries } from '../manifests'
import type { ApplicationRuntimeWorkload } from '../types'
import './workspace.css'

export function ServiceResourceModal({
  applicationId,
  serviceId,
  workloads,
  mode,
  onClose,
}: {
  applicationId: string
  serviceId?: string
  workloads: ApplicationRuntimeWorkload[]
  mode: 'related-resources' | 'resources'
  onClose: () => void
}) {
  const imported = workloads.filter((item) => item.targetId && item.source)
  const canView = hasPermission(usePermissionSnapshot().data?.data, 'delivery.applications.view')
  return (
    <Modal
      open
      width={1120}
      title={mode === 'resources' ? '资源清单' : '关联资源'}
      footer={null}
      onCancel={onClose}
    >
      {!canView ? (
        <ManagementState compact kind="no-permission" />
      ) : imported.length ? (
        <Tabs
          destroyOnHidden
          items={[
            {
              key: 'source',
              label: '集群源配置',
              children: <ImportedSourceBrowser workloads={imported} />,
            },
            {
              key: 'other',
              label: mode === 'resources' ? '清单文件' : '关联资源',
              children:
                mode === 'resources' ? (
                  <ManifestBrowser applicationId={applicationId} serviceId={serviceId} />
                ) : (
                  <RelatedBrowser
                    applicationId={applicationId}
                    workloads={workloads.filter((item) => !item.sourceStatus)}
                  />
                ),
            },
          ]}
        />
      ) : mode === 'resources' ? (
        serviceId ? (
          <ManifestBrowser applicationId={applicationId} serviceId={serviceId} />
        ) : (
          <ManagementState compact kind="not-found" title="服务不存在或不可访问" />
        )
      ) : (
        <RelatedBrowser applicationId={applicationId} workloads={workloads} />
      )}
    </Modal>
  )
}

function RelatedBrowser({
  applicationId,
  workloads,
}: {
  applicationId: string
  workloads: ApplicationRuntimeWorkload[]
}) {
  const [selectedKey, setSelectedKey] = useState('')
  const queries = useQueries({
    queries: workloads.map((workload) =>
      deliveryQueries.workloads.runtime({
        applicationId,
        applicationEnvironmentId: workload.applicationEnvironmentId,
        workloadName: workload.workloadName,
      }),
    ),
  })
  const resources = [
    ...new Map(
      queries
        .flatMap((query, index) =>
          (query.data?.deployment?.relatedResources ?? []).map((resource) => {
            const target = {
              kind: resource.kind,
              name: resource.name,
              namespace:
                resource.namespace || query.data?.workload.namespace || workloads[index].namespace,
              clusterId: workloads[index].clusterId,
            }
            return {
              ...target,
              key: [target.clusterId, target.namespace, target.kind, target.name].join('/'),
            }
          }),
        )
        .map((resource) => [resource.key, resource]),
    ).values(),
  ]
  const selected =
    resources.find((resource) => resource.key === selectedKey) ||
    (!selectedKey ? resources[0] : undefined)
  return (
    <div className="soha-resource-browser">
      <aside aria-label="关联资源列表" className="soha-resource-browser-list">
        {queries.some((query) => query.isPending) ? (
          <ManagementState compact kind="loading" />
        ) : null}
        {queries.some((query) => query.isError) ? (
          <ManagementState
            compact
            kind="error"
            actions={
              <Button onClick={() => queries.forEach((query) => void query.refetch())}>重试</Button>
            }
          />
        ) : null}
        {resources.map((resource) => (
          <button
            key={resource.key}
            type="button"
            aria-pressed={selected?.key === resource.key}
            onClick={() => setSelectedKey(resource.key)}
          >
            <strong>{resource.name}</strong>
            <span>{resource.kind}</span>
            <small>
              {resource.clusterId} / {resource.namespace}
            </small>
          </button>
        ))}
        {!resources.length && queries.every((query) => query.isSuccess) ? (
          <ManagementState compact kind="empty" title="暂无关联资源" />
        ) : null}
      </aside>
      <section className="soha-resource-browser-preview" aria-label="YAML 预览">
        {selected ? (
          <>
            <div className="soha-resource-browser-title">
              {selected.kind} / {selected.name} · 只读
            </div>
            <RelatedResourceYaml target={selected} />
          </>
        ) : (
          <ManagementState compact kind="empty" title="选择资源查看 YAML" />
        )}
      </section>
    </div>
  )
}

function ManifestBrowser({
  applicationId,
  serviceId,
}: {
  applicationId: string
  serviceId?: string
}) {
  const [page, setPage] = useState(1)
  const [selectedKey, setSelectedKey] = useState('')
  const query = useQuery(manifestQueries.list({ applicationId, serviceId, page, pageSize: 20 }))
  const files = (query.data?.items ?? []).flatMap((item) =>
    item.files.map((file) => ({ ...file, packageName: item.name, key: `${item.id}/${file.path}` })),
  )
  const selected =
    files.find((file) => file.key === selectedKey) || (!selectedKey ? files[0] : undefined)
  return (
    <div className="soha-resource-browser">
      <aside aria-label="清单文件列表" className="soha-resource-browser-list">
        {query.isPending ? (
          <ManagementState compact kind="loading" />
        ) : query.isError ? (
          <ManagementState
            compact
            kind="error"
            actions={<Button onClick={() => void query.refetch()}>重试</Button>}
          />
        ) : null}
        {files.map((file) => (
          <button
            key={file.key}
            type="button"
            aria-pressed={selected?.key === file.key}
            onClick={() => setSelectedKey(file.key)}
          >
            <strong>{file.path}</strong>
            <span>{file.packageName}</span>
          </button>
        ))}
        {query.isSuccess && !files.length ? (
          <ManagementState compact kind="empty" title="暂无资源清单" />
        ) : null}
        {(query.data?.total ?? 0) > 20 ? (
          <Pagination
            simple
            current={page}
            total={query.data?.total}
            pageSize={20}
            showSizeChanger={false}
            onChange={(next) => {
              setPage(next)
              setSelectedKey('')
            }}
          />
        ) : null}
      </aside>
      <section className="soha-resource-browser-preview" aria-label="YAML 预览">
        {selected ? (
          <>
            <div className="soha-resource-browser-title">{selected.path} · 只读</div>
            <pre className="soha-resource-yaml-content" tabIndex={0} aria-label="清单 YAML">
              {selected.content}
            </pre>
          </>
        ) : (
          <ManagementState compact kind="empty" title="选择清单查看 YAML" />
        )}
      </section>
    </div>
  )
}
