import { useMemo, useState } from 'react'
import { Alert, App, Avatar, Button, Col, Form, Input, InputNumber, Modal, Popconfirm, Popover, Row, Select, Space, Switch, Tag, Tree, Typography } from 'antd'
import { ApartmentOutlined, DeleteOutlined, EditOutlined, FolderOpenOutlined, PlusOutlined } from '@ant-design/icons'
import type { TableColumnsType } from 'antd'
import type { DataNode } from 'antd/es/tree'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Navigate } from 'react-router-dom'
import { AdminTable } from '@/components/admin-table'
import { ManagementIconButton, ManagementState, ManagementTableToolbar } from '@/components/management-list'
import { consolePermissionGroups, consolePermissionLabelMap } from '@/features/auth/permission-catalog'
import { hasPermission, invalidateAuthz, usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import { StatusTag } from '@/components/status-tag'
import { api } from '@/services/api-client'
import { resolveRoutePermission, routeMeta } from '@/routes/meta'
import type { ApiResponse, ScopeGrant } from '@/types'
import { formatDateTime } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import './access-pages.css'

const { Text } = Typography

const ACCESS_ACTION_OPTIONS = [
  { value: 'view', label: '查看 (view)' },
  { value: 'list', label: '列表 (list)' },
  { value: 'watch', label: '监听 (watch)' },
  { value: 'update', label: '修改 (update)' },
  { value: 'delete', label: '删除 (delete)' },
  { value: 'restart', label: '重启 (restart)' },
  { value: 'scale', label: '伸缩 (scale)' },
  { value: 'logs', label: '日志 (logs)' },
  { value: 'exec', label: 'Exec (exec)' },
]
const ACCESS_ACTION_LABEL_MAP = Object.fromEntries(
  ACCESS_ACTION_OPTIONS.map((option) => [option.value, option.label]),
) as Record<string, string>

const USER_STATUS_OPTIONS = [
  { value: 'active', label: '启用' },
  { value: 'disabled', label: '停用' },
]

const ROLE_SCOPE_OPTIONS = [
  { value: 'system', label: '系统角色' },
  { value: 'custom', label: '自定义角色' },
]

const ORG_ALL_KEY = '__all-organizations__'

const POLICY_EFFECT_OPTIONS = [
  { value: 'allow', label: '允许' },
  { value: 'deny', label: '拒绝' },
]

type ColumnProps<T> = TableColumnsType<T>[number]

interface AccessUser {
  id: string
  username: string
  email: string
  displayName: string
  status: string
  lastLoginAt?: string
  tags: string[]
  roles: string[]
  teams: string[]
  projects: string[]
}

interface AccessRole {
  id: string
  name: string
  scope: string
  capabilities: string[]
  permissionKeys?: string[]
  userCount: number
}

interface AccessTeam {
  id: string
  parentId?: string
  name: string
  slug: string
  path?: string
  source?: string
  externalId?: string
  metadata: Record<string, unknown>
  userCount: number
}

interface LoginProviderRef {
  id: string
  name: string
  type: string
  enabled?: boolean
}

interface IdentitySettingsResponse {
  providers?: LoginProviderRef[]
}

interface AccessPolicy {
  id: string
  name: string
  effect: string
  priority: number
  subjects: {
    roles: string[]
    teams: string[]
    projects: string[]
    users: string[]
    tags: string[]
  }
  clusters: {
    ids: string[]
    regions: string[]
    environments: string[]
    labels: Record<string, string[]>
  }
  namespaces: {
    names: string[]
    ownerTeams: string[]
    labels: Record<string, string[]>
  }
  resources: {
    kinds: string[]
    names: string[]
    labels: Record<string, string[]>
  }
  actions: string[]
  conditions: {
    sources: string[]
    approvalStates: string[]
  }
  reason: string
}

function parseCSV(value: unknown) {
  return String(value ?? '')
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
}

function toStringArray(value: unknown) {
  if (!Array.isArray(value)) {
    return []
  }
  return value
    .map((item) => String(item).trim())
    .filter(Boolean)
}

function joinCSV(items?: string[]) {
  return items?.join(', ') ?? ''
}

function getUserLabel(user?: Pick<AccessUser, 'displayName' | 'username' | 'email'> | null) {
  if (!user) {
    return '用户'
  }
  return user.displayName || user.username || user.email || '用户'
}

function getUserInitial(user: Pick<AccessUser, 'displayName' | 'username' | 'email'>) {
  const source = getUserLabel(user)
  const first = Array.from(source.trim())[0]
  return (first ?? 'U').toUpperCase()
}

function getGroupDescription(metadata?: Record<string, unknown>) {
  return String(metadata?.description ?? '').trim()
}

function getOrganizationLabel(item?: Pick<AccessTeam, 'name' | 'path' | 'slug'> | null) {
  if (!item) {
    return '全部组织'
  }
  return item.name || item.path || item.slug || '未命名组织'
}

function getOrganizationPathLabel(item: AccessTeam) {
  return item.path || `/${item.slug || item.id}`
}

const LOGIN_PROVIDER_TYPE_LABELS: Record<string, string> = {
  oidc: 'OIDC',
  oauth2: 'OAuth2',
  feishu: '飞书',
  dingtalk: '钉钉',
  wecom: '企业微信',
  saml: 'SAML',
}

const ORGANIZATION_SOURCE_TYPE_OPTIONS = [
  { value: 'oidc', label: 'OIDC 类型映射' },
  { value: 'oauth2', label: 'OAuth2 类型映射' },
  { value: 'feishu', label: '飞书类型映射' },
  { value: 'dingtalk', label: '钉钉类型映射' },
  { value: 'wecom', label: '企业微信类型映射' },
]

const DIRECTORY_SOURCE_OPTIONS = [
  { value: 'ldap', label: 'LDAP 同步' },
  { value: 'saml', label: 'SAML 映射' },
]

function loginProviderTypeLabel(type: string) {
  return LOGIN_PROVIDER_TYPE_LABELS[type] || type || '登录源'
}

function loginProviderOptionLabel(provider: LoginProviderRef) {
  const name = provider.name || provider.id
  const disabledSuffix = provider.enabled === false ? '（停用）' : ''
  return `${loginProviderTypeLabel(provider.type)} · ${name} (${provider.id})${disabledSuffix}`
}

function buildOrganizationSourceLabelMap(providers: LoginProviderRef[]) {
  const entries: Array<[string, string]> = [
    ['local', '本地维护'],
    ...ORGANIZATION_SOURCE_TYPE_OPTIONS.map((item) => [item.value, item.label] as [string, string]),
    ...DIRECTORY_SOURCE_OPTIONS.map((item) => [item.value, item.label] as [string, string]),
    ...providers.map((provider) => [provider.id, loginProviderOptionLabel(provider)] as [string, string]),
  ]
  return Object.fromEntries(entries)
}

function buildOrganizationSourceOptions(providers: LoginProviderRef[]) {
  const loginProviderOptions = providers
    .filter((provider) => ['oidc', 'oauth2', 'feishu', 'dingtalk', 'wecom'].includes(provider.type))
    .map((provider) => ({
      value: provider.id,
      label: loginProviderOptionLabel(provider),
    }))

  return [
    {
      label: '本地',
      options: [{ value: 'local', label: '本地维护' }],
    },
    ...(loginProviderOptions.length
      ? [{
          label: '登录源应用',
          options: loginProviderOptions,
        }]
      : []),
    {
      label: '按类型兼容',
      options: ORGANIZATION_SOURCE_TYPE_OPTIONS,
    },
    {
      label: '目录服务',
      options: DIRECTORY_SOURCE_OPTIONS,
    },
  ]
}

function organizationSourceLabel(value: string | undefined, labelMap: Record<string, string>) {
  const source = String(value || 'local')
  return labelMap[source] || source
}

function buildOrganizationTree(items: AccessTeam[], userCountByOrg: Map<string, number>): DataNode[] {
  const nodes = new Map<string, DataNode & { children?: DataNode[] }>()
  const roots: Array<DataNode & { children?: DataNode[] }> = []
  const sortedItems = [...items].sort((left, right) => {
    const pathCompare = getOrganizationPathLabel(left).localeCompare(getOrganizationPathLabel(right))
    if (pathCompare !== 0) return pathCompare
    return getOrganizationLabel(left).localeCompare(getOrganizationLabel(right))
  })

  sortedItems.forEach((item) => {
    nodes.set(item.id, {
      key: item.id,
      title: (
        <Space size={6} className="soha-org-tree-title">
          <span>{getOrganizationLabel(item)}</span>
          <Tag>{userCountByOrg.get(item.id) ?? item.userCount ?? 0}</Tag>
        </Space>
      ),
      children: [],
    })
  })
  sortedItems.forEach((item) => {
    const node = nodes.get(item.id)
    if (!node) return
    const parent = item.parentId ? nodes.get(item.parentId) : null
    if (parent) {
      parent.children = [...(parent.children ?? []), node]
      return
    }
    roots.push(node)
  })

  const trimEmptyChildren = (node: DataNode & { children?: DataNode[] }): DataNode => {
    const children = (node.children ?? []).map((child) => trimEmptyChildren(child as DataNode & { children?: DataNode[] }))
    return children.length ? { ...node, children } : { ...node, children: undefined }
  }

  return [
    {
      key: ORG_ALL_KEY,
      title: (
        <Space size={6} className="soha-org-tree-title">
          <span>全部组织</span>
          <Tag>{items.reduce((sum, item) => sum + (userCountByOrg.get(item.id) ?? item.userCount ?? 0), 0)}</Tag>
        </Space>
      ),
      children: roots.map(trimEmptyChildren),
    },
  ]
}

function collectOrganizationDescendantIds(items: AccessTeam[], organizationId: string) {
  const childrenByParent = new Map<string, string[]>()
  items.forEach((item) => {
    if (!item.parentId) return
    const children = childrenByParent.get(item.parentId) ?? []
    children.push(item.id)
    childrenByParent.set(item.parentId, children)
  })
  const result = new Set<string>()
  const visit = (id: string) => {
    ;(childrenByParent.get(id) ?? []).forEach((childID) => {
      if (result.has(childID)) return
      result.add(childID)
      visit(childID)
    })
  }
  visit(organizationId)
  return result
}

function organizationMatchesSelection(user: AccessUser, selectedOrgId: string, scopedOrganizationIds: Set<string>) {
  if (!selectedOrgId || selectedOrgId === ORG_ALL_KEY) {
    return true
  }
  return user.teams?.some((teamID) => scopedOrganizationIds.has(teamID)) ?? false
}

function renderMappedTags(values: string[], labelMap: Record<string, string>, emptyText = '-') {
  if (!values?.length) {
    return emptyText
  }
  return (
    <Space wrap size={4}>
      {values.map((value) => (
        <Tag key={value}>
          {labelMap[value] || value}
        </Tag>
      ))}
    </Space>
  )
}

interface CompactMappedTagsProps {
  emptyText: string
  itemLabel: string
  labelMap: Record<string, string>
  values: string[]
  visibleCount: number
}

function CompactMappedTags({ emptyText, itemLabel, labelMap, values, visibleCount }: CompactMappedTagsProps) {
  const [open, setOpen] = useState(false)

  if (!values?.length) {
    return emptyText
  }

  const visibleValues = values.slice(0, visibleCount)
  const hiddenCount = Math.max(values.length - visibleValues.length, 0)
  const renderTag = (value: string, className = 'soha-access-compact-tag') => {
    const label = labelMap[value] || value
    return (
      <Tag key={value} className={className} title={label}>
        <span className="soha-access-compact-tag-text">{label}</span>
      </Tag>
    )
  }
  if (hiddenCount === 0) {
    return (
      <Space wrap={false} size={4} className="soha-access-compact-tags">
        {visibleValues.map((value) => renderTag(value))}
      </Space>
    )
  }

  const trigger = (
    <Button
      type="text"
      size="small"
      className="soha-access-compact-tags-trigger"
      aria-label={`查看 ${values.length} 个${itemLabel}`}
      onClick={(event) => {
        event.preventDefault()
        event.stopPropagation()
        setOpen((current) => !current)
      }}
    >
      <Space wrap={false} size={4} className="soha-access-compact-tags">
        {visibleValues.map((value) => renderTag(value))}
        <Tag className="soha-access-compact-tag-more">{`+${hiddenCount}`}</Tag>
      </Space>
    </Button>
  )

  return (
    <Popover
      open={open}
      onOpenChange={setOpen}
      trigger={[]}
      placement="topLeft"
      title={`${values.length} 个${itemLabel}`}
      content={(
        <div className="soha-access-permission-popover">
          {values.map((value) => renderTag(value, 'soha-access-permission-popover-tag'))}
        </div>
      )}
    >
      {trigger}
    </Popover>
  )
}

function renderCompactMappedTags(values: string[], labelMap: Record<string, string>, emptyText = '-', visibleCount = 2, itemLabel = '权限项') {
  return (
    <CompactMappedTags
      emptyText={emptyText}
      itemLabel={itemLabel}
      labelMap={labelMap}
      values={values}
      visibleCount={visibleCount}
    />
  )
}

function normalizePermissionKeys(value: unknown) {
  return toStringArray(value).sort((left, right) => left.localeCompare(right))
}

const ROLE_PERMISSION_WORKBENCH_LABELS: Record<string, string> = {
  platform: '平台工作台',
  delivery: '应用交付',
  monitoring: '可观测与值班',
  ai: 'AI 工作台',
  aiGateway: 'AI Gateway',
  virtualization: '虚拟化',
  docker: 'Docker 工作台',
  settings: '设置中心',
  unknown: '其他菜单',
}

function permissionTreeKey(permissionKey: string) {
  return `permission:${permissionKey}`
}

function permissionFromTreeKey(key: string) {
  return key.startsWith('permission:') ? key.slice('permission:'.length) : ''
}

function routePermissionKeys(route: (typeof routeMeta)[number]) {
  const keys = route.permissionKeysAny?.length
    ? route.permissionKeysAny
    : [resolveRoutePermission(route)].filter(Boolean)
  return normalizePermissionKeys(keys)
}

function buildRolePermissionTreeData(): DataNode[] {
  const coveredRoutePermissionSet = new Set<string>()
  const emittedRoutePermissionSet = new Set<string>()
  const routeItems = routeMeta.filter((route) => route.requiresAuth && route.navVisible && route.menuId)
  const nodeByRouteID = new Map<string, DataNode & { children?: DataNode[] }>()
  const routeOrder = new Map(routeMeta.map((route, index) => [route.id, index]))

  routeItems.forEach((route) => {
    const permissions = routePermissionKeys(route)
    permissions.forEach((permissionKey) => coveredRoutePermissionSet.add(permissionKey))
    const uniquePermissions = permissions.filter((permissionKey) => {
      if (emittedRoutePermissionSet.has(permissionKey)) {
        return false
      }
      emittedRoutePermissionSet.add(permissionKey)
      return true
    })
    nodeByRouteID.set(route.id, {
      key: `route:${route.id}`,
      title: route.title,
      children: uniquePermissions.map((permissionKey) => ({
        key: permissionTreeKey(permissionKey),
        title: consolePermissionLabelMap[permissionKey] ? `${consolePermissionLabelMap[permissionKey]} (${permissionKey})` : permissionKey,
      })),
    })
  })

  const rootsByWorkbench = new Map<string, Array<DataNode & { children?: DataNode[] }>>()
  routeItems.forEach((route) => {
    const node = nodeByRouteID.get(route.id)
    if (!node) return
    const parent = route.parentId ? nodeByRouteID.get(route.parentId) : null
    if (parent) {
      parent.children = [...(parent.children ?? []), node]
      return
    }
    const workbench = route.workbenchId || route.group || 'unknown'
    const roots = rootsByWorkbench.get(workbench) ?? []
    roots.push(node)
    rootsByWorkbench.set(workbench, roots)
  })

  const pruneEmptyNodes = (nodes: DataNode[]): DataNode[] => nodes
    .map((node) => {
      const children = pruneEmptyNodes((node.children ?? []) as DataNode[])
      return children.length ? { ...node, children } : node
    })
    .filter((node) => String(node.key).startsWith('permission:') || (node.children?.length ?? 0) > 0)

  const routeTree = Array.from(rootsByWorkbench.entries()).map(([workbench, children]) => ({
    key: `workbench:${workbench}`,
    title: ROLE_PERMISSION_WORKBENCH_LABELS[workbench] || workbench,
    children: pruneEmptyNodes(children.sort((left, right) => {
      const leftID = String(left.key).replace('route:', '')
      const rightID = String(right.key).replace('route:', '')
      return (routeOrder.get(leftID) ?? 0) - (routeOrder.get(rightID) ?? 0)
    })),
  })).filter((node) => node.children.length > 0)

  const actionGroups = consolePermissionGroups
    .map((group) => ({
      key: `actions:${group.key}`,
      title: group.label,
      children: group.options
        .filter((option) => !coveredRoutePermissionSet.has(option.value))
        .map((option) => ({
          key: permissionTreeKey(option.value),
          title: `${option.label} (${option.value})`,
        })),
    }))
    .filter((group) => group.children.length > 0)

  return [
    {
      key: 'menus',
      title: '菜单与页面',
      children: routeTree,
    },
    {
      key: 'actions',
      title: '页面动作与外部调用',
      children: actionGroups,
    },
  ]
}

const rolePermissionTreeData = buildRolePermissionTreeData()

function checkedPermissionTreeKeys(permissionKeys: unknown) {
  return normalizePermissionKeys(permissionKeys).map(permissionTreeKey)
}

function extractPermissionKeysFromTreeCheck(checkedKeys: unknown) {
  const rawKeys = Array.isArray(checkedKeys)
    ? checkedKeys
    : Array.isArray((checkedKeys as { checked?: unknown[] })?.checked)
      ? ((checkedKeys as { checked: unknown[] }).checked)
      : []
  return normalizePermissionKeys(rawKeys.map((key) => permissionFromTreeKey(String(key))).filter(Boolean))
}

function buildPolicySubjectsSummary(policy: AccessPolicy, roleMap: Record<string, string>, teamMap: Record<string, string>) {
  const parts: string[] = []
  if (policy.subjects?.roles?.length) {
    parts.push(`角色: ${policy.subjects.roles.map((item) => roleMap[item] || item).join(', ')}`)
  }
  if (policy.subjects?.teams?.length) {
    parts.push(`组织: ${policy.subjects.teams.map((item) => teamMap[item] || item).join(', ')}`)
  }
  if (policy.subjects?.users?.length) {
    parts.push(`用户: ${policy.subjects.users.join(', ')}`)
  }
  if (policy.subjects?.tags?.length) {
    parts.push(`标签: ${policy.subjects.tags.join(', ')}`)
  }
  return parts.join(' | ') || '全部主体'
}

function buildPolicyTargetsSummary(policy: AccessPolicy, teamMap: Record<string, string>) {
  const parts: string[] = []
  if (policy.clusters?.environments?.length) {
    parts.push(`环境: ${policy.clusters.environments.join(', ')}`)
  }
  if (policy.clusters?.regions?.length) {
    parts.push(`地域: ${policy.clusters.regions.join(', ')}`)
  }
  if (policy.clusters?.ids?.length) {
    parts.push(`集群: ${policy.clusters.ids.join(', ')}`)
  }
  if (policy.namespaces?.names?.length) {
    parts.push(`命名空间: ${policy.namespaces.names.join(', ')}`)
  }
  if (policy.namespaces?.ownerTeams?.length) {
    parts.push(`归属组织: ${policy.namespaces.ownerTeams.map((item) => teamMap[item] || item).join(', ')}`)
  }
  if (policy.resources?.kinds?.length) {
    parts.push(`资源: ${policy.resources.kinds.join(', ')}`)
  }
  if (policy.resources?.names?.length) {
    parts.push(`对象: ${policy.resources.names.join(', ')}`)
  }
  return parts.join(' | ') || '全部资源'
}

function useCRUD<T extends { id: string }>(
  resource: string,
  options?: {
    invalidateKeys?: string[][]
    invalidateAuthz?: boolean
  },
) {
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const [modalVisible, setModalVisible] = useState(false)
  const [editing, setEditing] = useState<T | null>(null)

  const invalidateAll = () => {
    queryClient.invalidateQueries({ queryKey: [resource] })
    options?.invalidateKeys?.forEach((key) => queryClient.invalidateQueries({ queryKey: key }))
    if (options?.invalidateAuthz) {
      void invalidateAuthz(queryClient)
    }
  }

  const query = useQuery({
    queryKey: [resource],
    queryFn: () => api.get<ApiResponse<T[]>>(`/${resource}`),
  })

  const createMutation = useMutation({
    mutationFn: (values: Record<string, unknown>) => api.post(`/${resource}`, values),
    onSuccess: () => {
      message.success('创建成功')
      invalidateAll()
      setModalVisible(false)
    },
    onError: (err: Error) => message.error(err.message),
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, values }: { id: string; values: Record<string, unknown> }) =>
      api.put(`/${resource}/${id}`, values),
    onSuccess: () => {
      message.success('更新成功')
      invalidateAll()
      setModalVisible(false)
      setEditing(null)
    },
    onError: (err: Error) => message.error(err.message),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.delete(`/${resource}/${id}`),
    onSuccess: () => {
      message.success('删除成功')
      invalidateAll()
    },
    onError: (err: Error) => message.error(err.message),
  })

  const handleSubmit = (values: Record<string, unknown>) => {
    if (editing) {
      updateMutation.mutate({ id: editing.id, values })
      return
    }
    createMutation.mutate(values)
  }

  const openCreate = () => {
    setEditing(null)
    setModalVisible(true)
  }

  const openEdit = (record: T) => {
    setEditing(record)
    setModalVisible(true)
  }

  const closeModal = () => {
    setModalVisible(false)
    setEditing(null)
  }

  return {
    data: query.data?.data ?? [],
    isLoading: query.isLoading,
    modalVisible,
    editing,
    openCreate,
    openEdit,
    closeModal,
    handleSubmit,
    deleteMutation,
    isSaving: createMutation.isPending || updateMutation.isPending,
  }
}

