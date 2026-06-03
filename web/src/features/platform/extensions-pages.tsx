import { lazy, Suspense, useDeferredValue, useEffect, useMemo, useState } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import {
  Alert,
  Button,
  Card,
  Checkbox,
  Descriptions,
  Drawer,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Select,
  Space,
  Spin,
  Tabs,
  Table,
  Tag,
  Typography,
  message,
} from 'antd'
import {
  ArrowLeftOutlined,
  CloudDownloadOutlined,
  DeleteOutlined,
  EditOutlined,
  EyeOutlined,
  HistoryOutlined,
  LinkOutlined,
  PlusOutlined,
  RightOutlined,
  RocketOutlined,
  SearchOutlined,
} from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AdminTable } from '@/components/admin-table'
import {
  ManagementDetailHeader,
  ManagementDensityButton,
  ManagementIconButton,
  ManagementQueryField,
  ManagementQueryPanel,
  ManagementRefreshButton,
  ManagementState,
  ManagementTableToolbar,
} from '@/components/management-list'
import './extensions-pages.css'
import './platform-pages.css'
import { useI18n } from '@/i18n'
import { StatusTag } from '@/components/status-tag'
import { YamlDraftDiffEditor } from '@/components/yaml-draft-diff-editor'
import { hasAllowedAction } from '@/features/auth/permission-snapshot'
import { buildClusterScopedPath } from '@/features/platform/platform-scope-query'
import { api } from '@/services/api-client'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { formatAgeSeconds, formatDateTime, formatRelativeTime } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import type {
  ApiResponse,
  HelmChart,
  HelmChartCatalog,
  HelmChartDetail,
  HelmChartInstallResource,
  HelmChartInstallResult,
  HelmChartValuesTemplate,
  HelmRelease,
  HelmReleaseDetail,
  HelmReleaseHistory,
  HelmValues,
} from '@/types'
import type { TableColumnsType, TabsProps } from 'antd'

const { Text } = Typography
const HELM_CHART_DEFAULT_PAGE_SIZE = 20
const HELM_CHART_MAX_PAGE_SIZE = 60
const HELM_CHART_PAGE_SIZE_OPTIONS = [20, 40, 60]

const K8sYamlEditor = lazy(async () => {
  const mod = await import('@/components/k8s-yaml-editor')
  return { default: mod.K8sYamlEditor }
})

type SearchableValue = string | number | boolean | null | undefined

interface HelmChartInstallTarget {
  chartName: string
  namespace: string
  releaseName: string
  timeoutSeconds?: number
  version: string
  wait?: boolean
}

function normalizeSearchKeyword(value: string) {
  return value.trim().toLowerCase()
}

function includesSearch(values: SearchableValue[], keyword: string) {
  if (!keyword) return true
  return values.some((value) => String(value ?? '').toLowerCase().includes(keyword))
}

function ResourceQueryPanel({
  placeholder,
  searchKeyword,
  setSearchKeyword,
}: {
  placeholder: string
  searchKeyword: string
  setSearchKeyword: (value: string) => void
}) {
  const { localeCode } = useI18n()

  return (
    <ManagementQueryPanel
      onFinish={() => undefined}
      actions={(
        <>
          <Button autoInsertSpace={false} disabled={!searchKeyword.trim()} htmlType="button" onClick={() => setSearchKeyword('')}>
            {localeCode === 'zh_CN' ? '重置' : 'Reset'}
          </Button>
          <Button autoInsertSpace={false} htmlType="submit" type="primary">
            {localeCode === 'zh_CN' ? '查询' : 'Search'}
          </Button>
        </>
      )}
    >
      <ManagementQueryField label={localeCode === 'zh_CN' ? '关键词' : 'Keyword'}>
        <Input
          allowClear
          className="soha-platform-compact-field soha-workload-search-input"
          prefix={<SearchOutlined />}
          size="small"
          value={searchKeyword}
          variant="filled"
          onChange={(event) => setSearchKeyword(event.target.value)}
          placeholder={placeholder}
        />
      </ManagementQueryField>
    </ManagementQueryPanel>
  )
}
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

