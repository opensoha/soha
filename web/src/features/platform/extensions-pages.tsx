import { lazy, Suspense, useEffect, useMemo, useState } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import {
  Alert,
  Button,
  Card,
  Descriptions,
  Empty,
  Modal,
  Space,
  Spin,
  Tabs,
  Tag,
  Typography,
  message,
} from 'antd'
import {
  ArrowLeftOutlined,
  DeleteOutlined,
  EditOutlined,
  HistoryOutlined,
  PlusOutlined,
  RightOutlined,
} from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AdminTable } from '@/components/admin-table'
import { useI18n } from '@/i18n'
import { PageHeader } from '@/components/page-header'
import { PlatformClusterScopeHint } from '@/components/platform-cluster-scope-hint'
import { PlatformScopeToolbar } from '@/components/platform-scope-toolbar'
import { StatusTag } from '@/components/status-tag'
import { YamlDraftDiffEditor } from '@/components/yaml-draft-diff-editor'
import { buildClusterScopedPath } from '@/features/platform/platform-scope-query'
import { api } from '@/services/api-client'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { formatAgeSeconds, formatDateTime, formatRelativeTime } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import type { ApiResponse, HelmRelease, HelmReleaseDetail, HelmReleaseHistory, HelmValues } from '@/types'
import type { TableColumnsType, TabsProps } from 'antd'

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