function ScopeGrantManager({
  subjectType,
  subjectId,
  visible,
  title,
  onClose,
}: {
  subjectType: 'user' | 'team'
  subjectId: string | null
  visible: boolean
  title: string
  onClose: () => void
}) {
  const { message } = App.useApp()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const canManageScopeGrants = hasPermission(permissionSnapshotQuery.data?.data, 'access.scope-grants.manage')
  const queryClient = useQueryClient()
  const [form] = Form.useForm<Record<string, unknown>>()
  const [editing, setEditing] = useState<ScopeGrant | null>(null)
  const [grantModalVisible, setGrantModalVisible] = useState(false)

  const grantsQuery = useQuery({
    queryKey: ['scope-grants'],
    queryFn: () => api.get<ApiResponse<ScopeGrant[]>>('/access/scope-grants'),
    enabled: visible,
  })
  const applicationsQuery = useQuery({
    queryKey: ['applications'],
    queryFn: () => api.get<ApiResponse<Array<{ id: string; name: string }>>>('/applications'),
    enabled: visible,
  })

  const applicationMap = useMemo(
    () => Object.fromEntries((applicationsQuery.data?.data ?? []).map((item) => [item.id, item.name])),
    [applicationsQuery.data],
  )

  const grants = useMemo(
    () => (grantsQuery.data?.data ?? []).filter((item) => item.subjectType === subjectType && item.subjectId === subjectId),
    [grantsQuery.data, subjectId, subjectType],
  )

  const createMutation = useMutation({
    mutationFn: (values: Record<string, unknown>) => api.post('/access/scope-grants', values),
	    onSuccess: () => {
	      message.success('授权项创建成功')
	      queryClient.invalidateQueries({ queryKey: ['scope-grants'] })
	      void invalidateAuthz(queryClient)
	      setGrantModalVisible(false)
	    },
    onError: (err: Error) => message.error(err.message),
  })
  const updateMutation = useMutation({
    mutationFn: ({ id, values }: { id: string; values: Record<string, unknown> }) => api.put(`/access/scope-grants/${id}`, values),
	    onSuccess: () => {
	      message.success('授权项更新成功')
	      queryClient.invalidateQueries({ queryKey: ['scope-grants'] })
	      void invalidateAuthz(queryClient)
	      setEditing(null)
	      setGrantModalVisible(false)
    },
    onError: (err: Error) => message.error(err.message),
  })
  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.delete(`/access/scope-grants/${id}`),
	    onSuccess: () => {
	      message.success('授权项已删除')
	      queryClient.invalidateQueries({ queryKey: ['scope-grants'] })
	      void invalidateAuthz(queryClient)
	    },
    onError: (err: Error) => message.error(err.message),
  })

  const submitGrant = (values: Record<string, unknown>) => {
    const payload = {
      ...values,
      subjectType,
      subjectId,
      environmentIds: parseCSV(values.environmentIds),
      applicationIds: parseCSV(values.applicationIds),
    }
    if (editing) {
      updateMutation.mutate({ id: editing.id, values: payload })
      return
    }
    createMutation.mutate(payload)
  }

  const columns: ColumnProps<ScopeGrant>[] = [
    { title: '范围 Key', dataIndex: 'businessLineId', render: (value: string) => value || '-' },
    {
      title: '环境',
      dataIndex: 'environmentIds',
      render: (values: string[]) => values?.length ? values.map((item) => <Tag key={item}>{item}</Tag>) : '全部',
    },
    {
      title: '应用',
      dataIndex: 'applicationIds',
      render: (values: string[]) => values?.length ? values.map((item) => <Tag key={item}>{applicationMap[item] || item}</Tag>) : '全部',
    },
    { title: '角色', dataIndex: 'role' },
    { title: '效果', dataIndex: 'effect' },
    { title: '启用', dataIndex: 'enabled', render: (value: boolean) => <StatusTag value={value ? 'enabled' : 'disabled'} /> },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: unknown, record: ScopeGrant) => (
        <Space className="soha-row-action-icons">
          {canManageScopeGrants ? (
            <>
              <ManagementIconButton
                aria-label="编辑授权项"
                icon={<EditOutlined />}
                size="small"
                tooltip="编辑"
                onClick={() => { setEditing(record); setGrantModalVisible(true) }}
              />
              <Popconfirm title="确认删除？" onConfirm={() => deleteMutation.mutate(record.id)}>
                <ManagementIconButton
                  aria-label="删除授权项"
                  danger
                  icon={<DeleteOutlined />}
                  size="small"
                  tooltip="删除"
                />
              </Popconfirm>
            </>
          ) : '-'}
        </Space>
      ),
    },
  ]

  return (
    <>
      <Modal title={title} open={visible} onCancel={onClose} footer={null} width={880}>
        <div className="soha-page">
          <div className="flex justify-end">
            {canManageScopeGrants ? (
              <Button icon={<PlusOutlined />} type="primary" onClick={() => { setEditing(null); setGrantModalVisible(true) }}>
                新建授权项
              </Button>
            ) : null}
          </div>
          <AdminTable columns={columns} dataSource={grants} rowKey="id" loading={grantsQuery.isLoading} />
        </div>
      </Modal>
      <Modal
        title={editing ? '编辑授权项' : '新建授权项'}
        open={grantModalVisible}
        onCancel={() => { setGrantModalVisible(false); setEditing(null) }}
        onOk={async () => {
          try {
            const values = await form.validateFields()
            submitGrant(values)
          } catch {
            return
          }
        }}
        okText={editing ? '更新' : '创建'}
        cancelText="取消"
        confirmLoading={createMutation.isPending || updateMutation.isPending}
        width={760}
        destroyOnHidden
        mask={{ closable: false }}
        styles={{ body: { maxHeight: '65vh', overflow: 'auto' } }}
      >
        <Form
          form={form}
          key={editing?.id ?? 'create-scope-grant'}
          layout="vertical"
          initialValues={editing ? {
            ...editing,
            environmentIds: joinCSV(editing.environmentIds),
            applicationIds: joinCSV(editing.applicationIds),
          } : { enabled: true, effect: 'allow', role: 'developer' }}
        >
          <Form.Item name="businessLineId" label="范围 Key" rules={[{ required: true, message: '请输入范围 Key' }]}>
            <Input placeholder="应用组 / 历史 businessLineId" />
          </Form.Item>
          <Form.Item name="environmentIds" label="环境 IDs">
            <Input placeholder="留空表示全部环境，多个以逗号分隔" />
          </Form.Item>
          <Form.Item name="applicationIds" label="应用 IDs">
            <Input placeholder="留空表示全部应用，多个以逗号分隔" />
          </Form.Item>
          <Form.Item name="role" label="角色" rules={[{ required: true, message: '请输入角色' }]}>
            <Input />
          </Form.Item>
          <Form.Item name="effect" label="效果">
            <Input disabled />
          </Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked">
            <Switch />
          </Form.Item>
        </Form>
      </Modal>
    </>
  )
}

