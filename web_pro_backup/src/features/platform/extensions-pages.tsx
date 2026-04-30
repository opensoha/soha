import { lazy, Suspense, useEffect, useMemo, useState } from 'react'
import {
  Alert,
  Button,
  Card,
  Descriptions,
  Empty,
  Modal,
  Space,
  Spin,
  Tag,
  Typography,
  message,
} from 'antd'
import { DeleteOutlined, EditOutlined, PlusOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AdminTable } from '@/components/admin-table'
import { useI18n } from '@/i18n'
import { PlatformClusterScopeHint } from '@/components/platform-cluster-scope-hint'
import { PlatformScopeToolbar } from '@/components/platform-scope-toolbar'
import { StatusTag } from '@/components/status-tag'
import { buildClusterScopedPath } from '@/features/platform/platform-scope-query'
import { api } from '@/services/api-client'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { formatAgeSeconds, formatRelativeTime } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import type { ApiResponse } from '@/types'
import type { TableColumnsType } from 'antd'

const { Text } = Typography
const K8sYamlEditor = lazy(async () => {
  const mod = await import('@/components/k8s-yaml-editor')
  return { default: mod.K8sYamlEditor }
})

/* ─── CRDs ─── */

interface CRD {
  name: string
  group: string
  kind: string
  plural: string
  version: string
  versions?: string[]
  scope: string
  createdAt?: string
  ageSeconds?: number
}

interface CRDResourceInstance {
  apiVersion?: string
  createdAt?: string
  ageSeconds?: number
  kind?: string
  labels?: Record<string, string>
  name: string
  namespace?: string
  status?: string
  summary?: Record<string, string | number | boolean | null>
}

interface CRDResourceEditorModalProps {
  crd: CRD
  mode: 'create' | 'edit'
  onClose: () => void
  open: boolean
  resource?: CRDResourceInstance | null
}

function isNamespacedCRD(crd: CRD | null | undefined) {
  return (crd?.scope ?? '').toLowerCase() === 'namespaced'
}

function buildCustomResourceCollectionPath(clusterId: string, crd: CRD, namespace?: string | null) {
  return buildClusterScopedPath(
    clusterId,
    `extensions/crds/${encodeURIComponent(crd.name)}/resources`,
    isNamespacedCRD(crd) ? namespace : null,
    { version: crd.version },
  )
}

function buildCustomResourceItemPath(
  clusterId: string,
  crd: CRD,
  resourceName: string,
  namespace?: string | null,
  suffix?: 'yaml',
) {
  const encodedName = encodeURIComponent(resourceName)
  const resourcePath = suffix
    ? `extensions/crds/${encodeURIComponent(crd.name)}/resources/${encodedName}/${suffix}`
    : `extensions/crds/${encodeURIComponent(crd.name)}/resources/${encodedName}`
  return buildClusterScopedPath(
    clusterId,
    resourcePath,
    isNamespacedCRD(crd) ? namespace : null,
    { version: crd.version },
  )
}