function buildHelmReleaseDetailPath(name: string, namespace?: string | null, extraParams?: Record<string, string | null | undefined>) {
  const encodedName = encodeURIComponent(name)
  const params = new URLSearchParams()
  if (namespace) {
    params.set('namespace', namespace)
  }
  for (const [key, value] of Object.entries(extraParams ?? {})) {
    if (value) {
      params.set(key, value)
    }
  }
  const query = params.toString()
  return query ? `/helm/releases/${encodedName}?${query}` : `/helm/releases/${encodedName}`
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
    ? `soha:crd-yaml:${clusterId || 'none'}:${crd.name}:${effectiveNamespace}:${resource.name}`
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
      destroyOnHidden
      mask={{ closable: false }}
    >
      {!clusterId ? (
        <ManagementState bordered={false} compact kind="select-scope" title={localeCode === 'zh_CN' ? '请先选择集群' : 'Select a cluster first'} />
      ) : mode === 'edit' && yamlQuery.isLoading ? (
        <div style={{ height: 520, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
          <Spin size="large" />
        </div>
      ) : mode === 'edit' && yamlQuery.isError ? (
        <Alert
          type="error"
          showIcon
          title={localeCode === 'zh_CN' ? 'YAML 加载失败' : 'Failed to load YAML'}
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
  const { t, localeCode } = useI18n()
  const { clusterId, namespace } = usePlatformScopeStore()
  const queryClient = useQueryClient()
  const [createOpen, setCreateOpen] = useState(false)
  const [editingResource, setEditingResource] = useState<CRDResourceInstance | null>(null)
  const [deletingKey, setDeletingKey] = useState<string | null>(null)
  const [searchKeyword, setSearchKeyword] = useState('')
  const [tableSize, setTableSize] = useState<'small' | 'middle'>('small')
  const deferredSearchKeyword = useDeferredValue(searchKeyword)
  const normalizedKeyword = normalizeSearchKeyword(deferredSearchKeyword)

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

  const rawResources = resourcesQuery.data?.data ?? []
  const filteredResources = useMemo(
    () => rawResources.filter((item) => includesSearch([
      item.name,
      item.namespace,
      item.kind || crd.kind,
      item.apiVersion || `${crd.group}/${crd.version}`,
      item.status,
      ...Object.entries(item.summary ?? {}).flatMap(([key, value]) => [key, value]),
    ], normalizedKeyword)),
    [crd.group, crd.kind, crd.version, normalizedKeyword, rawResources],
  )
  const densityLabel = localeCode === 'zh_CN' ? '切换表格密度' : 'Toggle table density'

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
      width: 76,
      render: (_: unknown, record: CRDResourceInstance) => {
        const resourceKey = `${record.namespace || ''}/${record.name}`
        return (
          <Space size={2} className="soha-row-action-icons">
            <ManagementIconButton
              icon={<EditOutlined />}
              aria-label={t('common.edit', 'Edit')}
              tooltip={t('common.edit', 'Edit')}
              onClick={() => setEditingResource(record)}
            />
            <Popconfirm
              title={t('common.deleteConfirm', `Delete ${record.name}?`)}
              description={isNamespacedCRD(crd)
                ? (record.namespace
                    ? `${record.name} (${record.namespace})`
                    : record.name)
                : record.name}
              okText={t('common.delete', 'Delete')}
              cancelText={t('common.cancel', 'Cancel')}
              okButtonProps={{ danger: true, loading: deletingKey === resourceKey }}
              placement="topRight"
              onConfirm={() => deleteMutation.mutate({
                resourceName: record.name,
                resourceNamespace: record.namespace,
              })}
            >
              <ManagementIconButton
                danger
                icon={<DeleteOutlined />}
                aria-label={t('common.delete', 'Delete')}
                loading={deletingKey === resourceKey}
                tooltip={t('common.delete', 'Delete')}
              />
            </Popconfirm>
          </Space>
        )
      },
    },
  ]

  const instanceToolbar = (
    <ManagementTableToolbar>
      <Button autoInsertSpace={false} size="small" type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>
        {t('common.create', 'Create')}
      </Button>
      <ManagementDensityButton
        aria-label={densityLabel}
        title={densityLabel}
        tooltip={densityLabel}
        onClick={() => setTableSize((current) => current === 'middle' ? 'small' : 'middle')}
      />
      <ManagementRefreshButton
        aria-label={t('common.refresh', 'Refresh')}
        loading={resourcesQuery.isFetching}
        tooltip={t('common.refresh', 'Refresh')}
        onClick={() => void resourcesQuery.refetch()}
      />
    </ManagementTableToolbar>
  )

  return (
    <>
      <Card className="soha-detail-card" style={{ marginTop: 0 }}>
        <Space orientation="vertical" size={16} style={{ width: '100%' }}>
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
            title={isNamespacedCRD(crd)
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

      <ResourceQueryPanel
        placeholder={localeCode === 'zh_CN' ? '搜索资源名称 / Namespace / Kind / 状态 / 摘要' : 'Search resource name / namespace / kind / status / summary'}
        searchKeyword={searchKeyword}
        setSearchKeyword={setSearchKeyword}
      />
      <AdminTable
        className="soha-platform-table"
        columnSettingIconOnly
        columnSettingPlacement="header"
        shellClassName="soha-management-table-shell"
        columns={resourceColumns}
        dataSource={filteredResources}
        rowKey={(record) => `${record.namespace || '__cluster__'}:${record.name}`}
        loading={resourcesQuery.isLoading}
        paginationSummary={(
          <Text className="soha-workload-table-summary" type="secondary">
            {localeCode === 'zh_CN' ? `当前 ${filteredResources.length} / ${rawResources.length} 条` : `${filteredResources.length} / ${rawResources.length} items`}
          </Text>
        )}
        empty={resourcesQuery.isError
          ? (
            <Alert
              type="warning"
              showIcon
              title={t('page.extensions.crd.resourcesUnavailable', 'Custom resource list unavailable')}
              description={(resourcesQuery.error as Error)?.message}
            />
          )
          : <ManagementState bordered={false} compact title={t('page.extensions.crd.resourcesEmpty', 'No custom resources found for the selected CRD in the current scope.')} />}
        tableSize={tableSize}
        scroll={{ x: 'max-content' }}
        headerExtra={instanceToolbar}
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
  const [searchKeyword, setSearchKeyword] = useState('')
  const [tableSize, setTableSize] = useState<'small' | 'middle'>('small')
  const deferredSearchKeyword = useDeferredValue(searchKeyword)
  const normalizedKeyword = normalizeSearchKeyword(deferredSearchKeyword)

  const { data, isFetching, isLoading, refetch } = useQuery({
    queryKey: ['crds', clusterId],
    queryFn: () => api.get<ApiResponse<CRD[]>>(buildClusterScopedPath(clusterId!, 'extensions/crds')),
    enabled: !!clusterId,
  })

  const apiGroups = useMemo(() => groupCRDsByApi(data?.data ?? []), [data?.data])
  const filteredApiGroups = useMemo(
    () => apiGroups.filter((item) => includesSearch([
      item.group,
      ...item.crdNames,
      ...item.kindNames,
      ...item.versions,
    ], normalizedKeyword)),
    [apiGroups, normalizedKeyword],
  )
  const densityLabel = localeCode === 'zh_CN' ? '切换表格密度' : 'Toggle table density'

  const columns: TableColumnsType<CRDApiGroupSummary> = [
    {
      title: t('page.extensions.crd.apiGroupColumn', 'API Group'),
      dataIndex: 'group',
      width: 220,
      render: (_: string, record: CRDApiGroupSummary) => (
        <Button
          type="link"
          className="soha-crd-group-link"
          onClick={(event) => {
            event.stopPropagation()
            navigate(buildCRDApiGroupDetailPath(record.group))
          }}
        >
          <span className="soha-crd-group-card">
            <code className="soha-crd-group-card__value">{record.group}</code>
          </span>
        </Button>
      ),
    },
    {
      title: t('page.extensions.crd.crdNameColumn', 'CRD Names'),
      key: 'crdNames',
      width: 360,
      render: (_: unknown, record: CRDApiGroupSummary) => (
        <div className="soha-crd-name-chip-list">
          {record.crdNames.slice(0, 2).map((value) => (
            <code key={value} className="soha-crd-name-chip">{value}</code>
          ))}
          {record.crdNames.length > 2 ? (
            <code className="soha-crd-name-chip is-summary">
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
          iconPlacement="end"
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
    <div className="soha-page">
      <ResourceQueryPanel
        placeholder={localeCode === 'zh_CN' ? '搜索 API Group / CRD / Kind / Version' : 'Search API group / CRD / kind / version'}
        searchKeyword={searchKeyword}
        setSearchKeyword={setSearchKeyword}
      />
      <AdminTable
        className="soha-platform-table"
        columnSettingIconOnly
        columnSettingPlacement="header"
        shellClassName="soha-management-table-shell"
        columns={columns}
        dataSource={clusterId ? filteredApiGroups : []}
        rowKey="group"
        loading={isLoading}
        paginationSummary={(
          <Text className="soha-workload-table-summary" type="secondary">
            {localeCode === 'zh_CN' ? `当前 ${filteredApiGroups.length} / ${apiGroups.length} 条` : `${filteredApiGroups.length} / ${apiGroups.length} items`}
          </Text>
        )}
        pageSize={10}
        tableSize={tableSize}
        scroll={{ x: 'max-content' }}
        onRow={(record: CRDApiGroupSummary) => ({
          onClick: () => navigate(buildCRDApiGroupDetailPath(record.group)),
          style: { cursor: 'pointer' },
        })}
        headerExtra={(
          <ManagementTableToolbar>
            <ManagementDensityButton
              aria-label={densityLabel}
              title={densityLabel}
              tooltip={densityLabel}
              onClick={() => setTableSize((current) => current === 'middle' ? 'small' : 'middle')}
            />
            <ManagementRefreshButton
              aria-label={t('common.refresh', 'Refresh')}
              disabled={!clusterId}
              loading={isFetching}
              tooltip={t('common.refresh', 'Refresh')}
              onClick={() => {
                if (clusterId) {
                  void refetch()
                }
              }}
            />
          </ManagementTableToolbar>
        )}
        empty={<ManagementState bordered={false} compact kind={!clusterId ? 'select-scope' : 'empty'} title={!clusterId ? t('platformScope.clusterPlaceholder', 'Select cluster') : t('page.extensions.crd.empty', 'No CRDs in the current cluster.')} />}
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
    <div className="soha-page">
      <ManagementDetailHeader
        title={decodedGroupName || t('route.extensions-group-detail.title', 'API Detail')}
        description={t('page.extensions.crd.kindCatalogDesc', 'Select a CRD resource card to inspect its kind and resource instances.')}
        actions={(
          <ManagementTableToolbar>
            <Button autoInsertSpace={false} size="small" icon={<ArrowLeftOutlined />} onClick={() => navigate('/extensions')}>
              {t('page.extensions.crd.backToApis', localeCode === 'zh_CN' ? '返回 API 列表' : 'Back to API catalog')}
            </Button>
          </ManagementTableToolbar>
        )}
      />
      {!clusterId ? (
        <Card className="soha-detail-card" style={{ marginTop: 0 }}>
          <ManagementState compact kind="select-scope" title={t('platformScope.clusterPlaceholder', 'Select cluster')} />
        </Card>
      ) : isLoading ? (
        <Card className="soha-detail-card" style={{ marginTop: 0 }} loading />
      ) : !groupSummary ? (
        <Card className="soha-detail-card" style={{ marginTop: 0 }}>
          <ManagementState
            compact
            kind="not-found"
            title={t('page.extensions.crd.groupEmpty', 'The selected API group is not available in the current cluster.')}
            actions={(
              <Button onClick={() => navigate('/extensions')}>
                {t('page.extensions.crd.backToApis', localeCode === 'zh_CN' ? '返回 API 列表' : 'Back to API catalog')}
              </Button>
            )}
          />
        </Card>
      ) : (
        <div className="soha-crd-workspace">
          <Card className="soha-crd-sidebar-card" style={{ marginTop: 0 }}>
            <div className="soha-crd-sidebar-body">
              <div>
                <Text strong>{t('page.extensions.crd.kindCatalogTitle', 'CRD Resources')}</Text>
                <div>
                  <Text type="secondary">
                    {t('page.extensions.crd.kindCatalogDesc', 'Select a CRD resource card to inspect its kind and resource instances.')}
                  </Text>
                </div>
              </div>

              <div className="soha-tag-list">
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

              <div className="soha-tag-list">
                {groupSummary.versions.map((value) => (
                  <Tag key={value} color="blue">
                    {value}
                  </Tag>
                ))}
              </div>

              <div className="soha-crd-kind-list">
                {groupCRDs.map((crd) => {
                  const isActive = selectedCRD?.name === crd.name
                  return (
                    <button
                      key={crd.name}
                      type="button"
                      className={`soha-crd-kind-item ${isActive ? 'is-active' : ''}`}
                      onClick={() => setSelectedCRDName(crd.name)}
                    >
                      <span className="soha-crd-kind-item__header">
                        <span className="soha-crd-kind-item__name">{crd.name}</span>
                        <Tag color={isNamespacedCRD(crd) ? 'gold' : 'blue'} bordered={false}>
                          {isNamespacedCRD(crd)
                            ? t('page.extensions.crd.namespacedScoped', 'Namespaced')
                            : t('page.extensions.crd.clusterScoped', 'Cluster scoped')}
                        </Tag>
                      </span>
                      <span className="soha-crd-kind-item__meta">
                        {`${t('page.extensions.crd.kindLabel', 'Kind')}: ${crd.kind}`}
                      </span>
                      <span className="soha-crd-kind-item__meta">
                        {`${t('page.extensions.crd.pluralLabel', 'Plural')}: ${crd.plural}`}
                      </span>
                      <span className="soha-crd-kind-item__meta">{getServedVersions(crd).join(' · ')}</span>
                    </button>
                  )
                })}
              </div>
            </div>
          </Card>

          <div className="soha-crd-detail-column">
            {selectedCRD ? (
              <CRDKindWorkspace crd={selectedCRD} />
            ) : (
              <Card className="soha-detail-card" style={{ marginTop: 0 }}>
                <ManagementState compact kind="select-scope" title={t('page.extensions.crd.emptySelection', 'Select a kind to inspect its resources.')} />
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
  const { t, localeCode } = useI18n()
  const { clusterId, namespace } = usePlatformScopeStore()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [searchKeyword, setSearchKeyword] = useState('')
  const [tableSize, setTableSize] = useState<'small' | 'middle'>('small')
  const [deletingReleaseKey, setDeletingReleaseKey] = useState<string | null>(null)
  const deferredSearchKeyword = useDeferredValue(searchKeyword)
  const normalizedKeyword = normalizeSearchKeyword(deferredSearchKeyword)

  const { data, isFetching, isLoading, refetch } = useQuery({
    queryKey: ['helm-releases', clusterId, namespace],
    queryFn: () => api.get<ApiResponse<HelmRelease[]>>(buildClusterScopedPath(clusterId!, 'helm/releases', namespace)),
    enabled: !!clusterId,
  })

  const deleteReleaseMutation = useMutation({
    mutationFn: (record: HelmRelease) => {
      if (!clusterId) {
        throw new Error(t('platformScope.clusterPlaceholder', 'Select cluster'))
      }
      return api.delete(`/clusters/${clusterId}/helm/releases/${encodeURIComponent(record.name)}?namespace=${encodeURIComponent(record.namespace)}`)
    },
    onMutate: (record) => setDeletingReleaseKey(`${record.namespace}/${record.name}`),
    onSettled: () => setDeletingReleaseKey(null),
    onSuccess: () => {
      void message.success(localeCode === 'zh_CN' ? 'Helm Release 已删除' : 'Helm release deleted')
      void queryClient.invalidateQueries({ queryKey: ['helm-releases', clusterId] })
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const rawItems = data?.data ?? []
  const filteredItems = useMemo(
    () => rawItems.filter((item) => includesSearch([
      item.name,
      item.namespace,
      item.chart,
      item.revision,
      item.status,
      item.appVersion,
    ], normalizedKeyword)),
    [normalizedKeyword, rawItems],
  )
  const emptyTitle = !clusterId
    ? t('platformScope.clusterPlaceholder', 'Select cluster')
    : normalizedKeyword && rawItems.length > 0
      ? (localeCode === 'zh_CN' ? '没有匹配的 Helm Release' : 'No matching Helm releases')
      : t('page.extensions.helm.empty', 'No Helm releases in the current scope.')
  const densityLabel = localeCode === 'zh_CN' ? '切换表格密度' : 'Toggle table density'

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
    {
      title: '',
      key: 'actions',
      fixed: 'right',
      align: 'center',
      width: 116,
      render: (_: unknown, record: HelmRelease) => {
        const releaseKey = `${record.namespace}/${record.name}`
        const canUpdate = hasAllowedAction(record.allowedActions, 'update')
        const canDelete = hasAllowedAction(record.allowedActions, 'delete')
        return (
          <Space size={2} className="soha-row-action-icons" onClick={(event) => event.stopPropagation()}>
            <ManagementIconButton
              icon={<EyeOutlined />}
              aria-label={localeCode === 'zh_CN' ? '查看 values.yaml' : 'View values.yaml'}
              tooltip={localeCode === 'zh_CN' ? '查看 values.yaml' : 'View values.yaml'}
              onClick={() => navigate(buildHelmReleaseDetailPath(record.name, record.namespace, { tab: 'values', mode: 'diff' }))}
            />
            {canUpdate ? (
              <ManagementIconButton
                icon={<EditOutlined />}
                aria-label={localeCode === 'zh_CN' ? '编辑并比对 values.yaml' : 'Edit and compare values.yaml'}
                tooltip={localeCode === 'zh_CN' ? '编辑并比对 values.yaml' : 'Edit and compare values.yaml'}
                onClick={() => navigate(buildHelmReleaseDetailPath(record.name, record.namespace, { tab: 'values', mode: 'edit' }))}
              />
            ) : null}
            {canDelete ? (
              <Popconfirm
                title={localeCode === 'zh_CN' ? '删除 Helm Release?' : 'Delete Helm release?'}
                description={`${record.name} (${record.namespace})`}
                okText={localeCode === 'zh_CN' ? '删除' : 'Delete'}
                cancelText={t('common.cancel', 'Cancel')}
                okButtonProps={{ danger: true, loading: deletingReleaseKey === releaseKey }}
                placement="topRight"
                onConfirm={() => deleteReleaseMutation.mutate(record)}
              >
                <ManagementIconButton
                  danger
                  icon={<DeleteOutlined />}
                  aria-label={localeCode === 'zh_CN' ? '删除 Helm Release' : 'Delete Helm release'}
                  loading={deletingReleaseKey === releaseKey}
                  tooltip={localeCode === 'zh_CN' ? '删除 Helm Release' : 'Delete Helm release'}
                />
              </Popconfirm>
            ) : null}
          </Space>
        )
      },
    },
  ]

  return (
    <div className="soha-page">
      <ResourceQueryPanel
        placeholder={localeCode === 'zh_CN' ? '搜索 Release / Namespace / Chart / 状态 / 版本' : 'Search release / namespace / chart / status / version'}
        searchKeyword={searchKeyword}
        setSearchKeyword={setSearchKeyword}
      />
      <AdminTable
        className="soha-platform-table"
        columnSettingIconOnly
        columnSettingPlacement="header"
        shellClassName="soha-management-table-shell"
        columns={columns}
        dataSource={clusterId ? filteredItems : []}
        rowKey={(record) => `${record.namespace}:${record.name}`}
        loading={isLoading}
        paginationSummary={(
          <Text className="soha-workload-table-summary" type="secondary">
            {localeCode === 'zh_CN' ? `当前 ${filteredItems.length} / ${rawItems.length} 条` : `${filteredItems.length} / ${rawItems.length} items`}
          </Text>
        )}
        tableSize={tableSize}
        scroll={{ x: 'max-content' }}
        onRow={(record: HelmRelease) => ({
          onClick: () => navigate(buildHelmReleaseDetailPath(record.name, record.namespace)),
          style: { cursor: 'pointer' },
        })}
        headerExtra={(
          <ManagementTableToolbar>
            <ManagementDensityButton
              aria-label={densityLabel}
              title={densityLabel}
              tooltip={densityLabel}
              onClick={() => setTableSize((current) => current === 'middle' ? 'small' : 'middle')}
            />
            <ManagementRefreshButton
              aria-label={t('common.refresh', 'Refresh')}
              disabled={!clusterId}
              loading={isFetching}
              tooltip={t('common.refresh', 'Refresh')}
              onClick={() => {
                if (clusterId) {
                  void refetch()
                }
              }}
            />
          </ManagementTableToolbar>
        )}
        empty={<ManagementState bordered={false} compact kind={!clusterId ? 'select-scope' : 'empty'} title={emptyTitle} />}
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
  const queryClient = useQueryClient()
  const releaseName = params.releaseName as string
  const detailNamespace = searchParams.get('namespace') || namespace || ''
  const requestedTab = searchParams.get('tab') === 'history' ? 'history' : 'values'
  const [activeTab, setActiveTab] = useState(requestedTab)
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

  const updateValuesMutation = useMutation({
    mutationFn: () => {
      if (!clusterId || !detailNamespace) {
        throw new Error(t('platformScope.clusterPlaceholder', 'Select cluster'))
      }
      return api.put<ApiResponse<HelmValues>>(
        `/clusters/${clusterId}/helm/releases/${encodeURIComponent(releaseName)}/values?namespace=${encodeURIComponent(detailNamespace)}`,
        { content: valuesDraft },
      )
    },
    onSuccess: (response) => {
      setValuesDraft(response.data.content)
      void message.success(localeCode === 'zh_CN' ? 'values.yaml 已应用' : 'values.yaml applied')
      void queryClient.invalidateQueries({ queryKey: ['helm-release-values', clusterId, detailNamespace, releaseName] })
      void queryClient.invalidateQueries({ queryKey: ['helm-release-detail', clusterId, detailNamespace, releaseName] })
      void queryClient.invalidateQueries({ queryKey: ['helm-release-history', clusterId, detailNamespace, releaseName] })
      void queryClient.invalidateQueries({ queryKey: ['helm-releases', clusterId] })
    },
    onError: (err: Error) => void message.error(err.message),
  })

  useEffect(() => {
    setActiveTab(requestedTab)
  }, [requestedTab])

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
  const valuesOriginal = values?.original || values?.content || ''
  const canEditValues = Boolean((values?.editable || detail?.valuesEditable) && hasAllowedAction(values?.allowedActions ?? detail?.allowedActions, 'update'))
  const valuesChanged = valuesDraft !== valuesOriginal

  const tabs: TabsProps['items'] = [
    {
      key: 'values',
      label: 'values.yaml',
      children: valuesQuery.isError ? (
        <Alert
          type="error"
          showIcon
          title={localeCode === 'zh_CN' ? 'values.yaml 加载失败' : 'Failed to load values.yaml'}
          description={(valuesQuery.error as Error)?.message}
        />
      ) : (
        <YamlDraftDiffEditor
          title="values.yaml"
          description={t('page.extensions.helm.valuesDesc', 'Edit the values.yaml draft on the left; compare it against the Helm runtime values on the right before applying changes.')}
          original={valuesOriginal}
          modified={valuesDraft}
          onChange={setValuesDraft}
          onReset={() => setValuesDraft(valuesOriginal)}
          onApply={canEditValues ? () => updateValuesMutation.mutate() : undefined}
          applying={updateValuesMutation.isPending}
          applyDisabled={!canEditValues || !valuesDraft.trim() || !valuesChanged || updateValuesMutation.isPending}
          leftLabel={t('yamlDiffEditor.draftLabel', 'Values draft')}
          rightLabel={t('yamlDiffEditor.runtimeLabel', 'Helm runtime values')}
          editable={canEditValues}
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
          className="soha-platform-table"
          columnSettingIconOnly
          columnSettingPlacement="header"
          shellClassName="soha-management-table-shell"
          title={t('page.extensions.helm.historyTitle', 'Revision History')}
          columns={historyColumns}
          dataSource={history}
          rowKey={(record) => record.revision}
          pageSize={10}
          tableSize="small"
          scroll={{ x: 'max-content' }}
        />
      ),
    },
  ]

  return (
    <div className="soha-page">
      <ManagementDetailHeader
        title={detail?.name || releaseName}
        description={detailNamespace}
        actions={(
          <ManagementTableToolbar>
            <Button autoInsertSpace={false} size="small" icon={<ArrowLeftOutlined />} onClick={() => navigate('/helm/releases')}>
              {t('common.back', 'Back')}
            </Button>
          </ManagementTableToolbar>
        )}
      />
      {!clusterId || !detailNamespace ? (
        <Card className="soha-detail-card" style={{ marginTop: 0 }}>
          <ManagementState compact kind="select-scope" title={t('platformScope.clusterPlaceholder', 'Select cluster')} />
        </Card>
      ) : detailQuery.isLoading ? (
        <Card className="soha-detail-card" style={{ marginTop: 0 }} loading />
      ) : !detail ? (
        <Card className="soha-detail-card" style={{ marginTop: 0 }}>
          <ManagementState compact kind="not-found" title={t('common.notFound', 'Not found')} />
        </Card>
      ) : (
        <>
          <Card className="soha-detail-card" style={{ marginTop: 0 }}>
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
                title={localeCode === 'zh_CN' ? 'Release 描述' : 'Release Description'}
                description={detail.description}
              />
            ) : null}
          </Card>

          <Tabs activeKey={activeTab} onChange={setActiveTab} items={tabs} />
        </>
      )}
    </div>
  )
}

/* ─── Helm Charts ─── */

interface HelmChartInstallFormValues {
  repositoryName?: string
  repositoryUrl: string
  chartName: string
  version: string
  releaseName: string
  namespace: string
  createNamespace: boolean
  wait: boolean
  timeoutSeconds: number
}

function buildHelmChartDetailResourcePath(chart: HelmChart) {
  return `helm/charts/${encodeURIComponent(chart.repositoryName ?? '')}/${encodeURIComponent(chart.name)}`
}

function defaultHelmReleaseName(chartName?: string) {
  const normalized = (chartName || 'release')
    .toLowerCase()
    .replace(/[^a-z0-9-]+/g, '-')
    .replace(/^-+|-+$/g, '')
  return normalized || 'release'
}

function retryHelmReleaseName(releaseName?: string, chartName?: string) {
  const base = defaultHelmReleaseName(releaseName || chartName)
  const suffix = new Date().toISOString().slice(11, 19).replace(/:/g, '')
  const maxBaseLength = Math.max(1, 53 - suffix.length - 1)
  return `${base.slice(0, maxBaseLength).replace(/-+$/g, '') || 'release'}-${suffix}`
}

function isHelmReleaseNameConflictError(message?: string) {
  const normalized = (message || '').toLowerCase()
  return normalized.includes('cannot re-use a name that is still in use') || normalized.includes('already used by helm release history')
}

function formatHelmInstallError(message: string, localeCode: string, target?: HelmChartInstallTarget | null) {
  if (isHelmReleaseNameConflictError(message)) {
    if (localeCode === 'zh_CN') {
      return `Release "${target?.releaseName || '-'}" 在命名空间 "${target?.namespace || '-'}" 已有 Helm 记录。请换一个 Release 名，或先到 Helm Releases 清理已有 release 后重试。`
    }
    return `Release "${target?.releaseName || '-'}" in namespace "${target?.namespace || '-'}" already has Helm history. Choose another release name, or clean up the existing release in Helm Releases before retrying.`
  }
  return message
}

function normalizeHelmCompareValue(value?: string | null) {
  return (value || '').trim().toLowerCase()
}

function isHelmReleaseDeployed(status?: string) {
  return normalizeHelmCompareValue(status) === 'deployed'
}

function helmReleaseMatchesInstallTarget(release?: HelmReleaseDetail, target?: HelmChartInstallTarget | null) {
  if (!release || !target) return false
  if (normalizeHelmCompareValue(release.name) !== normalizeHelmCompareValue(target.releaseName)) return false
  if (normalizeHelmCompareValue(release.namespace) !== normalizeHelmCompareValue(target.namespace)) return false
  if (target.chartName && normalizeHelmCompareValue(release.chartName) !== normalizeHelmCompareValue(target.chartName)) return false
  if (target.version && (release.chartVersion || '').trim() !== target.version.trim()) return false
  return true
}

function mapObservedHelmReleaseToInstallResult(release: HelmReleaseDetail, localeCode: string): HelmChartInstallResult {
  return {
    name: release.name,
    namespace: release.namespace,
    revision: release.revision,
    status: release.status,
    chart: release.chart,
    chartName: release.chartName,
    chartVersion: release.chartVersion,
    appVersion: release.appVersion,
    description: release.description || (localeCode === 'zh_CN'
      ? '检测到同名 Helm Release 已部署，本次安装请求已满足。'
      : 'Detected an already deployed Helm release; this install request is already satisfied.'),
    notes: release.notes,
    resources: [],
  }
}

function defaultHelmChartInstallForm(chart: HelmChart, namespace?: string | null): HelmChartInstallFormValues {
  return {
    repositoryName: chart.repositoryName,
    repositoryUrl: chart.repositoryUrl || chart.urls?.[0] || '',
    chartName: chart.name,
    version: chart.latestVersion || '',
    releaseName: defaultHelmReleaseName(chart.name),
    namespace: namespace || 'default',
    createNamespace: true,
    wait: true,
    timeoutSeconds: 300,
  }
}

function getHelmChartVersionOptions(detail?: HelmChartDetail, chart?: HelmChart | null) {
  const versions = detail?.availableVersions?.length
    ? detail.availableVersions.map((item) => ({
        label: item.appVersion ? `${item.version} · ${item.appVersion}` : item.version,
        value: item.version,
      }))
    : (detail?.versions ?? chart?.versions ?? []).map((version) => ({ label: version, value: version }))
  const latest = detail?.latestVersion || chart?.latestVersion
  if (latest && !versions.some((item) => item.value === latest)) {
    versions.unshift({ label: latest, value: latest })
  }
  return versions
}

function getHelmChartBadges(chart: HelmChart) {
  const badges: Array<{ color?: string; label: string }> = []
  if (chart.official) badges.push({ color: 'blue', label: 'Official' })
  if (chart.verifiedPublisher) badges.push({ color: 'green', label: 'Verified' })
  if (chart.cncf) badges.push({ color: 'cyan', label: 'CNCF' })
  if (chart.signed) badges.push({ color: 'purple', label: 'Signed' })
  if (chart.deprecated) badges.push({ color: 'warning', label: 'Deprecated' })
  return badges
}

function hasHelmChartSecuritySummary(chart: HelmChart) {
  return Boolean((chart.securityCritical ?? 0) + (chart.securityHigh ?? 0) + (chart.securityMedium ?? 0) + (chart.securityLow ?? 0) + (chart.securityUnknown ?? 0))
}

function renderHelmReadme(readme?: string) {
  if (!readme?.trim()) {
    return <Text type="secondary">-</Text>
  }
  return (
    <pre style={{
      background: 'var(--soha-color-surface-muted)',
      border: '1px solid var(--soha-color-border)',
      borderRadius: 6,
      fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace',
      fontSize: 12,
      lineHeight: 1.55,
      maxHeight: 420,
      overflow: 'auto',
      padding: 12,
      whiteSpace: 'pre-wrap',
    }}>
      {readme}
    </pre>
  )
}

function formatHelmChartCount(value: number, localeCode: string) {
  return new Intl.NumberFormat(localeCode === 'zh_CN' ? 'zh-CN' : 'en-US').format(value)
}

export function HelmChartsPage() {
  const { t, localeCode } = useI18n()
  const { clusterId, namespace } = usePlatformScopeStore()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [installForm] = Form.useForm<HelmChartInstallFormValues>()
  const [searchKeyword, setSearchKeyword] = useState('')
  const [chartPage, setChartPage] = useState(1)
  const [chartPageSize, setChartPageSize] = useState(HELM_CHART_DEFAULT_PAGE_SIZE)
  const [tableSize, setTableSize] = useState<'small' | 'middle'>('small')
  const [selectedChart, setSelectedChart] = useState<HelmChart | null>(null)
  const [selectedVersion, setSelectedVersion] = useState('')
  const [valuesDraft, setValuesDraft] = useState('')
  const [drawerTabKey, setDrawerTabKey] = useState('overview')
  const [installTarget, setInstallTarget] = useState<HelmChartInstallTarget | null>(null)
  const [installResult, setInstallResult] = useState<HelmChartInstallResult | null>(null)
  const [installError, setInstallError] = useState('')
  const [installStartedAt, setInstallStartedAt] = useState<number | null>(null)
  const [installElapsedSeconds, setInstallElapsedSeconds] = useState(0)
  const deferredSearchKeyword = useDeferredValue(searchKeyword)
  const normalizedKeyword = normalizeSearchKeyword(deferredSearchKeyword)
  const chartOffset = (chartPage - 1) * chartPageSize

  const chartsQuery = useQuery({
    queryKey: ['helm-charts', clusterId, normalizedKeyword, chartPage, chartPageSize],
    queryFn: () => api.get<ApiResponse<HelmChartCatalog>>(buildClusterScopedPath(clusterId!, 'helm/charts', null, {
      keyword: normalizedKeyword,
      limit: chartPageSize,
      offset: chartOffset,
    })),
    enabled: !!clusterId,
  })

  const detailQuery = useQuery({
    queryKey: ['helm-chart-detail', clusterId, selectedChart?.repositoryName, selectedChart?.name, selectedVersion],
    queryFn: () => api.get<ApiResponse<HelmChartDetail>>(buildClusterScopedPath(clusterId!, buildHelmChartDetailResourcePath(selectedChart!), null, {
      version: selectedVersion,
    })),
    enabled: !!clusterId && !!selectedChart?.repositoryName && !!selectedChart?.name,
  })

  const catalog = chartsQuery.data?.data
  const rawItems = catalog?.charts ?? []
  const totalChartCount = catalog?.totalCount ?? catalog?.chartCount ?? rawItems.length
  const loadedChartCount = catalog?.loadedCount ?? catalog?.chartCount ?? rawItems.length
  const currentOffset = catalog?.offset ?? chartOffset
  const currentRangeStart = loadedChartCount > 0 ? currentOffset + 1 : 0
  const currentRangeEnd = loadedChartCount > 0 ? currentOffset + loadedChartCount : 0
  const formattedTotalChartCount = formatHelmChartCount(totalChartCount, localeCode)
  const detail = detailQuery.data?.data
  const activeChart = detail ?? selectedChart
  const activeVersion = selectedVersion || activeChart?.latestVersion || ''
  const canInstallActiveChart = hasAllowedAction(activeChart?.allowedActions, 'create')
  const installHasRecoverableConflict = isHelmReleaseNameConflictError(installError)

  const valuesQuery = useQuery({
    queryKey: ['helm-chart-values-template', clusterId, activeChart?.packageId, activeChart?.name, activeVersion],
    queryFn: () => api.get<ApiResponse<HelmChartValuesTemplate>>(buildClusterScopedPath(clusterId!, 'helm/charts/values', null, {
      packageId: activeChart?.packageId,
      name: activeChart?.name,
      version: activeVersion,
    })),
    enabled: !!clusterId && !!activeChart?.packageId && !!activeVersion && Boolean(selectedChart),
  })

  const installReleaseQuery = useQuery({
    queryKey: ['helm-install-release-progress', clusterId, installTarget?.namespace, installTarget?.releaseName],
    queryFn: () => api.get<ApiResponse<HelmReleaseDetail>>(
      `/clusters/${clusterId}/helm/releases/${encodeURIComponent(installTarget!.releaseName)}/detail?namespace=${encodeURIComponent(installTarget!.namespace)}`,
    ),
    enabled: !!clusterId
      && Boolean(installStartedAt && !installResult && (!installError || installHasRecoverableConflict))
      && !!installTarget?.namespace
      && !!installTarget?.releaseName,
    retry: false,
    refetchInterval: 5000,
  })

  const installMutation = useMutation({
    mutationFn: (values: HelmChartInstallFormValues) => api.post<ApiResponse<HelmChartInstallResult>>(buildClusterScopedPath(clusterId!, 'helm/charts/install'), {
      ...values,
      valuesYaml: valuesDraft,
    }),
    onSuccess: (response) => {
      const item = response.data
      setInstallResult(item)
      setInstallError('')
      message.success(localeCode === 'zh_CN' ? `已安装 ${item.name}` : `Installed ${item.name}`)
      void queryClient.invalidateQueries({ queryKey: ['helm-releases', clusterId] })
    },
    onError: (error) => {
      setInstallError(error instanceof Error ? error.message : String(error))
    },
  })

  useEffect(() => {
    if (!selectedChart) return
    installForm.resetFields()
    installForm.setFieldsValue(defaultHelmChartInstallForm(selectedChart, namespace))
  }, [installForm, namespace, selectedChart])

  useEffect(() => {
    setChartPage(1)
  }, [clusterId, normalizedKeyword])

  useEffect(() => {
    if (!selectedChart) return
    const nextVersion = selectedVersion || detail?.latestVersion || selectedChart.latestVersion || ''
    installForm.setFieldsValue({
      repositoryName: activeChart?.repositoryName || selectedChart.repositoryName,
      repositoryUrl: activeChart?.repositoryUrl || selectedChart.repositoryUrl || selectedChart.urls?.[0] || '',
      chartName: activeChart?.name || selectedChart.name,
      version: nextVersion,
    })
  }, [activeChart?.name, activeChart?.repositoryName, activeChart?.repositoryUrl, detail?.latestVersion, installForm, selectedChart, selectedVersion])

  useEffect(() => {
    if (!selectedChart) return
    const content = valuesQuery.data?.data.content
    if (typeof content === 'string') {
      setValuesDraft(content)
    }
  }, [selectedChart, valuesQuery.data?.data.content])

  useEffect(() => {
    if (!installStartedAt || installResult || installError) return
    setInstallElapsedSeconds(Math.max(0, Math.floor((Date.now() - installStartedAt) / 1000)))
    const timer = window.setInterval(() => {
      setInstallElapsedSeconds(Math.max(0, Math.floor((Date.now() - installStartedAt) / 1000)))
    }, 1000)
    return () => window.clearInterval(timer)
  }, [installError, installResult, installStartedAt])

  useEffect(() => {
    const release = installReleaseQuery.data?.data
    if (!release || installResult || !installError || !installHasRecoverableConflict) return
    if (!helmReleaseMatchesInstallTarget(release, installTarget) || !isHelmReleaseDeployed(release.status)) return
    const item = mapObservedHelmReleaseToInstallResult(release, localeCode)
    setInstallResult(item)
    setInstallError('')
    void queryClient.invalidateQueries({ queryKey: ['helm-releases', clusterId] })
  }, [clusterId, installError, installHasRecoverableConflict, installReleaseQuery.data?.data, installResult, installTarget, localeCode, queryClient])

  const errorMessage = chartsQuery.error instanceof Error ? chartsQuery.error.message : ''
  const emptyTitle = !clusterId
    ? t('platformScope.clusterPlaceholder', 'Select cluster')
    : chartsQuery.isError
      ? t('page.extensions.helmCharts.errorTitle', 'Chart catalog unavailable')
      : normalizedKeyword
        ? (localeCode === 'zh_CN' ? '没有匹配的 Helm Chart' : 'No matching Helm charts')
        : t('page.extensions.helmCharts.emptyTitle', 'No Helm charts')
  const emptyDescription = chartsQuery.isError
    ? errorMessage
    : t('page.extensions.helmCharts.emptyDesc', 'No charts were returned by Artifact Hub.')
  const densityLabel = localeCode === 'zh_CN' ? '切换表格密度' : 'Toggle table density'
  const versionOptions = getHelmChartVersionOptions(detail, selectedChart)
  const drawerTitle = activeChart
    ? `Chart: ${activeChart.repositoryName ? `${activeChart.repositoryName}/` : ''}${activeChart.name}`
    : 'Chart'

  const openChartDrawer = (chart: HelmChart) => {
    setSelectedChart(chart)
    setSelectedVersion(chart.latestVersion || '')
    setValuesDraft('')
    setDrawerTabKey('overview')
    setInstallTarget(null)
    setInstallResult(null)
    setInstallError('')
    setInstallStartedAt(null)
    setInstallElapsedSeconds(0)
  }

  const submitInstall = async () => {
    if (!canInstallActiveChart) {
      setDrawerTabKey('install')
      return
    }
    const values = await installForm.validateFields()
    setDrawerTabKey('install')
    setInstallTarget({
      chartName: values.chartName,
      namespace: values.namespace,
      releaseName: values.releaseName,
      timeoutSeconds: values.timeoutSeconds,
      version: values.version,
      wait: values.wait,
    })
    setInstallResult(null)
    setInstallError('')
    setInstallStartedAt(Date.now())
    setInstallElapsedSeconds(0)
    installMutation.mutate(values)
  }

  const columns: TableColumnsType<HelmChart> = [
    {
      title: 'Chart',
      dataIndex: 'name',
      width: 300,
      render: (value: string, record: HelmChart) => (
        <Space size={10} align="start">
          {record.logoImageUrl ? (
            <img
              src={record.logoImageUrl}
              alt=""
              style={{ width: 32, height: 32, borderRadius: 4, objectFit: 'contain', border: '1px solid var(--soha-color-border)' }}
            />
          ) : null}
          <Space orientation="vertical" size={2}>
            <Space size={6} wrap>
              <Text strong>{value}</Text>
              {getHelmChartBadges(record).slice(0, 3).map((badge) => <Tag key={badge.label} color={badge.color}>{badge.label}</Tag>)}
            </Space>
            <Text type="secondary" style={{ fontSize: 12 }}>{record.repositoryDisplay || record.repositoryName || '-'}</Text>
          </Space>
        </Space>
      ),
    },
    { title: localeCode === 'zh_CN' ? '仓库' : 'Repository', dataIndex: 'repositoryName', width: 180, render: (_value, record) => record.repositoryName || '-' },
    { title: localeCode === 'zh_CN' ? '最新版本' : 'Latest Version', dataIndex: 'latestVersion', width: 140 },
    { title: 'App Version', dataIndex: 'appVersion', width: 140, render: (value?: string) => value || '-' },
    {
      title: localeCode === 'zh_CN' ? '描述' : 'Description',
      dataIndex: 'description',
      width: 420,
      render: (value?: string) => value ? <Text type="secondary">{value}</Text> : '-',
    },
    {
      title: localeCode === 'zh_CN' ? '关键词' : 'Keywords',
      dataIndex: 'keywords',
      width: 240,
      render: (values?: string[]) => values?.length ? (
        <Space size={[4, 4]} wrap>
          {values.slice(0, 4).map((value) => <Tag key={value}>{value}</Tag>)}
          {values.length > 4 ? <Tag>+{values.length - 4}</Tag> : null}
        </Space>
      ) : '-',
    },
    {
      title: localeCode === 'zh_CN' ? '状态' : 'Signals',
      dataIndex: 'stars',
      width: 180,
      render: (_value, record) => (
        <Space size={[4, 4]} wrap>
          {typeof record.stars === 'number' ? <Tag>{localeCode === 'zh_CN' ? `Stars ${record.stars}` : `${record.stars} stars`}</Tag> : null}
          {record.hasValuesSchema ? <Tag color="processing">values.schema</Tag> : null}
          {hasHelmChartSecuritySummary(record) ? <Tag color={(record.securityCritical ?? 0) > 0 || (record.securityHigh ?? 0) > 0 ? 'error' : 'default'}>CVEs {(record.securityCritical ?? 0) + (record.securityHigh ?? 0)}</Tag> : null}
        </Space>
      ),
    },
    { title: localeCode === 'zh_CN' ? '版本数' : 'Versions', dataIndex: 'versionCount', width: 100 },
    {
      title: localeCode === 'zh_CN' ? '操作' : 'Actions',
      dataIndex: 'artifactHubUrl',
      width: 112,
      fixed: 'right',
      align: 'center',
      render: (_value, record) => {
        const openArtifactHubLabel = localeCode === 'zh_CN'
          ? '打开 Artifact Hub 外部页面'
          : 'Open Artifact Hub package page'
        const viewChartLabel = localeCode === 'zh_CN'
          ? '查看 Chart 详情、README 和默认 values'
          : 'View chart details, README, and default values'
        const installChartLabel = localeCode === 'zh_CN'
          ? '安装 Chart 到当前集群'
          : 'Install chart to the current cluster'
        return (
          <Space size={2} className="soha-row-action-icons">
            {record.artifactHubUrl ? (
              <ManagementIconButton
                autoInsertSpace={false}
                href={record.artifactHubUrl}
                icon={<LinkOutlined />}
                size="small"
                target="_blank"
                tooltip={openArtifactHubLabel}
                title={openArtifactHubLabel}
                aria-label={openArtifactHubLabel}
                onClick={(event) => event.stopPropagation()}
              />
            ) : null}
            <ManagementIconButton
              autoInsertSpace={false}
              icon={<RocketOutlined />}
              size="small"
              tooltip={viewChartLabel}
              title={viewChartLabel}
              aria-label={viewChartLabel}
              onClick={(event) => {
                event.stopPropagation()
                openChartDrawer(record)
              }}
            />
            {hasAllowedAction(record.allowedActions, 'create') ? (
              <ManagementIconButton
                autoInsertSpace={false}
                icon={<CloudDownloadOutlined />}
                size="small"
                tooltip={installChartLabel}
                title={installChartLabel}
                aria-label={installChartLabel}
                onClick={(event) => {
                  event.stopPropagation()
                  openChartDrawer(record)
                  setDrawerTabKey('install')
                }}
              />
            ) : null}
          </Space>
        )
      },
    },
	  ]

  const installResourceColumns: TableColumnsType<HelmChartInstallResource> = [
    { title: 'Kind', dataIndex: 'kind', width: 150 },
    { title: 'Name', dataIndex: 'name', render: (value?: string) => value || '-' },
    { title: localeCode === 'zh_CN' ? '命名空间' : 'Namespace', dataIndex: 'namespace', width: 160, render: (value?: string) => value || '-' },
    { title: 'API Version', dataIndex: 'apiVersion', width: 160, render: (value?: string) => value || '-' },
  ]

  const observedInstallRelease = installReleaseQuery.data?.data
  const installInFlight = Boolean(installStartedAt && !installResult && !installError)
  const installReleaseNameConflict = installHasRecoverableConflict
  const installErrorDescription = installError ? formatHelmInstallError(installError, localeCode, installTarget) : ''
  const installStatusColor = installMutation.isPending
    ? 'processing'
    : installResult
      ? 'success'
      : installError
        ? (installReleaseNameConflict ? 'warning' : 'error')
        : 'default'
  const installStatusLabel = installMutation.isPending
    ? (localeCode === 'zh_CN' ? '安装中' : 'Installing')
    : installResult
      ? (localeCode === 'zh_CN' ? '已完成' : 'Completed')
      : installError
        ? (localeCode === 'zh_CN' ? '失败' : 'Failed')
        : (localeCode === 'zh_CN' ? '未开始' : 'Not started')
  const installTimeoutSeconds = installTarget?.timeoutSeconds ?? 300

  return (
    <div className="soha-page">
      <ResourceQueryPanel
        placeholder={localeCode === 'zh_CN' ? '搜索 Chart / 版本 / 描述 / 关键词 / 维护者' : 'Search chart / version / description / keyword / maintainer'}
        searchKeyword={searchKeyword}
        setSearchKeyword={(value) => {
          setSearchKeyword(value)
          setChartPage(1)
        }}
      />
      <AdminTable
        className="soha-platform-table"
        columnSettingIconOnly
        columnSettingPlacement="toolbar"
        shellClassName="soha-management-table-shell soha-helm-chart-table-shell"
        columns={columns}
        dataSource={clusterId && !chartsQuery.isError ? rawItems : []}
        rowKey={(record) => record.packageId || `${record.repositoryName}:${record.name}:${record.latestVersion}`}
        loading={chartsQuery.isLoading}
        pagination={{
          current: chartPage,
          currentPage: chartPage,
          pageSize: chartPageSize,
          pageSizeOptions: HELM_CHART_PAGE_SIZE_OPTIONS,
          showQuickJumper: totalChartCount > chartPageSize,
          total: totalChartCount,
          onPageChange: (nextPage: number) => {
            setChartPage(nextPage)
          },
          onPageSizeChange: (nextPageSize: number) => {
            setChartPage(1)
            setChartPageSize(Math.min(nextPageSize, HELM_CHART_MAX_PAGE_SIZE))
          },
        }}
        paginationSummary={(
          <Text className="soha-workload-table-summary" type="secondary">
            {localeCode === 'zh_CN'
              ? `当前 ${currentRangeStart}-${currentRangeEnd} / 总计 ${formattedTotalChartCount} 条`
              : `${currentRangeStart}-${currentRangeEnd} / ${formattedTotalChartCount} total`}
          </Text>
        )}
        toolbar={catalog?.repository ? (
          <Space className="soha-helm-chart-catalog-toolbar" size={8}>
            <Tag color="processing">Artifact Hub</Tag>
            <Tag>{localeCode === 'zh_CN' ? '仅 Helm packages' : 'Helm packages only'}</Tag>
            <Text className="soha-helm-chart-catalog-url" type="secondary" title={catalog.repository.url}>{catalog.repository.url}</Text>
            <Text className="soha-helm-chart-catalog-total" type="secondary">
              {localeCode === 'zh_CN' ? `总计 ${formattedTotalChartCount} 个` : `${formattedTotalChartCount} total`}
            </Text>
          </Space>
        ) : null}
        tableSize={tableSize}
        scroll={{ x: 'max-content' }}
        onRow={(record: HelmChart) => ({
          onClick: () => openChartDrawer(record),
          style: { cursor: 'pointer' },
        })}
        toolbarExtra={(
          <ManagementTableToolbar>
            <ManagementDensityButton
              aria-label={densityLabel}
              title={densityLabel}
              tooltip={densityLabel}
              onClick={() => setTableSize((current) => current === 'middle' ? 'small' : 'middle')}
            />
            <ManagementRefreshButton
              aria-label={t('common.refresh', 'Refresh')}
              disabled={!clusterId}
              loading={chartsQuery.isFetching}
              tooltip={t('common.refresh', 'Refresh')}
              onClick={() => {
                if (clusterId) {
                  void chartsQuery.refetch()
                }
              }}
            />
          </ManagementTableToolbar>
        )}
        empty={(
          <ManagementState
            bordered={false}
            compact
            kind={!clusterId ? 'select-scope' : chartsQuery.isError ? 'error' : 'empty'}
            title={emptyTitle}
            description={emptyDescription}
          />
        )}
      />
      <Drawer
        destroyOnHidden
        open={Boolean(selectedChart)}
        title={drawerTitle}
        size="large"
        onClose={() => {
          setSelectedChart(null)
          setSelectedVersion('')
          setValuesDraft('')
          setDrawerTabKey('overview')
          setInstallTarget(null)
          setInstallResult(null)
          setInstallError('')
          setInstallStartedAt(null)
          setInstallElapsedSeconds(0)
        }}
        extra={(
          <Space>
            <Button autoInsertSpace={false} onClick={() => setSelectedChart(null)}>
              {localeCode === 'zh_CN' ? '取消' : 'Cancel'}
            </Button>
            <Button
              autoInsertSpace={false}
              icon={<CloudDownloadOutlined />}
              disabled={!canInstallActiveChart}
              loading={installMutation.isPending}
              type="primary"
              onClick={() => { void submitInstall() }}
            >
              {localeCode === 'zh_CN' ? '安装' : 'Install'}
            </Button>
          </Space>
        )}
      >
        {activeChart ? (
          <Space orientation="vertical" size={14} style={{ width: '100%' }}>
            <Space align="start" size={14}>
              {activeChart.logoImageUrl ? (
                <img
                  src={activeChart.logoImageUrl}
                  alt=""
                  style={{ width: 72, height: 72, borderRadius: 6, objectFit: 'contain', border: '1px solid var(--soha-color-border)' }}
                />
              ) : null}
              <Space orientation="vertical" size={6}>
                <Space size={6} wrap>
                  <Text strong style={{ fontSize: 16 }}>{activeChart.name}</Text>
                  {getHelmChartBadges(activeChart).map((badge) => <Tag key={badge.label} color={badge.color}>{badge.label}</Tag>)}
                </Space>
                <Text type="secondary">{activeChart.description || '-'}</Text>
                <Space size={8} wrap>
                  <Text type="secondary">{activeChart.repositoryDisplay || activeChart.repositoryName}</Text>
                  {activeChart.artifactHubUrl ? (
                    <Button autoInsertSpace={false} href={activeChart.artifactHubUrl} icon={<LinkOutlined />} size="small" target="_blank" type="link">
                      Artifact Hub
                    </Button>
                  ) : null}
                </Space>
              </Space>
            </Space>
            <Tabs
              size="small"
              activeKey={drawerTabKey}
              onChange={setDrawerTabKey}
              items={[
                {
                  key: 'overview',
                  label: localeCode === 'zh_CN' ? '包信息' : 'Package',
                  children: (
                    <Space orientation="vertical" size={12} style={{ width: '100%' }}>
                      <Descriptions bordered column={1} size="small">
                        <Descriptions.Item label={localeCode === 'zh_CN' ? 'Chart' : 'Chart'}>{activeChart.name}</Descriptions.Item>
                        <Descriptions.Item label={localeCode === 'zh_CN' ? '仓库' : 'Repository'}>
                          <Space wrap size={6}>
                            <Text>{activeChart.repositoryName || '-'}</Text>
                            {activeChart.repositoryUrl ? <Text type="secondary">{activeChart.repositoryUrl}</Text> : null}
                          </Space>
                        </Descriptions.Item>
                        <Descriptions.Item label={localeCode === 'zh_CN' ? '版本' : 'Version'}>{activeChart.latestVersion || '-'}</Descriptions.Item>
                        <Descriptions.Item label="App Version">{activeChart.appVersion || '-'}</Descriptions.Item>
                        <Descriptions.Item label="Home">{activeChart.homeUrl || activeChart.home || '-'}</Descriptions.Item>
                        <Descriptions.Item label={localeCode === 'zh_CN' ? '关键词' : 'Keywords'}>
                          {activeChart.keywords?.length ? (
                            <Space wrap size={[4, 4]}>
                              {activeChart.keywords.map((item) => <Tag key={item}>{item}</Tag>)}
                            </Space>
                          ) : '-'}
                        </Descriptions.Item>
                        <Descriptions.Item label={localeCode === 'zh_CN' ? '安全摘要' : 'Security'}>
                          {hasHelmChartSecuritySummary(activeChart)
                            ? `critical ${activeChart.securityCritical ?? 0} / high ${activeChart.securityHigh ?? 0} / medium ${activeChart.securityMedium ?? 0} / low ${activeChart.securityLow ?? 0}`
                            : '-'}
                        </Descriptions.Item>
                      </Descriptions>
                      {detailQuery.isLoading ? <Spin size="small" /> : null}
                      {detail?.links?.length ? (
                        <Space wrap size={8}>
                          {detail.links.map((link) => link.url ? (
                            <Button key={`${link.name}-${link.url}`} autoInsertSpace={false} href={link.url} target="_blank" size="small">
                              {link.name || link.url}
                            </Button>
                          ) : null)}
                        </Space>
                      ) : null}
                    </Space>
                  ),
                },
                {
                  key: 'readme',
                  label: 'README',
                  children: detailQuery.isLoading ? <Spin /> : renderHelmReadme(detail?.readme),
                },
                {
                  key: 'values',
                  label: 'Values',
                  children: (
                    <Space orientation="vertical" size={8} style={{ width: '100%' }}>
                      {valuesQuery.isLoading ? <Spin size="small" /> : null}
                      {valuesQuery.isError ? <Text type="secondary">{localeCode === 'zh_CN' ? 'Artifact Hub 未返回默认 values。' : 'Artifact Hub did not return default values.'}</Text> : null}
                      <Input.TextArea
                        value={valuesDraft}
                        onChange={(event) => setValuesDraft(event.target.value)}
                        rows={18}
                        style={{ fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace', fontSize: 12 }}
                        placeholder={localeCode === 'zh_CN' ? '填写 Helm values YAML' : 'Enter Helm values YAML'}
                      />
                    </Space>
                  ),
                },
                {
                  key: 'install',
                  label: localeCode === 'zh_CN' ? '安装' : 'Install',
                  forceRender: true,
                  children: (
                    <Space orientation="vertical" size={12} style={{ width: '100%' }}>
                      {!canInstallActiveChart ? (
                        <Alert
                          showIcon
                          type="warning"
                          title={localeCode === 'zh_CN' ? '无安装权限' : 'Install permission required'}
                          description={localeCode === 'zh_CN'
                            ? '当前账号没有在所选集群创建 Helm Release 的权限。'
                            : 'The current account cannot create Helm releases in the selected cluster.'}
                        />
                      ) : null}
                      <Form form={installForm} layout="vertical" size="small">
                        <Form.Item name="releaseName" label="Release" rules={[{ required: true }]}>
                          <Input disabled={!canInstallActiveChart || installMutation.isPending} />
                        </Form.Item>
                        <Form.Item name="namespace" label={localeCode === 'zh_CN' ? '命名空间' : 'Namespace'} rules={[{ required: true }]}>
                          <Input disabled={!canInstallActiveChart || installMutation.isPending} />
                        </Form.Item>
                        <Form.Item name="version" label={localeCode === 'zh_CN' ? '版本' : 'Version'} rules={[{ required: true }]}>
                          <Select
                            showSearch
                            disabled={!canInstallActiveChart || installMutation.isPending}
                            options={versionOptions}
                            onChange={(value) => {
                              setSelectedVersion(value)
                              setValuesDraft('')
                            }}
                          />
                        </Form.Item>
                        <Form.Item name="repositoryUrl" label={localeCode === 'zh_CN' ? 'Chart 仓库 URL' : 'Chart Repository URL'} rules={[{ required: true }]}>
                          <Input disabled={!canInstallActiveChart || installMutation.isPending} />
                        </Form.Item>
                        <Form.Item name="chartName" label="Chart" rules={[{ required: true }]}>
                          <Input disabled={!canInstallActiveChart || installMutation.isPending} />
                        </Form.Item>
                        <Form.Item name="timeoutSeconds" label={localeCode === 'zh_CN' ? '超时秒数' : 'Timeout seconds'} rules={[{ required: true }]}>
                          <InputNumber disabled={!canInstallActiveChart || installMutation.isPending} min={30} max={3600} step={30} style={{ width: '100%' }} />
                        </Form.Item>
                        <Space size={16} wrap>
                          <Form.Item name="createNamespace" valuePropName="checked">
                            <Checkbox disabled={!canInstallActiveChart || installMutation.isPending}>{localeCode === 'zh_CN' ? '创建命名空间' : 'Create namespace'}</Checkbox>
                          </Form.Item>
                          <Form.Item name="wait" valuePropName="checked">
                            <Checkbox disabled={!canInstallActiveChart || installMutation.isPending}>{localeCode === 'zh_CN' ? '等待资源就绪' : 'Wait for resources'}</Checkbox>
                          </Form.Item>
                        </Space>
                      </Form>

                      {installTarget ? (
                        <Card size="small" title={localeCode === 'zh_CN' ? '安装进度' : 'Install progress'}>
                          <Space orientation="vertical" size={10} style={{ width: '100%' }}>
                            <Space wrap size={8}>
                              <Tag color={installStatusColor}>{installStatusLabel}</Tag>
                              <Text strong>{installTarget.releaseName}</Text>
                              <Text type="secondary">{installTarget.namespace}</Text>
                              <Text type="secondary">
                                {localeCode === 'zh_CN'
                                  ? `已等待 ${installElapsedSeconds}s / 超时 ${installTimeoutSeconds}s`
                                  : `${installElapsedSeconds}s elapsed / ${installTimeoutSeconds}s timeout`}
                              </Text>
                            </Space>
                            <Descriptions bordered column={1} size="small">
                              <Descriptions.Item label={localeCode === 'zh_CN' ? '提交 Helm install' : 'Submit Helm install'}>
                                <Tag color={installStartedAt ? 'success' : 'default'}>{installStartedAt ? 'OK' : '-'}</Tag>
                              </Descriptions.Item>
                              <Descriptions.Item label={localeCode === 'zh_CN' ? '集群 Release' : 'Cluster release'}>
                                {observedInstallRelease ? (
                                  <Space size={6}>
                                    <StatusTag value={observedInstallRelease.status || 'detected'} />
                                    <Text type="secondary">rev {observedInstallRelease.revision || '-'}</Text>
                                  </Space>
                                ) : installInFlight ? (
                                  <Space size={6}>
                                    <Spin size="small" />
                                    <Text type="secondary">{localeCode === 'zh_CN' ? '等待 Helm release 出现' : 'Waiting for Helm release'}</Text>
                                  </Space>
                                ) : '-'}
                              </Descriptions.Item>
                              <Descriptions.Item label={localeCode === 'zh_CN' ? '等待资源' : 'Wait for resources'}>
                                {installTarget.wait
                                  ? (installResult
                                      ? <Tag color="success">OK</Tag>
                                      : installError
                                        ? <Tag color={installReleaseNameConflict ? 'default' : 'error'}>{installReleaseNameConflict ? (localeCode === 'zh_CN' ? '未开始' : 'Not started') : 'Failed'}</Tag>
                                        : <Tag color="processing">{localeCode === 'zh_CN' ? '等待中' : 'Waiting'}</Tag>)
                                  : <Tag>{localeCode === 'zh_CN' ? '未启用' : 'Disabled'}</Tag>}
                              </Descriptions.Item>
                            </Descriptions>
                            {installError ? (
                              <Alert
                                type={installReleaseNameConflict ? 'warning' : 'error'}
                                showIcon
                                title={installReleaseNameConflict ? (localeCode === 'zh_CN' ? 'Release 名已被占用' : 'Release name already in use') : (localeCode === 'zh_CN' ? '安装失败' : 'Install failed')}
                                description={installErrorDescription}
                                action={installReleaseNameConflict ? (
                                  <Space size={6}>
                                    <Button
                                      autoInsertSpace={false}
                                      size="small"
                                      onClick={() => {
                                        installForm.setFieldsValue({
                                          releaseName: retryHelmReleaseName(installTarget.releaseName, installTarget.chartName),
                                        })
                                        setInstallTarget(null)
                                        setInstallResult(null)
                                        setInstallError('')
                                        setInstallStartedAt(null)
                                        setInstallElapsedSeconds(0)
                                      }}
                                    >
                                      {localeCode === 'zh_CN' ? '换名重试' : 'Use new name'}
                                    </Button>
                                    <Button
                                      autoInsertSpace={false}
                                      size="small"
                                      onClick={() => {
                                        setSelectedChart(null)
                                        navigate('/helm/releases')
                                      }}
                                    >
                                      {localeCode === 'zh_CN' ? '查看 Releases' : 'View releases'}
                                    </Button>
                                  </Space>
                                ) : undefined}
                              />
                            ) : null}
                            {installResult ? (
                              <Space orientation="vertical" size={10} style={{ width: '100%' }}>
                                <Descriptions bordered column={1} size="small">
                                  <Descriptions.Item label="Status"><StatusTag value={installResult.status || 'unknown'} /></Descriptions.Item>
                                  <Descriptions.Item label="Revision">{installResult.revision || '-'}</Descriptions.Item>
                                  <Descriptions.Item label="Chart">{installResult.chart || installResult.chartName || '-'}</Descriptions.Item>
                                  <Descriptions.Item label={localeCode === 'zh_CN' ? '说明' : 'Description'}>{installResult.description || '-'}</Descriptions.Item>
                                </Descriptions>
                                <Table
                                  columns={installResourceColumns}
                                  dataSource={installResult.resources ?? []}
                                  rowKey={(record) => `${record.apiVersion}:${record.kind}:${record.namespace}:${record.name}`}
                                  size="small"
                                  pagination={{ pageSize: 8, size: 'small', showSizeChanger: false }}
                                  scroll={{ x: 720 }}
                                />
                                {installResult.notes ? (
                                  <pre className="soha-helm-install-output">{installResult.notes}</pre>
                                ) : null}
                              </Space>
                            ) : null}
                          </Space>
                        </Card>
                      ) : null}
                    </Space>
                  ),
                },
              ]}
            />
          </Space>
        ) : null}
      </Drawer>
    </div>
  )
}