export function AccessUsersPage() {
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const snapshot = permissionSnapshotQuery.data?.data
  const canViewUsers = hasPermission(snapshot, 'access.users.view')
  const canManageUsers = hasPermission(snapshot, 'access.users.manage')
  const canManageScopeGrants = hasPermission(snapshot, 'access.scope-grants.manage')
  const [form] = Form.useForm<Record<string, unknown>>()
  const [modalVisible, setModalVisible] = useState(false)
  const [editing, setEditing] = useState<AccessUser | null>(null)
  const [grantUser, setGrantUser] = useState<AccessUser | null>(null)
  const [searchText, setSearchText] = useState('')
  const [selectedOrgId, setSelectedOrgId] = useState(ORG_ALL_KEY)
  const [includeSubOrganizations, setIncludeSubOrganizations] = useState(true)

  const usersQuery = useQuery({
    queryKey: ['access/users'],
    queryFn: () => api.get<ApiResponse<AccessUser[]>>('/access/users'),
  })
  const rolesQuery = useQuery({
    queryKey: ['access/roles'],
    queryFn: () => api.get<ApiResponse<AccessRole[]>>('/access/roles'),
  })
  const teamsQuery = useQuery({
    queryKey: ['access/teams'],
    queryFn: () => api.get<ApiResponse<AccessTeam[]>>('/access/teams'),
  })

  const roleMap = useMemo(
    () => Object.fromEntries((rolesQuery.data?.data ?? []).map((item) => [item.id, item.name])),
    [rolesQuery.data],
  )
  const teamMap = useMemo(
    () => Object.fromEntries((teamsQuery.data?.data ?? []).map((item) => [item.id, item.name])),
    [teamsQuery.data],
  )
  const roleOptions = useMemo(
    () => (rolesQuery.data?.data ?? []).map((item) => ({ value: item.id, label: item.name })),
    [rolesQuery.data],
  )
  const teamOptions = useMemo(
    () => (teamsQuery.data?.data ?? []).map((item) => ({
      value: item.id,
      label: item.path ? `${item.name} (${item.path})` : item.name,
    })),
    [teamsQuery.data],
  )
  const userCountByOrg = useMemo(() => {
    const counts = new Map<string, number>()
    ;(teamsQuery.data?.data ?? []).forEach((item) => counts.set(item.id, 0))
    ;(usersQuery.data?.data ?? []).forEach((user) => {
      new Set(user.teams ?? []).forEach((teamID) => counts.set(teamID, (counts.get(teamID) ?? 0) + 1))
    })
    return counts
  }, [teamsQuery.data, usersQuery.data])
  const organizationTreeData = useMemo(
    () => buildOrganizationTree(teamsQuery.data?.data ?? [], userCountByOrg),
    [teamsQuery.data, userCountByOrg],
  )
  const scopedOrganizationIds = useMemo(() => {
    if (selectedOrgId === ORG_ALL_KEY) {
      return new Set<string>()
    }
    const ids = new Set<string>([selectedOrgId])
    if (includeSubOrganizations) {
      collectOrganizationDescendantIds(teamsQuery.data?.data ?? [], selectedOrgId).forEach((id) => ids.add(id))
    }
    return ids
  }, [includeSubOrganizations, selectedOrgId, teamsQuery.data])
  const selectedOrganization = useMemo(
    () => (teamsQuery.data?.data ?? []).find((item) => item.id === selectedOrgId) ?? null,
    [selectedOrgId, teamsQuery.data],
  )
  const filteredUsers = useMemo(() => {
    const query = searchText.trim().toLowerCase()
    const items = usersQuery.data?.data ?? []
    return items.filter((item) => {
      if (!organizationMatchesSelection(item, selectedOrgId, scopedOrganizationIds)) {
        return false
      }
      if (!query) {
        return true
      }
      return [item.username, item.displayName, item.email, ...(item.roles ?? []), ...(item.teams ?? []).map((teamID) => teamMap[teamID] || teamID)]
        .filter(Boolean)
        .some((value) => String(value).toLowerCase().includes(query))
    })
  }, [scopedOrganizationIds, searchText, selectedOrgId, teamMap, usersQuery.data])

  const createMutation = useMutation({
    mutationFn: (values: Record<string, unknown>) => api.post('/access/users', values),
	    onSuccess: () => {
	      message.success('用户创建成功')
	      queryClient.invalidateQueries({ queryKey: ['access/users'] })
	      void invalidateAuthz(queryClient)
	      setModalVisible(false)
	    },
    onError: (err: Error) => message.error(err.message),
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, values }: { id: string; values: Record<string, unknown> }) => api.put(`/access/users/${id}`, values),
	    onSuccess: () => {
	      message.success('用户更新成功')
	      queryClient.invalidateQueries({ queryKey: ['access/users'] })
	      void invalidateAuthz(queryClient)
	      setModalVisible(false)
	      setEditing(null)
    },
    onError: (err: Error) => message.error(err.message),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.delete(`/access/users/${id}`),
	    onSuccess: () => {
	      message.success('用户删除成功')
	      queryClient.invalidateQueries({ queryKey: ['access/users'] })
	      void invalidateAuthz(queryClient)
	    },
    onError: (err: Error) => message.error(err.message),
  })

  const closeModal = () => {
    setModalVisible(false)
    setEditing(null)
  }

  const submitUser = (values: Record<string, unknown>) => {
    const payload = {
      username: String(values.username ?? '').trim(),
      displayName: String(values.displayName ?? '').trim(),
      email: String(values.email ?? '').trim(),
      status: String(values.status ?? 'active'),
      password: String(values.password ?? ''),
      roleIds: toStringArray(values.roleIds),
      teamIds: toStringArray(values.teamIds),
      tags: toStringArray(values.tags),
    }
    if (editing) {
      updateMutation.mutate({ id: editing.id, values: payload })
      return
    }
    createMutation.mutate(payload)
  }

  const columns: ColumnProps<AccessUser>[] = [
    {
      title: '头像',
      key: 'avatar',
      width: 76,
      render: (_: unknown, record: AccessUser) => (
        <Avatar className="soha-user-avatar" size="small">
          {getUserInitial(record)}
        </Avatar>
      ),
    },
    {
      title: '用户名',
      dataIndex: 'username',
      width: 180,
      render: (value: string) => <Text strong>{value || '-'}</Text>,
    },
    {
      title: '显示名',
      dataIndex: 'displayName',
      width: 180,
      render: (value: string, record: AccessUser) => (
        <Text type={value ? undefined : 'secondary'}>{value || record.email || '-'}</Text>
      ),
    },
    { title: '邮箱', dataIndex: 'email', width: 240 },
    {
      title: '角色',
      dataIndex: 'roles',
      width: 180,
      render: (roles: string[]) => renderMappedTags(roles, roleMap, '未绑定'),
    },
    {
      title: '组织',
      dataIndex: 'teams',
      width: 160,
      render: (teams: string[]) => renderMappedTags(teams, teamMap, '未绑定'),
    },
    {
      ...tableColumnPresets.status,
      title: '状态',
      dataIndex: 'status',
      render: (value: string) => <StatusTag value={value} />,
    },
    {
      ...tableColumnPresets.datetime,
      title: '最近登录',
      dataIndex: 'lastLoginAt',
      render: (value?: string) => value ? formatDateTime(value) : '-',
    },
    {
      ...tableColumnPresets.action,
      title: '操作',
      width: 164,
      dataIndex: 'id',
      render: (_: unknown, record: AccessUser) => (
        <Space className="soha-row-action-icons">
          {canManageUsers || canManageScopeGrants ? (
            <>
              {canManageScopeGrants ? (
                <ManagementIconButton
                  aria-label="授权范围"
                  icon={<FolderOpenOutlined />}
                  size="small"
                  tooltip="授权范围"
                  onClick={() => setGrantUser(record)}
                />
              ) : null}
              {canManageUsers ? (
                <ManagementIconButton
                  aria-label="编辑用户"
                  icon={<EditOutlined />}
                  size="small"
                  tooltip="编辑"
                  onClick={() => { setEditing(record); setModalVisible(true) }}
                />
              ) : null}
              {canManageUsers ? (
                <Popconfirm title="确认删除？" onConfirm={() => deleteMutation.mutate(record.id)}>
                  <ManagementIconButton
                    aria-label="删除用户"
                    danger
                    icon={<DeleteOutlined />}
                    size="small"
                    tooltip="删除"
                  />
                </Popconfirm>
              ) : null}
            </>
          ) : '-'}
        </Space>
      ),
    },
  ]

  if (!canViewUsers) {
    return <div className="soha-page"><ManagementState kind="no-permission" description="当前账号没有用户管理权限。" /></div>
  }

  return (
    <div className="soha-page">
      <div className="soha-access-users-layout">
        <section className="soha-access-org-panel">
          <div className="soha-access-org-panel-header">
            <Space size={8}>
              <ApartmentOutlined />
              <Text strong>公司组织</Text>
            </Space>
            <Tag>{usersQuery.data?.data?.length ?? 0}</Tag>
          </div>
          <Tree
            blockNode
            defaultExpandedKeys={[ORG_ALL_KEY]}
            selectedKeys={[selectedOrgId]}
            treeData={organizationTreeData}
            onSelect={(keys) => setSelectedOrgId(String(keys[0] ?? ORG_ALL_KEY))}
          />
        </section>
        <section className="soha-access-users-panel">
          <AdminTable
            columnSettingIconOnly
            columnSettingPlacement="header"
            shellClassName="soha-management-table-shell"
            title={selectedOrganization ? `${getOrganizationLabel(selectedOrganization)} 用户` : '用户管理'}
            className="soha-access-table"
            toolbar={(
              <div className="soha-workload-table-filters">
                <Input
                  allowClear
                  className="soha-platform-compact-field"
                  size="small"
                  placeholder="搜索用户名、显示名、邮箱、角色或组织"
                  value={searchText}
                  onChange={(event) => setSearchText(event.target.value)}
                  style={{ width: 320 }}
                />
                {selectedOrgId !== ORG_ALL_KEY ? (
                  <Space size={6}>
                    <Text type="secondary">包含下级</Text>
                    <Switch size="small" checked={includeSubOrganizations} onChange={setIncludeSubOrganizations} />
                  </Space>
                ) : null}
              </div>
            )}
            toolbarExtra={canManageUsers ? (
              <ManagementTableToolbar>
                <Button
                  size="small"
                  icon={<PlusOutlined />}
                  type="primary"
                  onClick={() => {
                    setEditing(null)
                    setModalVisible(true)
                  }}
                >
                  添加用户
                </Button>
              </ManagementTableToolbar>
            ) : null}
            columns={columns}
            dataSource={filteredUsers}
            rowKey="id"
            loading={usersQuery.isLoading || teamsQuery.isLoading}
          />
        </section>
      </div>
      <Modal
        title={editing ? `编辑用户: ${getUserLabel(editing)}` : '添加用户'}
        open={modalVisible}
        onCancel={closeModal}
        onOk={async () => {
          try {
            const values = await form.validateFields()
            submitUser(values)
          } catch {
            return
          }
        }}
        okText={editing ? '更新' : '创建'}
        cancelText="取消"
        confirmLoading={createMutation.isPending || updateMutation.isPending}
        width={860}
        destroyOnHidden
        mask={{ closable: false }}
        styles={{ body: { maxHeight: '70vh', overflow: 'auto' } }}
      >
        <Form
          form={form}
          key={editing?.id ?? 'create-user'}
          layout="vertical"
          initialValues={editing ? {
            username: editing.username,
            displayName: editing.displayName,
            email: editing.email,
            status: editing.status || 'active',
            roleIds: editing.roles ?? [],
            teamIds: editing.teams ?? [],
            tags: editing.tags ?? [],
          } : {
            status: 'active',
            roleIds: [],
            teamIds: [],
            tags: [],
          }}
        >
          <Row gutter={16}>
            <Col span={12}>
              <Form.Item name="username" label="用户名" rules={[{ required: true, message: '请输入用户名' }]}>
                <Input />
              </Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item name="displayName" label="显示名">
                <Input placeholder="留空时顶部默认展示用户名" />
              </Form.Item>
            </Col>
          </Row>
          <Row gutter={16}>
            <Col span={12}>
              <Form.Item name="email" label="邮箱" rules={[{ required: true, message: '请输入邮箱' }]}>
                <Input />
              </Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item name="status" label="状态" rules={[{ required: true, message: '请选择状态' }]}>
                <Select options={USER_STATUS_OPTIONS} />
              </Form.Item>
            </Col>
          </Row>
          <Row gutter={16}>
            <Col span={12}>
              <Form.Item name="roleIds" label="角色">
                <Select mode="multiple" options={roleOptions} placeholder="选择用户角色" />
              </Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item name="teamIds" label="所属组织">
                <Select mode="multiple" optionFilterProp="label" options={teamOptions} placeholder="选择所属组织或部门" />
              </Form.Item>
            </Col>
          </Row>
          <Form.Item name="tags" label="标签">
            <Select mode="tags" tokenSeparators={[',']} placeholder="输入标签后按回车确认" />
          </Form.Item>
          <Form.Item
            name="password"
            label={editing ? '重置密码' : '登录密码'}
            rules={editing ? undefined : [{ required: true, message: '请输入密码' }]}
          >
            <Input.Password placeholder={editing ? '留空表示不修改密码' : '请输入初始密码'} />
          </Form.Item>
        </Form>
      </Modal>
      <ScopeGrantManager
        subjectType="user"
        subjectId={grantUser?.id ?? null}
        visible={!!grantUser}
        title={grantUser ? `用户授权范围: ${getUserLabel(grantUser)}` : '用户授权范围'}
        onClose={() => setGrantUser(null)}
      />
    </div>
  )
}