function toKebabCase(value: string) {
  return value
    .replace(/([a-z0-9])([A-Z])/g, '$1-$2')
    .replace(/[^a-zA-Z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .toLowerCase()
}

function buildDefaultCustomResourceTemplate(crd: CRD, namespace?: string | null) {
  const lines = [
    `apiVersion: ${crd.group}/${crd.version}`,
    `kind: ${crd.kind}`,
    'metadata:',
    `  name: example-${toKebabCase(crd.kind || crd.plural || 'resource')}`,
  ]
  if (isNamespacedCRD(crd) && namespace) {
    lines.push(`  namespace: ${namespace}`)
  }
  lines.push('spec: {}', '')
  return lines.join('\n')
}

function formatSummary(summary?: Record<string, string | number | boolean | null>) {
  if (!summary) return '-'
  const entries = Object.entries(summary)
    .filter(([, value]) => value != null && value !== '')
    .slice(0, 3)
  if (entries.length === 0) return '-'
  return entries.map(([key, value]) => `${key}: ${String(value)}`).join(' · ')
}

function formatResourceAge(createdAt?: string, ageSeconds?: number) {
  if (createdAt) return formatRelativeTime(createdAt)
  if (typeof ageSeconds === 'number') return formatAgeSeconds(ageSeconds)
  return '-'
}

function CRDResourceEditorModal({
  crd,
  mode,
  onClose,
  open,
  resource,
}: CRDResourceEditorModalProps) {
  const { t, localeCode } = useI18n()
  const { clusterId, namespace } = usePlatformScopeStore()
  const queryClient = useQueryClient()
  const [draft, setDraft] = useState('')

  const effectiveNamespace = isNamespacedCRD(crd) ? (resource?.namespace ?? namespace ?? '') : ''
  const yamlPath = clusterId && mode === 'edit' && resource
    ? buildCustomResourceItemPath(clusterId, crd, resource.name, effectiveNamespace, 'yaml')
    : null
  const listQueryKey = ['crd-resources', clusterId, crd.name, isNamespacedCRD(crd) ? (namespace ?? '') : '__cluster__']
  const draftStorageKey = mode === 'edit' && resource
    ? `kubecrux:crd-yaml:${clusterId || 'none'}:${crd.name}:${effectiveNamespace}:${resource.name}`
    : null

  const yamlQuery = useQuery({
    queryKey: ['crd-resource-yaml', clusterId, crd.name, resource?.name, effectiveNamespace],
    queryFn: () => api.get<ApiResponse<{ content: string }>>(yamlPath!),
    enabled: open && !!yamlPath,
  })

  useEffect(() => {
    if (!open) return
    if (mode === 'create') {
      setDraft(buildDefaultCustomResourceTemplate(crd, namespace))
      return
    }
    if (draftStorageKey && typeof window !== 'undefined') {
      const savedDraft = window.localStorage.getItem(draftStorageKey)
      if (savedDraft) {
        setDraft(savedDraft)
        return
      }
    }
    setDraft(yamlQuery.data?.data?.content ?? '')
  }, [crd, draftStorageKey, mode, namespace, open, yamlQuery.data?.data?.content])

  const applyMutation = useMutation({
    mutationFn: () => {
      if (!clusterId) {
        throw new Error(localeCode === 'zh_CN' ? '请先选择集群' : 'Select a cluster first')
      }
      const body = isNamespacedCRD(crd) && namespace
        ? { content: draft, namespace }
        : { content: draft }
      if (mode === 'create') {
        return api.post<ApiResponse<{ content: string }>>(
          buildCustomResourceCollectionPath(clusterId, crd, namespace),
          body,
        )
      }
      return api.put<ApiResponse<{ content: string }>>(yamlPath!, body)
    },
    onSuccess: () => {
      if (draftStorageKey && typeof window !== 'undefined') {
        window.localStorage.removeItem(draftStorageKey)
      }
      void message.success(
        localeCode === 'zh_CN'
          ? (mode === 'create' ? `${crd.kind} 已创建` : `${crd.kind} YAML 已更新`)
          : (mode === 'create' ? `${crd.kind} created` : `${crd.kind} YAML updated`),
      )
      void queryClient.invalidateQueries({ queryKey: listQueryKey })
      onClose()
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const bannerDescription = isNamespacedCRD(crd)
    ? (
      localeCode === 'zh_CN'
        ? `当前资源遵循命名空间 scope。${effectiveNamespace ? `请求默认带上 namespace=${effectiveNamespace}，也可在 YAML 中覆盖 metadata.namespace。` : '当前为全部命名空间视图，请在 YAML 中显式填写 metadata.namespace。'}`
        : `This resource is namespaced. ${effectiveNamespace ? `Requests default to namespace=${effectiveNamespace}; you can still override metadata.namespace in YAML.` : 'The current view spans all namespaces, so set metadata.namespace explicitly in YAML.'}`
    )
    : (
      localeCode === 'zh_CN'
        ? '当前资源为 cluster scope，命名空间选择不会参与请求。'
        : 'This resource is cluster-scoped, so the namespace selector is ignored for requests.'
    )

  return (
    <Modal
      title={localeCode === 'zh_CN'
        ? `${mode === 'create' ? '新建' : '编辑'} ${crd.kind}`
        : `${mode === 'create' ? 'Create' : 'Edit'} ${crd.kind}`}
      open={open}
      onCancel={onClose}
      footer={null}
      width={1080}
      destroyOnClose
      maskClosable={false}
    >
      {!clusterId ? (
        <Empty description={localeCode === 'zh_CN' ? '请先选择集群' : 'Select a cluster first'} />
      ) : mode === 'edit' && yamlQuery.isLoading ? (
        <div style={{ height: 520, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
          <Spin size="large" />
        </div>
      ) : mode === 'edit' && yamlQuery.isError ? (
        <Alert
          type="error"
          showIcon
          message={localeCode === 'zh_CN' ? 'YAML 加载失败' : 'Failed to load YAML'}
          description={(yamlQuery.error as Error)?.message}
        />
      ) : (
        <Suspense fallback={<div style={{ height: 520, display: 'flex', alignItems: 'center', justifyContent: 'center' }}><Spin size="large" /></div>}>
          <Alert banner showIcon type="info" description={bannerDescription} />
          <div style={{ height: 560, marginTop: 12 }}>
            <K8sYamlEditor
              value={draft}
              onChange={setDraft}
              onReset={() => {
                if (mode === 'create') {
                  setDraft(buildDefaultCustomResourceTemplate(crd, namespace))
                  return
                }
                if (draftStorageKey && typeof window !== 'undefined') {
                  window.localStorage.removeItem(draftStorageKey)
                }
                setDraft(yamlQuery.data?.data?.content ?? '')
                void message.success(t('yamlEditor.resetSuccess', 'YAML draft reset'))
              }}
              onSave={() => {
                if (!draftStorageKey || typeof window === 'undefined') {
                  void message.info(localeCode === 'zh_CN' ? '新建模式不支持本地草稿' : 'Draft saving disabled in create mode')
                  return
                }
                window.localStorage.setItem(draftStorageKey, draft)
                void message.success(t('yamlEditor.saveSuccess', 'YAML draft saved locally'))
              }}
              onApply={() => applyMutation.mutate()}
              saveDisabled={!draftStorageKey}
              applyDisabled={!draft.trim() || applyMutation.isPending}
              applying={applyMutation.isPending}
            />
          </div>
          <div style={{ marginTop: 12, textAlign: 'right' }}>
            <Button onClick={onClose}>{t('common.cancel', 'Cancel')}</Button>
          </div>
        </Suspense>
      )}
    </Modal>
  )
}

export function CRDPage() {
  const { t } = useI18n()
  const { clusterId, namespace } = usePlatformScopeStore()
  const queryClient = useQueryClient()
  const [selectedCRDName, setSelectedCRDName] = useState<string | null>(null)
  const [createOpen, setCreateOpen] = useState(false)
  const [editingResource, setEditingResource] = useState<CRDResourceInstance | null>(null)
  const [deletingKey, setDeletingKey] = useState<string | null>(null)

  const { data, isLoading } = useQuery({
    queryKey: ['crds', clusterId],
    queryFn: () => api.get<ApiResponse<CRD[]>>(buildClusterScopedPath(clusterId!, 'extensions/crds')),
    enabled: !!clusterId,
  })

  const crds = data?.data ?? []
  const selectedCRD = useMemo(
    () => crds.find((item) => item.name === selectedCRDName) ?? crds[0] ?? null,
    [crds, selectedCRDName],
  )
  const selectedScopeNamespace = selectedCRD && isNamespacedCRD(selectedCRD) ? (namespace ?? '') : null
  const selectedListQueryKey = ['crd-resources', clusterId, selectedCRD?.name ?? null, selectedScopeNamespace ?? '__cluster__']

  useEffect(() => {
    if (!crds.length) {
      setSelectedCRDName(null)
      return
    }
    if (selectedCRDName && crds.some((item) => item.name === selectedCRDName)) {
      return
    }
    setSelectedCRDName(crds[0].name)
  }, [crds, selectedCRDName])

  const resourcesQuery = useQuery({
    queryKey: selectedListQueryKey,
    queryFn: () => api.get<ApiResponse<CRDResourceInstance[]>>(
      buildCustomResourceCollectionPath(clusterId!, selectedCRD!, namespace),
    ),
    enabled: !!clusterId && !!selectedCRD,
  })

  const deleteMutation = useMutation({
    mutationFn: ({ resourceName, resourceNamespace }: { resourceName: string; resourceNamespace?: string }) => {
      if (!clusterId || !selectedCRD) {
        throw new Error(t('platformScope.clusterPlaceholder', 'Select cluster'))
      }
      return api.delete(
        buildCustomResourceItemPath(clusterId, selectedCRD, resourceName, resourceNamespace ?? namespace ?? ''),
      )
    },
    onMutate: ({ resourceName, resourceNamespace }) => setDeletingKey(`${resourceNamespace || ''}/${resourceName}`),
    onSettled: () => setDeletingKey(null),
    onSuccess: () => {
      void message.success(t('common.deleteSuccess', 'Deleted successfully'))
      void queryClient.invalidateQueries({ queryKey: selectedListQueryKey })
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const columns: TableColumnsType<CRD> = [
    { title: '名称', dataIndex: 'name' },
    { title: 'Kind', dataIndex: 'kind', width: 180 },
    { title: 'Group', dataIndex: 'group' },
    { title: 'Plural', dataIndex: 'plural', width: 180 },
    {
      title: 'Version',
      dataIndex: 'version',
      render: (_: string, record: CRD) => (
        <Space size={[4, 4]} wrap>
          {(record.versions?.length ? record.versions : [record.version]).map((value) => (
            <Tag key={value} color={value === record.version ? 'blue' : 'default'}>
              {value}
            </Tag>
          ))}
        </Space>
      ),
    },
    {
      title: 'Scope',
      dataIndex: 'scope',
      render: (s: string) => <Tag>{s}</Tag>,
    },
    {
      title: 'Age',
      key: 'age',
      render: (_: unknown, record: CRD) => formatResourceAge(record.createdAt, record.ageSeconds),
    },
  ]

  const resourceColumns: TableColumnsType<CRDResourceInstance> = [
    {
      title: '名称',
      dataIndex: 'name',
      render: (value: string, record: CRDResourceInstance) => (
        <Button type="link" style={{ paddingInline: 0 }} onClick={() => setEditingResource(record)}>
          {value}
        </Button>
      ),
    },
    ...(selectedCRD && isNamespacedCRD(selectedCRD)
      ? [{ title: '命名空间', dataIndex: 'namespace', width: 180 } satisfies TableColumnsType<CRDResourceInstance>[number]]
      : []),
    {
      title: 'Kind',
      dataIndex: 'kind',
      width: 180,
      render: (value?: string) => value || selectedCRD?.kind || '-',
    },
    {
      title: 'API Version',
      dataIndex: 'apiVersion',
      width: 220,
      render: (value?: string) => value || (selectedCRD ? `${selectedCRD.group}/${selectedCRD.version}` : '-'),
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 140,
      render: (value?: string) => value ? <StatusTag value={value} /> : <Text type="secondary">-</Text>,
    },
    {
      title: '摘要',
      dataIndex: 'summary',
      render: (value?: Record<string, string | number | boolean | null>) => formatSummary(value),
    },
    {
      ...tableColumnPresets.datetime,
      title: 'Age',
      key: 'age',
      width: 140,
      render: (_: unknown, record: CRDResourceInstance) => formatResourceAge(record.createdAt, record.ageSeconds),
    },
    {
      title: '',
      key: 'actions',
      fixed: 'right',
      align: 'center',
      width: 88,
      render: (_: unknown, record: CRDResourceInstance) => {
        const resourceKey = `${record.namespace || ''}/${record.name}`
        return (
          <Space size={4}>
            <Button
              size="small"
              type="text"
              icon={<EditOutlined />}
              aria-label={t('common.edit', 'Edit')}
              onClick={() => setEditingResource(record)}
            />
            <Button
              size="small"
              type="text"
              danger
              icon={<DeleteOutlined />}
              aria-label={t('common.delete', 'Delete')}
              loading={deletingKey === resourceKey}
              onClick={() => {
                Modal.confirm({
                  title: t('common.deleteConfirm', `Delete ${record.name}?`),
                  content: isNamespacedCRD(selectedCRD)
                    ? (record.namespace
                        ? `${record.name} (${record.namespace})`
                        : record.name)
                    : record.name,
                  okButtonProps: { danger: true },
                  onOk: () => deleteMutation.mutate({
                    resourceName: record.name,
                    resourceNamespace: record.namespace,
                  }),
                })
              }}
            />
          </Space>
        )
      },
    },
  ]

  const instanceToolbar = selectedCRD ? (
    <Space wrap>
      <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>
        {t('common.create', 'Create')}
      </Button>
      <Tag color={isNamespacedCRD(selectedCRD) ? 'gold' : 'blue'}>
        {isNamespacedCRD(selectedCRD)
          ? (namespace
              ? `${t('common.namespace', 'Namespace')}: ${namespace}`
              : t('platformScope.allNamespaces', 'All namespaces'))
          : t('page.extensions.crd.clusterScoped', 'Cluster scoped')}
      </Tag>
    </Space>
  ) : null

  return (
    <div className="kc-page">
      <PlatformScopeToolbar />
      <PlatformClusterScopeHint resourceLabel="CRD" />
      <AdminTable
        columns={columns}
        dataSource={crds}
        rowKey="name"
        loading={isLoading}
        pageSize={10}
        enableColumnSelection={false}
        rowSelection={{
          type: 'radio',
          selectedRowKeys: selectedCRD ? [selectedCRD.name] : [],
          onChange: (selectedRowKeys: React.Key[]) => {
            const next = typeof selectedRowKeys[0] === 'string' ? selectedRowKeys[0] : null
            setSelectedCRDName(next)
          },
        }}
        title={(
          <Space direction="vertical" size={0}>
            <Text strong>{t('page.extensions.crd.catalogTitle', 'CRD Catalog')}</Text>
            <Text type="secondary">{t('page.extensions.crd.catalogDesc', 'Select a CRD to inspect the custom resources it serves.')}</Text>
          </Space>
        )}
      />

      <Card className="kc-detail-card" style={{ marginTop: 16 }}>
        {!selectedCRD ? (
          <Empty description={clusterId ? t('page.extensions.crd.emptySelection', 'Select a CRD from the catalog to inspect its resources.') : t('platformScope.clusterPlaceholder', 'Select cluster')} />
        ) : (
          <Space direction="vertical" size={16} style={{ width: '100%' }}>
            <Descriptions
              column={{ xs: 1, sm: 2, lg: 4 }}
              items={[
                { key: 'name', label: 'CRD', children: selectedCRD.name },
                { key: 'kind', label: 'Kind', children: selectedCRD.kind },
                { key: 'group', label: 'Group', children: selectedCRD.group },
                { key: 'plural', label: 'Plural', children: selectedCRD.plural },
                {
                  key: 'versions',
                  label: 'Versions',
                  span: 2,
                  children: (
                    <Space size={[4, 4]} wrap>
                      {(selectedCRD.versions?.length ? selectedCRD.versions : [selectedCRD.version]).map((value) => (
                        <Tag key={value} color={value === selectedCRD.version ? 'blue' : 'default'}>
                          {value}
                        </Tag>
                      ))}
                    </Space>
                  ),
                },
                { key: 'scope', label: 'Scope', children: <Tag>{selectedCRD.scope}</Tag> },
                { key: 'age', label: 'Age', children: formatResourceAge(selectedCRD.createdAt, selectedCRD.ageSeconds) },
              ]}
            />
            <Alert
              type="info"
              showIcon
              message={isNamespacedCRD(selectedCRD)
                ? t('page.extensions.crd.namespacedTitle', 'Namespaced custom resources')
                : t('page.extensions.crd.clusterTitle', 'Cluster-scoped custom resources')}
              description={isNamespacedCRD(selectedCRD)
                ? (namespace
                    ? t('page.extensions.crd.namespacedDesc', `The lower table is filtered by namespace ${namespace}. Clear the namespace selector to inspect all namespaces for this CRD.`)
                    : t('page.extensions.crd.namespacedAllDesc', 'The lower table spans all namespaces for this CRD because no namespace filter is active.'))
                : t('page.extensions.crd.clusterDesc', 'The lower table ignores the namespace selector because the selected CRD is cluster-scoped.')}
            />
            <AdminTable
              columns={resourceColumns}
              dataSource={resourcesQuery.data?.data ?? []}
              rowKey={(record) => `${record.namespace || '__cluster__'}:${record.name}`}
              loading={resourcesQuery.isLoading}
              empty={resourcesQuery.isError
                ? (
                  <Alert
                    type="warning"
                    showIcon
                    message={t('page.extensions.crd.resourcesUnavailable', 'Custom resource list unavailable')}
                    description={(resourcesQuery.error as Error)?.message}
                  />
                )
                : <Empty description={t('page.extensions.crd.resourcesEmpty', 'No custom resources found for the selected CRD in the current scope.')} />}
              enableColumnSelection={false}
              scroll={{ x: 'max-content' }}
              title={(
                <Space direction="vertical" size={0}>
                  <Text strong>{`${selectedCRD.kind} ${t('page.extensions.crd.resourcesTitle', 'Resources')}`}</Text>
                  <Text type="secondary">
                    {t('page.extensions.crd.resourcesDesc', 'Create, edit, or delete resources through generic YAML flows driven by the selected CRD metadata.')}
                  </Text>
                </Space>
              )}
              toolbar={instanceToolbar}
            />
          </Space>
        )}
      </Card>

      {selectedCRD ? (
        <CRDResourceEditorModal
          crd={selectedCRD}
          mode="create"
          open={createOpen}
          onClose={() => setCreateOpen(false)}
        />
      ) : null}
      {selectedCRD && editingResource ? (
        <CRDResourceEditorModal
          crd={selectedCRD}
          mode="edit"
          open={!!editingResource}
          resource={editingResource}
          onClose={() => setEditingResource(null)}
        />
      ) : null}
    </div>
  )
}

/* ─── Helm Releases ─── */

interface HelmRelease {
  name: string
  namespace: string
  chart: string
  version: string
  appVersion: string
  status: string
  updatedAt: string
}

export function HelmReleasesPage() {
  const { t } = useI18n()
  const { clusterId, namespace } = usePlatformScopeStore()

  const { data, isLoading } = useQuery({
    queryKey: ['helm-releases', clusterId, namespace],
    queryFn: () => api.get<ApiResponse<HelmRelease[]>>(buildClusterScopedPath(clusterId!, 'helm/releases', namespace)),
    enabled: !!clusterId,
  })

  const columns: TableColumnsType<HelmRelease> = [
    { title: '名称', dataIndex: 'name' },
    { title: '命名空间', dataIndex: 'namespace' },
    { title: 'Chart', dataIndex: 'chart' },
    { title: 'Version', dataIndex: 'version' },
    {
      ...tableColumnPresets.status,
      title: '状态',
      dataIndex: 'status',
      render: (s: string) => <StatusTag value={s} />,
    },
    { ...tableColumnPresets.datetime, title: '更新时间', dataIndex: 'updatedAt', render: (t: string) => formatRelativeTime(t) },
  ]

  return (
    <div className="kc-page">
      <PlatformScopeToolbar />
      <AdminTable columns={columns} dataSource={data?.data ?? []} rowKey="name" loading={isLoading} />
    </div>
  )
}

/* ─── Helm Charts ─── */

export function HelmChartsPage() {
  const { t } = useI18n()
  return (
    <div className="kc-page">
      <Card>
        <Empty
          description={(
            <div>
              <div>{t('page.extensions.helmCharts.emptyTitle', 'Helm Charts not available')}</div>
              <div>{t('page.extensions.helmCharts.emptyDesc', 'The backend currently has no /helm/charts endpoint. Restore the list view after the backend capability is added.')}</div>
            </div>
          )}
        />
      </Card>
    </div>
  )
}
