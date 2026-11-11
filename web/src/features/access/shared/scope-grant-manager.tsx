import { MutationFeedback, ReadFeedback } from '@/components/request-feedback'
import { readableQueryData, writeOutcomeUnknown } from '@/utils/query-feedback'
import { useMemo, useRef, useState } from 'react'
import { App, Button, Modal, Popconfirm, Space, Tag, Tooltip } from 'antd'
import { DeleteOutlined, EditOutlined, PlusOutlined } from '@ant-design/icons'
import type { TableColumnsType } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AdminTable } from '@/components/admin-table'
import {
  ManagementIconButton,
  ManagementRefreshButton,
  ManagementState,
  ManagementTableToolbar,
} from '@/components/management-list'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { tableColumnPresets } from '@/utils/table-columns'
import { accessMutations, invalidateAccessScopeGrants } from './mutations'
import { accessQueries } from './queries'
import { ScopeGrantEditor } from './scope-grant-editor'
import type { AccessScopeGrant } from './types'

type ColumnProps<T> = TableColumnsType<T>[number]

interface ScopeGrantManagerProps {
  fixedApplication?: { businessLineId: string; id: string; name?: string }
  inline?: boolean
  subjectType?: 'user' | 'team'
  subjectId?: string | null
  visible: boolean
  title: string
  onClose?: () => void
}

export function ScopeGrantManager(props: ScopeGrantManagerProps) {
  return (
    <ScopeGrantSession
      key={`${props.fixedApplication?.id ?? ''}:${props.subjectType ?? 'user'}:${props.subjectId ?? ''}`}
      {...props}
    />
  )
}