export function AccessRolesPage() {
  const permissionSnapshotQuery = usePermissionSnapshot()
  const snapshot = permissionSnapshotQuery.data?.data
  const canViewRoles = hasPermission(snapshot, 'access.roles.view')
  const canManageRoles = hasPermission(snapshot, 'access.roles.manage')
  const [form] = Form.useForm<Record<string, unknown>>()
  const crud = useCRUD<AccessRole>('access/roles', { invalidateKeys: [['access/users']], invalidateAuthz: true })

  const columns: ColumnProps<AccessRole>[] = [
    { title: '角色名称', dataIndex: 'name', width: 128 },
    { title: '范围', dataIndex: 'scope', width: 88, render: (value: string) => value || 'custom' },
    {
      title: '权限动作',
      dataIndex: 'capabilities',
      width: 170,
      render: (values: string[]) => renderCompactMappedTags(values, ACCESS_ACTION_LABEL_MAP, '未配置', 1, '权限动作'),
    },
    {
      title: '菜单/动作权限',
      dataIndex: 'permissionKeys',
      width: 320,
      render: (values: string[] | undefined) => renderCompactMappedTags(normalizePermissionKeys(values), consolePermissionLabelMap, '未配置', 1, '权限键'),
    },
    { title: '绑定用户', dataIndex: 'userCount', width: 88 },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: unknown, record: AccessRole) => (
        <Space className="soha-row-action-icons">
          {canManageRoles ? (
            <>
              <ManagementIconButton
                aria-label="编辑角色"
                icon={<EditOutlined />}
                size="small"
                tooltip="编辑"
                onClick={() => crud.openEdit(record)}
              />
              <Popconfirm title="确认删除？" onConfirm={() => crud.deleteMutation.mutate(record.id)}>
                <ManagementIconButton
                  aria-label="删除角色"
                  danger
                  icon={<DeleteOutlined />}
                  size="small"
                  tooltip="删除"
                />
              </Popconfirm>
            </>
          ) : '-'}
        </Space>
      ),
    },
  ]

  const submitRole = (values: Record<string, unknown>) => {
    crud.handleSubmit({
      name: String(values.name ?? '').trim(),
      scope: String(values.scope ?? 'custom'),
      capabilities: toStringArray(values.capabilities),
      permissionKeys: normalizePermissionKeys(values.permissionKeys),
    })
  }

  if (!canViewRoles) {
    return <div className="soha-page"><ManagementState kind="no-permission" description="当前账号没有角色管理权限。" /></div>
  }

  return (
    <div className="soha-page">
      <Alert
        className="mb-4"
        type="info"
        showIcon
        title="角色授权按菜单和页面动作选择，保存时仍提交 permissionKeys，由后端 API 与 AI Gateway 统一校验。"
      />
      <AdminTable
        columnSettingIconOnly
        columnSettingPlacement="header"
        shellClassName="soha-management-table-shell"
        title="角色管理"
        className="soha-access-table"
        toolbarExtra={canManageRoles ? (
          <ManagementTableToolbar>
            <Button size="small" icon={<PlusOutlined />} type="primary" onClick={crud.openCreate}>
              添加角色
            </Button>
          </ManagementTableToolbar>
        ) : null}
        columns={columns}
        dataSource={crud.data}
        rowKey="id"
        loading={crud.isLoading}
      />
      <Modal
        title={crud.editing ? `编辑角色: ${crud.editing.name}` : '添加角色'}
        open={crud.modalVisible}
        onCancel={crud.closeModal}
        onOk={async () => {
          try {
            const values = await form.validateFields()
            submitRole(values)
          } catch {
            return
          }
        }}
        okText={crud.editing ? '更新' : '创建'}
        cancelText="取消"
        confirmLoading={crud.isSaving}
        width={720}
        destroyOnHidden
        mask={{ closable: false }}
      >
        <Form
          form={form}
          key={crud.editing?.id ?? 'create-role'}
          layout="vertical"
          initialValues={crud.editing ? {
            name: crud.editing.name,
            scope: crud.editing.scope || 'custom',
            capabilities: crud.editing.capabilities ?? [],
            permissionKeys: normalizePermissionKeys(crud.editing.permissionKeys),
          } : {
            scope: 'custom',
            capabilities: [],
            permissionKeys: [],
          }}
        >
          <Form.Item name="name" label="角色名称" rules={[{ required: true, message: '请输入角色名称' }]}>
            <Input />
          </Form.Item>
          <Form.Item name="scope" label="角色范围">
            <Select options={ROLE_SCOPE_OPTIONS} />
          </Form.Item>
          <Form.Item
            name="capabilities"
            label="权限动作"
            rules={[{
              validator: (_, value) => toStringArray(value).length > 0
                ? Promise.resolve()
                : Promise.reject(new Error('请选择至少一个权限动作')),
            }]}
          >
            <Select mode="multiple" options={ACCESS_ACTION_OPTIONS} />
          </Form.Item>
          <Form.Item label="菜单与动作权限">
            <Form.Item noStyle shouldUpdate={(prev, next) => prev.permissionKeys !== next.permissionKeys}>
              {({ getFieldValue, setFieldsValue }) => {
                const permissionKeys = normalizePermissionKeys(getFieldValue('permissionKeys'))
                return (
                  <Space orientation="vertical" size={8} className="soha-role-permission-tree">
                    <Space size={8} wrap>
                      <Tag>{`${permissionKeys.length} 个权限键`}</Tag>
                      <Text type="secondary">父级菜单勾选会自动带出下级页面与动作权限。</Text>
                    </Space>
                    <Tree
                      checkable
                      defaultExpandAll
                      height={380}
                      treeData={rolePermissionTreeData}
                      checkedKeys={checkedPermissionTreeKeys(permissionKeys)}
                      onCheck={(checkedKeys) => setFieldsValue({ permissionKeys: extractPermissionKeysFromTreeCheck(checkedKeys) })}
                    />
                  </Space>
                )
              }}
            </Form.Item>
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}

