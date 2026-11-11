import { MutationFeedback, OperationFeedback, ReadFeedback } from '@/components/request-feedback'
import { readableQueryData, writeOutcomeUnknown } from '@/utils/query-feedback'
import { useAccessResourceCrud } from '../shared/use-resource-crud'
import { AccessMutationFooter, accessMutationModalStyles } from '../shared/mutation-footer'
import { useMemo, useRef, useState } from 'react'
import type { CSSProperties } from 'react'
import {
  App,
  Avatar,
  Button,
  Col,
  Form,
  Input,
  Modal,
  Popconfirm,
  Row,
  Select,
  Space,
  Switch,
  Tree,
  Typography,
} from 'antd'
import {
  ApartmentOutlined,
  DeleteOutlined,
  EditOutlined,
  FolderOpenOutlined,
  PlusOutlined,
  SafetyCertificateOutlined,
} from '@ant-design/icons'
import type { TableColumnsType } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AdminTable } from '@/components/admin-table'
import {
  ManagementIconButton,
  ManagementDensityButton,
  ManagementKeywordField,
  ManagementQueryField,
  ManagementQueryPanel,
  ManagementState,
  ManagementRefreshButton,
  ManagementTableToolbar,
} from '@/components/management-list'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { renderCompactMappedTags } from '../shared/compact-mapped-tags'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { formatDateTime } from '@/utils/time'
import { loginProviderLabel, loginProviderTagColor } from '@/utils/login-provider'
import { tableColumnPresets } from '@/utils/table-columns'
import { accessMutations, invalidateAccessUsers } from '../shared/mutations'
import { accessQueries } from '../shared/queries'
import { ScopeGrantManager } from '../shared/scope-grant-manager'
import type { AccessUser } from '../shared/types'
import {
  collectOrganizationDescendantIds,
  getUserInitial,
  getUserLabel,
  toStringArray,
} from '../shared/utils'
import {
  buildOrganizationTree,
  buildOrganizationUserCounts,
  ORG_ALL_KEY,
  organizationMatchesSelection,
} from './view-model'
import '../shared/styles.css'

const { Text } = Typography
type ColumnProps<T> = TableColumnsType<T>[number]
const USER_STATUS_OPTIONS = [
  { value: 'active', label: '启用' },
  { value: 'disabled', label: '停用' },
]

function avatarFit(value?: string) {
  return value === 'contain' || value === 'fill' ? value : 'cover'
}

