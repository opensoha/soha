import { MutationFeedback, ReadFeedback } from '@/components/request-feedback'
import { readableQueryData, writeOutcomeUnknown } from '@/utils/query-feedback'
import { useMemo, useState } from 'react'
import { Button, Col, Form, Input, Modal, Popconfirm, Row, Select, Space, Typography } from 'antd'
import { MetadataTag } from '@/components/status-tag'
import { TableCellText } from '@/components/table-cell-content'
import { DeleteOutlined, EditOutlined, FolderOpenOutlined, PlusOutlined } from '@ant-design/icons'
import type { TableColumnsType } from 'antd'
import { useQuery } from '@tanstack/react-query'
import {
  ManagementIconButton,
  ManagementState,
  useManagementTextFilter,
} from '@/components/management-list'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { tableColumnPresets } from '@/utils/table-columns'
import { AccessManagementTablePage } from '../shared/management-page'
import { accessMutations, invalidateAccessTeams } from '../shared/mutations'
import { accessQueries } from '../shared/queries'
import { ScopeGrantManager } from '../shared/scope-grant-manager'
import type { AccessTeam } from '../shared/types'
import { useAccessResourceCrud } from '../shared/use-resource-crud'
import { AccessMutationFooter, accessMutationModalStyles } from '../shared/mutation-footer'
import {
  collectOrganizationDescendantIds,
  getGroupDescription,
  getOrganizationDisplayPath,
  getOrganizationLabel,
} from '../shared/utils'
import {
  buildOrganizationSourceLabelMap,
  buildOrganizationSourceOptions,
  organizationSourceLabel,
} from './provider-options'
import '../shared/styles.css'

const { Text } = Typography
type ColumnProps<T> = TableColumnsType<T>[number]
const directoryProviderLabels: Record<string, string> = {
  feishu: '飞书',
  wecom: '企业微信',
  dingtalk: '钉钉',
  ldap: 'LDAP',
  scim: 'SCIM',
}

