import { AdminTable } from '@/components/admin-table'
import { ManagementDataPage } from '@/components/management-data-page'
import { operationModalStyles } from '@/components/modal-styles'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useI18n } from '@/i18n'
import { isApiError } from '@/services/api-error'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import type {
  NetworkVPNProfileConfig,
  NetworkVPNSelectionPolicyConfig,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, Button, Drawer, Form, Modal, Popconfirm, Space, Typography } from 'antd'
import { useEffect, useState } from 'react'
import { useLocation } from 'react-router-dom'
import { TablePane } from './table-pane'
import { formatDateTime } from '@/utils/time'
import './styles.css'
import {
  deleteVPNDocument,
  publishVPNDocument,
  saveVPNDocument,
  vpnDocuments,
  vpnKeys,
  vpnRevisions,
  type VPNDocument,
  type VPNRevision,
} from './vpn-api'
import {
  VPNNameField,
  VPNPolicyFields,
  VPNProfileFields,
  vpnPolicyDefaults,
  vpnProfileDefaults,
} from './vpn-fields'
export default function VPNDocumentsPage() {
  const { pathname } = useLocation()
  const kind = pathname.endsWith('selection-policies') ? 'selection-policies' : 'profiles'
  const key = kind === 'profiles' ? 'vpn_profiles' : 'vpn_selection_policies'
  const { localeCode } = useI18n()
  const text = (zh: string, en: string) => (localeCode === 'zh_CN' ? zh : en)
  const permissions = usePermissionSnapshot()
  const can = (permission: string) => hasPermission(permissions.data?.data, permission)
  const allowed = (action: string) => can(`network_access.${key}.${action}`)
  const client = useQueryClient()

  const [editing, setEditing] = useState<VPNDocument | null | undefined>()
  const [history, setHistory] = useState<VPNDocument | null>(null)
  const [form] = Form.useForm<NetworkVPNProfileConfig & NetworkVPNSelectionPolicyConfig>()
  const query = useQuery({
    queryKey: vpnKeys.documents(kind),
    queryFn: () => vpnDocuments(kind),
    enabled: allowed('view'),
  })
  const visibleQueryData = query.isEnabled === false ? undefined : readableQueryData(query)
  const revisions = useQuery({
    queryKey: vpnKeys.revisions(kind, history?.id),
    queryFn: () => vpnRevisions(kind, history!.id),
    enabled: !!history && allowed('view'),
  })
  const visibleRevisionsData =
    revisions.isEnabled === false ? undefined : readableQueryData(revisions)
  const mutation = useOperationMutation(
    {
      mutationFn: (work: { target: string; action: string; run: () => Promise<unknown> }) =>
        work.run(),
      onSuccess: () => {
        setEditing(undefined)
        setHistory(null)
        void client.invalidateQueries({ queryKey: vpnKeys.all })
      },
    },
    {
      label: ['VPN 配置操作', 'VPN configuration operation'],
      target: (v) => v.action + ' / ' + v.target,
      receipt: (_, v) => ({
        message: v.action === '保存草稿' ? '草稿已保存，尚未发布' : v.action + '请求已返回',
        evidence: '请核对已发布修订；终端应用结果以运行记录为准。',
      }),
    },
  )
  const operationBlocked =
    mutation.blocked || (query.isEnabled !== false && queryAccessDenied(query))
  useEffect(() => {
    if ([query.error].some((error) => isApiError(error) && [401, 403].includes(error.status))) {
      setEditing(undefined)
      setHistory(null)
      form.resetFields()
    }
  }, [query.error, form])
  const edit = (item: VPNDocument | null) => {
    if (operationBlocked) return
    form.resetFields()
    form.setFieldsValue(
      item?.configuration || (kind === 'profiles' ? vpnProfileDefaults : vpnPolicyDefaults),
    )
    setEditing(item)
  }
  const title =
    kind === 'profiles'
      ? text('VPN 连接方案', 'VPN connection profiles')
      : text('VPN Auto 策略', 'VPN Auto policies')
  return (
    <ManagementDataPage
      className="soha-network-page"
      tableNode={
        <TablePane<VPNDocument>
          items={visibleQueryData}
          loading={query.isPending && allowed('view')}
          error={query.isEnabled === false ? null : query.error}
          refreshing={query.isFetching}
          onRefresh={() => void (query.isEnabled === false ? undefined : query.refetch())}
          searchPlaceholder={text('搜索方案或策略', 'Search profiles or policies')}
          getSearchValues={(d) => [d.configuration.name, d.id]}
          createAction={
            allowed('create') ? (
              <Button type="primary" onClick={() => edit(null)}>
                {text('新建', 'Create')}
              </Button>
            ) : undefined
          }
          columns={[
            {
              title: text('名称', 'Name'),
              key: 'name',
              render: (_, d) => <Typography.Text strong>{d.configuration.name}</Typography.Text>,
            },
            {
              title: text('修订', 'Revision'),
              key: 'revision',
              render: (_, d) => `r${d.revision}`,
            },
            {
              title: text('发布状态', 'Publication'),
              key: 'published',
              render: (_, d) => (
                <StatusTag
                  value={d.publishedRevision === d.revision ? 'published' : 'draft'}
                  label={
                    d.publishedRevision
                      ? `${text('已发布', 'Published')} r${d.publishedRevision}${d.revision > d.publishedRevision ? ` · ${text('有草稿', 'Draft changes')}` : ''}`
                      : text('未发布', 'Unpublished')
                  }
                />
              ),
            },
            {
              title: text('操作', 'Actions'),
              key: 'actions',
              render: (_, d) => (
                <Space>
                  {allowed('update') ? (
                    <Button size="small" onClick={() => edit(d)}>
                      {text('编辑', 'Edit')}
                    </Button>
                  ) : null}
                  <Button size="small" onClick={() => setHistory(d)}>
                    {text('修订与回滚', 'Revisions')}
                  </Button>
                  {allowed('publish') ? (
                    <Popconfirm
                      title={text(
                        '发布此修订？安全范围变更将撤销相关会话。',
                        'Publish this revision? Security changes revoke affected sessions.',
                      )}
                      onConfirm={() =>
                        mutation.mutate({
                          target: d.id,
                          action: '发布',
                          run: () => publishVPNDocument(kind, d),
                        })
                      }
                      okButtonProps={{ disabled: operationBlocked }}
                      disabled={operationBlocked}
                    >
                      <Button
                        size="small"
                        disabled={mutation.isPending || d.publishedRevision === d.revision}
                      >
                        {text('发布', 'Publish')}
                      </Button>
                    </Popconfirm>
                  ) : null}
                  {allowed('delete') ? (
                    <Popconfirm
                      title={text('删除此配置？', 'Delete this configuration?')}
                      onConfirm={() =>
                        mutation.mutate({
                          target: d.id,
                          action: '删除',
                          run: () => deleteVPNDocument(kind, d),
                        })
                      }
                      okButtonProps={{ disabled: operationBlocked }}
                      disabled={operationBlocked}
                    >
                      <Button size="small" danger disabled={mutation.isPending}>
                        {text('删除', 'Delete')}
                      </Button>
                    </Popconfirm>
                  ) : null}
                </Space>
              ),
            },
          ]}
        />
      }
    >
      <h1 className="soha-network-sr-only">{title}</h1>
      <Typography.Paragraph type="secondary" className="soha-network-note">
        {text(
          '发布后的修订控制新连接；保存草稿不会直接应用到终端。安全范围变更会撤销受影响会话。',
          'Published revisions govern new connections; drafts are not applied to endpoints. Security scope changes revoke affected sessions.',
        )}
      </Typography.Paragraph>
      {!(
        (editing !== undefined && !(query.isEnabled !== false && queryAccessDenied(query))) ||
        (!!history && !(query.isEnabled !== false && queryAccessDenied(query)))
      ) && <> {mutation.feedback} </>}

      {!allowed('view') && !permissions.isPending ? (
        <Alert
          type="warning"
          showIcon
          description={text('没有查看此配置的权限', 'You cannot view these configurations')}
        />
      ) : null}
      <Modal
        styles={operationModalStyles}
        style={{ top: 32 }}
        title={title}
        open={editing !== undefined && !(query.isEnabled !== false && queryAccessDenied(query))}
        onCancel={() => {
          if (!operationBlocked) (() => setEditing(undefined))()
        }}
        confirmLoading={mutation.isPending}
        width={680}
        destroyOnHidden
        onOk={() => form.submit()}
        closable={!operationBlocked}
        cancelButtonProps={{ disabled: operationBlocked }}
        okButtonProps={{ disabled: operationBlocked }}
      >
        {mutation.feedback}

        <Form
          form={form}
          layout="vertical"
          preserve={false}
          onFinish={(values) =>
            mutation.mutate({
              target: editing?.id || values.name,
              action: '保存草稿',
              run: () => saveVPNDocument(kind, editing?.id, editing?.revision || 0, values),
            })
          }
          disabled={operationBlocked}
        >
          <VPNNameField />
          {kind === 'profiles' ? <VPNProfileFields can={can} /> : <VPNPolicyFields />}
        </Form>
      </Modal>
      <Drawer
        open={!!history && !(query.isEnabled !== false && queryAccessDenied(query))}
        onClose={() => {
          if (!operationBlocked) (() => setHistory(null))()
        }}
        title={text('修订记录', 'Revision history')}
        size={760}
      >
        {mutation.feedback}

        <ReadFeedback canRead={revisions.isEnabled} query={revisions} label="修订记录" />

        <AdminTable
          rowKey="revision"
          loading={revisions.isPending}
          dataSource={visibleRevisionsData || []}
          columns={[
            { title: text('修订', 'Revision'), dataIndex: 'revision' },
            { title: text('操作者', 'Actor'), dataIndex: 'createdBy' },
            { title: text('时间', 'Time'), dataIndex: 'createdAt', render: formatDateTime },
            {
              title: text('操作', 'Action'),
              key: 'rollback',
              render: (_: unknown, r: VPNRevision) => (
                <Button
                  disabled={
                    operationBlocked ||
                    !allowed('publish') ||
                    mutation.isPending ||
                    r.revision === history?.publishedRevision
                  }
                  onClick={() =>
                    history &&
                    mutation.mutate({
                      target: history.id,
                      action: '回滚',
                      run: () => publishVPNDocument(kind, history, r.revision),
                    })
                  }
                >
                  {text('回滚为新修订', 'Restore as new revision')}
                </Button>
              ),
            },
          ]}
          expandable={{
            expandedRowRender: (r: VPNRevision) => (
              <pre>{JSON.stringify(r.configuration, null, 2)}</pre>
            ),
          }}
        />
      </Drawer>
    </ManagementDataPage>
  )
}