export function AccessTeamsPage() {
  const permissionSnapshotQuery = usePermissionSnapshot()
  const snapshot = permissionSnapshotQuery.data?.data
  const canViewGroups = hasPermission(snapshot, 'access.groups.view')
  const canManageGroups = hasPermission(snapshot, 'access.groups.manage')
  const canManageScopeGrants = hasPermission(snapshot, 'access.scope-grants.manage')
  const canViewLoginSettings = hasPermission(snapshot, 'settings.identity.view')
  const [form] = Form.useForm<Record<string, unknown>>()
  const crud = useCRUD<AccessTeam>('access/teams', { invalidateKeys: [['access/users'], ['scope-grants']], invalidateAuthz: true })
  const [grantTeam, setGrantTeam] = useState<AccessTeam | null>(null)
  const loginProvidersQuery = useQuery({
    queryKey: ['settings-identity'],
    queryFn: () => api.get<ApiResponse<IdentitySettingsResponse>>('/settings/identity'),
    enabled: canManageGroups && canViewLoginSettings,
    retry: false,
    select: (response) => Array.isArray(response.data?.providers) ? response.data.providers : [],
  })
  const loginProviders = loginProvidersQuery.data ?? []
  const organizationSourceOptions = useMemo(() => buildOrganizationSourceOptions(loginProviders), [loginProviders])
  const organizationSourceLabelMap = useMemo(() => buildOrganizationSourceLabelMap(loginProviders), [loginProviders])
  const blockedOrganizationIds = new Set(crud.editing ? [crud.editing.id, ...collectOrganizationDescendantIds(crud.data, crud.editing.id)] : [])
  const parentOrganizationOptions = [
    { value: '', label: '根组织' },
    ...crud.data
      .filter((item) => !blockedOrganizationIds.has(item.id))
      .map((item) => ({ value: item.id, label: `${getOrganizationLabel(item)} (${getOrganizationPathLabel(item)})` })),
  ]

  const columns: ColumnProps<AccessTeam>[] = [
    { title: '组织名称', dataIndex: 'name', render: (value: string) => <Text strong>{value}</Text> },
    { title: '上级组织', dataIndex: 'parentId', render: (value: string) => value ? getOrganizationLabel(crud.data.find((item) => item.id === value)) : '根组织' },
    { title: '组织路径', dataIndex: 'path', render: (value: string) => value || '-' },
    { title: '标识', dataIndex: 'slug', render: (value: string) => value || '-' },
    { title: '映射来源', dataIndex: 'source', render: (value: string) => organizationSourceLabel(value, organizationSourceLabelMap) },
    { title: '说明', dataIndex: 'metadata', render: (value: Record<string, unknown>) => getGroupDescription(value) || '-' },
    { title: '成员数', dataIndex: 'userCount' },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: unknown, record: AccessTeam) => (
        <Space className="soha-row-action-icons">
          {canManageGroups || canManageScopeGrants ? (
            <>
              {canManageScopeGrants ? (
                <ManagementIconButton
                  aria-label="授权范围"
                  icon={<FolderOpenOutlined />}
                  size="small"
                  tooltip="授权范围"
                  onClick={() => setGrantTeam(record)}
                />
              ) : null}
              {canManageGroups ? (
                <ManagementIconButton
                  aria-label="编辑组织"
                  icon={<EditOutlined />}
                  size="small"
                  tooltip="编辑"
                  onClick={() => crud.openEdit(record)}
                />
              ) : null}
              {canManageGroups ? (
                <Popconfirm title="确认删除？" onConfirm={() => crud.deleteMutation.mutate(record.id)}>
                  <ManagementIconButton
                    aria-label="删除组织"
                    danger
                    icon={<DeleteOutlined />}
                    size="small"
                    tooltip="删除"
                  />
                </Popconfirm>
              ) : null}
            </>
          ) : '-'}
        </Space>
      ),
    },
  ]

  const submitGroup = (values: Record<string, unknown>) => {
    crud.handleSubmit({
      name: String(values.name ?? '').trim(),
      slug: String(values.slug ?? '').trim(),
      parentId: String(values.parentId ?? '').trim(),
      source: String(values.source ?? 'local').trim() || 'local',
      externalId: String(values.externalId ?? '').trim(),
      metadata: {
        ...(crud.editing?.metadata ?? {}),
        description: String(values.description ?? '').trim(),
      },
    })
  }

  if (!canViewGroups) {
    return <div className="soha-page"><ManagementState kind="no-permission" description="当前账号没有组织管理权限。" /></div>
  }

  return (
    <div className="soha-page">
      <AdminTable
        columnSettingIconOnly
        columnSettingPlacement="header"
        shellClassName="soha-management-table-shell"
        title="组织架构"
        className="soha-access-table"
        toolbarExtra={canManageGroups ? (
          <ManagementTableToolbar>
            <Button size="small" icon={<PlusOutlined />} type="primary" onClick={crud.openCreate}>
              添加组织
            </Button>
          </ManagementTableToolbar>
        ) : null}
        columns={columns}
        dataSource={crud.data}
        rowKey="id"
        loading={crud.isLoading}
      />
      <Modal
        title={crud.editing ? `编辑组织: ${crud.editing.name}` : '添加组织'}
        open={crud.modalVisible}
        onCancel={crud.closeModal}
        onOk={async () => {
          try {
            const values = await form.validateFields()
            submitGroup(values)
          } catch {
            return
          }
        }}
        okText={crud.editing ? '更新' : '创建'}
        cancelText="取消"
        confirmLoading={crud.isSaving}
        width={720}
        destroyOnHidden
        mask={{ closable: false }}
      >
        <Form
          form={form}
          key={crud.editing?.id ?? 'create-group'}
          layout="vertical"
          initialValues={crud.editing ? {
            name: crud.editing.name,
            slug: crud.editing.slug,
            parentId: crud.editing.parentId ?? '',
            source: crud.editing.source || 'local',
            externalId: crud.editing.externalId ?? '',
            description: getGroupDescription(crud.editing.metadata),
          } : { parentId: '', source: 'local' }}
        >
          <Form.Item name="parentId" label="上级组织">
            <Select allowClear optionFilterProp="label" options={parentOrganizationOptions} />
          </Form.Item>
          <Form.Item name="name" label="组织名称" rules={[{ required: true, message: '请输入组织名称' }]}>
            <Input />
          </Form.Item>
          <Form.Item name="slug" label="标识">
            <Input placeholder="留空时按名称自动生成" />
          </Form.Item>
          <Row gutter={16}>
            <Col span={12}>
              <Form.Item
                name="source"
                label="映射来源"
                extra="App Key 和 Secret 在登录设置的登录源应用中维护。"
              >
                <Select
                  loading={loginProvidersQuery.isFetching}
                  optionFilterProp="label"
                  options={organizationSourceOptions}
                  showSearch
                />
              </Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item
                name="externalId"
                label="外部组织 ID"
                extra="填写第三方目录返回的部门或组织 ID，用于登录后匹配本地组织。"
              >
                <Input placeholder="飞书 department_id / 钉钉 dept_id / 企业微信 department_id" />
              </Form.Item>
            </Col>
          </Row>
          <Form.Item name="description" label="说明">
            <Input.TextArea rows={4} placeholder="说明该组织的职责边界和适用成员" />
          </Form.Item>
        </Form>
      </Modal>
      <ScopeGrantManager
        subjectType="team"
        subjectId={grantTeam?.id ?? null}
        visible={!!grantTeam}
        title={grantTeam ? `组织授权范围: ${grantTeam.name}` : '组织授权范围'}
        onClose={() => setGrantTeam(null)}
      />
    </div>
  )
}