export function AccessTeamsPage() {
  const permissionSnapshotQuery = usePermissionSnapshot()
  const snapshot = permissionSnapshotQuery.data?.data
  const canViewGroups = hasPermission(snapshot, 'access.groups.view')
  const canCreateGroups = hasPermission(snapshot, 'access.groups.create')
  const canUpdateGroups = hasPermission(snapshot, 'access.groups.update')
  const canDeleteGroups = hasPermission(snapshot, 'access.groups.delete')
  const canViewScopeGrants = hasPermission(snapshot, 'access.scope-grants.view')
  const canViewLoginSettings = hasPermission(snapshot, 'settings.identity.view')
  const [form] = Form.useForm<Record<string, unknown>>()
  const crud = useAccessResourceCrud({
    query: accessQueries.teams(canViewGroups),
    create: accessMutations.teams.create(),
    update: accessMutations.teams.update(),
    delete: accessMutations.teams.delete(),
    invalidate: invalidateAccessTeams,
  })
  const [grantTeam, setGrantTeam] = useState<AccessTeam | null>(null)
  const [searchKeyword, setSearchKeyword] = useState('')
  const loginProvidersQuery = useQuery(
    accessQueries.loginProviders((canCreateGroups || canUpdateGroups) && canViewLoginSettings),
  )
  const loginProviders =
    (canViewLoginSettings ? readableQueryData(loginProvidersQuery) : undefined) ?? []
  const organizationSourceOptions = useMemo(
    () => buildOrganizationSourceOptions(loginProviders),
    [loginProviders],
  )
  const organizationSourceLabelMap = useMemo(
    () => buildOrganizationSourceLabelMap(loginProviders),
    [loginProviders],
  )
  const blockedOrganizationIds = new Set(
    crud.editing
      ? [crud.editing.id, ...collectOrganizationDescendantIds(crud.data, crud.editing.id)]
      : [],
  )
  const parentOrganizationOptions = [
    { value: '', label: '根组织' },
    ...crud.data
      .filter((item) => !blockedOrganizationIds.has(item.id))
      .map((item) => ({
        value: item.id,
        label: `${getOrganizationLabel(item)} (${getOrganizationDisplayPath(crud.data, item)})`,
      })),
  ]
  const columns: ColumnProps<AccessTeam>[] = [
    {
      title: '组织名称',
      dataIndex: 'name',
      render: (value: string) => <Text strong>{value}</Text>,
    },
    {
      title: '上级组织',
      dataIndex: 'parentId',
      render: (value: string) =>
        value ? getOrganizationLabel(crud.data.find((item) => item.id === value)) : '根组织',
    },
    {
      title: '组织路径',
      key: 'displayPath',
      render: (_value: unknown, record: AccessTeam) => (
        <TableCellText value={getOrganizationDisplayPath(crud.data, record)} />
      ),
    },
    {
      title: '映射来源',
      dataIndex: 'source',
      render: (value: string, record: AccessTeam) => {
        const providerType = String(record.metadata?.directoryProviderType ?? '').trim()
        const sourceLabel = providerType
          ? (directoryProviderLabels[providerType] ?? providerType)
          : organizationSourceLabel(value, organizationSourceLabelMap)
        const directoryName = String(record.metadata?.directoryConnectionName ?? '').trim()
        const technicalDetails = [directoryName, value, record.externalId]
          .filter(Boolean)
          .join(' · ')
        return <MetadataTag tone="blue" title={technicalDetails} label={sourceLabel} />
      },
    },
    {
      title: '说明',
      dataIndex: 'metadata',
      render: (value: Record<string, unknown>) => getGroupDescription(value) || '-',
    },
    { title: '成员数', dataIndex: 'userCount' },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: unknown, record: AccessTeam) => (
        <Space className="soha-row-action-icons">
          {canUpdateGroups || canDeleteGroups || canViewScopeGrants ? (
            <>
              {canViewScopeGrants ? (
                <ManagementIconButton
                  aria-label="授权范围"
                  icon={<FolderOpenOutlined />}
                  size="small"
                  tooltip="授权范围"
                  onClick={() => setGrantTeam(record)}
                />
              ) : null}
              {canUpdateGroups ? (
                <ManagementIconButton
                  aria-label="编辑组织"
                  disabled={crud.saveBlocked && crud.editing?.id !== record.id}
                  icon={<EditOutlined />}
                  size="small"
                  tooltip="编辑"
                  onClick={() => crud.openEdit(record)}
                />
              ) : null}
              {canDeleteGroups ? (
                <Popconfirm
                  title={`确认删除 ${record.name}？`}
                  onConfirm={() => crud.deleteRecord(record.id)}
                >
                  <ManagementIconButton
                    aria-label="删除组织"
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
  const filteredTeams = useManagementTextFilter(crud.data, searchKeyword, (item) => [
    item.name,
    item.slug,
    item.path,
    item.source,
    item.externalId,
    getGroupDescription(item.metadata),
  ])

  if (!canViewGroups) {
    return (
      <div className="soha-page">
        <ManagementState kind="no-permission" description="当前账号没有组织管理权限。" />
      </div>
    )
  }

  return (
    <AccessManagementTablePage<AccessTeam>
      resourceName="组织"
      columns={columns}
      createAction={
        canCreateGroups ? (
          <Button
            aria-label={crud.saveBlocked ? '查看待核对草稿' : '添加组织'}
            disabled={readableQueryData(crud.query) === undefined}
            size="small"
            icon={<PlusOutlined />}
            type="primary"
            onClick={crud.openCreate}
          >
            {crud.saveBlocked ? '查看待核对草稿' : '添加组织'}
          </Button>
        ) : null
      }
      error={crud.query.error}
      hasReadData={crud.query.data !== undefined}
      dataSource={filteredTeams}
      rowKey="id"
      loading={crud.isLoading}
      refreshing={crud.isFetching}
      onRefresh={() => void crud.refetch()}
      placeholder="搜索组织、路径或来源"
      searchKeyword={searchKeyword}
      setSearchKeyword={setSearchKeyword}
    >
      <ReadFeedback query={loginProvidersQuery} label="组织映射来源" />
      <MutationFeedback
        operations={[
          {
            mutation: crud.createMutation,
            label: '创建组织',
            target: String(crud.createMutation.variables?.name ?? ''),
            receipt: { message: '组织已创建' },
          },
          {
            mutation: crud.updateMutation,
            label: '更新组织',
            target: String(
              crud.updateMutation.variables?.values.name ?? crud.updateMutation.variables?.id ?? '',
            ),
            receipt: { message: '组织已更新' },
          },
          {
            mutation: crud.deleteMutation,
            label: '删除组织',
            target: crud.deleteMutation.variables,
            receipt: { message: '组织已删除' },
          },
        ]}
      />
      {writeOutcomeUnknown(crud.deleteMutation.error) ? (
        <Button size="small" onClick={() => void crud.refetch()}>
          重读组织记录（不会重发删除）
        </Button>
      ) : null}
      <Modal
        title={crud.editing ? `编辑组织: ${crud.editing.name}` : '添加组织'}
        open={crud.modalVisible && readableQueryData(crud.query) !== undefined}
        onCancel={crud.closeModal}
        onOk={async () => {
          try {
            const values = await form.validateFields()
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
          } catch {
            return
          }
        }}
        okText={crud.editing ? '更新' : '创建'}
        cancelText="取消"
        confirmLoading={crud.isSaving}
        okButtonProps={{ disabled: crud.saveBlocked || crud.query.isError }}
        cancelButtonProps={{ disabled: crud.isSaving, 'aria-label': '取消' }}
        closable={{ disabled: crud.isSaving }}
        keyboard={!crud.isSaving}
        styles={accessMutationModalStyles}
        style={{ top: 32, marginTop: 0 }}
        footer={(buttons) => (
          <AccessMutationFooter
            error={crud.saveError}
            saving={crud.isSaving}
            target={crud.editing?.name ?? String(crud.createMutation.variables?.name ?? '')}
            onCheck={() => void crud.refetch()}
          >
            {buttons}
          </AccessMutationFooter>
        )}
        width={720}
        destroyOnHidden={!crud.saveBlocked}
        mask={{ closable: false }}
      >
        <ReadFeedback query={crud.query} label="组织目录" />
        <ReadFeedback query={loginProvidersQuery} label="组织映射来源" />
        <Form
          form={form}
          disabled={crud.isSaving || crud.saveBlocked || crud.query.isError}
          key={crud.editing?.id ?? 'create-group'}
          layout="vertical"
          initialValues={
            crud.editing
              ? {
                  name: crud.editing.name,
                  slug: crud.editing.slug,
                  parentId: crud.editing.parentId ?? '',
                  source: crud.editing.source || 'local',
                  externalId: crud.editing.externalId ?? '',
                  description: getGroupDescription(crud.editing.metadata),
                }
              : { parentId: '', source: 'local' }
          }
        >
          <Form.Item name="parentId" label="上级组织">
            <Select
              allowClear
              options={parentOrganizationOptions}
              showSearch={{ optionFilterProp: 'label' }}
            />
          </Form.Item>
          <Form.Item
            name="name"
            label="组织名称"
            rules={[{ required: true, message: '请输入组织名称' }]}
          >
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
                  options={organizationSourceOptions}
                  showSearch={{ optionFilterProp: 'label' }}
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
        visible={Boolean(grantTeam)}
        title={grantTeam ? `组织授权范围: ${grantTeam.name}` : '组织授权范围'}
        onClose={() => setGrantTeam(null)}
      />
    </AccessManagementTablePage>
  )
}