interface CRDApiGroupSummary {
  clusterCount: number
  crdCount: number
  crdNames: string[]
  crds: CRD[]
  group: string
  kindNames: string[]
  namespacedCount: number
  versions: string[]
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

function buildCRDApiGroupDetailPath(group: string) {
  return `/extensions/apis/${encodeURIComponent(group)}`
}

function buildHelmReleaseDetailPath(name: string, namespace?: string | null) {
  const encodedName = encodeURIComponent(name)
  if (!namespace) {
    return `/helm/releases/${encodedName}`
  }
  const params = new URLSearchParams()
  params.set('namespace', namespace)
  return `/helm/releases/${encodedName}?${params.toString()}`
}

function getServedVersions(crd: CRD) {
  return Array.from(new Set((crd.versions?.length ? crd.versions : [crd.version]).filter(Boolean)))
}

function groupCRDsByApi(crds: CRD[]) {
  const grouped = new Map<string, CRD[]>()
  for (const crd of crds) {
    const current = grouped.get(crd.group) ?? []
    current.push(crd)
    grouped.set(crd.group, current)
  }

  return Array.from(grouped.entries())
    .map(([group, items]) => {
      const sortedCRDs = [...items].sort((left, right) => left.kind.localeCompare(right.kind))
      const versions = Array.from(new Set(sortedCRDs.flatMap((item) => getServedVersions(item)))).sort((left, right) => left.localeCompare(right))
      return {
        clusterCount: sortedCRDs.filter((item) => !isNamespacedCRD(item)).length,
        crdCount: sortedCRDs.length,
        crdNames: sortedCRDs.map((item) => item.name),
        crds: sortedCRDs,
        group,
        kindNames: sortedCRDs.map((item) => item.kind),
        namespacedCount: sortedCRDs.filter((item) => isNamespacedCRD(item)).length,
        versions,
      } satisfies CRDApiGroupSummary
    })
    .sort((left, right) => left.group.localeCompare(right.group))
}

function safeDecodeURIComponent(value?: string) {
  if (!value) return ''
  try {
    return decodeURIComponent(value)
  } catch {
    return value
  }
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

function CRDKindWorkspace({ crd }: { crd: CRD }) {
  const { t } = useI18n()
  const { clusterId, namespace } = usePlatformScopeStore()
  const queryClient = useQueryClient()
  const [createOpen, setCreateOpen] = useState(false)
  const [editingResource, setEditingResource] = useState<CRDResourceInstance | null>(null)
  const [deletingKey, setDeletingKey] = useState<string | null>(null)

  const selectedScopeNamespace = isNamespacedCRD(crd) ? (namespace ?? '') : null
  const selectedListQueryKey = ['crd-resources', clusterId, crd.name, selectedScopeNamespace ?? '__cluster__']

  const resourcesQuery = useQuery({
    queryKey: selectedListQueryKey,
    queryFn: () => api.get<ApiResponse<CRDResourceInstance[]>>(
      buildCustomResourceCollectionPath(clusterId!, crd, namespace),
    ),
    enabled: !!clusterId,
  })

  const deleteMutation = useMutation({
    mutationFn: ({ resourceName, resourceNamespace }: { resourceName: string; resourceNamespace?: string }) => {
      if (!clusterId) {
        throw new Error(t('platformScope.clusterPlaceholder', 'Select cluster'))
      }
      return api.delete(
        buildCustomResourceItemPath(clusterId, crd, resourceName, resourceNamespace ?? namespace ?? ''),
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
    ...(isNamespacedCRD(crd)
      ? [{ title: '命名空间', dataIndex: 'namespace', width: 180 } satisfies TableColumnsType<CRDResourceInstance>[number]]
      : []),
    {
      title: 'Kind',
      dataIndex: 'kind',
      width: 180,
      render: (value?: string) => value || crd.kind || '-',
    },
    {
      title: 'API Version',
      dataIndex: 'apiVersion',
      width: 220,
      render: (value?: string) => value || `${crd.group}/${crd.version}`,
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
                  content: isNamespacedCRD(crd)
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

  const instanceToolbar = (
    <Space wrap>
      <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>
        {t('common.create', 'Create')}
      </Button>
      <Tag color={isNamespacedCRD(crd) ? 'gold' : 'blue'}>
        {isNamespacedCRD(crd)
          ? (namespace
              ? `${t('common.namespace', 'Namespace')}: ${namespace}`
              : t('platformScope.allNamespaces', 'All namespaces'))
          : t('page.extensions.crd.clusterScoped', 'Cluster scoped')}
      </Tag>
    </Space>
  )

  return (
    <>
      <Card className="kc-detail-card" style={{ marginTop: 0 }}>
        <Space direction="vertical" size={16} style={{ width: '100%' }}>
          <Descriptions
            column={{ xs: 1, sm: 2, lg: 4 }}
            items={[
              { key: 'name', label: 'CRD', children: crd.name },
              { key: 'kind', label: 'Kind', children: crd.kind },
              { key: 'group', label: 'Group', children: crd.group },
              { key: 'plural', label: 'Plural', children: crd.plural },
              {
                key: 'versions',
                label: 'Versions',
                span: 2,
                children: (
                  <Space size={[4, 4]} wrap>
                    {getServedVersions(crd).map((value) => (
                      <Tag key={value} color={value === crd.version ? 'blue' : 'default'}>
                        {value}
                      </Tag>
                    ))}
                  </Space>
                ),
              },
              { key: 'scope', label: 'Scope', children: <Tag>{crd.scope}</Tag> },
              { key: 'age', label: 'Age', children: formatResourceAge(crd.createdAt, crd.ageSeconds) },
            ]}
          />
          <Alert
            type="info"
            showIcon
            message={isNamespacedCRD(crd)
              ? t('page.extensions.crd.namespacedTitle', 'Namespaced custom resources')
              : t('page.extensions.crd.clusterTitle', 'Cluster-scoped custom resources')}
            description={isNamespacedCRD(crd)
              ? (namespace
                  ? t('page.extensions.crd.namespacedDesc', `The lower table is filtered by namespace ${namespace}. Clear the namespace selector to inspect all namespaces for this CRD.`)
                  : t('page.extensions.crd.namespacedAllDesc', 'The lower table spans all namespaces for this CRD because no namespace filter is active.'))
              : t('page.extensions.crd.clusterDesc', 'The lower table ignores the namespace selector because the selected CRD is cluster-scoped.')}
          />
        </Space>
      </Card>

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
            <Text strong>{`${crd.kind} ${t('page.extensions.crd.resourcesTitle', 'Resources')}`}</Text>
            <Text type="secondary">
              {t('page.extensions.crd.resourcesDesc', 'Create, edit, or delete resources through generic YAML flows driven by the selected CRD metadata.')}
            </Text>
          </Space>
        )}
        toolbar={instanceToolbar}
      />

      <CRDResourceEditorModal
        crd={crd}
        mode="create"
        open={createOpen}
        onClose={() => setCreateOpen(false)}
      />

      {editingResource ? (
        <CRDResourceEditorModal
          crd={crd}
          mode="edit"
          open={!!editingResource}
          resource={editingResource}
          onClose={() => setEditingResource(null)}
        />
      ) : null}
    </>
  )
}

export function CRDPage() {
  const { t, localeCode } = useI18n()
  const { clusterId } = usePlatformScopeStore()
  const navigate = useNavigate()

  const { data, isLoading } = useQuery({
    queryKey: ['crds', clusterId],
    queryFn: () => api.get<ApiResponse<CRD[]>>(buildClusterScopedPath(clusterId!, 'extensions/crds')),
    enabled: !!clusterId,
  })

  const apiGroups = useMemo(() => groupCRDsByApi(data?.data ?? []), [data?.data])

  const columns: TableColumnsType<CRDApiGroupSummary> = [
    {
      title: t('page.extensions.crd.apiGroupColumn', 'API Group'),
      dataIndex: 'group',
      width: 220,
      render: (_: string, record: CRDApiGroupSummary) => (
        <Button
          type="link"
          className="kc-crd-group-link"
          onClick={(event) => {
            event.stopPropagation()
            navigate(buildCRDApiGroupDetailPath(record.group))
          }}
        >
          <span className="kc-crd-group-card">
            <code className="kc-crd-group-card__value">{record.group}</code>
          </span>
        </Button>
      ),
    },
    {
      title: t('page.extensions.crd.crdNameColumn', 'CRD Names'),
      key: 'crdNames',
      width: 360,
      render: (_: unknown, record: CRDApiGroupSummary) => (
        <div className="kc-crd-name-chip-list">
          {record.crdNames.slice(0, 2).map((value) => (
            <code key={value} className="kc-crd-name-chip">{value}</code>
          ))}
          {record.crdNames.length > 2 ? (
            <code className="kc-crd-name-chip is-summary">
              {localeCode === 'zh_CN'
                ? `+${record.crdNames.length - 2} 个 CRD`
                : `+${record.crdNames.length - 2} more CRDs`}
            </code>
          ) : null}
        </div>
      ),
    },
    {
      title: t('page.extensions.crd.kindCountColumn', 'Kind Count'),
      key: 'kindCount',
      width: 120,
      render: (_: unknown, record: CRDApiGroupSummary) => (
        <Text>{localeCode === 'zh_CN' ? `${record.crdCount} 个` : String(record.crdCount)}</Text>
      ),
    },
    {
      title: t('page.extensions.crd.kindPreviewColumn', 'Served kinds'),
      key: 'kinds',
      width: 360,
      render: (_: unknown, record: CRDApiGroupSummary) => (
        <Space size={[4, 4]} wrap>
          {record.kindNames.slice(0, 6).map((value) => (
            <Tag key={value}>{value}</Tag>
          ))}
          {record.kindNames.length > 6 ? <Tag>{`+${record.kindNames.length - 6}`}</Tag> : null}
        </Space>
      ),
    },
    {
      title: t('page.extensions.crd.versionsColumn', 'Versions'),
      key: 'versions',
      width: 240,
      render: (_: unknown, record: CRDApiGroupSummary) => (
        <Space size={[4, 4]} wrap>
          {record.versions.map((value) => (
            <Tag key={value} color="blue">
              {value}
            </Tag>
          ))}
        </Space>
      ),
    },
    {
      title: t('page.extensions.crd.scopeColumn', 'Scope mix'),
      key: 'scope',
      width: 220,
      render: (_: unknown, record: CRDApiGroupSummary) => (
        <Space size={[4, 4]} wrap>
          {record.namespacedCount > 0 ? (
            <Tag color="gold">
              {localeCode === 'zh_CN' ? `Namespaced ${record.namespacedCount}` : `Namespaced ${record.namespacedCount}`}
            </Tag>
          ) : null}
          {record.clusterCount > 0 ? (
            <Tag color="blue">
              {localeCode === 'zh_CN' ? `Cluster ${record.clusterCount}` : `Cluster ${record.clusterCount}`}
            </Tag>
          ) : null}
        </Space>
      ),
    },
    {
      title: '',
      key: 'action',
      width: 132,
      align: 'right',
      render: (_: unknown, record: CRDApiGroupSummary) => (
        <Button
          type="link"
          icon={<RightOutlined />}
          iconPosition="end"
          onClick={(event) => {
            event.stopPropagation()
            navigate(buildCRDApiGroupDetailPath(record.group))
          }}
        >
          {t('page.extensions.crd.openDetail', localeCode === 'zh_CN' ? '查看详情' : 'Open')}
        </Button>
      ),
    },
  ]

  return (
    <div className="kc-page">
      <PlatformClusterScopeHint resourceLabel="CRD" />
      <AdminTable
        columns={columns}
        dataSource={apiGroups}
        rowKey="group"
        loading={isLoading}
        pageSize={10}
        enableColumnSelection={false}
        scroll={{ x: 'max-content' }}
        onRow={(record: CRDApiGroupSummary) => ({
          onClick: () => navigate(buildCRDApiGroupDetailPath(record.group)),
          style: { cursor: 'pointer' },
        })}
        title={(
          <div className="kc-admin-table-title-block">
            <Text strong>{t('page.extensions.crd.title', 'CustomResourceDefinitions')}</Text>
            <Text type="secondary">{t('page.extensions.crd.desc', 'Review CRD API groups and definition names in the current cluster, then open one group to inspect its served kinds and resources.')}</Text>
          </div>
        )}
        toolbar={(
          <div className="kc-workload-table-filters">
            <PlatformScopeToolbar embedded showLabel={false} clusterWidth={180} namespaceWidth={180} />
          </div>
        )}
      />
    </div>
  )
}

export function CRDApiGroupDetailPage() {
  const { t, localeCode } = useI18n()
  const { groupName } = useParams()
  const { clusterId } = usePlatformScopeStore()
  const navigate = useNavigate()
  const [selectedCRDName, setSelectedCRDName] = useState<string | null>(null)

  const decodedGroupName = safeDecodeURIComponent(groupName)

  const { data, isLoading } = useQuery({
    queryKey: ['crds', clusterId],
    queryFn: () => api.get<ApiResponse<CRD[]>>(buildClusterScopedPath(clusterId!, 'extensions/crds')),
    enabled: !!clusterId,
  })

  const apiGroups = useMemo(() => groupCRDsByApi(data?.data ?? []), [data?.data])
  const groupSummary = useMemo(
    () => apiGroups.find((item) => item.group === decodedGroupName) ?? null,
    [apiGroups, decodedGroupName],
  )
  const groupCRDs = groupSummary?.crds ?? []
  const selectedCRD = useMemo(
    () => groupCRDs.find((item) => item.name === selectedCRDName) ?? groupCRDs[0] ?? null,
    [groupCRDs, selectedCRDName],
  )

  useEffect(() => {
    if (!groupCRDs.length) {
      setSelectedCRDName(null)
      return
    }
    if (selectedCRDName && groupCRDs.some((item) => item.name === selectedCRDName)) {
      return
    }
    setSelectedCRDName(groupCRDs[0].name)
  }, [groupCRDs, selectedCRDName])

  return (
    <div className="kc-page">
      <PageHeader
        title={decodedGroupName || t('route.extensions-group-detail.title', 'API Detail')}
        description={t('page.extensions.crd.groupDesc', 'Inspect the kinds served by this CRD entry and switch resources from the left-side CRD cards.')}
        actions={(
          <Button icon={<ArrowLeftOutlined />} onClick={() => navigate('/extensions')}>
            {t('page.extensions.crd.backToApis', localeCode === 'zh_CN' ? '返回 API 列表' : 'Back to API catalog')}
          </Button>
        )}
      />
      <PlatformScopeToolbar />
      <PlatformClusterScopeHint resourceLabel="CRD" />

      {!clusterId ? (
        <Card className="kc-detail-card" style={{ marginTop: 0 }}>
          <Empty description={t('platformScope.clusterPlaceholder', 'Select cluster')} />
        </Card>
      ) : isLoading ? (
        <Card className="kc-detail-card" style={{ marginTop: 0 }} loading />
      ) : !groupSummary ? (
        <Card className="kc-detail-card" style={{ marginTop: 0 }}>
          <Empty description={t('page.extensions.crd.groupEmpty', 'The selected API group is not available in the current cluster.')}>
            <Button onClick={() => navigate('/extensions')}>
              {t('page.extensions.crd.backToApis', localeCode === 'zh_CN' ? '返回 API 列表' : 'Back to API catalog')}
            </Button>
          </Empty>
        </Card>
      ) : (
        <div className="kc-crd-workspace">
          <Card className="kc-crd-sidebar-card" style={{ marginTop: 0 }}>
            <div className="kc-crd-sidebar-body">
              <div>
                <Text strong>{t('page.extensions.crd.kindCatalogTitle', 'CRD Resources')}</Text>
                <div>
                  <Text type="secondary">
                    {t('page.extensions.crd.kindCatalogDesc', 'Select a CRD resource card to inspect its kind and resource instances.')}
                  </Text>
                </div>
              </div>

              <div className="kc-tag-list">
                <Tag color="geekblue">{`${groupSummary.crdCount} ${localeCode === 'zh_CN' ? 'Kinds' : 'Kinds'}`}</Tag>
                {groupSummary.namespacedCount > 0 ? (
                  <Tag color="gold">
                    {localeCode === 'zh_CN' ? `Namespaced ${groupSummary.namespacedCount}` : `Namespaced ${groupSummary.namespacedCount}`}
                  </Tag>
                ) : null}
                {groupSummary.clusterCount > 0 ? (
                  <Tag color="blue">
                    {localeCode === 'zh_CN' ? `Cluster ${groupSummary.clusterCount}` : `Cluster ${groupSummary.clusterCount}`}
                  </Tag>
                ) : null}
              </div>

              <div className="kc-tag-list">
                {groupSummary.versions.map((value) => (
                  <Tag key={value} color="blue">
                    {value}
                  </Tag>
                ))}
              </div>

              <div className="kc-crd-kind-list">
                {groupCRDs.map((crd) => {
                  const isActive = selectedCRD?.name === crd.name
                  return (
                    <button
                      key={crd.name}
                      type="button"
                      className={`kc-crd-kind-item ${isActive ? 'is-active' : ''}`}
                      onClick={() => setSelectedCRDName(crd.name)}
                    >
                      <span className="kc-crd-kind-item__header">
                        <span className="kc-crd-kind-item__name">{crd.name}</span>
                        <Tag color={isNamespacedCRD(crd) ? 'gold' : 'blue'} bordered={false}>
                          {isNamespacedCRD(crd)
                            ? t('page.extensions.crd.namespacedScoped', 'Namespaced')
                            : t('page.extensions.crd.clusterScoped', 'Cluster scoped')}
                        </Tag>
                      </span>
                      <span className="kc-crd-kind-item__meta">
                        {`${t('page.extensions.crd.kindLabel', 'Kind')}: ${crd.kind}`}
                      </span>
                      <span className="kc-crd-kind-item__meta">
                        {`${t('page.extensions.crd.pluralLabel', 'Plural')}: ${crd.plural}`}
                      </span>
                      <span className="kc-crd-kind-item__meta">{getServedVersions(crd).join(' · ')}</span>
                    </button>
                  )
                })}
              </div>
            </div>
          </Card>

          <div className="kc-crd-detail-column">
            {selectedCRD ? (
              <CRDKindWorkspace crd={selectedCRD} />
            ) : (
              <Card className="kc-detail-card" style={{ marginTop: 0 }}>
                <Empty description={t('page.extensions.crd.emptySelection', 'Select a kind to inspect its resources.')} />
              </Card>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

/* ─── Helm Releases ─── */

export function HelmReleasesPage() {
  const { t } = useI18n()
  const { clusterId, namespace } = usePlatformScopeStore()
  const navigate = useNavigate()

  const { data, isLoading } = useQuery({
    queryKey: ['helm-releases', clusterId, namespace],
    queryFn: () => api.get<ApiResponse<HelmRelease[]>>(buildClusterScopedPath(clusterId!, 'helm/releases', namespace)),
    enabled: !!clusterId,
  })

  const columns: TableColumnsType<HelmRelease> = [
    {
      title: '名称',
      dataIndex: 'name',
      render: (value: string, record: HelmRelease) => (
        <Button type="link" style={{ paddingInline: 0 }} onClick={() => navigate(buildHelmReleaseDetailPath(value, record.namespace))}>
          {value}
        </Button>
      ),
    },
    { title: '命名空间', dataIndex: 'namespace' },
    { title: 'Chart', dataIndex: 'chart' },
    { title: 'Revision', dataIndex: 'revision' },
    {
      ...tableColumnPresets.status,
      title: '状态',
      dataIndex: 'status',
      render: (s: string) => <StatusTag value={s} />,
    },
    { title: 'App Version', dataIndex: 'appVersion' },
    { ...tableColumnPresets.datetime, title: 'Age', dataIndex: 'ageSeconds', render: (value: number) => formatAgeSeconds(value) },
  ]

  return (
    <div className="kc-page">
      <PageHeader title={t('page.extensions.helm.title', 'Helm Releases')} description={t('page.extensions.helm.desc', 'Inspect Helm release status, charts, and versions by cluster and namespace.')} />
      <PlatformScopeToolbar />
      <AdminTable
        columns={columns}
        dataSource={data?.data ?? []}
        rowKey={(record) => `${record.namespace}:${record.name}`}
        loading={isLoading}
        onRow={(record: HelmRelease) => ({
          onClick: () => navigate(buildHelmReleaseDetailPath(record.name, record.namespace)),
          style: { cursor: 'pointer' },
        })}
      />
    </div>
  )
}

export function HelmReleaseDetailPage() {
  const { t, localeCode } = useI18n()
  const { clusterId, namespace } = usePlatformScopeStore()
  const params = useParams()
  const [searchParams] = useSearchParams()
  const navigate = useNavigate()
  const releaseName = params.releaseName as string
  const detailNamespace = searchParams.get('namespace') || namespace || ''
  const [valuesDraft, setValuesDraft] = useState('')

  const detailQuery = useQuery({
    queryKey: ['helm-release-detail', clusterId, detailNamespace, releaseName],
    queryFn: () => api.get<ApiResponse<HelmReleaseDetail>>(
      `/clusters/${clusterId}/helm/releases/${encodeURIComponent(releaseName)}/detail?namespace=${encodeURIComponent(detailNamespace)}`,
    ),
    enabled: !!clusterId && !!detailNamespace,
  })

  const valuesQuery = useQuery({
    queryKey: ['helm-release-values', clusterId, detailNamespace, releaseName],
    queryFn: () => api.get<ApiResponse<HelmValues>>(
      `/clusters/${clusterId}/helm/releases/${encodeURIComponent(releaseName)}/values?namespace=${encodeURIComponent(detailNamespace)}`,
    ),
    enabled: !!clusterId && !!detailNamespace,
  })

  const historyQuery = useQuery({
    queryKey: ['helm-release-history', clusterId, detailNamespace, releaseName],
    queryFn: () => api.get<ApiResponse<HelmReleaseHistory[]>>(
      `/clusters/${clusterId}/helm/releases/${encodeURIComponent(releaseName)}/history?namespace=${encodeURIComponent(detailNamespace)}`,
    ),
    enabled: !!clusterId && !!detailNamespace,
  })

  useEffect(() => {
    setValuesDraft(valuesQuery.data?.data?.content ?? '')
  }, [valuesQuery.data?.data?.content])

  const historyColumns: TableColumnsType<HelmReleaseHistory> = [
    { title: 'Revision', dataIndex: 'revision', width: 96 },
    { title: 'Status', dataIndex: 'status', width: 140, render: (value?: string) => value ? <StatusTag value={value} /> : '-' },
    { title: 'Chart', dataIndex: 'chart' },
    { title: 'App Version', dataIndex: 'appVersion', width: 160 },
    { title: 'Values Digest', dataIndex: 'valuesDigest', width: 220, render: (value?: string) => value ? value.slice(0, 12) : '-' },
    { title: 'Manifest Digest', dataIndex: 'manifestDigest', width: 220, render: (value?: string) => value ? value.slice(0, 12) : '-' },
    { ...tableColumnPresets.datetime, title: localeCode === 'zh_CN' ? '更新时间' : 'Updated', dataIndex: 'updatedAt', render: (value?: string) => value ? formatDateTime(value) : '-' },
  ]

  const detail = detailQuery.data?.data
  const values = valuesQuery.data?.data
  const history = historyQuery.data?.data ?? []

  const tabs: TabsProps['items'] = [
    {
      key: 'values',
      label: 'values.yaml',
      children: (
        <YamlDraftDiffEditor
          title="values.yaml"
          description={t('page.extensions.helm.valuesDesc', 'Review the current values.yaml, edit a local draft, and inspect a side-by-side diff before applying changes.')}
          original={values?.original || values?.content || ''}
          modified={valuesDraft}
          onChange={setValuesDraft}
          onReset={() => setValuesDraft(values?.original || values?.content || '')}
          leftLabel={t('yamlDiffEditor.originalLabel', 'Original')}
          rightLabel={t('yamlDiffEditor.modifiedLabel', 'Modified')}
          editable={true}
          saveDisabled
        />
      ),
    },
    {
      key: 'history',
      label: (
        <Space size={6}>
          <HistoryOutlined />
          <span>{t('page.extensions.helm.historyTitle', 'Revision History')}</span>
        </Space>
      ),
      children: (
        <AdminTable
          columns={historyColumns}
          dataSource={history}
          rowKey={(record) => record.revision}
          enableColumnSelection={false}
          pageSize={10}
        />
      ),
    },
  ]

  return (
    <div className="kc-page">
      <PageHeader
        title={detail?.name || releaseName}
        description={t('page.extensions.helm.detailDesc', 'Inspect Helm release summary, values.yaml, and revision history in one workspace.')}
        actions={(
          <Button icon={<ArrowLeftOutlined />} onClick={() => navigate('/helm/releases')}>
            {t('common.back', 'Back')}
          </Button>
        )}
      />
      <PlatformScopeToolbar />

      {!clusterId || !detailNamespace ? (
        <Card className="kc-detail-card" style={{ marginTop: 0 }}>
          <Empty description={t('platformScope.clusterPlaceholder', 'Select cluster')} />
        </Card>
      ) : detailQuery.isLoading ? (
        <Card className="kc-detail-card" style={{ marginTop: 0 }} loading />
      ) : !detail ? (
        <Card className="kc-detail-card" style={{ marginTop: 0 }}>
          <Empty description={t('common.notFound', 'Not found')} />
        </Card>
      ) : (
        <>
          <Card className="kc-detail-card" style={{ marginTop: 0 }}>
            <Descriptions
              column={{ xs: 1, sm: 2, lg: 4 }}
              items={[
                { key: 'name', label: 'Release', children: detail.name },
                { key: 'namespace', label: 'Namespace', children: detail.namespace },
                { key: 'revision', label: 'Revision', children: detail.revision || '-' },
                { key: 'status', label: 'Status', children: detail.status ? <StatusTag value={detail.status} /> : '-' },
                { key: 'chart', label: 'Chart', children: detail.chart || '-' },
                { key: 'appVersion', label: 'App Version', children: detail.appVersion || '-' },
                { key: 'storageDriver', label: 'Storage', children: detail.storageDriver || '-' },
                { key: 'updatedAt', label: localeCode === 'zh_CN' ? '更新时间' : 'Updated', children: detail.updatedAt ? formatDateTime(detail.updatedAt) : '-' },
              ]}
            />
            {detail.description ? (
              <Alert
                style={{ marginTop: 16 }}
                type="info"
                showIcon
                message={localeCode === 'zh_CN' ? 'Release 描述' : 'Release Description'}
                description={detail.description}
              />
            ) : null}
          </Card>

          <Tabs items={tabs} />
        </>
      )}
    </div>
  )
}

/* ─── Helm Charts ─── */

export function HelmChartsPage() {
  const { t } = useI18n()
  return (
    <div className="kc-page">
      <PageHeader title={t('page.extensions.helmCharts.title', 'Helm Charts')} description={t('page.extensions.helmCharts.desc', 'The backend does not provide a Helm Charts API yet, so this page remains a standard empty placeholder.')} />
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