export function AccessPoliciesPage() {
  const permissionSnapshotQuery = usePermissionSnapshot()
  const snapshot = permissionSnapshotQuery.data?.data
  const canViewPolicies = hasPermission(snapshot, 'access.policies.view')
  const canManagePolicies = hasPermission(snapshot, 'access.policies.manage')
  const [form] = Form.useForm<Record<string, unknown>>()
  const crud = useCRUD<AccessPolicy>('access/policies', { invalidateAuthz: true })

  const rolesQuery = useQuery({
    queryKey: ['access/roles'],
    queryFn: () => api.get<ApiResponse<AccessRole[]>>('/access/roles'),
  })
  const teamsQuery = useQuery({
    queryKey: ['access/teams'],
    queryFn: () => api.get<ApiResponse<AccessTeam[]>>('/access/teams'),
  })

  const roleMap = useMemo(
    () => Object.fromEntries((rolesQuery.data?.data ?? []).map((item) => [item.id, item.name])),
    [rolesQuery.data],
  )
  const teamMap = useMemo(
    () => Object.fromEntries((teamsQuery.data?.data ?? []).map((item) => [item.id, item.name])),
    [teamsQuery.data],
  )
  const roleOptions = useMemo(
    () => (rolesQuery.data?.data ?? []).map((item) => ({ value: item.id, label: item.name })),
    [rolesQuery.data],
  )
  const teamOptions = useMemo(
    () => (teamsQuery.data?.data ?? []).map((item) => ({
      value: item.id,
      label: item.path ? `${item.name} (${item.path})` : item.name,
    })),
    [teamsQuery.data],
  )

  const columns: ColumnProps<AccessPolicy>[] = [
    { title: '策略名称', dataIndex: 'name' },
    {
      title: '效果',
      dataIndex: 'effect',
      render: (value: string) => <StatusTag value={value} />,
    },
    { title: '优先级', dataIndex: 'priority' },
    {
      title: '动作',
      dataIndex: 'actions',
      render: (values: string[]) => renderMappedTags(values, {}, '未配置'),
    },
    {
      title: '主体',
      dataIndex: 'subjects',
      ellipsis: true,
      render: (_: unknown, record: AccessPolicy) => buildPolicySubjectsSummary(record, roleMap, teamMap),
    },
    {
      title: '目标',
      dataIndex: 'resources',
      ellipsis: true,
      render: (_: unknown, record: AccessPolicy) => buildPolicyTargetsSummary(record, teamMap),
    },
    { title: '原因', dataIndex: 'reason', ellipsis: true, render: (value: string) => value || '-' },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: unknown, record: AccessPolicy) => (
        <Space className="soha-row-action-icons">
          {canManagePolicies ? (
            <>
              <ManagementIconButton
                aria-label="编辑策略"
                icon={<EditOutlined />}
                size="small"
                tooltip="编辑"
                onClick={() => crud.openEdit(record)}
              />
              <Popconfirm title="确认删除？" onConfirm={() => crud.deleteMutation.mutate(record.id)}>
                <ManagementIconButton
                  aria-label="删除策略"
                  danger
                  icon={<DeleteOutlined />}
                  size="small"
                  tooltip="删除"
                />
              </Popconfirm>
            </>
          ) : '-'}
        </Space>
      ),
    },
  ]

  const submitPolicy = (values: Record<string, unknown>) => {
    const current = crud.editing
    const baseSubjects = current?.subjects ?? { roles: [], teams: [], projects: [], users: [], tags: [] }
    const baseClusters = current?.clusters ?? { ids: [], regions: [], environments: [], labels: {} }
    const baseNamespaces = current?.namespaces ?? { names: [], ownerTeams: [], labels: {} }
    const baseResources = current?.resources ?? { kinds: [], names: [], labels: {} }
    const baseConditions = current?.conditions ?? { sources: [], approvalStates: [] }

    crud.handleSubmit({
      name: String(values.name ?? '').trim(),
      effect: String(values.effect ?? 'allow'),
      priority: Number(values.priority ?? 0),
      actions: toStringArray(values.actions),
      subjects: {
        ...baseSubjects,
        roles: toStringArray(values.subjectRoleIds),
        teams: toStringArray(values.subjectTeamIds),
        users: parseCSV(values.subjectUsers),
        tags: parseCSV(values.subjectTags),
      },
      clusters: {
        ...baseClusters,
        ids: parseCSV(values.clusterIds),
        regions: parseCSV(values.clusterRegions),
        environments: parseCSV(values.clusterEnvironments),
      },
      namespaces: {
        ...baseNamespaces,
        names: parseCSV(values.namespaceNames),
        ownerTeams: toStringArray(values.ownerTeamIds),
      },
      resources: {
        ...baseResources,
        kinds: parseCSV(values.resourceKinds),
        names: parseCSV(values.resourceNames),
      },
      conditions: {
        ...baseConditions,
        sources: parseCSV(values.sources),
        approvalStates: parseCSV(values.approvalStates),
      },
      reason: String(values.reason ?? '').trim(),
    })
  }

  if (!canViewPolicies) {
    return <div className="soha-page"><ManagementState kind="no-permission" description="当前账号没有策略管理权限。" /></div>
  }

  return (
    <div className="soha-page">
      <AdminTable
        columnSettingIconOnly
        columnSettingPlacement="header"
        shellClassName="soha-management-table-shell"
        title="策略管理"
        className="soha-access-table"
        toolbarExtra={canManagePolicies ? (
          <ManagementTableToolbar>
            <Button size="small" icon={<PlusOutlined />} type="primary" onClick={crud.openCreate}>
              添加策略
            </Button>
          </ManagementTableToolbar>
        ) : null}
        columns={columns}
        dataSource={crud.data}
        rowKey="id"
        loading={crud.isLoading}
      />
      <Modal
        title={crud.editing ? `编辑策略: ${crud.editing.name}` : '添加策略'}
        open={crud.modalVisible}
        onCancel={crud.closeModal}
        onOk={async () => {
          try {
            const values = await form.validateFields()
            submitPolicy(values)
          } catch {
            return
          }
        }}
        okText={crud.editing ? '更新' : '创建'}
        cancelText="取消"
        confirmLoading={crud.isSaving}
        width={920}
        destroyOnHidden
        mask={{ closable: false }}
        styles={{ body: { maxHeight: '72vh', overflow: 'auto' } }}
      >
        <Form
          form={form}
          key={crud.editing?.id ?? 'create-policy'}
          layout="vertical"
          initialValues={crud.editing ? {
            name: crud.editing.name,
            effect: crud.editing.effect,
            priority: crud.editing.priority,
            actions: crud.editing.actions ?? [],
            subjectRoleIds: crud.editing.subjects?.roles ?? [],
            subjectTeamIds: crud.editing.subjects?.teams ?? [],
            subjectUsers: joinCSV(crud.editing.subjects?.users),
            subjectTags: joinCSV(crud.editing.subjects?.tags),
            clusterIds: joinCSV(crud.editing.clusters?.ids),
            clusterRegions: joinCSV(crud.editing.clusters?.regions),
            clusterEnvironments: joinCSV(crud.editing.clusters?.environments),
            namespaceNames: joinCSV(crud.editing.namespaces?.names),
            ownerTeamIds: crud.editing.namespaces?.ownerTeams ?? [],
            resourceKinds: joinCSV(crud.editing.resources?.kinds),
            resourceNames: joinCSV(crud.editing.resources?.names),
            sources: joinCSV(crud.editing.conditions?.sources),
            approvalStates: joinCSV(crud.editing.conditions?.approvalStates),
            reason: crud.editing.reason,
          } : {
            effect: 'allow',
            priority: 0,
            actions: [],
            subjectRoleIds: [],
            subjectTeamIds: [],
            ownerTeamIds: [],
          }}
        >
          <Row gutter={16}>
            <Col span={12}>
              <Form.Item name="name" label="策略名称" rules={[{ required: true, message: '请输入策略名称' }]}>
                <Input />
              </Form.Item>
            </Col>
            <Col span={6}>
              <Form.Item name="effect" label="效果" rules={[{ required: true, message: '请选择效果' }]}>
                <Select options={POLICY_EFFECT_OPTIONS} />
              </Form.Item>
            </Col>
            <Col span={6}>
              <Form.Item name="priority" label="优先级">
                <InputNumber style={{ width: '100%' }} />
              </Form.Item>
            </Col>
          </Row>
          <Form.Item
            name="actions"
            label="动作"
            rules={[{
              validator: (_, value) => toStringArray(value).length > 0
                ? Promise.resolve()
                : Promise.reject(new Error('请选择至少一个动作')),
            }]}
          >
            <Select mode="multiple" options={ACCESS_ACTION_OPTIONS} />
          </Form.Item>
          <Row gutter={16}>
            <Col span={12}>
              <Form.Item name="subjectRoleIds" label="主体角色">
                <Select mode="multiple" options={roleOptions} />
              </Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item name="subjectTeamIds" label="主体组织">
                <Select mode="multiple" optionFilterProp="label" options={teamOptions} />
              </Form.Item>
            </Col>
          </Row>
          <Row gutter={16}>
            <Col span={12}>
              <Form.Item name="subjectUsers" label="主体用户">
                <Input placeholder="多个用户名以逗号分隔" />
              </Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item name="subjectTags" label="主体标签">
                <Input placeholder="多个标签以逗号分隔" />
              </Form.Item>
            </Col>
          </Row>
          <Row gutter={16}>
            <Col span={8}>
              <Form.Item name="clusterEnvironments" label="集群环境">
                <Input placeholder="development, staging" />
              </Form.Item>
            </Col>
            <Col span={8}>
              <Form.Item name="clusterRegions" label="集群地域">
                <Input placeholder="cn-beijing, us-west" />
              </Form.Item>
            </Col>
            <Col span={8}>
              <Form.Item name="clusterIds" label="集群 IDs">
                <Input placeholder="多个集群 ID 以逗号分隔" />
              </Form.Item>
            </Col>
          </Row>
          <Row gutter={16}>
            <Col span={12}>
              <Form.Item name="namespaceNames" label="命名空间">
                <Input placeholder="多个命名空间以逗号分隔" />
              </Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item name="ownerTeamIds" label="归属组织">
                <Select mode="multiple" optionFilterProp="label" options={teamOptions} />
              </Form.Item>
            </Col>
          </Row>
          <Row gutter={16}>
            <Col span={12}>
              <Form.Item name="resourceKinds" label="资源类型">
                <Input placeholder="Pod, Deployment, Namespace" />
              </Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item name="resourceNames" label="资源名称">
                <Input placeholder="多个资源名称以逗号分隔" />
              </Form.Item>
            </Col>
          </Row>
          <Row gutter={16}>
            <Col span={12}>
              <Form.Item name="sources" label="请求来源">
                <Input placeholder="console, api, oidc" />
              </Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item name="approvalStates" label="审批状态">
                <Input placeholder="approved, pending" />
              </Form.Item>
            </Col>
          </Row>
          <Form.Item name="reason" label="原因说明">
            <Input.TextArea rows={3} placeholder="说明该策略的意图和生效边界" />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}

export function AccessCenterPage() {
  const permissionSnapshotQuery = usePermissionSnapshot()
  const snapshot = permissionSnapshotQuery.data?.data
  const firstAccessiblePath = hasPermission(snapshot, 'access.users.view')
    ? '/access/users'
    : hasPermission(snapshot, 'access.roles.view')
      ? '/access/roles'
      : hasPermission(snapshot, 'access.groups.view')
        ? '/access/teams'
        : hasPermission(snapshot, 'access.policies.view')
          ? '/access/policies'
          : null

  if (permissionSnapshotQuery.isLoading) {
    return <div className="soha-page"><div className="flex items-center justify-center h-32">加载中...</div></div>
  }

  if (!firstAccessiblePath) {
    return <div className="soha-page"><ManagementState kind="no-permission" description="当前账号没有访问控制页面权限。" /></div>
  }

  return <Navigate to={firstAccessiblePath} replace />
}