function ScopeGrantSession({
  fixedApplication,
  inline = false,
  subjectType = 'user',
  subjectId = null,
  visible,
  title,
  onClose,
}: ScopeGrantManagerProps) {
  const { message } = App.useApp()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const snapshot = permissionSnapshotQuery.data?.data
  const canViewScopeGrants = hasPermission(snapshot, 'access.scope-grants.view')
  const canCreateScopeGrants = hasPermission(snapshot, 'access.scope-grants.create')
  const canUpdateScopeGrants = hasPermission(snapshot, 'access.scope-grants.update')
  const canDeleteScopeGrants = hasPermission(snapshot, 'access.scope-grants.delete')
  const canViewUsers = hasPermission(snapshot, 'access.users.view')
  const canViewTeams = hasPermission(snapshot, 'access.groups.view')
  const canViewRoles = hasPermission(snapshot, 'access.roles.view')
  const canViewApplications = hasPermission(snapshot, 'delivery.applications.view')
  const canViewApplicationEnvironments = hasPermission(
    snapshot,
    'delivery.application-environments.view',
  )
  const canViewClusters = hasPermission(snapshot, 'platform.clusters.view')
  const listsApplicationGrants = inline && Boolean(fixedApplication)
  const canUseEditor =
    (listsApplicationGrants ? canViewUsers || canViewTeams : Boolean(subjectId)) &&
    canViewRoles &&
    ((canViewApplications && canViewApplicationEnvironments) || canViewClusters)
  const queryClient = useQueryClient()
  const [editing, setEditing] = useState<AccessScopeGrant | null>(null)
  const [grantModalVisible, setGrantModalVisible] = useState(false)
  const subject = { subjectId: subjectId ?? '', subjectType }
  const applicationGrantsQuery = useQuery(
    accessQueries.scopeGrantsAll(visible && canViewScopeGrants && listsApplicationGrants),
  )
  const subjectGrantsQuery = useQuery(
    accessQueries.scopeGrants(
      subject,
      visible && canViewScopeGrants && !listsApplicationGrants && Boolean(subjectId),
    ),
  )
  const grantsQuery = listsApplicationGrants ? applicationGrantsQuery : subjectGrantsQuery
  const usersQuery = useQuery(
    accessQueries.users(visible && listsApplicationGrants && canViewUsers),
  )
  const teamsQuery = useQuery(
    accessQueries.teams(visible && listsApplicationGrants && canViewTeams),
  )
  const applicationsQuery = useQuery(
    accessQueries.applicationOptions(visible && canViewApplications),
  )
  const applicationEnvironmentsQuery = useQuery(
    accessQueries.applicationEnvironments(
      visible && listsApplicationGrants && canViewApplicationEnvironments,
    ),
  )
  const clustersQuery = useQuery(accessQueries.clusterOptions(visible && canViewClusters))
  const applicationMap = useMemo(
    () =>
      Object.fromEntries(
        ((canViewApplications ? readableQueryData(applicationsQuery) : undefined) ?? []).map(
          (item) => [item.id, item.name],
        ),
      ),
    [canViewApplications, applicationsQuery.data, applicationsQuery.error],
  )
  const clusterMap = useMemo(
    () =>
      Object.fromEntries(
        ((canViewClusters ? readableQueryData(clustersQuery) : undefined) ?? []).map((item) => [
          item.id,
          item.name,
        ]),
      ),
    [canViewClusters, clustersQuery.data, clustersQuery.error],
  )
  const subjectMap = useMemo(
    () =>
      Object.fromEntries([
        ...((canViewUsers ? readableQueryData(usersQuery) : undefined) ?? []).map((item) => [
          `user:${item.id}`,
          item.displayName || item.username,
        ]),
        ...((canViewTeams ? readableQueryData(teamsQuery) : undefined) ?? []).map((item) => [
          `team:${item.id}`,
          item.name,
        ]),
      ]),
    [
      canViewTeams,
      canViewUsers,
      teamsQuery.data,
      usersQuery.data,
      teamsQuery.error,
      usersQuery.error,
    ],
  )
  const environmentMap = useMemo(
    () =>
      Object.fromEntries(
        (
          (canViewApplicationEnvironments
            ? readableQueryData(applicationEnvironmentsQuery)
            : undefined) ?? []
        ).map((item) => [item.environmentId, item.environmentKey || item.environmentId]),
      ),
    [
      canViewApplicationEnvironments,
      applicationEnvironmentsQuery.data,
      applicationEnvironmentsQuery.error,
    ],
  )
  const grants = useMemo(
    () =>
      fixedApplication
        ? (readableQueryData(grantsQuery) ?? []).filter(
            (grant) =>
              grant.scopeType === 'delivery' && grant.applicationIds?.includes(fixedApplication.id),
          )
        : (readableQueryData(grantsQuery) ?? []),
    [fixedApplication, grantsQuery.data, grantsQuery.error],
  )

  const writing = useRef(false)
  const createMutation = useMutation({
    ...accessMutations.scopeGrants.create(),
    onSuccess: async () => {
      message.success('授权项创建成功')
      await invalidateAccessScopeGrants(queryClient)
      setGrantModalVisible(false)
    },
    onSettled: () => {
      writing.current = false
    },
  })
  const updateMutation = useMutation({
    ...accessMutations.scopeGrants.update(),
    onSuccess: async () => {
      message.success('授权项更新成功')
      await invalidateAccessScopeGrants(queryClient)
      setEditing(null)
      setGrantModalVisible(false)
    },
    onSettled: () => {
      writing.current = false
    },
  })
  const deleteMutation = useMutation({
    ...accessMutations.scopeGrants.delete(),
    onSuccess: async () => {
      message.success('授权项已删除')
      await invalidateAccessScopeGrants(queryClient)
    },
    onSettled: () => {
      writing.current = false
    },
  })

  const blocked = [createMutation, updateMutation, deleteMutation].some(
    (mutation) => mutation.isPending || writeOutcomeUnknown(mutation.error),
  )
  const saveError =
    createMutation.submittedAt > updateMutation.submittedAt
      ? createMutation.error
      : updateMutation.error
  const feedback = (
    <MutationFeedback
      operations={[
        {
          mutation: createMutation,
          label: '创建授权项',
          target: createMutation.variables?.subjectId,
          receipt: { message: '授权项已创建' },
        },
        {
          mutation: updateMutation,
          label: '更新授权项',
          target: updateMutation.variables?.id,
          receipt: { message: '授权项已更新' },
        },
        {
          mutation: deleteMutation,
          label: '删除授权项',
          target: deleteMutation.variables?.id,
          receipt: { message: '授权项已删除' },
        },
      ]}
    />
  )
  const openEditor = (record: AccessScopeGrant | null) => {
    if (writing.current) return
    if (!blocked) {
      createMutation.reset()
      updateMutation.reset()
      setEditing(record)
    }
    setGrantModalVisible(true)
  }
  const submitGrant = (payload: Record<string, unknown>) => {
    const targetSubject = editing
      ? { subjectId: editing.subjectId, subjectType: editing.subjectType }
      : listsApplicationGrants
        ? {
            subjectId: String(payload.subjectId ?? ''),
            subjectType: payload.subjectType as 'team' | 'user',
          }
        : subject
    if (
      !targetSubject.subjectId ||
      writing.current ||
      blocked ||
      grantsQuery.isError ||
      !canViewScopeGrants
    )
      return
    writing.current = true
    if (editing) {
      updateMutation.mutate({ ...targetSubject, id: editing.id, values: payload })
      return
    }
    createMutation.mutate({ ...targetSubject, values: payload })
  }

  const actionColumn: ColumnProps<AccessScopeGrant> = {
    ...tableColumnPresets.action,
    title: '操作',
    dataIndex: 'id',
    render: (_: unknown, record: AccessScopeGrant) => {
      const targetSubject = listsApplicationGrants
        ? { subjectId: record.subjectId, subjectType: record.subjectType }
        : subject
      return (
        <Space className="soha-row-action-icons">
          {canUpdateScopeGrants || canDeleteScopeGrants ? (
            <>
              {canUpdateScopeGrants ? (
                <ManagementIconButton
                  aria-label="编辑授权项"
                  icon={<EditOutlined />}
                  size="small"
                  tooltip="编辑"
                  onClick={() => openEditor(record)}
                />
              ) : null}
              {canDeleteScopeGrants ? (
                <Popconfirm
                  title={`确认删除授权项 ${record.id}？`}
                  onConfirm={() => {
                    if (writing.current || blocked || grantsQuery.isError) return
                    writing.current = true
                    deleteMutation.mutate({ ...targetSubject, id: record.id })
                  }}
                >
                  <ManagementIconButton
                    aria-label="删除授权项"
                    disabled={blocked || grantsQuery.isError}
                    danger
                    icon={<DeleteOutlined />}
                    size="small"
                    tooltip="删除"
                  />
                </Popconfirm>
              ) : null}
            </>
          ) : (
            '-'
          )}
        </Space>
      )
    },
  }
  const columns: ColumnProps<AccessScopeGrant>[] = [
    ...(listsApplicationGrants
      ? [
          {
            title: '授权主体',
            key: 'subject',
            render: (_: unknown, record: AccessScopeGrant) => (
              <Space wrap size={6}>
                <Tooltip title={record.subjectId}>
                  <span>
                    {subjectMap[`${record.subjectType}:${record.subjectId}`] || record.subjectId}
                  </span>
                </Tooltip>
                <MetadataTag
                  label={record.subjectType === 'team' ? '组织' : '用户'}
                  tone={record.subjectType === 'team' ? 'purple' : 'blue'}
                />
              </Space>
            ),
          },
          {
            title: '环境范围',
            dataIndex: 'environmentIds',
            render: (values: string[] | undefined) =>
              values?.length ? (
                <Space wrap size={[0, 4]}>
                  {values.map((item) => (
                    <MetadataTag key={item} label={environmentMap[item] || item} />
                  ))}
                </Space>
              ) : (
                <MetadataTag label="应用默认" tone="blue" />
              ),
          },
        ]
      : [
          {
            title: '范围类型',
            dataIndex: 'scopeType',
            render: (value: string) => (value === 'platform' ? '平台范围' : '交付范围'),
          },
          {
            title: '资源范围',
            key: 'resourceScope',
            render: (_: unknown, record: AccessScopeGrant) =>
              record.scopeType === 'platform'
                ? record.clusterIds?.map((item) => <Tag key={item}>{clusterMap[item] || item}</Tag>)
                : record.businessLineId || '-',
          },
          {
            title: '环境',
            dataIndex: 'environmentIds',
            render: (values: string[] | undefined, record: AccessScopeGrant) =>
              record.scopeType === 'platform'
                ? '-'
                : values?.length
                  ? values.map((item) => <Tag key={item}>{item}</Tag>)
                  : '全部',
          },
          {
            title: '应用',
            dataIndex: 'applicationIds',
            render: (values: string[] | undefined, record: AccessScopeGrant) =>
              record.scopeType === 'platform'
                ? '-'
                : values?.length
                  ? values.map((item) => <Tag key={item}>{applicationMap[item] || item}</Tag>)
                  : '全部',
          },
        ]),
    { title: '角色', dataIndex: 'role' },
    {
      title: '效果',
      dataIndex: 'effect',
      render: (value: AccessScopeGrant['effect']) => (
        <StatusTag value={value} label={value === 'deny' ? '拒绝' : '允许'} />
      ),
    },
    {
      title: '状态',
      dataIndex: 'enabled',
      render: (value: boolean) => <StatusTag value={value ? 'enabled' : 'disabled'} />,
    },
    actionColumn,
  ]

  const table = (
    <>
      <ReadFeedback query={applicationsQuery} label="应用目录" />
      <ReadFeedback query={clustersQuery} label="集群目录" />
      <ReadFeedback query={usersQuery} label="用户目录" />
      <ReadFeedback query={teamsQuery} label="组织目录" />
      <ReadFeedback query={applicationEnvironmentsQuery} label="环境目录" />
      <AdminTable
        columnSettingIconOnly
        columnSettingPlacement="header"
        shellClassName="soha-management-table-shell"
        title={inline ? title : '授权项'}
        headerExtra={
          <ManagementTableToolbar>
            <ManagementRefreshButton
              aria-label="刷新授权记录"
              tooltip="刷新授权记录"
              loading={grantsQuery.isFetching}
              onClick={() => void grantsQuery.refetch()}
            />
            {canCreateScopeGrants ? (
              <Tooltip title={canUseEditor ? undefined : '需要主体、角色及应用环境目录查看权限'}>
                <span>
                  <Button
                    disabled={!canUseEditor}
                    icon={<PlusOutlined />}
                    type="primary"
                    onClick={() => openEditor(null)}
                  >
                    {inline ? '新增授权' : '新建授权项'}
                  </Button>
                </span>
              </Tooltip>
            ) : null}
          </ManagementTableToolbar>
        }
        columns={columns}
        dataSource={grants}
        error={grantsQuery.error}
        hasReadData={grantsQuery.data !== undefined}
        onRetry={() => void grantsQuery.refetch()}
        empty={
          <ManagementState
            bordered={false}
            compact
            title={inline ? '暂无额外授权' : '未设置额外范围'}
            description="未设置时仅使用角色与访问策略。"
          />
        }
        rowKey="id"
        loading={grantsQuery.isLoading}
        localSorting
        scroll={{ x: 'max-content' }}
      />
    </>
  )

  if (!canViewScopeGrants && (visible || inline))
    return <ManagementState kind="no-permission" description="当前账号没有查看授权范围的权限。" />

  return (
    <>
      {inline ? (
        <>
          {feedback}
          {blocked ? (
            <Button size="small" onClick={() => void grantsQuery.refetch()}>
              重读授权记录（不会重发操作）
            </Button>
          ) : null}
          {table}
        </>
      ) : (
        <Modal
          title={title}
          open={visible}
          onCancel={() => {
            if (!writing.current) onClose?.()
          }}
          footer={null}
          width={880}
        >
          <div className="soha-page">
            {feedback}
            {blocked ? (
              <Button size="small" onClick={() => void grantsQuery.refetch()}>
                重读授权记录（不会重发操作）
              </Button>
            ) : null}
            {table}
          </div>
        </Modal>
      )}
      <ScopeGrantEditor
        editing={editing}
        fixedApplication={fixedApplication}
        open={
          visible &&
          grantModalVisible &&
          canViewScopeGrants &&
          readableQueryData(grantsQuery) !== undefined
        }
        fixedSubject={
          editing
            ? { id: editing.subjectId, type: editing.subjectType }
            : !listsApplicationGrants && subjectId
              ? { id: subjectId, type: subjectType }
              : undefined
        }
        onCancel={() => {
          if (writing.current) return
          setGrantModalVisible(false)
          if (!blocked) setEditing(null)
        }}
        error={saveError}
        blocked={blocked}
        readBlocked={grantsQuery.isError}
        readFeedback={<ReadFeedback query={grantsQuery} label="授权记录" />}
        onCheck={() => void grantsQuery.refetch()}
        onSubmit={submitGrant}
        pending={createMutation.isPending || updateMutation.isPending}
      />
    </>
  )
}