export function AccessUsersPage() {
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const snapshot = permissionSnapshotQuery.data?.data
  const canViewUsers = hasPermission(snapshot, 'access.users.view')
  const canViewRoles = hasPermission(snapshot, 'access.roles.view')
  const canViewTeams = hasPermission(snapshot, 'access.groups.view')
  const canCreateUsers = hasPermission(snapshot, 'access.users.create')
  const canUpdateUsers = hasPermission(snapshot, 'access.users.update')
  const canDeleteUsers = hasPermission(snapshot, 'access.users.delete')
  const canReplaceUserRoles = hasPermission(snapshot, 'access.users.replace-roles')
  const canReplaceUserTeams = hasPermission(snapshot, 'access.users.replace-teams')
  const canViewScopeGrants = hasPermission(snapshot, 'access.scope-grants.view')
  const [form] = Form.useForm<Record<string, unknown>>()
  const [grantUser, setGrantUser] = useState<AccessUser | null>(null)
  const [searchText, setSearchText] = useState('')
  const [selectedOrgId, setSelectedOrgId] = useState(ORG_ALL_KEY)
  const [includeSubOrganizations, setIncludeSubOrganizations] = useState(true)
  const [tableSize, setTableSize] = useState<'small' | 'middle'>('small')
  const crud = useAccessResourceCrud({
    query: accessQueries.users(canViewUsers),
    create: accessMutations.users.create(),
    update: accessMutations.users.update(),
    delete: accessMutations.users.delete(),
    invalidate: invalidateAccessUsers,
  })
  const { modalVisible, editing, createMutation, updateMutation, deleteMutation } = crud
  const usersQuery = crud.query
  const resettingMFA = useRef(false)
  const rolesQuery = useQuery(accessQueries.roles(canViewRoles))
  const teamsQuery = useQuery(accessQueries.teams(canViewTeams))

  const roleMap = useMemo(
    () =>
      Object.fromEntries(
        ((canViewRoles ? readableQueryData(rolesQuery) : undefined) ?? []).map((item) => [
          item.id,
          item.name,
        ]),
      ),
    [canViewRoles, rolesQuery.data, rolesQuery.error],
  )
  const teamMap = useMemo(
    () =>
      Object.fromEntries(
        ((canViewTeams ? readableQueryData(teamsQuery) : undefined) ?? []).map((item) => [
          item.id,
          item.name,
        ]),
      ),
    [canViewTeams, teamsQuery.data, teamsQuery.error],
  )
  const roleOptions = useMemo(
    () =>
      ((canViewRoles ? readableQueryData(rolesQuery) : undefined) ?? []).map((item) => ({
        value: item.id,
        label: item.name,
      })),
    [canViewRoles, rolesQuery.data, rolesQuery.error],
  )
  const teamOptions = useMemo(
    () =>
      ((canViewTeams ? readableQueryData(teamsQuery) : undefined) ?? []).map((item) => ({
        value: item.id,
        label: item.path ? `${item.name} (${item.path})` : item.name,
      })),
    [canViewTeams, teamsQuery.data, teamsQuery.error],
  )
  const userCountByOrg = useMemo(() => {
    return buildOrganizationUserCounts(
      (canViewTeams ? readableQueryData(teamsQuery) : undefined) ?? [],
      readableQueryData(usersQuery) ?? [],
    )
  }, [canViewTeams, teamsQuery.data, usersQuery.data, teamsQuery.error, usersQuery.error])
  const organizationTreeData = useMemo(
    () =>
      buildOrganizationTree(
        (canViewTeams ? readableQueryData(teamsQuery) : undefined) ?? [],
        userCountByOrg,
      ),
    [canViewTeams, teamsQuery.data, userCountByOrg, teamsQuery.error],
  )
  const scopedOrganizationIds = useMemo(() => {
    if (selectedOrgId === ORG_ALL_KEY) return new Set<string>()
    const ids = new Set<string>([selectedOrgId])
    if (includeSubOrganizations) {
      collectOrganizationDescendantIds(
        (canViewTeams ? readableQueryData(teamsQuery) : undefined) ?? [],
        selectedOrgId,
      ).forEach((id) => ids.add(id))
    }
    return ids
  }, [canViewTeams, includeSubOrganizations, selectedOrgId, teamsQuery.data, teamsQuery.error])
  const filteredUsers = useMemo(() => {
    const query = searchText.trim().toLowerCase()
    return (readableQueryData(usersQuery) ?? []).filter((item) => {
      if (!organizationMatchesSelection(item, selectedOrgId, scopedOrganizationIds)) return false
      if (!query) return true
      return [
        item.username,
        item.displayName,
        item.email,
        ...(item.roles ?? []),
        ...(item.teams ?? []).map((teamID) => teamMap[teamID] || teamID),
        ...(item.loginSources ?? []).flatMap((source) => [
          source.type,
          source.providerId,
          loginProviderLabel(source.type),
        ]),
      ]
        .filter(Boolean)
        .some((value) => String(value).toLowerCase().includes(query))
    })
  }, [scopedOrganizationIds, searchText, selectedOrgId, teamMap, usersQuery.data, usersQuery.error])

  const resetMFAMutation = useMutation({
    ...accessMutations.users.resetMFA(),
    onSuccess: async (result) => {
      message.success(
        `MFA 已重置，撤销 ${result.revokedCredentialCount} 个凭据和 ${result.sessionsRevoked} 个会话`,
      )
      await invalidateAccessUsers(queryClient)
    },
    onSettled: () => {
      resettingMFA.current = false
    },
  })

  const closeModal = crud.closeModal
  const submitUser = (values: Record<string, unknown>) => {
    const payload = {
      username: String(values.username ?? '').trim(),
      displayName: String(values.displayName ?? '').trim(),
      email: String(values.email ?? '').trim(),
      status: String(values.status ?? 'active'),
      password: String(values.password ?? ''),
      roleIds: canReplaceUserRoles ? toStringArray(values.roleIds) : undefined,
      teamIds: canReplaceUserTeams ? toStringArray(values.teamIds) : undefined,
      tags: toStringArray(values.tags),
    }
    if (
      (canReplaceUserRoles && canViewRoles && (rolesQuery.isPending || rolesQuery.isError)) ||
      (canReplaceUserTeams && canViewTeams && (teamsQuery.isPending || teamsQuery.isError))
    )
      return
    crud.handleSubmit(payload)
  }

  const columns: ColumnProps<AccessUser>[] = [
    {
      title: '头像',
      key: 'avatar',
      width: 76,
      render: (_: unknown, record: AccessUser) => (
        <Avatar
          alt={getUserLabel(record)}
          className="soha-user-avatar"
          size="small"
          src={record.avatarUrl || undefined}
          style={{ '--soha-avatar-fit': avatarFit(record.avatarFit) } as CSSProperties}
        >
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
      title: '登录源',
      dataIndex: 'loginSources',
      width: 140,
      render: (sources: AccessUser['loginSources']) =>
        sources?.length ? (
          <Space size={[4, 4]} wrap>
            {sources.map((source) => (
              <MetadataTag
                key={`${source.type}:${source.providerId || ''}`}
                tone={loginProviderTagColor(source.type)}
                label={loginProviderLabel(source.type)}
              />
            ))}
          </Space>
        ) : (
          <Text type="secondary">-</Text>
        ),
    },
    {
      title: '角色',
      dataIndex: 'roles',
      width: 180,
      render: (roles: string[]) =>
        renderCompactMappedTags(roles, roleMap, '未绑定', 2, '角色', 'purple'),
    },
    {
      title: '组织',
      dataIndex: 'teams',
      width: 160,
      render: (teams: string[]) =>
        renderCompactMappedTags(teams, teamMap, '未绑定', 2, '组织', 'cyan'),
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
      render: (value?: string) => (value ? formatDateTime(value) : '-'),
    },
    {
      ...tableColumnPresets.action,
      title: '操作',
      width: 204,
      dataIndex: 'id',
      render: (_: unknown, record: AccessUser) => (
        <Space className="soha-row-action-icons">
          {canUpdateUsers || canDeleteUsers || canViewScopeGrants ? (
            <>
              {canViewScopeGrants ? (
                <ManagementIconButton
                  aria-label="授权范围"
                  icon={<FolderOpenOutlined />}
                  size="small"
                  tooltip="授权范围"
                  onClick={() => setGrantUser(record)}
                />
              ) : null}
              {canUpdateUsers ? (
                <ManagementIconButton
                  aria-label="编辑用户"
                  disabled={crud.saveBlocked && editing?.id !== record.id}
                  icon={<EditOutlined />}
                  size="small"
                  tooltip="编辑"
                  onClick={() => crud.openEdit(record)}
                />
              ) : null}
              {canUpdateUsers ? (
                <Popconfirm
                  title={`确认重置 ${record.username} 的 MFA？`}
                  description="所有 MFA 凭据和恢复码将失效，并撤销现有会话。"
                  okButtonProps={{ 'aria-label': '确认重置 MFA' }}
                  onConfirm={() => {
                    if (resettingMFA.current || writeOutcomeUnknown(resetMFAMutation.error)) return
                    resettingMFA.current = true
                    resetMFAMutation.mutate({
                      id: record.id,
                      input: {
                        reason: 'Reset by administrator from user management',
                        revokeSessions: true,
                      },
                    })
                  }}
                >
                  <ManagementIconButton
                    aria-label="重置 MFA"
                    disabled={
                      resetMFAMutation.isPending || writeOutcomeUnknown(resetMFAMutation.error)
                    }
                    danger
                    icon={<SafetyCertificateOutlined />}
                    loading={resetMFAMutation.isPending}
                    size="small"
                    tooltip="重置 MFA"
                  />
                </Popconfirm>
              ) : null}
              {canDeleteUsers ? (
                <Popconfirm
                  title={`确认删除用户 ${record.username}？`}
                  onConfirm={() => crud.deleteRecord(record.id)}
                >
                  <ManagementIconButton
                    aria-label="删除用户"
                    disabled={crud.deleteBlocked}
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
      ),
    },
  ]

  if (!canViewUsers) {
    return (
      <div className="soha-page">
        <ManagementState kind="no-permission" description="当前账号没有用户管理权限。" />
      </div>
    )
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
            <MetadataTag label={readableQueryData(usersQuery)?.length ?? '—'} />
          </div>
          <ReadFeedback query={teamsQuery} label="组织目录" />
          {readableQueryData(usersQuery) !== undefined ? (
            <Tree
              blockNode
              defaultExpandedKeys={[ORG_ALL_KEY]}
              selectedKeys={[selectedOrgId]}
              treeData={organizationTreeData}
              onSelect={(keys) => setSelectedOrgId(String(keys[0] ?? ORG_ALL_KEY))}
            />
          ) : (
            <Text type="secondary">用户列表读取后展示组织人数</Text>
          )}
        </section>
        <section className="soha-access-users-panel">
          <ManagementQueryPanel
            onFinish={() => undefined}
            actions={
              <>
                <Button
                  autoInsertSpace={false}
                  disabled={!searchText.trim() && includeSubOrganizations}
                  htmlType="button"
                  onClick={() => {
                    setSearchText('')
                    setIncludeSubOrganizations(true)
                  }}
                >
                  重置
                </Button>
                <Button autoInsertSpace={false} htmlType="submit" type="primary">
                  查询
                </Button>
              </>
            }
          >
            <ManagementKeywordField
              label="关键词"
              placeholder="搜索用户名、显示名、邮箱、角色或组织"
              value={searchText}
              inputProps={{
                className: 'soha-platform-compact-field soha-workload-search-input',
                size: 'small',
              }}
              onChange={setSearchText}
            />
            {selectedOrgId !== ORG_ALL_KEY ? (
              <ManagementQueryField label="下级组织" width={184} minWidth={160}>
                <Switch
                  size="small"
                  checked={includeSubOrganizations}
                  checkedChildren="包含"
                  unCheckedChildren="仅当前"
                  onChange={setIncludeSubOrganizations}
                />
              </ManagementQueryField>
            ) : null}
          </ManagementQueryPanel>
          <ReadFeedback query={rolesQuery} label="角色目录" />
          <MutationFeedback
            operations={[
              {
                mutation: createMutation,
                label: '创建用户',
                target: String(createMutation.variables?.username ?? ''),
                receipt: { message: '用户已创建' },
              },
              {
                mutation: updateMutation,
                label: '更新用户',
                target: updateMutation.variables?.id,
                receipt: { message: '用户已更新' },
              },
              {
                mutation: deleteMutation,
                label: '删除用户',
                target: deleteMutation.variables,
                receipt: { message: '用户已删除' },
              },
            ]}
          />
          <OperationFeedback
            label="重置 MFA"
            target={resetMFAMutation.variables?.id}
            pending={resetMFAMutation.isPending}
            error={resetMFAMutation.error}
            receipt={
              resetMFAMutation.isSuccess
                ? {
                    message: `MFA 已重置，撤销 ${resetMFAMutation.data?.revokedCredentialCount ?? 0} 个凭据和 ${resetMFAMutation.data?.sessionsRevoked ?? 0} 个会话`,
                  }
                : undefined
            }
          />
          {writeOutcomeUnknown(resetMFAMutation.error) ? (
            <Text type="secondary">
              用户列表不包含 MFA 重置结果。请使用请求 ID 核对审计记录，确认前暂停再次重置。
            </Text>
          ) : null}
          {writeOutcomeUnknown(deleteMutation.error) ? (
            <Button size="small" onClick={() => void usersQuery.refetch()}>
              重读用户记录（不会重发删除）
            </Button>
          ) : null}
          <AdminTable
            error={usersQuery.error}
            hasReadData={usersQuery.data !== undefined}
            onRetry={() => void usersQuery.refetch()}
            columnSettingIconOnly
            columnSettingPlacement="header"
            shellClassName="soha-management-table-shell"
            className="soha-access-table"
            headerExtra={
              <ManagementTableToolbar>
                {canCreateUsers ? (
                  <Button
                    aria-label={crud.saveBlocked ? '查看待核对草稿' : '添加用户'}
                    disabled={readableQueryData(usersQuery) === undefined}
                    size="small"
                    icon={<PlusOutlined />}
                    type="primary"
                    onClick={crud.openCreate}
                  >
                    {crud.saveBlocked ? '查看待核对草稿' : '添加用户'}
                  </Button>
                ) : null}
                <ManagementDensityButton
                  aria-label="切换用户表格密度"
                  size="small"
                  tooltip={tableSize === 'small' ? '切换为宽松密度' : '切换为紧凑密度'}
                  onClick={() =>
                    setTableSize((current) => (current === 'small' ? 'middle' : 'small'))
                  }
                />
                <ManagementRefreshButton
                  aria-label="刷新用户"
                  loading={usersQuery.isFetching || rolesQuery.isFetching || teamsQuery.isFetching}
                  size="small"
                  tooltip="刷新"
                  onClick={() =>
                    void Promise.all([
                      usersQuery.refetch(),
                      rolesQuery.refetch(),
                      teamsQuery.refetch(),
                    ])
                  }
                />
              </ManagementTableToolbar>
            }
            columns={columns}
            dataSource={filteredUsers}
            rowKey="id"
            loading={usersQuery.isLoading || teamsQuery.isLoading}
            scroll={{ x: 'max-content' }}
            tableSize={tableSize}
          />
        </section>
      </div>
      <Modal
        title={editing ? `编辑用户: ${getUserLabel(editing)}` : '添加用户'}
        open={modalVisible && readableQueryData(usersQuery) !== undefined}
        onCancel={closeModal}
        onOk={async () => {
          try {
            submitUser(await form.validateFields())
          } catch {
            return
          }
        }}
        okText={editing ? '更新' : '创建'}
        cancelText="取消"
        confirmLoading={crud.isSaving}
        okButtonProps={{
          'aria-label': editing ? '更新' : '创建',
          disabled:
            crud.saveBlocked ||
            usersQuery.isError ||
            (canReplaceUserRoles && canViewRoles && (rolesQuery.isPending || rolesQuery.isError)) ||
            (canReplaceUserTeams && canViewTeams && (teamsQuery.isPending || teamsQuery.isError)),
        }}
        cancelButtonProps={{ disabled: crud.isSaving, 'aria-label': '取消' }}
        closable={{ disabled: crud.isSaving }}
        keyboard={!crud.isSaving}
        style={{ top: 32, marginTop: 0 }}
        footer={(buttons) => (
          <AccessMutationFooter
            error={crud.saveError}
            saving={crud.isSaving}
            target={editing?.username ?? String(createMutation.variables?.username ?? '')}
            onCheck={() => void usersQuery.refetch()}
          >
            {buttons}
          </AccessMutationFooter>
        )}
        width={860}
        destroyOnHidden={!crud.saveBlocked}
        mask={{ closable: false }}
        styles={accessMutationModalStyles}
      >
        <ReadFeedback query={rolesQuery} label="角色目录" />
        <ReadFeedback query={teamsQuery} label="组织目录" />
        <ReadFeedback query={usersQuery} label="用户列表" />
        <Form
          form={form}
          disabled={crud.isSaving || crud.saveBlocked || usersQuery.isError}
          key={editing?.id ?? 'create-user'}
          layout="vertical"
          initialValues={
            editing
              ? {
                  username: editing.username,
                  displayName: editing.displayName,
                  email: editing.email,
                  status: editing.status || 'active',
                  roleIds: editing.roles ?? [],
                  teamIds: editing.teams ?? [],
                  tags: editing.tags ?? [],
                }
              : { status: 'active', roleIds: [], teamIds: [], tags: [] }
          }
        >
          <Row gutter={16}>
            <Col span={12}>
              <Form.Item
                name="username"
                label="用户名"
                rules={[{ required: true, message: '请输入用户名' }]}
              >
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
              <Form.Item
                name="email"
                label="邮箱"
                rules={[{ required: true, message: '请输入邮箱' }]}
              >
                <Input />
              </Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item
                name="status"
                label="状态"
                rules={[{ required: true, message: '请选择状态' }]}
              >
                <Select options={USER_STATUS_OPTIONS} />
              </Form.Item>
            </Col>
          </Row>
          <Row gutter={16}>
            <Col span={12}>
              <Form.Item name="roleIds" label="角色">
                <Select
                  disabled={!canReplaceUserRoles || rolesQuery.isError || rolesQuery.isPending}
                  mode="multiple"
                  options={roleOptions}
                  placeholder="选择用户角色"
                />
              </Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item name="teamIds" label="所属组织">
                <Select
                  disabled={!canReplaceUserTeams || teamsQuery.isError || teamsQuery.isPending}
                  mode="multiple"
                  options={teamOptions}
                  placeholder="选择所属组织或部门"
                  showSearch={{ optionFilterProp: 'label' }}
                />
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
        visible={Boolean(grantUser)}
        title={grantUser ? `用户授权范围: ${getUserLabel(grantUser)}` : '用户授权范围'}
        onClose={() => setGrantUser(null)}
      />
    </div>
  )
}
